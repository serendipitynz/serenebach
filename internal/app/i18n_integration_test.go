package app_test

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// TestAdminSidebarHonoursAcceptLanguage proves the i18n chain
// (middleware → cookie/header resolver → template `T` func → rendered
// page) works end-to-end. Sending `Accept-Language: en` to any
// authenticated admin page should render the English sidebar copy;
// `Accept-Language: ja` (or nothing) falls back to Japanese.
func TestAdminSidebarHonoursAcceptLanguage(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	// English via Accept-Language
	req := httptest.NewRequest("GET", "/admin/", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	req.Header.Set("Accept-Language", "en-US,en;q=0.9,ja;q=0.5")
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{"New entry", "Entries", "Settings", "Public site"} {
		if !strings.Contains(body, want) {
			t.Errorf("en body missing %q", want)
		}
	}
	// Pages beyond layout.html haven't been translated yet — only
	// assert that the sidebar itself rendered English, not that the
	// whole response is Japanese-free.
	sidebar := sliceBetween(body, `<aside class="sidebar">`, `</aside>`)
	if strings.Contains(sidebar, "新規記事") {
		t.Errorf("en sidebar should not contain Japanese copy; got:\n%s", sidebar)
	}

	// Japanese via explicit cookie — even when Accept-Language says en.
	req2 := httptest.NewRequest("GET", "/admin/", nil)
	for _, c := range cookies {
		req2.AddCookie(c)
	}
	req2.AddCookie(&http.Cookie{Name: "sb_admin_lang", Value: "ja"})
	req2.Header.Set("Accept-Language", "en-US,en;q=0.9")
	w2 := httptest.NewRecorder()
	a.Handler().ServeHTTP(w2, req2)
	if w2.Code != 200 {
		t.Fatalf("status = %d", w2.Code)
	}
	body2 := w2.Body.String()
	if !strings.Contains(body2, "新規記事") {
		t.Errorf("cookie-ja body should render Japanese sidebar")
	}
	if strings.Contains(body2, "New entry") {
		t.Errorf("cookie-ja body should not render English sidebar")
	}
}

// sliceBetween returns the substring between the first `start` match
// and the first `end` match after it. Used by the sidebar assertion
// to scope "no Japanese copy" to the `<aside>` region only.
func sliceBetween(s, start, end string) string {
	i := strings.Index(s, start)
	if i < 0 {
		return ""
	}
	s = s[i+len(start):]
	j := strings.Index(s, end)
	if j < 0 {
		return s
	}
	return s[:j]
}

func TestAdminHTMLLangAttrReflectsLocale(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	req := httptest.NewRequest("GET", "/admin/", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	req.Header.Set("Accept-Language", "en")
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if !strings.Contains(w.Body.String(), `<html lang="en"`) {
		t.Errorf("English request should render `<html lang=\"en\"`; first 400 bytes:\n%s", w.Body.String()[:min2(400, len(w.Body.String()))])
	}
}

// TestAdminLanguageEndpointSetsCookie verifies that POST /admin/settings/language
// issues sb_admin_lang via Set-Cookie and that a follow-up GET renders the
// requested locale. Server-issued cookie is the only path that survives
// Sakura's ENC_ cookie protection layer in CGI deployments.
func TestAdminLanguageEndpointSetsCookie(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	// Pull a CSRF token off the screen settings page.
	getReq := httptest.NewRequest("GET", "/admin/settings/screen", nil)
	for _, c := range cookies {
		getReq.AddCookie(c)
	}
	getRec := httptest.NewRecorder()
	a.Handler().ServeHTTP(getRec, getReq)
	token := extractCSRFToken(t, getRec.Body.String())

	// POST the language change.
	body := strings.NewReader("lang=en&csrf_token=" + token)
	postReq := httptest.NewRequest("POST", "/admin/settings/language", body)
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range cookies {
		postReq.AddCookie(c)
	}
	postRec := httptest.NewRecorder()
	a.Handler().ServeHTTP(postRec, postReq)
	if postRec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", postRec.Code)
	}
	var langCookie *http.Cookie
	for _, c := range postRec.Result().Cookies() {
		if c.Name == "sb_admin_lang" {
			langCookie = c
			break
		}
	}
	if langCookie == nil {
		t.Fatal("sb_admin_lang cookie not set on response")
	}
	if langCookie.Value != "en" {
		t.Errorf("cookie value = %q, want en", langCookie.Value)
	}

	// Follow-up GET with the new cookie should render English.
	nextReq := httptest.NewRequest("GET", "/admin/", nil)
	for _, c := range cookies {
		nextReq.AddCookie(c)
	}
	nextReq.AddCookie(langCookie)
	nextRec := httptest.NewRecorder()
	a.Handler().ServeHTTP(nextRec, nextReq)
	if !strings.Contains(nextRec.Body.String(), `<html lang="en"`) {
		t.Errorf("post-toggle response should render English `<html lang=\"en\"`")
	}
}

func TestAdminLanguageEndpointRejectsUnsupported(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	getReq := httptest.NewRequest("GET", "/admin/settings/screen", nil)
	for _, c := range cookies {
		getReq.AddCookie(c)
	}
	getRec := httptest.NewRecorder()
	a.Handler().ServeHTTP(getRec, getReq)
	token := extractCSRFToken(t, getRec.Body.String())

	body := strings.NewReader("lang=fr&csrf_token=" + token)
	postReq := httptest.NewRequest("POST", "/admin/settings/language", body)
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range cookies {
		postReq.AddCookie(c)
	}
	postRec := httptest.NewRecorder()
	a.Handler().ServeHTTP(postRec, postReq)
	if postRec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", postRec.Code)
	}
	for _, c := range postRec.Result().Cookies() {
		if c.Name == "sb_admin_lang" {
			t.Fatal("should not set sb_admin_lang for unsupported language")
		}
	}
}

func TestAdminLanguageEndpointRequiresCSRF(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	body := strings.NewReader("lang=en")
	postReq := httptest.NewRequest("POST", "/admin/settings/language", body)
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range cookies {
		postReq.AddCookie(c)
	}
	postRec := httptest.NewRecorder()
	a.Handler().ServeHTTP(postRec, postReq)
	if postRec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", postRec.Code)
	}
}

// TestAdminLanguageSelectorReflectsLocale verifies the screen-settings
// language <select> renders with the active locale as its selected
// option, so JS doesn't have to decode the (Sakura-encrypted) cookie
// to keep the dropdown in sync with the rest of the UI.
func TestAdminLanguageSelectorReflectsLocale(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	cases := []struct {
		cookieVal  string
		wantOption string // <option value="X" ... selected>
	}{
		{"en", `value="en" selected`},
		{"ja", `value="ja" selected`},
	}
	for _, tc := range cases {
		req := httptest.NewRequest("GET", "/admin/settings/screen", nil)
		for _, c := range cookies {
			req.AddCookie(c)
		}
		req.AddCookie(&http.Cookie{Name: "sb_admin_lang", Value: tc.cookieVal})
		rec := httptest.NewRecorder()
		a.Handler().ServeHTTP(rec, req)
		if !strings.Contains(rec.Body.String(), tc.wantOption) {
			t.Errorf("locale=%s: response missing %q", tc.cookieVal, tc.wantOption)
		}
	}
}

// extractCSRFToken pulls the value out of the first
// <input name="csrf_token" value="..."> found in html.
func extractCSRFToken(t *testing.T, html string) string {
	t.Helper()
	var re = regexp.MustCompile(`<input[^>]*name="csrf_token"[^>]*value="([^"]*)"`)
	m := re.FindStringSubmatch(html)
	if len(m) < 2 {
		t.Fatal("csrf_token input not found in response")
	}
	return m[1]
}
