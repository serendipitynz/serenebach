package app_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/serendipitynz/serenebach/internal/csrf"
)

// A POST without any CSRF cookie at all must be rejected outright.
func TestCSRFMissingCookieRejected(t *testing.T) {
	a := newTestApp(t)

	form := url.Values{"name": {"admin"}, "password": {"changeme"}}
	req := httptest.NewRequest("POST", "/admin/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
}

// A POST whose csrf_token doesn't match the sb_csrf cookie must be
// rejected even when the cookie itself is attached.
func TestCSRFMismatchedTokenRejected(t *testing.T) {
	a := newTestApp(t)

	form := url.Values{
		"name":       {"admin"},
		"password":   {"changeme"},
		"csrf_token": {"obviously-wrong"},
	}
	req := httptest.NewRequest("POST", "/admin/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: csrf.CookieName, Value: "the-actual-cookie-value"})
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
}

// Any safe method (GET, HEAD, OPTIONS) gets a CSRF cookie minted for it
// without any challenge.
func TestCSRFCookieSetOnPublicGET(t *testing.T) {
	a := newTestApp(t)

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if c := findCookie(w.Result().Cookies(), csrf.CookieName); c == nil || c.Value == "" {
		t.Errorf("expected %s cookie to be minted on GET /", csrf.CookieName)
	}
}

// Public POST with no Origin / Referer = comment submit from a non-
// browser client (or a forged cross-site script that omitted the
// header). The same-origin guard that replaced the CSRF check on
// reader endpoints rejects it before the handler runs, so no message
// lands in the DB. Bare-token tests for admin endpoints live in the
// admin integration suite, not here.
func TestPublicMutationBlocksHeaderlessPOST(t *testing.T) {
	a := newTestApp(t)

	form := url.Values{
		"name":        {"visitor"},
		"description": {"this should be dropped"},
	}
	req := httptest.NewRequest("POST", "/entry/1/comment", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	var n int
	if err := a.DB.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("bare POST should never reach the DB; got %d messages", n)
	}
}

// TestPublicMutationBlocksForeignOrigin pins the cross-site case: a
// browser tricked into POSTing from another site sends an Origin
// pointing at the attacker, which the same-origin guard must reject.
// A "benign" Referer trailing it should NOT rescue the request —
// Referer is only consulted when Origin is absent.
func TestPublicMutationBlocksForeignOrigin(t *testing.T) {
	a := newTestApp(t)

	form := url.Values{
		"name":        {"visitor"},
		"description": {"shouldn't land"},
	}
	req := httptest.NewRequest("POST", "/entry/1/comment", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://attacker.example")
	req.Header.Set("Referer", testPublicOrigin+"/entry/1/")
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	var n int
	if err := a.DB.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("foreign-origin POST must not reach the DB; got %d messages", n)
	}
}
