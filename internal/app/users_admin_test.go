package app_test

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/serendipitynz/serenebach/internal/auth"
)

// TestAdminUsersListRejectsNonAdmin confirms a power-tier session
// cannot reach /admin/users even if it guesses the URL.
func TestAdminUsersListRejectsNonAdmin(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	// Create a power-tier user and log in as them.
	hash, _ := auth.HashPassword("secret")
	if _, err := a.DB.Exec(`INSERT INTO users (wid, name, display_name, email, password_hash, role, created_at, updated_at, list_visible, description_format)
		VALUES (1, 'powerguy', 'Power Guy', '', ?, 2, strftime('%s','now'), strftime('%s','now'), 1, 'html')`, hash); err != nil {
		t.Fatal(err)
	}
	cookies := login(t, a.Handler(), "powerguy", "secret")

	w := authedGET(t, a.Handler(), "/admin/users", cookies)
	if w.Code != 403 {
		t.Errorf("status = %d, want 403", w.Code)
	}
}

// TestAdminUsersListShowsSeededAdmin sanity-checks the list renders
// for an admin session, includes the seeded admin row, and links to
// the separate /admin/users/new form (the inline create form was
// moved out to match /admin/categories).
func TestAdminUsersListShowsSeededAdmin(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	w := authedGET(t, a.Handler(), "/admin/users", cookies)
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{
		`href="/admin/users/new"`,
		`新規ユーザー →`,
		`>admin<`,
		`管理ユーザー`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	// Inline create-form fields should no longer live on the list
	// page — they're on /admin/users/new now.
	if strings.Contains(body, `name="password_confirm"`) {
		t.Errorf("inline create form still present on list page")
	}
}

// TestAdminUserNewFormRenders confirms the dedicated /admin/users/new
// page carries the create fields the list page no longer hosts.
func TestAdminUserNewFormRenders(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	w := authedGET(t, a.Handler(), "/admin/users/new", cookies)
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{
		`新規ユーザー`,
		`action="/admin/users/new"`,
		`name="display_name"`,
		`name="password_confirm"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
}

// TestAdminUserCreateHappyPath creates a regular-tier user and confirms
// it lands in the DB with the hashed password + role wired correctly.
func TestAdminUserCreateHappyPath(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	csrfCookie, token := fetchCSRF(t, a.Handler())

	form := url.Values{
		"csrf_token":       {token},
		"name":             {"alice"},
		"display_name":     {"Alice"},
		"password":         {"pa55word"},
		"password_confirm": {"pa55word"},
		"email":            {"alice@example.com"},
		"role":             {"3"},
	}
	req := httptest.NewRequest("POST", "/admin/users/new", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrfCookie)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != 302 {
		t.Fatalf("status = %d", w.Code)
	}

	var role int
	var hash string
	if err := a.DB.QueryRow(`SELECT role, password_hash FROM users WHERE name = ?`, "alice").Scan(&role, &hash); err != nil {
		t.Fatal(err)
	}
	if role != 3 {
		t.Errorf("role = %d, want 3 (regular)", role)
	}
	// Hash should verify against the original plaintext.
	if err := auth.VerifyPassword(hash, "pa55word"); err != nil {
		t.Errorf("password verify failed: %v", err)
	}
}

// TestAdminUserCreateMismatchedPasswords confirms the confirm-field
// check catches typos before anything lands in the DB.
func TestAdminUserCreateMismatchedPasswords(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	csrfCookie, token := fetchCSRF(t, a.Handler())
	form := url.Values{
		"csrf_token":       {token},
		"name":             {"bob"},
		"password":         {"aaa"},
		"password_confirm": {"bbb"},
		"role":             {"3"},
	}
	req := httptest.NewRequest("POST", "/admin/users/new", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrfCookie)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status = %d, want 200 (stay on form)", w.Code)
	}
	if !strings.Contains(w.Body.String(), "一致しません") {
		t.Errorf("missing mismatch message")
	}
	var n int
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM users WHERE name = 'bob'`).Scan(&n)
	if n != 0 {
		t.Errorf("bob row written despite validation failure")
	}
}

// TestAdminUserDeleteRefusesLastAdmin confirms the last-admin guard —
// deleting the only admin would lock the site out of user management.
func TestAdminUserDeleteRefusesLastAdmin(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	csrfCookie, token := fetchCSRF(t, a.Handler())

	// Admin deleting their own row — also guarded by the self-delete
	// check, but this confirms the redirect message is "cannot-delete-self"
	// (a distinct flash) and no row was removed.
	var adminID int64
	if err := a.DB.QueryRow(`SELECT id FROM users WHERE name = 'admin'`).Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	form := url.Values{"csrf_token": {token}}.Encode()
	req := httptest.NewRequest("POST", "/admin/users/"+itoa64str(adminID)+"/delete", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrfCookie)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != 302 {
		t.Fatalf("status = %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "cannot-delete-self") && !strings.Contains(loc, "cannot-delete-last-admin") {
		t.Errorf("expected guard redirect, got Location=%q", loc)
	}
	var n int
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM users WHERE id = ?`, adminID).Scan(&n)
	if n != 1 {
		t.Errorf("admin row deleted despite guard")
	}
}

// TestAdminCannotDemoteSelf — even with a second admin present (so
// the last-admin guard wouldn't kick in), an admin editing their own
// row can't lower their role. Handler silently pins role to
// RoleAdmin; form template also disables the select client-side.
func TestAdminCannotDemoteSelf(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	// Seed a second admin so the last-admin safeguard doesn't interfere.
	hash, _ := auth.HashPassword("secret")
	if _, err := a.DB.Exec(`INSERT INTO users (wid, name, display_name, email, password_hash, role, created_at, updated_at, list_visible, description_format)
		VALUES (1, 'admin2', 'Admin Two', '', ?, 1, strftime('%s','now'), strftime('%s','now'), 1, 'html')`, hash); err != nil {
		t.Fatal(err)
	}
	cookies := login(t, a.Handler(), "admin", "changeme")

	var selfID int64
	if err := a.DB.QueryRow(`SELECT id FROM users WHERE name = 'admin'`).Scan(&selfID); err != nil {
		t.Fatal(err)
	}

	// Attempt to change own role to 一般 (3). Form reload would disable
	// the select, but a tampered POST shouldn't go through either.
	csrfCookie, token := fetchCSRF(t, a.Handler())
	form := url.Values{
		"csrf_token":   {token},
		"name":         {"admin"},
		"display_name": {"admin"},
		"email":        {""},
		"role":         {"3"},
	}
	req := httptest.NewRequest("POST", "/admin/users/"+itoa64str(selfID)+"/edit", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrfCookie)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != 302 {
		t.Fatalf("status = %d, want 302 (save succeeds, role silently pinned)", w.Code)
	}

	var role int
	if err := a.DB.QueryRow(`SELECT role FROM users WHERE id = ?`, selfID).Scan(&role); err != nil {
		t.Fatal(err)
	}
	if role != 1 {
		t.Errorf("self role demoted to %d despite self-guard", role)
	}

	// The edit form must render the role select as disabled so the
	// UI communicates the block.
	w = authedGET(t, a.Handler(), "/admin/users/"+itoa64str(selfID)+"/edit", cookies)
	if !strings.Contains(w.Body.String(), `<select name="role" disabled>`) {
		t.Errorf("expected role select to render as disabled on self-edit\nbody: %s", w.Body.String())
	}
}

// TestAdminCanDemoteOtherAdmin sanity-checks that the self-guard is
// self-only — an admin can still change another admin's role (as long
// as at least one admin remains).
func TestAdminCanDemoteOtherAdmin(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	hash, _ := auth.HashPassword("secret")
	if _, err := a.DB.Exec(`INSERT INTO users (wid, name, display_name, email, password_hash, role, created_at, updated_at, list_visible, description_format)
		VALUES (1, 'admin2', 'Admin Two', '', ?, 1, strftime('%s','now'), strftime('%s','now'), 1, 'html')`, hash); err != nil {
		t.Fatal(err)
	}
	cookies := login(t, a.Handler(), "admin", "changeme")

	var targetID int64
	if err := a.DB.QueryRow(`SELECT id FROM users WHERE name = 'admin2'`).Scan(&targetID); err != nil {
		t.Fatal(err)
	}
	csrfCookie, token := fetchCSRF(t, a.Handler())
	form := url.Values{
		"csrf_token":   {token},
		"name":         {"admin2"},
		"display_name": {"Admin Two"},
		"email":        {""},
		"role":         {"2"},
	}
	req := httptest.NewRequest("POST", "/admin/users/"+itoa64str(targetID)+"/edit", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrfCookie)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != 302 {
		t.Fatalf("status = %d", w.Code)
	}
	var role int
	_ = a.DB.QueryRow(`SELECT role FROM users WHERE id = ?`, targetID).Scan(&role)
	if role != 2 {
		t.Errorf("other admin's role = %d, want 2 (power)", role)
	}
}

// TestRegularUserMenuHidesDesignAndUsersLinks verifies a regular-tier
// session's admin home renders with the locked menu items removed.
func TestRegularUserMenuHidesDesignAndUsersLinks(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	hash, _ := auth.HashPassword("secret")
	if _, err := a.DB.Exec(`INSERT INTO users (wid, name, display_name, email, password_hash, role, created_at, updated_at, list_visible, description_format)
		VALUES (1, 'regular', 'Reg', '', ?, 3, strftime('%s','now'), strftime('%s','now'), 1, 'html')`, hash); err != nil {
		t.Fatal(err)
	}
	cookies := login(t, a.Handler(), "regular", "secret")
	w := authedGET(t, a.Handler(), "/admin/", cookies)
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	for _, forbidden := range []string{
		`href="/admin/categories"`,
		`href="/admin/templates/active/edit"`,
		`href="/admin/templates"`,
		`href="/admin/users"`,
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("regular-tier menu still shows %q", forbidden)
		}
	}
	// Sanity: allowed links are still present.
	for _, allowed := range []string{
		`href="/admin/entries"`,
		`href="/admin/images"`,
		`href="/admin/comments"`,
	} {
		if !strings.Contains(body, allowed) {
			t.Errorf("regular-tier menu missing %q", allowed)
		}
	}
}
