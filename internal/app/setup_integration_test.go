package app_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/serendipitynz/serenebach/internal/app"
	"github.com/serendipitynz/serenebach/internal/auth"
	"github.com/serendipitynz/serenebach/internal/config"
	"github.com/serendipitynz/serenebach/internal/csrf"
)

// newUnseededTestApp builds a fresh app exactly like newTestApp but
// skips the initial Seed so the first-run setup gate is exercised.
func newUnseededTestApp(t *testing.T) *app.App {
	t.Helper()
	rebuildOut := os.Getenv("SB_REBUILD_OUT")
	if rebuildOut == "" {
		rebuildOut = filepath.Join(t.TempDir(), "public")
	}
	cfg := &config.Config{
		Mode:                 config.ModeServer,
		Addr:                 ":0",
		DBPath:               filepath.Join(t.TempDir(), "test.db"),
		RebuildOutDir:        rebuildOut,
		ImageDir:             filepath.Join(t.TempDir(), "img"),
		TemplateDir:          filepath.Join(t.TempDir(), "templates"),
		UploadMaxBytes:       10 << 20,
		PublicAllowedOrigins: []string{"http://example.com"},
	}
	a, err := app.New(cfg)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a
}

// TestSetupGateRedirectsHomeWhenNoAdmin verifies that a fresh install
// (no users yet) bounces the home page to /setup, so the operator
// always lands on the install screen.
func TestSetupGateRedirectsHomeWhenNoAdmin(t *testing.T) {
	a := newUnseededTestApp(t)

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/setup" {
		t.Errorf("Location = %q, want /setup", loc)
	}
}

// TestSetupFormRendersWhenNoAdmin checks the GET /setup happy path —
// form fields visible, CSRF token embedded.
func TestSetupFormRendersWhenNoAdmin(t *testing.T) {
	a := newUnseededTestApp(t)

	req := httptest.NewRequest("GET", "/setup", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{
		`name="csrf_token"`,
		`name="name"`,
		`name="password"`,
		`name="password_confirm"`,
		`name="weblog_title"`,
		`name="sample_entries"`,
		`action="/setup"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("setup body missing %q", want)
		}
	}
}

// TestSetupSubmitCreatesAdminAndRedirects walks the full happy path:
// pull a CSRF cookie+token off GET /setup, POST the form with valid
// credentials, expect a redirect to /admin/login, and verify the user
// row plus password hash are correct.
func TestSetupSubmitCreatesAdminAndRedirects(t *testing.T) {
	a := newUnseededTestApp(t)

	token, cookie := setupCSRFToken(t, a)

	form := url.Values{
		"csrf_token":       {token},
		"name":             {"admin"},
		"password":         {"correcthorse"},
		"password_confirm": {"correcthorse"},
		"email":            {"admin@example.com"},
		"weblog_title":     {"My Blog"},
		"sample_entries":   {"1"},
	}
	req := httptest.NewRequest("POST", "/setup", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302; body=%s", w.Code, w.Body.String())
	}
	if loc := w.Header().Get("Location"); loc != "/admin/login" {
		t.Errorf("Location = %q, want /admin/login", loc)
	}

	var name, hash, email string
	var role int
	row := a.DB.QueryRowContext(context.Background(),
		`SELECT name, password_hash, email, role FROM users WHERE name = ?`, "admin")
	if err := row.Scan(&name, &hash, &email, &role); err != nil {
		t.Fatalf("admin row missing: %v", err)
	}
	if role != 1 {
		t.Errorf("role = %d, want 1 (admin)", role)
	}
	if email != "admin@example.com" {
		t.Errorf("email = %q, want admin@example.com", email)
	}
	if err := auth.VerifyPassword(hash, "correcthorse"); err != nil {
		t.Errorf("password hash does not verify: %v", err)
	}

	// Sample entries flag was on, so seedSampleContent should have run.
	var entryCount int
	if err := a.DB.QueryRow(`SELECT COUNT(*) FROM entries`).Scan(&entryCount); err != nil {
		t.Fatal(err)
	}
	if entryCount == 0 {
		t.Errorf("sample_entries=1 but no entries inserted")
	}
}

// TestSetupSubmitWithoutSampleEntries pins the checkbox-off path:
// admin is created, demo content is *not*.
func TestSetupSubmitWithoutSampleEntries(t *testing.T) {
	a := newUnseededTestApp(t)

	token, cookie := setupCSRFToken(t, a)

	form := url.Values{
		"csrf_token":       {token},
		"name":             {"admin"},
		"password":         {"correcthorse"},
		"password_confirm": {"correcthorse"},
		"weblog_title":     {"My Blog"},
		// sample_entries deliberately absent
	}
	req := httptest.NewRequest("POST", "/setup", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302; body=%s", w.Code, w.Body.String())
	}
	var entryCount int
	if err := a.DB.QueryRow(`SELECT COUNT(*) FROM entries`).Scan(&entryCount); err != nil {
		t.Fatal(err)
	}
	if entryCount != 0 {
		t.Errorf("entries = %d, want 0 when sample_entries is unchecked", entryCount)
	}
}

// TestSetupReturns404OnceAdminExists verifies the gate flips off and
// /setup is invisible after the install completes.
func TestSetupReturns404OnceAdminExists(t *testing.T) {
	a := newTestApp(t) // newTestApp seeds an admin

	req := httptest.NewRequest("GET", "/setup", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

// TestSetupMismatchedPasswordRendersError walks one validation branch
// to make sure errors stay on the form (no admin created, no redirect).
func TestSetupMismatchedPasswordRendersError(t *testing.T) {
	a := newUnseededTestApp(t)

	token, cookie := setupCSRFToken(t, a)
	form := url.Values{
		"csrf_token":       {token},
		"name":             {"admin"},
		"password":         {"correcthorse"},
		"password_confirm": {"different"},
	}
	req := httptest.NewRequest("POST", "/setup", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200 (re-rendered form)", w.Code)
	}
	var n int
	if err := a.DB.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("user count = %d, want 0; mismatched-pw POST must not create admin", n)
	}
}

// setupCSRFToken hits GET /setup, scrapes the csrf_token from the
// rendered form, and returns it along with the sb_csrf cookie that
// must accompany the matching POST.
func setupCSRFToken(t *testing.T, a *app.App) (string, *http.Cookie) {
	t.Helper()
	req := httptest.NewRequest("GET", "/setup", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("setupCSRFToken: GET /setup status = %d", w.Code)
	}
	m := regexp.MustCompile(`name="csrf_token" value="([^"]+)"`).FindStringSubmatch(w.Body.String())
	if len(m) < 2 {
		t.Fatalf("csrf_token not found in /setup body")
	}
	c := findCookie(w.Result().Cookies(), csrf.CookieName)
	if c == nil {
		t.Fatalf("sb_csrf cookie not set on GET /setup")
	}
	return m[1], c
}
