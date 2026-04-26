package app_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestSettingsAITabHiddenWithoutSecret verifies the AI-config form on
// the 設定 > AI タブ disappears when SB_AI_SECRET isn't set on the
// server. The tab itself still appears (per the product rule
// "タブは残したままでよい"), but the configuration panel is
// replaced with a "env missing" notice.
func TestSettingsAITabHiddenWithoutSecret(t *testing.T) {
	t.Setenv("SB_AI_SECRET", "")

	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	w := authedGET(t, a.Handler(), "/admin/settings/ai", cookies)
	if w.Code != 200 {
		t.Fatalf("ai tab GET = %d", w.Code)
	}
	body := w.Body.String()
	// Tabbar link should still render so the user can navigate back.
	if !strings.Contains(body, `href="/admin/settings/ai"`) {
		t.Errorf("AI tab link should still render without secret")
	}
	// But the config form itself should NOT be there.
	if strings.Contains(body, `action="/admin/settings/ai"`) {
		t.Errorf("AI config form should be hidden without secret")
	}
	// The "env var missing" notice should surface.
	if !strings.Contains(body, "SB_AI_SECRET") {
		t.Errorf("page should mention SB_AI_SECRET when env is unset")
	}
}

// TestSettingsAllTabsVisibleForAdmin confirms admin users see all
// three tabs (画面設定 / 基本設定 / AI 設定) on every settings page.
func TestSettingsAllTabsVisibleForAdmin(t *testing.T) {
	t.Setenv("SB_AI_SECRET", "test-secret-tabs")

	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	// /admin/settings is the sidebar entry point that 302s admins to
	// /settings/basic — the per-tab paths are exercised directly here.
	for _, path := range []string{"/admin/settings/screen", "/admin/settings/basic", "/admin/settings/ai"} {
		w := authedGET(t, a.Handler(), path, cookies)
		if w.Code != 200 {
			t.Fatalf("%s = %d", path, w.Code)
		}
		body := w.Body.String()
		for _, link := range []string{
			`href="/admin/settings/screen"`,
			`href="/admin/settings/basic"`,
			`href="/admin/settings/ai"`,
		} {
			if !strings.Contains(body, link) {
				t.Errorf("%s missing tab link %q", path, link)
			}
		}
	}
}

// TestSettingsAISaveRoundtrip saves an AI config and re-loads to
// confirm the values round-trip through the db.
func TestSettingsAISaveRoundtrip(t *testing.T) {
	t.Setenv("SB_AI_SECRET", "test-secret-for-settings-ai-roundtrip")

	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	form := url.Values{
		"ai_enabled":  {"on"},
		"ai_kind":     {"openai-compat"},
		"ai_base_url": {"http://127.0.0.1:1234/v1"},
		"ai_model":    {"google/gemma-3-12b"},
		"ai_api_key":  {""},
		"ai_auto_alt": {"on"},
	}
	w := authedPOSTForm(t, a.Handler(), "/admin/settings/ai", form, cookies)
	if w.Code != http.StatusFound {
		t.Fatalf("POST /admin/settings/ai = %d, want 302; body=%s", w.Code, w.Body.String())
	}
	if loc := w.Header().Get("Location"); !strings.Contains(loc, "ok=ai_saved") {
		t.Errorf("redirect Location = %q, want ok=ai_saved", loc)
	}
	if loc := w.Header().Get("Location"); !strings.HasPrefix(loc, "/admin/settings/ai") {
		t.Errorf("redirect should stay on settings/ai tab, got %q", loc)
	}

	var kind, baseURL, model string
	var autoAlt int
	err := a.DB.QueryRow(`SELECT ai_kind, ai_base_url, ai_model, ai_auto_alt FROM users WHERE name = 'admin'`).
		Scan(&kind, &baseURL, &model, &autoAlt)
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if kind != "openai-compat" || baseURL != "http://127.0.0.1:1234/v1" || model != "google/gemma-3-12b" || autoAlt != 1 {
		t.Errorf("row = (%s / %s / %s / autoAlt=%d)", kind, baseURL, model, autoAlt)
	}

	// Re-render the AI tab with the flash — message should surface.
	w = authedGET(t, a.Handler(), "/admin/settings/ai?ok=ai_saved", cookies)
	if !strings.Contains(w.Body.String(), "AI 設定を保存しました") {
		t.Errorf("flash message missing from body")
	}
}

// TestSettingsAIDisablePath empties out the config. Regression:
// un-checking "enabled" on the form must clear every AI column,
// not just the kind.
func TestSettingsAIDisablePath(t *testing.T) {
	t.Setenv("SB_AI_SECRET", "test-secret-disable")

	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	_ = authedPOSTForm(t, a.Handler(), "/admin/settings/ai", url.Values{
		"ai_enabled": {"on"},
		"ai_kind":    {"claude"},
		"ai_model":   {"claude-opus-4-5"},
		"ai_api_key": {"sk-ant-seed-key"},
	}, cookies)

	var enc1 string
	_ = a.DB.QueryRow(`SELECT ai_api_key_enc FROM users WHERE name = 'admin'`).Scan(&enc1)
	if enc1 == "" {
		t.Fatalf("expected ciphertext after first save")
	}

	_ = authedPOSTForm(t, a.Handler(), "/admin/settings/ai", url.Values{
		"ai_kind":  {"claude"},
		"ai_model": {"claude-opus-4-5"},
	}, cookies)

	var kind, enc2 string
	var autoAlt int
	_ = a.DB.QueryRow(`SELECT ai_kind, ai_api_key_enc, ai_auto_alt FROM users WHERE name = 'admin'`).
		Scan(&kind, &enc2, &autoAlt)
	if kind != "" || enc2 != "" || autoAlt != 0 {
		t.Errorf("after disable: kind=%q enc=%q autoAlt=%d (want empty / 0)", kind, enc2, autoAlt)
	}
}
