package app_test

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestDisplaySettingsApplyPageSizeAndSort validates the end-to-end path
// through the design-settings form: save entries_per_page=2 and
// entry_sort_order=asc, then confirm the home page honours both (fewer
// entries, oldest-first).
func TestDisplaySettingsApplyPageSizeAndSort(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	// Start from a clean slate so the test asserts only on the rows
	// it inserts — seed entries' posted_at is time.Now() which would
	// otherwise beat the fixed 2026-03 timestamps below.
	if _, err := a.DB.Exec(`DELETE FROM entries`); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 4; i++ {
		ts := time.Date(2026, 3, i, 10, 0, 0, 0, time.UTC).Unix()
		if _, err := a.DB.Exec(`INSERT INTO entries (wid, author_id, category_id, title, body, more, format, status, posted_at, created_at, updated_at)
			VALUES (1, 1, -1, ?, ?, '', 'html', 1, ?, ?, ?)`,
			"Extra "+smallItoa(i), "<p>body "+smallItoa(i)+"</p>", ts, ts, ts); err != nil {
			t.Fatal(err)
		}
	}

	cookies := login(t, a.Handler(), "admin", "changeme")
	csrfCookie, token := fetchCSRF(t, a.Handler())
	form := url.Values{
		"csrf_token":          {token},
		"archive_template_id": {"0"},
		"profile_template_id": {"0"},
		"entries_per_page":    {"2"},
		"entry_sort_order":    {"asc"},
		"comment_sort_order":  {"asc"},
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
		t.Fatalf("save status = %d", w.Code)
	}

	// Home: only 2 entries should land in the render, and of those
	// the oldest (smaller posted_at) comes first. Seeded entries are
	// posted_at 2026-03-01..04; with limit=2 + newest-first fetch
	// + asc reversal, we expect "Extra 3" then "Extra 4".
	w = httptest.NewRecorder()
	a.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 200 {
		t.Fatalf("home status = %d", w.Code)
	}
	// Scope to main content — the sidebar's {latest_entry_list} widget
	// surfaces entries regardless of the page-size cap, so a body-wide
	// strings.Contains would always trip on Extra 1 / Extra 2.
	body := mainArea(w.Body.String())
	pos3 := strings.Index(body, "Extra 3")
	pos4 := strings.Index(body, "Extra 4")
	if pos3 < 0 || pos4 < 0 {
		t.Fatalf("both Extra 3 and Extra 4 should render when limit=2 asc\nbody: %s", body)
	}
	if pos3 > pos4 {
		t.Errorf("asc order broken: Extra 3 (older) should appear before Extra 4")
	}
	// Extra 1 / Extra 2 must NOT render — they'd show only if limit
	// weren't respected.
	if strings.Contains(body, "Extra 1") || strings.Contains(body, "Extra 2") {
		t.Errorf("older entries leaked past the entries_per_page=2 cap")
	}
}

// TestDisplaySettingsPageRendersControls confirms the settings form
// shows all three inputs with the expected name attributes so the JS
// live preview + the form POST can find them.
func TestDisplaySettingsPageRendersControls(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	w := authedGET(t, a.Handler(), "/admin/templates/settings", cookies)
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{
		`name="entries_per_page"`,
		`name="entry_sort_order"`,
		`name="comment_sort_order"`,
		`日付の新しいものを上に`,
		`日付の古いものを上に`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("settings page missing %q", want)
		}
	}
}

func smallItoa(n int) string {
	// Single-digit helper (inputs are in [1, 9]) so the test file
	// doesn't have to clash with the package-level itoa in another
	// test file.
	return string(rune('0' + n))
}
