package admin

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	admintpl "github.com/serendipitynz/serenebach/web/templates/admin"
)

// TestServeAdminCSSBundleOrder verifies the concatenated admin stylesheet
// serves with the CSS MIME type and preserves the authored cascade order
// across the css/ sources. The cascade is a behavioural contract (dark
// theme must stay after tokens; responsive is last), so we assert the
// section banners appear in sequence rather than snapshotting the file.
func TestServeAdminCSSBundleOrder(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/static/admin.css", nil)
	serveAdminCSS()(rec, req)
	if rec.Code != 200 {
		t.Fatalf("GET admin.css: status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/css; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/css; charset=utf-8", ct)
	}
	body := rec.Body.String()
	// One stable marker per source, in the order they must concatenate.
	markers := []string{
		":root",          // 00-tokens
		"dark theme",     // 10-theme
		"=== layout ===", // 20-layout
		"=== alerts ===", // 30-components
		"=== modal ===",  // 40-features
		"=== responsive", // 90-responsive
	}
	prev := -1
	for _, m := range markers {
		idx := strings.Index(body, m)
		if idx < 0 {
			t.Fatalf("bundle missing marker %q", m)
		}
		if idx <= prev {
			t.Errorf("marker %q out of cascade order (idx %d <= prev %d)", m, idx, prev)
		}
		prev = idx
	}
}

// TestServeAssetReturns304OnMatchingETag verifies that admin static
// asset handlers honour If-None-Match for browser-side cache reuse.
// Without 304, Sakura CGI hosts re-pay the ~200ms cgi.Serve startup
// cost on every page load even when the asset hasn't changed.
func TestServeAssetReturns304OnMatchingETag(t *testing.T) {
	h := serveAdminCSS()

	// First request: get the ETag.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/static/admin.css", nil)
	h(rec, req)
	if rec.Code != 200 {
		t.Fatalf("first GET: status = %d", rec.Code)
	}
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("first GET: missing ETag header")
	}

	// Conditional request: must return 304.
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/static/admin.css", nil)
	req2.Header.Set("If-None-Match", etag)
	h(rec2, req2)
	if rec2.Code != http.StatusNotModified {
		t.Errorf("conditional GET: status = %d, want 304", rec2.Code)
	}
	if rec2.Body.Len() != 0 {
		t.Errorf("304 response body should be empty, got %d bytes", rec2.Body.Len())
	}
	if rec2.Header().Get("ETag") != etag {
		t.Errorf("304 response ETag mismatch: got %q, want %q", rec2.Header().Get("ETag"), etag)
	}
}

// TestServeAssetReturns200OnDifferentETag verifies that a mismatched
// If-None-Match still returns 200 with the full body.
func TestServeAssetReturns200OnDifferentETag(t *testing.T) {
	h := serveAdminCSS()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/static/admin.css", nil)
	req.Header.Set("If-None-Match", `"stale-value"`)
	h(rec, req)
	if rec.Code != 200 {
		t.Errorf("mismatched ETag: status = %d, want 200", rec.Code)
	}
	if rec.Body.Len() == 0 {
		t.Error("mismatched ETag: expected body, got empty")
	}
}

// TestServeEmbeddedReturns304OnMatchingETag checks the same behaviour
// for the logo / favicon helper.
func TestServeEmbeddedReturns304OnMatchingETag(t *testing.T) {
	h := serveEmbedded("assets/sb_logo_dark.svg", "image/svg+xml")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/static/sb_logo_dark.svg", nil)
	h(rec, req)
	if rec.Code != 200 {
		t.Fatalf("first GET: status = %d", rec.Code)
	}
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("first GET: missing ETag header")
	}

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/static/sb_logo_dark.svg", nil)
	req2.Header.Set("If-None-Match", etag)
	h(rec2, req2)
	if rec2.Code != http.StatusNotModified {
		t.Errorf("conditional GET: status = %d, want 304", rec2.Code)
	}
	if rec2.Body.Len() != 0 {
		t.Errorf("304 response body should be empty, got %d bytes", rec2.Body.Len())
	}
}

// TestServeModulesReturns200And304 verifies the module subtree handler
// honours the same ETag / MIME contract as the top-level asset helpers.
func TestServeModulesReturns200And304(t *testing.T) {
	modulesSub, err := fs.Sub(admintpl.FS(), "modules")
	if err != nil {
		t.Skip("modules directory not embedded")
	}
	h := serveStaticSubtree(modulesSub, "/admin/static/modules/", "text/javascript; charset=utf-8")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/admin/static/modules/core/i18n.js", nil)
	h(rec, req)
	if rec.Code != 200 {
		t.Fatalf("first GET: status = %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if ct != "text/javascript; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/javascript; charset=utf-8", ct)
	}
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("first GET: missing ETag")
	}

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/admin/static/modules/core/i18n.js", nil)
	req2.Header.Set("If-None-Match", etag)
	h(rec2, req2)
	if rec2.Code != http.StatusNotModified {
		t.Errorf("conditional GET: status = %d, want 304", rec2.Code)
	}
	if rec2.Body.Len() != 0 {
		t.Errorf("304 response body should be empty, got %d bytes", rec2.Body.Len())
	}
}
