package app_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAdminSidebarHonoursAcceptLanguage proves the i18n chain
// (middleware → cookie/header resolver → template `T` func → rendered
// page) works end-to-end. Sending `Accept-Language: en` to any
// authenticated admin page should render the English sidebar copy;
// `Accept-Language: ja` (or nothing) falls back to Japanese.
func TestAdminSidebarHonoursAcceptLanguage(t *testing.T) {
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
