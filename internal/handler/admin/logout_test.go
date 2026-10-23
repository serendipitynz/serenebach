package admin

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/serendipitynz/serenebach/internal/session"
)

// logoutWithSession signs a session in, runs logout with its cookie, and
// returns the response.
func logoutWithSession(t *testing.T, h *Handler) *httptest.ResponseRecorder {
	t.Helper()
	login := httptest.NewRecorder()
	if _, err := h.Sessions.Create(context.Background(), login, httptest.NewRequest(http.MethodPost, "/admin/login", nil), 1); err != nil {
		t.Fatalf("Create session: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/logout", nil)
	for _, c := range login.Result().Cookies() {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.logout(rec, req)
	return rec
}

func assertSessionCookieCleared(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == session.CookieName {
			if c.MaxAge >= 0 || c.Value != "" {
				t.Errorf("session cookie not cleared: %+v", c)
			}
			return
		}
	}
	t.Errorf("no Set-Cookie for %s; want it cleared", session.CookieName)
}

func countSessions(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestLogoutClearsCookieAndSessionRow(t *testing.T) {
	h, db := newAdminTestHandler(t)
	h.Sessions = session.NewManager(h.Store)

	rec := logoutWithSession(t, h)

	if loc := rec.Header().Get("Location"); !strings.HasSuffix(loc, "/admin/login") {
		t.Errorf("Location = %q, want /admin/login", loc)
	}
	assertSessionCookieCleared(t, rec)
	if n := countSessions(t, db); n != 0 {
		t.Errorf("sessions rows = %d, want 0", n)
	}
}

func TestLogoutClearsCookieAndWarnsWhenSessionDeleteFails(t *testing.T) {
	h, db := newAdminTestHandler(t)
	h.Sessions = session.NewManager(h.Store)
	// Stand-in for SQLITE_FULL: the disk-full case cannot be reproduced
	// against an in-memory DB, but any DELETE failure takes the same path.
	if _, err := db.Exec(`CREATE TRIGGER sessions_delete_fails BEFORE DELETE ON sessions
		BEGIN SELECT RAISE(ABORT, 'database or disk is full'); END`); err != nil {
		t.Fatal(err)
	}

	rec := logoutWithSession(t, h)

	loc := rec.Header().Get("Location")
	if !strings.HasSuffix(loc, "/admin/login?logout=incomplete") {
		t.Errorf("Location = %q, want /admin/login?logout=incomplete", loc)
	}
	assertSessionCookieCleared(t, rec)
	if n := countSessions(t, db); n != 1 {
		t.Errorf("sessions rows = %d, want the undeleted row to remain", n)
	}

	req := httptest.NewRequest(http.MethodGet, loc, nil)
	form := httptest.NewRecorder()
	h.loginForm(form, req)
	if form.Code != http.StatusOK {
		t.Fatalf("login form status = %d, want 200", form.Code)
	}
	if want := tr(req, "login.warnLogoutIncomplete"); !strings.Contains(form.Body.String(), want) {
		t.Errorf("login form missing the incomplete-logout warning %q", want)
	}
}
