package app_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRebuildGETShowsInitialState(t *testing.T) {
	a := newTestApp(t)
	cookie := login(t, a.Handler(), "admin", "changeme")
	w := authedGET(t, a.Handler(), "/admin/rebuild", cookie)
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "出力先") {
		t.Errorf("page missing OutDir section")
	}
	if !strings.Contains(body, "まだ再構築されていません") {
		t.Errorf("expected 'not yet rebuilt' message; body:\n%s", body)
	}
}

func TestRebuildPOSTRequiresLogin(t *testing.T) {
	a := newTestApp(t)
	csrfCookie, token := fetchCSRF(t, a.Handler())
	body := url.Values{"csrf_token": {token}}.Encode()
	req := httptest.NewRequest("POST", "/admin/rebuild", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrfCookie)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
}

func TestRebuildPOSTWritesFilesAndRedirectsWithReport(t *testing.T) {
	// Point the rebuild output at a sandbox so the test never touches
	// the project's own data/public/ directory.
	out := filepath.Join(t.TempDir(), "public")
	t.Setenv("SB_REBUILD_OUT", out)

	a := newTestApp(t)
	cookie := login(t, a.Handler(), "admin", "changeme")

	w := authedPOSTForm(t, a.Handler(), "/admin/rebuild", url.Values{}, cookie)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303; body:\n%s", w.Code, w.Body.String())
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "status=ok") {
		t.Errorf("Location missing status=ok; got %q", loc)
	}
	if !strings.Contains(loc, "entries=2") {
		t.Errorf("Location missing entries count from seeded samples; got %q", loc)
	}

	// Output directory should actually have the home page.
	if _, err := os.Stat(filepath.Join(out, "index.html")); err != nil {
		t.Errorf("expected index.html in output dir: %v", err)
	}

	// Follow-up GET should render the parsed report without server state.
	w2 := authedGET(t, a.Handler(), loc, cookie)
	if w2.Code != 200 {
		t.Fatalf("follow-up GET status = %d", w2.Code)
	}
	if !strings.Contains(w2.Body.String(), "直前の実行結果") {
		t.Errorf("follow-up page missing report section")
	}
}
