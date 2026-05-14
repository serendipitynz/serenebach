package app_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestPublicRouteBuiltInsBeatFlatPageCatchall pins the chi-trie
// behaviour the public router relies on: every named route registered
// in public.Mount must outrank the trailing `/*` flat-page handler,
// even when the DB carries a flat page whose slug looks like a
// reserved prefix.
func TestPublicRouteBuiltInsBeatFlatPageCatchall(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	now := time.Now().Unix()

	// Seed flat pages with slugs that collide with reserved prefixes.
	// Admin validation rejects these; raw SQL is the way to plant a
	// row that simulates "what if a regression in validation let one
	// of these in?".
	rows := []struct{ slug, body string }{
		{"/entry/forged", "FORGED-ENTRY-PAGE"},
		{"/category/forged", "FORGED-CATEGORY-PAGE"},
		{"/tag/forged", "FORGED-TAG-PAGE"},
		{"/archive/forged", "FORGED-ARCHIVE-PAGE"},
		{"/profile/forged", "FORGED-PROFILE-PAGE"},
		{"/rss.xml", "FORGED-RSS-PAGE"},
		{"/atom.xml", "FORGED-ATOM-PAGE"},
		{"/style.css", "FORGED-STYLE-PAGE"},
	}
	for _, r := range rows {
		if _, err := a.DB.Exec(`INSERT INTO pages (wid, author_id, title, body, format, slug, template_id, sort_order, status, og_bg_image_path, created_at, updated_at)
			VALUES (1, 1, 'forged', ?, 'html', ?, 0, 0, 1, '', ?, ?)`,
			r.body, r.slug, now, now); err != nil {
			t.Fatalf("seed page %s: %v", r.slug, err)
		}
	}

	// Each case is a request that *could* be served by the catch-all
	// page handler if precedence regressed; instead the named handler
	// must answer.
	cases := []struct {
		path       string
		wantStatus int
		// wantBodyAbsent is the unique sentinel from the seeded flat
		// page — its absence proves the catch-all did not run.
		wantBodyAbsent string
	}{
		{"/entry/1", http.StatusOK, "FORGED-ENTRY-PAGE"},
		{"/category/news/", http.StatusOK, "FORGED-CATEGORY-PAGE"},
		{"/archive/2026/", http.StatusOK, "FORGED-ARCHIVE-PAGE"},
		{"/profile/1/", http.StatusOK, "FORGED-PROFILE-PAGE"},
		{"/rss.xml", http.StatusOK, "FORGED-RSS-PAGE"},
		{"/atom.xml", http.StatusOK, "FORGED-ATOM-PAGE"},
		{"/style.css", http.StatusOK, "FORGED-STYLE-PAGE"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			req := httptest.NewRequest("GET", tc.path, nil)
			w := httptest.NewRecorder()
			a.Handler().ServeHTTP(w, req)
			if w.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tc.wantStatus)
			}
			if strings.Contains(w.Body.String(), tc.wantBodyAbsent) {
				t.Errorf("flat-page body leaked into %s; the catch-all handler ran instead of the named one", tc.path)
			}
		})
	}
}

// TestPublicRouteTagBuiltInBeatsFlatPage handles the /tag/<slug>/
// case separately because it requires both a tag row and a published
// entry to render a 200. The forged flat page at /tag/forged must not
// run when a real tag with that slug exists.
func TestPublicRouteTagBuiltInBeatsFlatPage(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	now := time.Now().Unix()

	// Real tag + entry association.
	if _, err := a.DB.Exec(`INSERT INTO tags (wid, name, slug, created_at, updated_at) VALUES (1, 'diary', 'diary', ?, ?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := a.DB.Exec(`INSERT INTO entry_tags (entry_id, tag_id) VALUES (1, (SELECT id FROM tags WHERE slug='diary'))`); err != nil {
		t.Fatal(err)
	}
	// Forged flat page at the same path.
	if _, err := a.DB.Exec(`INSERT INTO pages (wid, author_id, title, body, format, slug, template_id, sort_order, status, og_bg_image_path, created_at, updated_at)
		VALUES (1, 1, 'forged', 'FORGED-TAG-PAGE', 'html', '/tag/diary', 0, 0, 1, '', ?, ?)`, now, now); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/tag/diary/", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if strings.Contains(w.Body.String(), "FORGED-TAG-PAGE") {
		t.Errorf("flat-page body leaked into /tag/diary/")
	}
}

// TestPublicRouteImgFileServerWinsOverPage confirms the /img/* file
// server registered at the root mux outranks the public catch-all
// flat-page handler. A real PNG is dropped under a.Config.ImageDir
// and a forged flat page is planted at the matching slug; the
// request must come back 200 with the file bytes, not the page
// body. Asserting the file content (and not just "non-page") is
// what locks in "the named /img/* route really won" — accepting
// only "anything but the page" would still pass if the FileServer
// were removed and the catch-all returned 404.
func TestPublicRouteImgFileServerWinsOverPage(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	now := time.Now().Unix()

	// Real PNG file under the test app's ImageDir.
	pngBytes := []byte("\x89PNG\r\n\x1a\nFIXTURE")
	imgPath := filepath.Join(a.Config.ImageDir, "fixture.png")
	if err := os.MkdirAll(a.Config.ImageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(imgPath, pngBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	// Forged flat page at the same path so a regression that skips the
	// /img/* FileServer would surface FORGED-IMG-PAGE in the response.
	if _, err := a.DB.Exec(`INSERT INTO pages (wid, author_id, title, body, format, slug, template_id, sort_order, status, og_bg_image_path, created_at, updated_at)
		VALUES (1, 1, 'forged', 'FORGED-IMG-PAGE', 'html', '/img/fixture.png', 0, 0, 1, '', ?, ?)`, now, now); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/img/fixture.png", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !bytes.Equal(w.Body.Bytes(), pngBytes) {
		t.Errorf("body bytes do not match the seeded PNG; FileServer did not handle the request")
	}
	if strings.Contains(w.Body.String(), "FORGED-IMG-PAGE") {
		t.Errorf("flat-page body leaked into /img/fixture.png")
	}
}

// TestPublicRouteAdminRedirectsToLogin confirms /admin (without
// trailing slash) is owned by the admin route group and falls
// through to the login redirect rather than rendering a flat page.
func TestPublicRouteAdminRedirectsToLogin(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	now := time.Now().Unix()

	// Forged flat page at /admin to prove the admin group still wins.
	if _, err := a.DB.Exec(`INSERT INTO pages (wid, author_id, title, body, format, slug, template_id, sort_order, status, og_bg_image_path, created_at, updated_at)
		VALUES (1, 1, 'forged', 'FORGED-ADMIN-PAGE', 'html', '/admin', 0, 0, 1, '', ?, ?)`, now, now); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/admin/", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Errorf("status = %d, want 302", w.Code)
	}
	if strings.Contains(w.Body.String(), "FORGED-ADMIN-PAGE") {
		t.Errorf("flat-page body leaked into /admin/")
	}
}

// TestPublicRouteFlatPageCatchallServesUnreservedPaths covers the
// happy-path side of the catch-all: paths that don't match any named
// route fall through to servePage, which renders the flat page when
// one exists.
func TestPublicRouteFlatPageCatchallServesUnreservedPaths(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	now := time.Now().Unix()

	rows := []struct{ slug, body string }{
		{"/about", "ABOUT-PAGE-BODY"},
		{"/service/pricing", "PRICING-PAGE-BODY"},
	}
	for _, r := range rows {
		if _, err := a.DB.Exec(`INSERT INTO pages (wid, author_id, title, body, format, slug, template_id, sort_order, status, og_bg_image_path, created_at, updated_at)
			VALUES (1, 1, ?, ?, 'html', ?, 0, 0, 1, '', ?, ?)`,
			r.slug, r.body, r.slug, now, now); err != nil {
			t.Fatalf("seed page %s: %v", r.slug, err)
		}
	}

	for _, r := range rows {
		t.Run(r.slug, func(t *testing.T) {
			req := httptest.NewRequest("GET", r.slug, nil)
			w := httptest.NewRecorder()
			a.Handler().ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", w.Code)
			}
			if !strings.Contains(w.Body.String(), r.body) {
				t.Errorf("expected page body %q in response", r.body)
			}
		})
	}
}

// TestPublicRouteUnknownPathReturns404 verifies a request that
// matches no named route AND no flat-page slug falls through to a
// 404, not a 500 or empty 200.
func TestPublicRouteUnknownPathReturns404(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)

	req := httptest.NewRequest("GET", "/this-path-does-not-exist", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// TestPublicRouteDraftPageReturns404 confirms the catch-all does NOT
// expose draft (status=0) or otherwise-unpublished pages, even when
// the path matches the slug exactly.
func TestPublicRouteDraftPageReturns404(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	now := time.Now().Unix()

	if _, err := a.DB.Exec(`INSERT INTO pages (wid, author_id, title, body, format, slug, template_id, sort_order, status, og_bg_image_path, created_at, updated_at)
		VALUES (1, 1, 'Draft', 'DRAFT-BODY', 'html', '/secret-draft', 0, 0, 0, '', ?, ?)`, now, now); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/secret-draft", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
	if strings.Contains(w.Body.String(), "DRAFT-BODY") {
		t.Errorf("draft body leaked into response")
	}
}

// TestPublicRouteLegacyCGIRedirects exercises the SB3 compatibility
// shim mounted at /sb.cgi. Each redirect target is documented in
// legacy_cgi.go; the test only sanity-checks the request shape lands
// somewhere under the canonical Go URL space.
func TestPublicRouteLegacyCGIRedirects(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)

	// Plant a legacy_id on entry 1 so ?eid=42 resolves to it.
	if _, err := a.DB.Exec(`UPDATE entries SET legacy_id = 42 WHERE id = 1`); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		path       string
		wantStatus int
		wantPrefix string
	}{
		{"/sb.cgi?eid=42", http.StatusMovedPermanently, "/entry/"},
		{"/sb.cgi?month=202604", http.StatusMovedPermanently, "/archive/2026/04/"},
		{"/sb.cgi?mode=archive&cond=2026", http.StatusMovedPermanently, "/archive/2026/"},
		// Unknown mode falls back to the home page.
		{"/sb.cgi?mode=mobile", http.StatusMovedPermanently, "/"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			req := httptest.NewRequest("GET", tc.path, nil)
			w := httptest.NewRecorder()
			a.Handler().ServeHTTP(w, req)
			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", w.Code, tc.wantStatus)
			}
			loc := w.Header().Get("Location")
			if !strings.Contains(loc, tc.wantPrefix) {
				t.Errorf("Location = %q, want prefix %q", loc, tc.wantPrefix)
			}
		})
	}
}

// TestPublicRouteTemplateStyleCSSWins confirms /template/{id}/style.css
// is served by the public templateStyleCSS handler and outranks the
// /template/* file-server fallback (registered for on-disk template
// assets).
func TestPublicRouteTemplateStyleCSSWins(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)

	req := httptest.NewRequest("GET", "/template/1/style.css", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
		t.Errorf("Content-Type = %q, want text/css prefix", ct)
	}
}
