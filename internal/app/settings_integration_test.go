package app_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestAdminSettingsFormRendersCurrentWeblog(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	// 基本設定 tab renders the weblog form + inlines the env-var snapshot
	// under the same page; the env-var snapshot lives here because 基本設定
	// is admin-only.
	w := authedGET(t, a.Handler(), "/admin/settings/basic", cookies)
	if w.Code != 200 {
		t.Fatalf("status = %d; body:\n%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{
		"設定",
		`name="title"`,
		`name="base_url"`,
		"画面設定",             // tabbar screen
		"基本設定",             // tabbar basic
		"AI",               // tabbar AI
		"SB_UPLOAD_MAX_MB", // env-var panel lives here now
	} {
		if !strings.Contains(body, want) {
			t.Errorf("basic settings page missing %q", want)
		}
	}

	// /admin/settings/ops is a 301 to the AI tab.
	wOps := authedGET(t, a.Handler(), "/admin/settings/ops", cookies)
	if wOps.Code != http.StatusMovedPermanently {
		t.Fatalf("legacy /settings/ops should 301 to /settings/ai; got %d", wOps.Code)
	}
	if loc := wOps.Header().Get("Location"); loc != "/admin/settings/ai" {
		t.Errorf("redirect Location = %q, want /admin/settings/ai", loc)
	}

	// Comment-related fields live on /admin/comments/settings now —
	// basic-settings page should NOT surface them.
	for _, gone := range []string{
		`name="comment_mode"`,
		`name="spam_words"`,
	} {
		if strings.Contains(body, gone) {
			t.Errorf("basic settings page should no longer render %q (moved to comment settings tab)", gone)
		}
	}
}

func TestAdminSettingsPersistsChanges(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	// Note: comment_mode / spam_words are intentionally absent from the
	// form — they live on /admin/comments/settings now and should be
	// preserved here rather than overwritten.
	form := url.Values{
		"title":       {"新しいタイトル"},
		"description": {"説明テキスト"},
		"base_url":    {"https://example.com/"},
		"lang":        {"ja"},
	}
	w := authedPOSTForm(t, a.Handler(), "/admin/settings/basic", form, cookies)
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d; body:\n%s", w.Code, w.Body.String())
	}

	var title, desc, baseURL, lang string
	err := a.DB.QueryRow(`SELECT title, description, base_url, lang FROM weblogs WHERE id = 1`).
		Scan(&title, &desc, &baseURL, &lang)
	if err != nil {
		t.Fatal(err)
	}
	if title != "新しいタイトル" {
		t.Errorf("title = %q", title)
	}
	if baseURL != "https://example.com/" {
		t.Errorf("baseURL = %q", baseURL)
	}

	// Follow-up GET should surface the flash and the new values.
	w2 := authedGET(t, a.Handler(), "/admin/settings/basic?ok=1", cookies)
	body := w2.Body.String()
	for _, want := range []string{
		"保存しました",
		`value="新しいタイトル"`,
		`value="https://example.com/"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("after save: missing %q", want)
		}
	}
}

func TestAdminSettingsPreservesCommentFieldsFromOtherTab(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	// Seed non-default comment settings via the new tab, then save the
	// main settings form without those fields and confirm they survived.
	if _, err := a.DB.Exec(`UPDATE weblogs SET comment_mode = 'open', spam_words = 'keep-me' WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	form := url.Values{
		"title":       {"update"},
		"description": {""},
		"base_url":    {""},
		"lang":        {"ja"},
	}
	if w := authedPOSTForm(t, a.Handler(), "/admin/settings/basic", form, cookies); w.Code != http.StatusFound {
		t.Fatalf("save status = %d", w.Code)
	}
	var mode, spam string
	_ = a.DB.QueryRow(`SELECT comment_mode, spam_words FROM weblogs WHERE id = 1`).Scan(&mode, &spam)
	if mode != "open" || spam != "keep-me" {
		t.Errorf("/admin/settings overwrote comment fields: mode=%q spam=%q", mode, spam)
	}
}

func TestAdminSettingsRejectsBlankTitle(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	form := url.Values{
		"title": {"   "},
		"lang":  {"ja"},
	}
	w := authedPOSTForm(t, a.Handler(), "/admin/settings/basic", form, cookies)
	if w.Code != 200 {
		t.Fatalf("blank title status = %d, want 200 (stay on form); body:\n%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "ブログタイトルを入力してください") {
		t.Errorf("validation message missing")
	}
}

func TestAdminSettingsRejectsBadBaseURL(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	form := url.Values{
		"title":    {"keep"},
		"base_url": {"not a url"},
		"lang":     {"ja"},
	}
	w := authedPOSTForm(t, a.Handler(), "/admin/settings/basic", form, cookies)
	if w.Code != 200 {
		t.Fatalf("bad url status = %d, want 200; body:\n%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "ベース URL は") {
		t.Errorf("validation message missing")
	}
	var title string
	_ = a.DB.QueryRow(`SELECT title FROM weblogs WHERE id = 1`).Scan(&title)
	if title == "keep" {
		t.Errorf("bad-form submission should not persist; title is now %q", title)
	}
}

func TestAdminCommentSettingsForm(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	w := authedGET(t, a.Handler(), "/admin/comments/settings", cookies)
	if w.Code != 200 {
		t.Fatalf("status = %d; body:\n%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{
		`name="comment_mode"`,
		`name="spam_words"`,
		"リスト",
		"設定",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("comment settings page missing %q", want)
		}
	}
}

func TestAdminCommentSettingsPersists(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	form := url.Values{
		"comment_mode": {"open"},
		"spam_words":   {"cialis\nviagra"},
	}
	w := authedPOSTForm(t, a.Handler(), "/admin/comments/settings", form, cookies)
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d; body:\n%s", w.Code, w.Body.String())
	}
	var mode, spam string
	_ = a.DB.QueryRow(`SELECT comment_mode, spam_words FROM weblogs WHERE id = 1`).Scan(&mode, &spam)
	if mode != "open" || spam != "cialis\nviagra" {
		t.Errorf("persisted = (%q, %q)", mode, spam)
	}
}

func TestAdminCommentSettingsRejectsBadMode(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	form := url.Values{
		"comment_mode": {"bogus"},
		"spam_words":   {""},
	}
	w := authedPOSTForm(t, a.Handler(), "/admin/comments/settings", form, cookies)
	if w.Code != 200 {
		t.Fatalf("bad mode status = %d; body:\n%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "コメントモードの値が不正です") {
		t.Errorf("validation message missing")
	}
}
