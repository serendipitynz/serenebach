package app_test

import (
	"net/url"
	"strings"
	"testing"
)

// TestAdminTemplateEditStaysQuietOnCleanInitialLoad: opening the
// edit page on a clean template should NOT surface the green
// ✅ all-clear banner — it only appears after a manual recheck.
func TestAdminTemplateEditStaysQuietOnCleanInitialLoad(t *testing.T) {
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	if _, err := a.DB.Exec(`UPDATE templates SET main_body = ?, entry_body = '' WHERE is_active = 1`,
		"<!-- BEGIN entry -->\n<article>{entry_title}</article>\n<!-- END entry -->\n"); err != nil {
		t.Fatal(err)
	}
	var id int64
	_ = a.DB.QueryRow(`SELECT id FROM templates WHERE is_active = 1`).Scan(&id)

	w := authedGET(t, a.Handler(), "/admin/templates/"+itoa64(id)+"/edit", cookies)
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "テンプレートステータス") {
		t.Errorf("lint panel heading missing")
	}
	// Initial load with no findings: the success banner must NOT
	// appear, otherwise the page is noisy on every open.
	if strings.Contains(body, "互換性に関する警告はありません") {
		t.Errorf("all-clear banner shouldn't fire on initial load; body:\n%s", body)
	}
	if strings.Contains(body, `class="badge warning"`) {
		t.Errorf("no warning badge expected on a clean template")
	}
}

// TestAdminTemplateRecheckShowsAllClearOnSuccess: pressing the
// recheck button against a clean body DOES show the green ✅
// banner — that's the affirmation the operator is asking for.
func TestAdminTemplateRecheckShowsAllClearOnSuccess(t *testing.T) {
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	if _, err := a.DB.Exec(`UPDATE templates SET main_body = ?, entry_body = '' WHERE is_active = 1`,
		"<html>{entry_title}</html>\n"); err != nil {
		t.Fatal(err)
	}
	var id int64
	_ = a.DB.QueryRow(`SELECT id FROM templates WHERE is_active = 1`).Scan(&id)

	form := url.Values{
		"main_body":  {"<html>{entry_title}</html>\n"},
		"entry_body": {""},
		"css":        {""},
	}
	w := authedPOSTForm(t, a.Handler(), "/admin/templates/"+itoa64(id)+"/recheck", form, cookies)
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "互換性に関する警告はありません") {
		t.Errorf("manual recheck on clean body should show all-clear; body:\n%s", body)
	}
}

// TestAdminTemplateEditFlagsTrackback: a body using trackback_area +
// site_mobile + amazon_link surfaces the ⚠️ badge on the body
// section, the unsupported findings inside the lint panel with line
// numbers, and the differs-only site_mobile finding.
func TestAdminTemplateEditFlagsTrackback(t *testing.T) {
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	body := "<html>\n" +
		"<body>\n" +
		"<!-- BEGIN trackback_area -->\n" +
		"x\n" +
		"<!-- END trackback_area -->\n" +
		"{amazon_link}\n" +
		"{site_mobile}\n" +
		"</body>\n" +
		"</html>\n"
	if _, err := a.DB.Exec(`UPDATE templates SET main_body = ?, entry_body = '' WHERE is_active = 1`, body); err != nil {
		t.Fatal(err)
	}
	var id int64
	_ = a.DB.QueryRow(`SELECT id FROM templates WHERE is_active = 1`).Scan(&id)

	w := authedGET(t, a.Handler(), "/admin/templates/"+itoa64(id)+"/edit", cookies)
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	out := w.Body.String()
	for _, want := range []string{
		`class="badge warning"`, // section badge appears (unsupported present)
		"trackback_area",        // listed in lint panel
		"amazon_link",
		"site_mobile",
		"(行 3)", // trackback_area starts on line 3
		"(行 6)", // amazon_link on line 6
		"(行 7)", // site_mobile on line 7
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
			return
		}
	}
}

// TestAdminTemplateRecheckRunsAgainstSubmittedBody: posting the
// /recheck endpoint with new body content re-renders the form using
// the submitted bytes — without saving to the DB.
func TestAdminTemplateRecheckRunsAgainstSubmittedBody(t *testing.T) {
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	// Start with a clean stored body.
	if _, err := a.DB.Exec(`UPDATE templates SET main_body = ?, entry_body = '' WHERE is_active = 1`,
		"<html><body>{entry_title}</body></html>\n"); err != nil {
		t.Fatal(err)
	}
	var id int64
	_ = a.DB.QueryRow(`SELECT id FROM templates WHERE is_active = 1`).Scan(&id)

	dirty := "<html>\n{trackback_url}\n</html>\n"
	form := url.Values{
		"main_body":  {dirty},
		"entry_body": {""},
		"css":        {""},
	}
	w := authedPOSTForm(t, a.Handler(), "/admin/templates/"+itoa64(id)+"/recheck", form, cookies)
	if w.Code != 200 {
		t.Fatalf("recheck status = %d; body:\n%s", w.Code, w.Body.String())
	}
	out := w.Body.String()
	if !strings.Contains(out, "trackback_url") {
		t.Errorf("recheck didn't lint the submitted body; out:\n%s", out)
	}

	// DB body must NOT have changed — recheck is read-only.
	var stored string
	_ = a.DB.QueryRow(`SELECT main_body FROM templates WHERE id = ?`, id).Scan(&stored)
	if strings.Contains(stored, "trackback_url") {
		t.Errorf("recheck should not persist; DB has:\n%s", stored)
	}
}
