package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/session"
	"github.com/serendipitynz/serenebach/internal/storage/repo"
)

func seedPasswordUser(t *testing.T, h *Handler, name string, role int, tokens ...string) *domain.User {
	t.Helper()
	ctx := context.Background()
	id, err := h.Store.CreateUser(ctx, domain.User{
		WID: 1, Name: name, DisplayName: name, Email: name + "@example.com", Role: role,
	}, "old-hash")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	for _, tok := range tokens {
		if err := h.Store.CreateSession(ctx, tok, id, time.Now().Add(time.Hour).Unix()); err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
	}
	u, err := h.Store.UserByID(ctx, id)
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	return u
}

func passwordForm(u *domain.User) url.Values {
	return url.Values{
		"name":             {u.Name},
		"email":            {u.Email},
		"role":             {strconv.Itoa(u.Role)},
		"password":         {"brand-new-password"},
		"password_confirm": {"brand-new-password"},
	}
}

func postAs(actor *domain.User, cookieToken, target string, form url.Values) *http.Request {
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: session.CookieName, Value: cookieToken})
	return req.WithContext(session.WithUser(req.Context(), actor))
}

func assertSessionGone(t *testing.T, h *Handler, token string) {
	t.Helper()
	if _, err := h.Store.SessionUser(context.Background(), token); !errors.Is(err, repo.ErrNotFound) {
		t.Errorf("session %q: err = %v, want ErrNotFound", token, err)
	}
}

// issuedSession returns the token the response set and checks that it
// signs in as want.
func issuedSession(t *testing.T, h *Handler, rec *httptest.ResponseRecorder, want int64) string {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == session.CookieName && c.Value != "" {
			u, err := h.Store.SessionUser(context.Background(), c.Value)
			if err != nil || u.ID != want {
				t.Fatalf("issued session = %v, %v; want user %d", u, err, want)
			}
			return c.Value
		}
	}
	t.Fatal("no replacement session cookie was set")
	return ""
}

func TestProfilePasswordChangeReplacesEverySession(t *testing.T) {
	h, _ := newAdminTestHandler(t)
	h.Sessions = session.NewManager(h.Store)
	me := seedPasswordUser(t, h, "writer", domain.RoleRegular, "this-browser", "stolen-elsewhere")

	rec := httptest.NewRecorder()
	h.profileSave(rec, postAs(me, "this-browser", "/admin/profile", passwordForm(me)))

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302; body: %s", rec.Code, rec.Body.String())
	}
	assertSessionGone(t, h, "stolen-elsewhere")
	// The current token goes too: a stolen cookie is a copy of it.
	assertSessionGone(t, h, "this-browser")
	if tok := issuedSession(t, h, rec, me.ID); tok == "this-browser" {
		t.Error("replacement reused the old token")
	}
}

func TestAdminPasswordChangeRevokesTargetSessionsOnly(t *testing.T) {
	h, _ := newAdminTestHandler(t)
	h.Sessions = session.NewManager(h.Store)
	admin := seedPasswordUser(t, h, "boss", domain.RoleAdmin, "admin-browser")
	target := seedPasswordUser(t, h, "writer", domain.RoleRegular, "writer-1", "writer-2")

	rec := httptest.NewRecorder()
	path := "/admin/users/" + strconv.FormatInt(target.ID, 10)
	h.userUpdate(rec, withChiID(postAs(admin, "admin-browser", path, passwordForm(target)), target.ID))

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302; body: %s", rec.Code, rec.Body.String())
	}
	assertSessionGone(t, h, "writer-1")
	assertSessionGone(t, h, "writer-2")
	if _, err := h.Store.SessionUser(context.Background(), "admin-browser"); err != nil {
		t.Errorf("admin's own session should survive: %v", err)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == session.CookieName {
			t.Errorf("changing another user's password set a session cookie on the admin: %v", c)
		}
	}
}

func TestAdminChangingOwnPasswordReplacesOwnSession(t *testing.T) {
	h, _ := newAdminTestHandler(t)
	h.Sessions = session.NewManager(h.Store)
	admin := seedPasswordUser(t, h, "boss", domain.RoleAdmin, "admin-browser", "admin-elsewhere")

	rec := httptest.NewRecorder()
	path := "/admin/users/" + strconv.FormatInt(admin.ID, 10)
	h.userUpdate(rec, withChiID(postAs(admin, "admin-browser", path, passwordForm(admin)), admin.ID))

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302; body: %s", rec.Code, rec.Body.String())
	}
	assertSessionGone(t, h, "admin-browser")
	assertSessionGone(t, h, "admin-elsewhere")
	issuedSession(t, h, rec, admin.ID)
}
