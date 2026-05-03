package app_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdminEntriesListShowsSeededRows(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookie := login(t, a.Handler(), "admin", "changeme")

	w := authedGET(t, a.Handler(), "/admin/entries", cookie)
	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{
		"ようこそ Serene Bach へ",
		"カテゴリとテンプレートについて",
		"新規記事",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q\nbody:\n%s", want, body)
			return
		}
	}
}

func TestAdminEntriesListRequiresLogin(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	req := httptest.NewRequest("GET", "/admin/entries", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 redirect to login", w.Code)
	}
	if loc := w.Header().Get("Location"); !strings.HasPrefix(loc, "/admin/login") {
		t.Errorf("Location = %q, want /admin/login prefix", loc)
	}
}

func TestAdminEntryCreateAndEditRoundtrip(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookie := login(t, a.Handler(), "admin", "changeme")

	// Create
	form := url.Values{
		"title":       {"integration entry"},
		"body":        {"<p>from test</p>"},
		"more":        {""},
		"category_id": {"-1"},
		"status":      {"1"}, // published
		"posted_at":   {"2026-04-19T12:00"},
	}
	w := authedPOSTForm(t, a.Handler(), "/admin/entries/new", form, cookie)
	if w.Code != http.StatusFound {
		t.Fatalf("create status = %d, want 302; body:\n%s", w.Code, w.Body.String())
	}
	loc := w.Header().Get("Location")
	if !strings.HasPrefix(loc, "/admin/entries/") || !strings.Contains(loc, "/edit") {
		t.Fatalf("unexpected redirect target %q", loc)
	}
	// Strip the flash query for the follow-up GET so the edit page
	// renders without the stray "saved" flag on the URL.
	if i := strings.Index(loc, "?"); i >= 0 {
		loc = loc[:i]
	}

	// Edit form should render with the new title + body
	w2 := authedGET(t, a.Handler(), loc, cookie)
	if w2.Code != 200 {
		t.Fatalf("edit form status = %d", w2.Code)
	}
	body := w2.Body.String()
	// textarea values come back HTML-escaped (that's correct — the user
	// needs to see them as source text, not rendered markup).
	for _, want := range []string{
		`value="integration entry"`,
		`&lt;p&gt;from test&lt;/p&gt;`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("edit form missing %q", want)
		}
	}

	// Public home should surface the new entry (published).
	homeBody := authedGET(t, a.Handler(), "/", cookie).Body.String()
	if !strings.Contains(homeBody, "integration entry") {
		t.Errorf("published entry missing from public home; body:\n%s", homeBody)
	}
}

func TestAdminEntryUpdateChangesContent(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookie := login(t, a.Handler(), "admin", "changeme")

	form := url.Values{
		"title":       {"edited"},
		"body":        {"<p>edited body</p>"},
		"more":        {""},
		"category_id": {"-1"},
		"status":      {"1"},
		"posted_at":   {"2026-04-19T12:00"},
	}
	w := authedPOSTForm(t, a.Handler(), "/admin/entries/1/edit", form, cookie)
	if w.Code != http.StatusFound {
		t.Fatalf("update status = %d, body:\n%s", w.Code, w.Body.String())
	}
	// Confirm persisted
	w2 := authedGET(t, a.Handler(), "/admin/entries/1/edit", cookie)
	if !strings.Contains(w2.Body.String(), `value="edited"`) {
		t.Errorf("updated title not persisted")
	}
}

func TestAdminEntryDeleteRemovesFromListing(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookie := login(t, a.Handler(), "admin", "changeme")

	w := authedPOSTForm(t, a.Handler(), "/admin/entries/2/delete", url.Values{}, cookie)
	if w.Code != http.StatusFound {
		t.Fatalf("delete status = %d, want 302", w.Code)
	}
	list := authedGET(t, a.Handler(), "/admin/entries", cookie)
	if strings.Contains(list.Body.String(), "カテゴリとテンプレートについて") {
		t.Errorf("deleted entry still appears in list:\n%s", list.Body.String())
	}
}

func TestAdminEntryCreateRejectsEmptyTitle(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookie := login(t, a.Handler(), "admin", "changeme")

	form := url.Values{
		"title":       {"   "},
		"body":        {"x"},
		"category_id": {"-1"},
		"status":      {"0"},
		"posted_at":   {"2026-04-19T12:00"},
	}
	w := authedPOSTForm(t, a.Handler(), "/admin/entries/new", form, cookie)
	if w.Code != 200 {
		t.Fatalf("empty-title status = %d, want 200 (stay on form)", w.Code)
	}
	if !strings.Contains(w.Body.String(), "タイトルを入力してください") {
		t.Errorf("missing validation message")
	}
}

// TestAdminEntryFormLayoutShape confirms the entry form structure:
// title input, folding 追記, category/status/posted_at options row, and
// the two save-mode buttons.
func TestAdminEntryFormLayoutShape(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookie := login(t, a.Handler(), "admin", "changeme")

	w := authedGET(t, a.Handler(), "/admin/entries/1/edit", cookie)
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{
		`class="entry-form"`,
		`class="entry-title-input"`,
		`data-image-picker-open`,
		`data-image-picker-filter`,
		`<details class="entry-form-more"`,
		`data-entry-status`,
		`data-save-mode="draft"`,
		`data-save-mode="dynamic"`,
		"下書きで保存",
		"公開して保存",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("entry form missing %q", want)
		}
	}
}

func TestAdminEntryEdit404ForUnknownID(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookie := login(t, a.Handler(), "admin", "changeme")
	w := authedGET(t, a.Handler(), "/admin/entries/9999/edit", cookie)
	if w.Code != 404 {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestAdminEntryOGRegenerate(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookie := login(t, a.Handler(), "admin", "changeme")

	// POST to regenerate OG card for entry 1
	w := authedPOSTForm(t, a.Handler(), "/admin/entries/1/og", url.Values{}, cookie)
	if w.Code != 200 {
		t.Fatalf("status = %d, want 200; body:\n%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `"ok":true`) {
		t.Errorf("missing ok=true in response: %s", body)
	}

	// File should exist on disk
	ogPath := filepath.Join(a.Config.ImageDir, "og", "1.png")
	if _, err := os.Stat(ogPath); err != nil {
		t.Errorf("OG card not written to disk: %v", err)
	}
}

func TestAdminEntryOGRegenerate404ForUnknownID(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookie := login(t, a.Handler(), "admin", "changeme")

	w := authedPOSTForm(t, a.Handler(), "/admin/entries/9999/og", url.Values{}, cookie)
	if w.Code != 404 {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestAdminEntryOGRegenerateRequiresCSRF(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	req := httptest.NewRequest("POST", "/admin/entries/1/og", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatalf("status = %d, want 403 (CSRF missing)", w.Code)
	}
}

// TestAdminEntryEditForbiddenForOtherAuthor pins the authorization rule
// for the GET form, POST update, and POST /og endpoints: a regular user
// must not be able to view or modify another author's entry.
func TestAdminEntryEditForbiddenForOtherAuthor(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	adminCookie := login(t, a.Handler(), "admin", "changeme")

	// Create a regular-tier user so we can test cross-author access.
	createForm := url.Values{
		"name":             {"alice"},
		"display_name":     {"Alice"},
		"password":         {"pa55word"},
		"password_confirm": {"pa55word"},
		"email":            {"alice@example.com"},
		"role":             {"3"}, // RoleRegular
	}
	if w := authedPOSTForm(t, a.Handler(), "/admin/users/new", createForm, adminCookie); w.Code != http.StatusFound {
		t.Fatalf("user create status = %d, body:\n%s", w.Code, w.Body.String())
	}

	aliceCookie := login(t, a.Handler(), "alice", "pa55word")

	// Entry 1 was seeded as admin's, so alice must not be allowed to
	// reach the edit form for it.
	if w := authedGET(t, a.Handler(), "/admin/entries/1/edit", aliceCookie); w.Code != http.StatusForbidden {
		t.Errorf("GET edit: status = %d, want 403", w.Code)
	}

	// POSTing the update path must also be rejected — without this the
	// form button can be hidden but the endpoint is still reachable.
	updateForm := url.Values{
		"title":       {"hijacked"},
		"body":        {"<p>nope</p>"},
		"category_id": {"-1"},
		"status":      {"1"},
		"posted_at":   {"2026-04-19T12:00"},
	}
	if w := authedPOSTForm(t, a.Handler(), "/admin/entries/1/edit", updateForm, aliceCookie); w.Code != http.StatusForbidden {
		t.Errorf("POST update: status = %d, want 403", w.Code)
	}

	// The OG regeneration endpoint must also be guarded — otherwise a
	// regular user could overwrite another author's OG card image.
	if w := authedPOSTForm(t, a.Handler(), "/admin/entries/1/og", url.Values{}, aliceCookie); w.Code != http.StatusForbidden {
		t.Errorf("POST og: status = %d, want 403", w.Code)
	}

	// Confirm the row was not modified.
	var title string
	if err := a.DB.QueryRow(`SELECT title FROM entries WHERE id = 1`).Scan(&title); err != nil {
		t.Fatal(err)
	}
	if title == "hijacked" {
		t.Errorf("entry was modified despite 403 — title = %q", title)
	}
}
