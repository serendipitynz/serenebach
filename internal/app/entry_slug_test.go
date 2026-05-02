package app_test

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestPublicEntryResolvesBySlug confirms /entry/<slug>/ loads the entry
// when a slug is assigned. Also guards against regressing the id route
// for entries without a slug.
func TestPublicEntryResolvesBySlug(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	if _, err := a.DB.Exec(`UPDATE entries SET slug = 'first-post' WHERE id = 1`); err != nil {
		t.Fatalf("set slug: %v", err)
	}

	// Slug route lands on the page.
	req := httptest.NewRequest("GET", "/entry/first-post/", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("slug route status = %d", w.Code)
	}

	// Numeric id on a slugged entry 301s to the canonical slug URL so
	// cached reader bookmarks and RSS hashes still resolve.
	req = httptest.NewRequest("GET", "/entry/1/", nil)
	w = httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != 301 {
		t.Fatalf("id redirect status = %d, want 301", w.Code)
	}
	if got := w.Header().Get("Location"); got != "/entry/first-post/" {
		t.Errorf("redirect Location = %q", got)
	}

	// Unknown slug 404s (not the catch-all server error).
	req = httptest.NewRequest("GET", "/entry/nope/", nil)
	w = httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != 404 {
		t.Errorf("unknown slug status = %d, want 404", w.Code)
	}
}

// TestAdminEntryCreateRejectsInvalidSlug confirms the form validator
// stops bad slugs before they reach the DB. The error message surface
// matters: an opaque 500 would hide the fix from the user.
func TestAdminEntryCreateRejectsInvalidSlug(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	csrfCookie, token := fetchCSRF(t, a.Handler())

	form := url.Values{
		"csrf_token":  {token},
		"title":       {"test"},
		"body":        {"body"},
		"format":      {"html"},
		"status":      {"0"},
		"posted_at":   {"2026-04-21T10:00"},
		"category_id": {"-1"},
		"slug":        {"Bad Slug!"},
	}
	req := httptest.NewRequest("POST", "/admin/entries/new", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrfCookie)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("bad-slug status = %d, want 200 (stay on form)", w.Code)
	}
	if !strings.Contains(w.Body.String(), "スラッグは英小文字") {
		t.Errorf("missing slug validation message")
	}
}

// TestAdminEntryUpdateRejectsDuplicateSlug confirms the partial unique
// index + ErrSlugInUse error path is wired all the way back to the form.
func TestAdminEntryUpdateRejectsDuplicateSlug(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	if _, err := a.DB.Exec(`UPDATE entries SET slug = 'first-post' WHERE id = 1`); err != nil {
		t.Fatalf("set slug: %v", err)
	}

	csrfCookie, token := fetchCSRF(t, a.Handler())
	form := url.Values{
		"csrf_token":  {token},
		"title":       {"existing"},
		"body":        {"x"},
		"format":      {"html"},
		"status":      {"1"},
		"posted_at":   {"2026-04-21T10:00"},
		"category_id": {"-1"},
		// Try to reuse entry 1's slug on entry 2.
		"slug": {"first-post"},
	}
	req := httptest.NewRequest("POST", "/admin/entries/2/edit", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrfCookie)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("dup-slug status = %d, want 200 (stay on form)", w.Code)
	}
	if !strings.Contains(w.Body.String(), "既に使われています") {
		t.Errorf("missing duplicate-slug message\n%s", w.Body.String())
	}
}
