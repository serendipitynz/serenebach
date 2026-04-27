package app_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
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

// TestSetupConcurrentSubmitCreatesOneAdmin pins the race the gate
// must close: without serialisation, two POSTs that arrive between
// the HasAdminUser check and the Seed insert would each pass the
// check and each insert an admin row. The handler protects the
// check+seed pair with a process-local mutex, so this test fires
// several concurrent submissions with distinct usernames and
// verifies exactly one admin lands and the rest see a non-200
// response (302 to /admin/login for the winner, 404 for late
// arrivals once the gate flips).
func TestSetupConcurrentSubmitCreatesOneAdmin(t *testing.T) {
	a := newUnseededTestApp(t)

	const workers = 8
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		winners int
	)
	start := make(chan struct{})

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Each goroutine needs its own CSRF token + cookie pair —
			// the cookie value is deterministic per GET, but parallel
			// GETs make sure we exercise the POST path with no
			// inter-goroutine dependency.
			token, cookie := setupCSRFToken(t, a)
			form := url.Values{
				"csrf_token":       {token},
				"name":             {fmt.Sprintf("admin%d", i)},
				"password":         {"correcthorse"},
				"password_confirm": {"correcthorse"},
				"weblog_title":     {"Race Test"},
			}
			<-start // line every goroutine up at the gate
			req := httptest.NewRequest("POST", "/setup",
				strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.AddCookie(cookie)
			w := httptest.NewRecorder()
			a.Handler().ServeHTTP(w, req)
			if w.Code == http.StatusFound {
				mu.Lock()
				winners++
				mu.Unlock()
			}
		}(i)
	}
	close(start)
	wg.Wait()

	if winners != 1 {
		t.Errorf("redirect winners = %d, want 1", winners)
	}
	var n int
	if err := a.DB.QueryRow(`SELECT COUNT(*) FROM users WHERE role = 1`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("admin row count = %d, want 1", n)
	}
}

// TestSetupConcurrentSubmitAcrossInstancesCreatesOneAdmin pins the
// CGI cross-process race that the in-process mutex cannot close.
// Each CGI request is its own process with its own App, its own
// connection pool, and its own setupMu — so we simulate that by
// spinning up two App instances pointing at the same SQLite file
// and firing concurrent /setup POSTs through each. The atomic
// `INSERT ... WHERE NOT EXISTS` in seedAdminUser is the synchronisation
// point; SQLite serialises writes, so only one INSERT lands and the
// loser surfaces ErrAdminAlreadyExists which the Setup callback
// translates to ErrSetupAlreadyDone (404).
func TestSetupConcurrentSubmitAcrossInstancesCreatesOneAdmin(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "shared.db")
	a1 := newAppPointingAt(t, dbPath)
	a2 := newAppPointingAt(t, dbPath)

	const perInstance = 4
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		winners int
	)
	start := make(chan struct{})

	fire := func(a *app.App, namePrefix string, i int) {
		defer wg.Done()
		token, cookie := setupCSRFToken(t, a)
		form := url.Values{
			"csrf_token":       {token},
			"name":             {fmt.Sprintf("%s%d", namePrefix, i)},
			"password":         {"correcthorse"},
			"password_confirm": {"correcthorse"},
		}
		<-start
		req := httptest.NewRequest("POST", "/setup",
			strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(cookie)
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, req)
		if w.Code == http.StatusFound {
			mu.Lock()
			winners++
			mu.Unlock()
		}
	}

	for i := 0; i < perInstance; i++ {
		wg.Add(2)
		go fire(a1, "alpha", i)
		go fire(a2, "beta", i)
	}
	close(start)
	wg.Wait()

	if winners != 1 {
		t.Errorf("redirect winners across two instances = %d, want 1", winners)
	}
	var n int
	if err := a1.DB.QueryRow(`SELECT COUNT(*) FROM users WHERE role = 1`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("admin row count = %d, want 1 across two-instance race", n)
	}
}

// newAppPointingAt builds a fresh App whose SQLite file is the
// caller-supplied path. Lets multiple instances share one DB so a
// single test can exercise CGI-style cross-process behaviour.
func newAppPointingAt(t *testing.T, dbPath string) *app.App {
	t.Helper()
	cfg := &config.Config{
		Mode:                 config.ModeServer,
		Addr:                 ":0",
		DBPath:               dbPath,
		RebuildOutDir:        filepath.Join(t.TempDir(), "public"),
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
