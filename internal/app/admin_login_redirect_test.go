package app_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestLoginAfterUnauthenticatedAdminDoesNotDoubleEncodeNext walks the
// bug the user flagged: GET /admin while unauthenticated → redirect to
// /admin/login?next=/admin/ → post credentials → Location was double-
// encoded (/admin/%2Fadmin%2F) because safeNextQueryParam ran
// url.QueryEscape on top of html/template's URL-context encoding.
// Regression guard: the post-login redirect MUST land on "/admin/".
func TestLoginAfterUnauthenticatedAdminDoesNotDoubleEncodeNext(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)

	// 1. Unauthenticated GET /admin → 302 to /admin/login?next=/admin/
	req := httptest.NewRequest("GET", "/admin", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("initial /admin status = %d, want 302", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "/admin/login") || !strings.Contains(loc, "next=") {
		t.Fatalf("initial redirect Location = %q, want /admin/login?next=...", loc)
	}

	// 2. Render the login form as the browser would; confirm the form's
	// action attribute renders a single encoding (%2Fadmin%2F), not two.
	loginReq := httptest.NewRequest("GET", loc, nil)
	loginW := httptest.NewRecorder()
	a.Handler().ServeHTTP(loginW, loginReq)
	if loginW.Code != 200 {
		t.Fatalf("login GET status = %d", loginW.Code)
	}
	body := loginW.Body.String()
	if strings.Contains(body, "%252F") {
		t.Errorf("login form action double-escapes the next value; body contained %%252F:\n%s", body)
	}

	// 3. POST credentials with next=/admin/ on the URL. Must 302 to
	// /admin/ (not /admin/%2Fadmin%2F).
	csrfCookie, token := fetchCSRF(t, a.Handler())
	form := url.Values{
		"name":       {"admin"},
		"password":   {"changeme"},
		"csrf_token": {token},
	}
	req2 := httptest.NewRequest("POST", "/admin/login?next=/admin/", strings.NewReader(form.Encode()))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req2.AddCookie(csrfCookie)
	w2 := httptest.NewRecorder()
	a.Handler().ServeHTTP(w2, req2)
	if w2.Code != http.StatusFound {
		t.Fatalf("login POST status = %d; body:\n%s", w2.Code, w2.Body.String())
	}
	if loc := w2.Header().Get("Location"); loc != "/admin/" {
		t.Errorf("post-login Location = %q, want /admin/", loc)
	}
}
