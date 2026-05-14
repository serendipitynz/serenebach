package app_test

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/serendipitynz/serenebach/internal/app"
	"github.com/serendipitynz/serenebach/internal/config"
)

// testPublicOrigin is the Origin header that newTestApp's
// PublicAllowedOrigins accepts. Public POST tests set it on every
// request to satisfy the same-origin guard the dynamic backend now
// uses in place of CSRF tokens for reader-facing endpoints.
const testPublicOrigin = "http://example.com"

// mainArea returns the substring of body up to the sidebar <aside>,
// or the whole body if no sidebar marker is found. Tests that assert
// "entry X must not appear" should scope to this — the default
// template's sidebar widgets ({latest_entry_list}, {category_list}, …)
// legitimately surface entries from outside the current page filter.
func mainArea(body string) string {
	if i := strings.Index(body, `<aside`); i >= 0 {
		return body[:i]
	}
	return body
}

// newTestApp spins up a full App against a fresh temp-dir SQLite file,
// runs migrations, and seeds in demo data so the home page has content.
// Honours SB_REBUILD_OUT so tests that exercise the admin rebuild trigger
// can point output at a writable temp dir.
func newTestApp(t *testing.T) *app.App {
	t.Helper()
	rebuildOut := os.Getenv("SB_REBUILD_OUT")
	if rebuildOut == "" {
		rebuildOut = filepath.Join(t.TempDir(), "public")
	}
	cfg := &config.Config{
		Mode:           config.ModeServer,
		Addr:           ":0",
		DBPath:         filepath.Join(t.TempDir(), "test.db"),
		RebuildOutDir:  rebuildOut,
		ImageDir:       filepath.Join(t.TempDir(), "img"),
		TemplateDir:    filepath.Join(t.TempDir(), "templates"),
		UploadMaxBytes: 10 << 20,
		// httptest.NewRequest sets r.Host = "example.com" by default, so
		// allow that origin for the same-origin guard on public POSTs
		// (comment / like / stamp). Tests still need to set the Origin
		// header explicitly — see helper publicSameOrigin().
		PublicAllowedOrigins: []string{"http://example.com"},
	}
	a, err := app.New(cfg)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })

	if err := a.Seed(context.Background(), app.DefaultSeed()); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	return a
}

func TestHomeRendersSeededContent(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)

	res := w.Result()
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html prefix", ct)
	}
	body := w.Body.String()

	for _, want := range []string{
		`<title>Serene Bach</title>`,
		`<h1 id="top"><a href="/">Serene Bach</a></h1>`,
		`ようこそ Serene Bach へ`,
		`カテゴリとテンプレートについて`,
		`<span class="entry_state_meta"><a href="/category/news/">お知らせ</a>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("home body missing %q\nfull body:\n%s", want, body)
			return
		}
	}
}

func TestEntryPermalinkReturns200ForPublished(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)

	req := httptest.NewRequest("GET", "/entry/1", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "ようこそ Serene Bach へ") {
		t.Errorf("missing seeded title; body:\n%s", body)
	}
}

func TestEntryPermalink404ForUnknownID(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)

	req := httptest.NewRequest("GET", "/entry/9999", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)

	if w.Code != 404 {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestEntryPermalink404ForBadID(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)

	req := httptest.NewRequest("GET", "/entry/notanumber", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)

	if w.Code != 404 {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestEntryPermalink410ForClosedEntry(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)

	// mark entry 1 as closed (status = -1)
	if _, err := a.DB.ExecContext(context.Background(),
		`UPDATE entries SET status = -1 WHERE id = 1`); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/entry/1", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)

	if w.Code != 410 {
		t.Fatalf("status = %d, want 410", w.Code)
	}
}

func TestSeedIsIdempotent(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)

	// second call should be a no-op and must not error
	if err := a.Seed(context.Background(), app.DefaultSeed()); err != nil {
		t.Fatalf("second Seed: %v", err)
	}

	// Render should still succeed and still produce the sample content
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("second render status = %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "ようこそ Serene Bach へ") {
		t.Fatalf("sample entry missing after second seed; body:\n%s", body)
	}
}
