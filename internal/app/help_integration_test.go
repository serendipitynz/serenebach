package app_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Help docs: /admin/help renders the index with a
// sidebar linking to every known slug, /admin/help/{slug} renders
// that page's Markdown as HTML, and unknown slugs 404. Locale
// resolution pulls from the sb_admin_lang cookie (same mechanism
// as the rest of the admin UI); missing translations fall back to
// the Japanese original and show a notice.

func TestHelpIndexRendersSidebar(t *testing.T) {
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	body := authedGET(t, a.Handler(), "/admin/help", cookies).Body.String()
	if !strings.Contains(body, "Serene Bach ヘルプ") {
		t.Errorf("index body missing title; body head:\n%s", head(body, 400))
	}
	for _, slug := range []string{"getting-started", "entries", "templates", "mcp"} {
		if !strings.Contains(body, "/admin/help/"+slug) {
			t.Errorf("sidebar missing link to %q", slug)
		}
	}
}

func TestHelpPageRenders(t *testing.T) {
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	body := authedGET(t, a.Handler(), "/admin/help/getting-started", cookies).Body.String()
	if !strings.Contains(body, "<h1>") {
		t.Errorf("getting-started body missing h1; body head:\n%s", head(body, 400))
	}
	if !strings.Contains(body, "はじめに") {
		t.Errorf("getting-started body missing ja page title")
	}
}

func TestHelpUnknownSlug404s(t *testing.T) {
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	w := authedGET(t, a.Handler(), "/admin/help/does-not-exist", cookies)
	if w.Code != 404 {
		t.Fatalf("unknown slug status = %d, want 404", w.Code)
	}
}

func TestHelpEnglishLocaleRendersTranslatedPage(t *testing.T) {
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	// sb_admin_lang cookie = en → getting-started is translated, so
	// the English title should render and no fallback banner should
	// appear.
	req := httptest.NewRequest("GET", "/admin/help/getting-started", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	req.AddCookie(&http.Cookie{Name: "sb_admin_lang", Value: "en"})
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	body := w.Body.String()

	if !strings.Contains(body, "Getting started") {
		t.Errorf("English getting-started missing translated title; body head:\n%s", head(body, 400))
	}
	if strings.Contains(body, "This page has not been translated yet") {
		t.Errorf("fallback banner should not appear on a translated page")
	}
}

// Fallback to ja used to be exercised here against an untranslated en
// slug. Now that every help page is translated, the path is verified
// in helpdocs's own unit test (Lookup with a non-existent locale)
// instead of via an HTTP fixture — see docs/help/embed_test.go.

func head(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
