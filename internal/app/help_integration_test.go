package app_test

import (
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/serendipitynz/serenebach/internal/config"
)

// Help docs: /admin/help redirects to /admin/help/index, which
// renders the index with a sidebar linking to every known slug,
// /admin/help/{slug} renders that page's Markdown as HTML, and
// unknown slugs 404. Locale
// resolution pulls from the sb_admin_lang cookie (same mechanism
// as the rest of the admin UI); missing translations fall back to
// the Japanese original and show a notice.

func TestHelpIndexRendersSidebar(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	body := authedGET(t, a.Handler(), "/admin/help/index", cookies).Body.String()
	if !strings.Contains(body, "Serene Bach ヘルプ") {
		t.Errorf("index body missing title; body head:\n%s", head(body, 400))
	}
	for _, slug := range []string{"getting-started", "entries", "templates", "mcp"} {
		if !strings.Contains(body, "/admin/help/"+slug) {
			t.Errorf("sidebar missing link to %q", slug)
		}
	}
}

var hrefPattern = regexp.MustCompile(`href="([^"]*)"`)

// The index Markdown links to the other pages by bare slug, so those
// links only land on help pages when the index is served from a URL
// under /admin/help/. Opening the index from the admin sidebar's
// /admin/help must therefore end up there, mounted at the root or under
// a base path alike.
func TestHelpBareIndexLinksResolveToHelpPages(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, basePath, lang string
	}{
		{"root/ja", "", "ja"},
		{"root/en", "", "en"},
		{"basepath/ja", "/sb4", "ja"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := newTestAppWith(t, func(c *config.Config) { c.BasePath = tc.basePath })
			cookies := append(login(t, a.Handler(), "admin", "changeme"),
				&http.Cookie{Name: "sb_admin_lang", Value: tc.lang})

			w := authedGET(t, a.Handler(), "/admin/help", cookies)
			if w.Code != http.StatusFound {
				t.Fatalf("/admin/help status = %d, want %d", w.Code, http.StatusFound)
			}
			loc := w.Header().Get("Location")
			if want := tc.basePath + "/admin/help/index"; loc != want {
				t.Fatalf("/admin/help Location = %q, want %q", loc, want)
			}

			// The handler under test sits behind the base-path StripPrefix
			// that cmd/serenebach applies, so requests drop the prefix.
			body := authedGET(t, a.Handler(), strings.TrimPrefix(loc, tc.basePath), cookies).Body.String()
			start := strings.Index(body, `<article class="help-body`)
			end := strings.Index(body, `</article>`)
			if start < 0 || end < start {
				t.Fatalf("index body missing help article; body head:\n%s", head(body, 400))
			}
			pageURL, _ := url.Parse("http://example.com" + loc)
			helpPrefix := tc.basePath + "/admin/help/"
			checked := 0
			for _, m := range hrefPattern.FindAllStringSubmatch(body[start:end], -1) {
				ref, err := url.Parse(html.UnescapeString(m[1]))
				if err != nil {
					t.Errorf("unparsable href %q: %v", m[1], err)
					continue
				}
				if ref.IsAbs() || ref.Path == "" {
					continue
				}
				target := pageURL.ResolveReference(ref).Path
				if !strings.HasPrefix(target, helpPrefix) || target == helpPrefix {
					t.Errorf("href %q resolves to %q, want a page under %q", m[1], target, helpPrefix)
					continue
				}
				if code := authedGET(t, a.Handler(), strings.TrimPrefix(target, tc.basePath), cookies).Code; code != http.StatusOK {
					t.Errorf("href %q → %q status = %d, want 200", m[1], target, code)
				}
				checked++
			}
			if checked < 10 {
				t.Errorf("checked %d page links in the index body, want at least 10", checked)
			}
		})
	}
}

func TestHelpPageRenders(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	w := authedGET(t, a.Handler(), "/admin/help/does-not-exist", cookies)
	if w.Code != 404 {
		t.Fatalf("unknown slug status = %d, want 404", w.Code)
	}
}

func TestHelpEnglishLocaleRendersTranslatedPage(t *testing.T) {
	t.Parallel()
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
