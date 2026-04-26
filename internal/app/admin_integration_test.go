package app_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/serendipitynz/serenebach/internal/csrf"
	"github.com/serendipitynz/serenebach/internal/session"
)

func TestAdminHomeRedirectsWhenUnauthenticated(t *testing.T) {
	a := newTestApp(t)

	req := httptest.NewRequest("GET", "/admin/", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.HasPrefix(loc, "/admin/login") {
		t.Fatalf("Location = %q, want prefix /admin/login", loc)
	}
	if !strings.Contains(loc, "next=") {
		t.Errorf("expected next= in %q", loc)
	}
}

func TestLoginFormRendersForAnonymous(t *testing.T) {
	a := newTestApp(t)

	req := httptest.NewRequest("GET", "/admin/login", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), `name="password"`) {
		t.Errorf("missing password field in login form")
	}
	if !strings.Contains(w.Body.String(), `name="csrf_token"`) {
		t.Errorf("login form should carry a csrf_token hidden input")
	}
}

func TestLoginSuccessIssuesSessionCookieAndRedirects(t *testing.T) {
	a := newTestApp(t)

	csrfCookie, token := fetchCSRF(t, a.Handler())
	form := url.Values{
		"name":       {"admin"},
		"password":   {"changeme"},
		"csrf_token": {token},
	}
	req := httptest.NewRequest("POST", "/admin/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrfCookie)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302; body:\n%s", w.Code, w.Body.String())
	}
	if loc := w.Header().Get("Location"); loc != "/admin/" {
		t.Errorf("Location = %q, want /admin/", loc)
	}
	if c := findCookie(w.Result().Cookies(), session.CookieName); c == nil || c.Value == "" {
		t.Fatalf("no %s cookie set; got %v", session.CookieName, w.Result().Cookies())
	}
}

func TestLoginFailureKeepsUserOnForm(t *testing.T) {
	a := newTestApp(t)

	csrfCookie, token := fetchCSRF(t, a.Handler())
	form := url.Values{
		"name":       {"admin"},
		"password":   {"wrong"},
		"csrf_token": {token},
	}
	req := httptest.NewRequest("POST", "/admin/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrfCookie)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200 (stay on form)", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "ユーザー名またはパスワードが違います") {
		t.Errorf("expected error message in body; got:\n%s", body)
	}
	if c := findCookie(w.Result().Cookies(), session.CookieName); c != nil && c.Value != "" {
		t.Errorf("session cookie should not be set on failure; got %q", c.Value)
	}
}

func TestLoggedInRequestReachesAdminHome(t *testing.T) {
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	w := authedGET(t, a.Handler(), "/admin/", cookies)
	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "admin") {
		t.Errorf("expected admin name on admin home; body:\n%s", w.Body.String())
	}
}

func TestLogoutClearsSession(t *testing.T) {
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	w := authedPOSTForm(t, a.Handler(), "/admin/logout", url.Values{}, cookies)
	if w.Code != http.StatusFound {
		t.Fatalf("logout status = %d, want 302; body:\n%s", w.Code, w.Body.String())
	}

	// Subsequent /admin/ request still carries the now-useless session
	// cookie, so it must redirect back to login.
	w2 := authedGET(t, a.Handler(), "/admin/", cookies)
	if w2.Code != http.StatusFound {
		t.Fatalf("after logout status = %d, want 302", w2.Code)
	}
}

func TestLoginRejectsOpenRedirectNext(t *testing.T) {
	a := newTestApp(t)

	csrfCookie, token := fetchCSRF(t, a.Handler())
	form := url.Values{
		"name":       {"admin"},
		"password":   {"changeme"},
		"csrf_token": {token},
	}
	req := httptest.NewRequest("POST", "/admin/login?next=https://evil.example/steal", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrfCookie)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/admin/" {
		t.Errorf("open-redirect not blocked: Location = %q", loc)
	}
}

// ---- helpers -------------------------------------------------------------

func findCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, c := range cookies {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// fetchCSRF runs a preliminary GET / so the global CSRF middleware mints
// a sb_csrf cookie, then returns it plus the token value ready to echo
// back in form POSTs.
func fetchCSRF(t *testing.T, h http.Handler) (*http.Cookie, string) {
	t.Helper()
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	c := findCookie(w.Result().Cookies(), csrf.CookieName)
	if c == nil || c.Value == "" {
		t.Fatalf("fetchCSRF: no %s cookie from initial GET", csrf.CookieName)
	}
	return c, c.Value
}

// csrfFromJar peels the CSRF cookie + its value out of an accumulated jar,
// so authedPOSTForm can plug the token into form bodies automatically.
func csrfFromJar(cookies []*http.Cookie) (*http.Cookie, string) {
	c := findCookie(cookies, csrf.CookieName)
	if c == nil {
		return nil, ""
	}
	return c, c.Value
}

// authedGET drives a GET through the handler with every cookie the jar
// carries, matching how a browser would replay them.
func authedGET(t *testing.T, h http.Handler, path string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// authedPOSTForm posts form values under an authenticated jar. It auto-
// injects csrf_token using the jar's sb_csrf cookie so callers don't
// have to re-thread the token through every test case.
func authedPOSTForm(t *testing.T, h http.Handler, path string, fields url.Values, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	if _, token := csrfFromJar(cookies); token != "" && fields.Get("csrf_token") == "" {
		fields.Set("csrf_token", token)
	}
	req := httptest.NewRequest("POST", path, strings.NewReader(fields.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// login fetches a CSRF cookie, submits /admin/login with the paired
// token, and returns the accumulated jar (CSRF + session cookies)
// that subsequent tests replay.
func login(t *testing.T, h http.Handler, name, password string) []*http.Cookie {
	t.Helper()
	csrfCookie, token := fetchCSRF(t, h)
	form := url.Values{
		"name":       {name},
		"password":   {password},
		"csrf_token": {token},
	}
	req := httptest.NewRequest("POST", "/admin/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrfCookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("login: status = %d, want 302; body:\n%s", w.Code, w.Body.String())
	}
	sessionCookie := findCookie(w.Result().Cookies(), session.CookieName)
	if sessionCookie == nil {
		t.Fatalf("login: no session cookie set")
	}
	return []*http.Cookie{csrfCookie, sessionCookie}
}
