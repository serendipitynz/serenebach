package app_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/serendipitynz/serenebach/internal/auth"
)

// TestProfileFormOpensForEveryRole confirms /admin/profile is not
// gated to admins — a regular-tier session must be able to reach
// their own profile editor.
func TestProfileFormOpensForEveryRole(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	hash, _ := auth.HashPassword("secret")
	if _, err := a.DB.Exec(`INSERT INTO users (wid, name, display_name, email, password_hash, role, created_at, updated_at, list_visible, description_format)
		VALUES (1, 'regular', 'Reg', '', ?, 3, strftime('%s','now'), strftime('%s','now'), 1, 'html')`, hash); err != nil {
		t.Fatal(err)
	}
	cookies := login(t, a.Handler(), "regular", "secret")
	w := authedGET(t, a.Handler(), "/admin/profile", cookies)
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	for _, want := range []string{
		`name="name"`,
		`name="description"`,
		`name="password"`,
		`name="list_visible"`,
	} {
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("profile form missing %q", want)
		}
	}
}

// TestProfileSavePersistsFieldsAndOptionallyPassword round-trips a
// profile edit: description / display name / password all land in
// the DB, the session user's password is replaced so login with the
// new password works afterwards.
func TestProfileSavePersistsFieldsAndOptionallyPassword(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	csrfCookie, token := fetchCSRF(t, a.Handler())

	form := url.Values{
		"csrf_token":         {token},
		"name":               {"admin"},
		"display_name":       {"Takkyun"},
		"email":              {"admin@example.com"},
		"description":        {"hello\nworld"},
		"description_format": {"markdown"},
		"list_visible":       {"on"},
		"password":           {"newsecret"},
		"password_confirm":   {"newsecret"},
	}
	req := httptest.NewRequest("POST", "/admin/profile", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrfCookie)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != 302 {
		t.Fatalf("status = %d", w.Code)
	}

	// Fields persisted.
	var displayName, description, descFmt string
	var listVis int
	if err := a.DB.QueryRow(`SELECT display_name, description, description_format, list_visible FROM users WHERE name = 'admin'`).
		Scan(&displayName, &description, &descFmt, &listVis); err != nil {
		t.Fatal(err)
	}
	if displayName != "Takkyun" || description != "hello\nworld" || descFmt != "markdown" || listVis != 1 {
		t.Errorf("persisted = (%q, %q, %q, %d)", displayName, description, descFmt, listVis)
	}

	// Old password no longer logs in, new one does.
	oldCookies := loginAttempt(t, a.Handler(), "admin", "changeme")
	if oldCookies != nil {
		t.Errorf("old password still accepted after profile password change")
	}
	freshCookies := loginAttempt(t, a.Handler(), "admin", "newsecret")
	if freshCookies == nil {
		t.Errorf("new password rejected")
	}
}

// TestProfileSaveMismatchedPasswordKeepsProfileButNotPassword verifies
// the partial-save UX: profile fields go through even when the
// password-confirm mismatches, and the caller is informed. The prior
// password must still work.
func TestProfileSaveMismatchedPasswordKeepsProfileButNotPassword(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	csrfCookie, token := fetchCSRF(t, a.Handler())

	form := url.Values{
		"csrf_token":         {token},
		"name":               {"admin"},
		"display_name":       {"Updated"},
		"description":        {"updated bio"},
		"description_format": {"markdown"},
		"list_visible":       {"on"},
		"password":           {"aaa"},
		"password_confirm":   {"bbb"},
	}
	req := httptest.NewRequest("POST", "/admin/profile", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrfCookie)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status = %d, want 200 (stay on form with message)", w.Code)
	}
	if !strings.Contains(w.Body.String(), "一致しません") {
		t.Errorf("missing mismatch message")
	}

	// Profile fields saved before the mismatch check.
	var displayName string
	_ = a.DB.QueryRow(`SELECT display_name FROM users WHERE name = 'admin'`).Scan(&displayName)
	if displayName != "Updated" {
		t.Errorf("display name not persisted despite password mismatch: %q", displayName)
	}

	// Original password still works.
	if loginAttempt(t, a.Handler(), "admin", "changeme") == nil {
		t.Errorf("old password no longer works — mismatched change should not have overwritten it")
	}
}

// TestLayoutHeaderLinksToProfile confirms the sidebar account block
// renders the username as an anchor pointing at /admin/profile — the
// discoverability hook for self-edit.
func TestLayoutHeaderLinksToProfile(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	w := authedGET(t, a.Handler(), "/admin/", cookies)
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `<a class="sidebar-account" href="/admin/profile"`) {
		t.Errorf("sidebar account block is not linking to /admin/profile")
	}
}

// TestProfileBlockEmitsUserList confirms the SB3 `profile` block
// renders once when there's at least one list_visible user and the
// `{user_list}` tag inside it carries the <ul><li> fragment of their
// anchor-wrapped display names.
func TestProfileBlockEmitsUserList(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)

	newMain := "<!doctype html><html><body>\n" +
		"<!-- BEGIN entry -->\n<article></article>\n<!-- END entry -->\n" +
		"<!-- BEGIN profile -->\n<nav class=\"people\">{user_list}</nav>\n<!-- END profile -->\n" +
		"</body></html>\n"
	if _, err := a.DB.Exec(`UPDATE templates SET main_body = ? WHERE is_active = 1`, newMain); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `<nav class="people"><ul><li><a href="/profile/1/">admin</a></li></ul></nav>`) {
		t.Errorf("profile block did not emit {user_list}\nbody: %s", body)
	}

	// Toggle list_visible off — the profile block must strip (SB3
	// `_profile` returns 0 when the list is empty).
	if _, err := a.DB.Exec(`UPDATE users SET list_visible = 0 WHERE name = 'admin'`); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	a.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if strings.Contains(w.Body.String(), `class="people"`) {
		t.Errorf("profile block still renders after list_visible=0")
	}
}

// loginAttempt is like login() but returns nil instead of t.Fatal'ing
// when the credentials are rejected — used by password-change tests
// that need to assert "old password no longer works".
func loginAttempt(t *testing.T, h http.Handler, name, password string) []*http.Cookie {
	t.Helper()
	csrfCookie, token := fetchCSRF(t, h)
	form := url.Values{"name": {name}, "password": {password}, "csrf_token": {token}}
	req := httptest.NewRequest("POST", "/admin/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrfCookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 302 {
		return nil
	}
	return w.Result().Cookies()
}
