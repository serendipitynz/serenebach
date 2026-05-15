package app_test

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestDateFormatSettingsRoundtripAndApply saves a custom entry-date
// pattern through the design-settings form and confirms the public
// entry page renders with that pattern afterwards.
func TestDateFormatSettingsRoundtripAndApply(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	csrfCookie, token := fetchCSRF(t, a.Handler())

	form := url.Values{
		"csrf_token":          {token},
		"archive_template_id": {"0"},
		"profile_template_id": {"0"},
		"date_format_entry":   {"%Year%年%MonNum%月%DayShort%日(%Week%)"},
		"time_format_entry":   {"%Hour%:%Min%"},
		"date_format_comment": {""},
		"date_format_list":    {""},
		"date_format_archive": {""},
	}
	req := httptest.NewRequest("POST", "/admin/templates/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrfCookie)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != 302 {
		t.Fatalf("save status = %d, want 302", w.Code)
	}

	// Persisted column value matches the submitted string verbatim.
	var stored string
	if err := a.DB.QueryRow(`SELECT date_format_entry FROM weblogs WHERE id = 1`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != "%Year%年%MonNum%月%DayShort%日(%Week%)" {
		t.Errorf("stored pattern = %q", stored)
	}

	// Public entry page must now render the entry date with the new
	// pattern. The seed weblog ships lang="ja" so %Week% picks the
	// Japanese short form (日/月/...).
	req = httptest.NewRequest("GET", "/entry/1/", nil)
	w = httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("entry status = %d", w.Code)
	}
	body := w.Body.String()
	// The entry's seed posted_at is a known date; rather than hard-code
	// the result, just prove the template tokens expanded (no raw
	// "%Year%" / literal pattern slipped through) and the output
	// contains Japanese era markers introduced by the custom pattern.
	if strings.Contains(body, "%Year%") || strings.Contains(body, "%MonNum%") {
		t.Errorf("pattern tokens leaked unexpanded into the page")
	}
	if !strings.Contains(body, "年") || !strings.Contains(body, "月") || !strings.Contains(body, "日") {
		t.Errorf("Japanese era markers missing from rendered entry date\nbody snippet: %s", body[:min(len(body), 400)])
	}

	// SB3 parity regression guard: {entry_date} must render via the
	// same DateFormatEntry pattern on list pages (home / category /
	// archive). Previously list views used FormatListDate, so the
	// admin's "記事日付" setting silently dropped on every page except
	// the permalink — see fix(content): use DateFormatEntry for
	// {entry_date} on lists.
	for _, path := range []string{"/", "/category/news"} {
		req = httptest.NewRequest("GET", path, nil)
		w = httptest.NewRecorder()
		a.Handler().ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("GET %s status = %d", path, w.Code)
		}
		body := w.Body.String()
		if strings.Contains(body, "%Year%") || strings.Contains(body, "%MonNum%") {
			t.Errorf("GET %s: pattern tokens leaked unexpanded", path)
		}
		if !strings.Contains(body, "年") || !strings.Contains(body, "月") || !strings.Contains(body, "日") {
			t.Errorf("GET %s: DateFormatEntry not applied to {entry_date}\nbody snippet: %s", path, body[:min(len(body), 400)])
		}
	}
}

// TestDateFormatSettingsPageRendersPreview sanity-checks that the form
// page exposes both the placeholder (= default) and the server-rendered
// sample so the section is useful before any JS runs.
func TestDateFormatSettingsPageRendersPreview(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	w := authedGET(t, a.Handler(), "/admin/templates/settings", cookies)
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{
		// Input + preview pair for every context.
		`name="date_format_entry"`,
		`name="time_format_entry"`,
		`name="date_format_comment"`,
		`name="date_format_list"`,
		`name="date_format_archive"`,
		`data-date-format-input`,
		`data-date-format-preview`,
		// Placeholder carries the package default — the fallback the
		// public site actually uses when the field is empty.
		`placeholder="%Year%-%Mon%-%Day% (%Week%)"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("settings page missing %q", want)
		}
	}
}
