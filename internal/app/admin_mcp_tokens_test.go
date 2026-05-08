package app_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/serendipitynz/serenebach/internal/auth"
)

// TestAdminMCPTokenLegacyGetRequiresLogin confirms the legacy GET
// path (and by extension every route in the MCP-token group) is
// gated by the outer RequireUser middleware. Unauthenticated POSTs
// trip the CSRF guard first; testing the GET path is the cleanest
// way to exercise the auth gate in isolation.
func TestAdminMCPTokenLegacyGetRequiresLogin(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)

	w := authedGET(t, a.Handler(), "/admin/settings/mcp", nil)
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	if loc := w.Header().Get("Location"); !strings.Contains(loc, "/admin/login") {
		t.Errorf("Location = %q, want prefix /admin/login", loc)
	}
}

// TestAdminMCPTokenCreateRejectsPowerUser confirms requireAdmin on
// the MCP token group blocks power-tier sessions, not just regular
// ones. MCP tokens grant API write access to the blog so only the
// site administrator may mint them.
func TestAdminMCPTokenCreateRejectsPowerUser(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	hash, _ := auth.HashPassword("secret")
	if _, err := a.DB.Exec(`INSERT INTO users (wid, name, display_name, email, password_hash, role, created_at, updated_at, list_visible, description_format)
		VALUES (1, 'powerguy', 'Power', '', ?, 2, strftime('%s','now'), strftime('%s','now'), 1, 'html')`, hash); err != nil {
		t.Fatal(err)
	}
	cookies := login(t, a.Handler(), "powerguy", "secret")

	form := url.Values{"name": {"t1"}, "author_id": {"1"}}
	w := authedPOSTForm(t, a.Handler(), "/admin/settings/mcp/new", form, cookies)
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
}

// TestAdminMCPTokenCreateRejectsRegularUser pairs with the power
// rejection — defence in depth so a future role-shuffle does not
// silently widen the gate.
func TestAdminMCPTokenCreateRejectsRegularUser(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	hash, _ := auth.HashPassword("secret")
	if _, err := a.DB.Exec(`INSERT INTO users (wid, name, display_name, email, password_hash, role, created_at, updated_at, list_visible, description_format)
		VALUES (1, 'regular', 'Reg', '', ?, 3, strftime('%s','now'), strftime('%s','now'), 1, 'html')`, hash); err != nil {
		t.Fatal(err)
	}
	cookies := login(t, a.Handler(), "regular", "secret")

	form := url.Values{"name": {"t1"}, "author_id": {"1"}}
	w := authedPOSTForm(t, a.Handler(), "/admin/settings/mcp/new", form, cookies)
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
	w2 := authedPOSTForm(t, a.Handler(), "/admin/settings/mcp/9999/revoke", url.Values{}, cookies)
	if w2.Code != http.StatusForbidden {
		t.Errorf("revoke status = %d, want 403", w2.Code)
	}
}

// TestAdminMCPTokenRevokeMissingID confirms the admin gets a clean
// 404 when revoking a non-existent token id rather than a generic
// 500. Important so test-driven exploration doesn't generate noisy
// error logs.
func TestAdminMCPTokenRevokeMissingID(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	w := authedPOSTForm(t, a.Handler(), "/admin/settings/mcp/9999/revoke", url.Values{}, cookies)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// TestAdminMCPTokenRevokeMalformedID confirms a non-numeric id under
// /admin/settings/mcp/{id}/revoke bypasses the ParseInt and lands as
// a 404 rather than 500.
func TestAdminMCPTokenRevokeMalformedID(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	w := authedPOSTForm(t, a.Handler(), "/admin/settings/mcp/notanumber/revoke", url.Values{}, cookies)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// TestAdminMCPTokenLegacyGetRedirects confirms the historical
// /admin/settings/mcp GET path 301s to /admin/settings/ai so old
// sidebar bookmarks keep working.
func TestAdminMCPTokenLegacyGetRedirects(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	w := authedGET(t, a.Handler(), "/admin/settings/mcp", cookies)
	if w.Code != http.StatusMovedPermanently {
		t.Errorf("status = %d, want 301", w.Code)
	}
	if loc := w.Header().Get("Location"); !strings.HasSuffix(loc, "/admin/settings/ai") {
		t.Errorf("Location = %q, want suffix /admin/settings/ai", loc)
	}
}
