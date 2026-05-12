// Package session handles admin session issuance and lookup. Sessions are
// stored in SQLite (see migrations/0001_init.sql) and carried by an opaque
// HttpOnly cookie. CSRF is currently delegated to SameSite=Lax; a token-based
// layer can be added later without changing this API.
package session

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/storage/repo"
)

const (
	CookieName    = "sb_session"
	DefaultMaxAge = 7 * 24 * time.Hour
)

// Manager wires the session store to the cookie-handling logic. One is
// constructed at app boot and shared by middleware and handlers.
type Manager struct {
	Store  *repo.Store
	MaxAge time.Duration
	Secure bool // force Secure on cookies even when request.TLS is nil (behind TLS proxy)
}

func NewManager(store *repo.Store) *Manager {
	return &Manager{Store: store, MaxAge: DefaultMaxAge}
}

// Create authenticates a new session for userID, writes the Set-Cookie
// header onto w, and returns the raw token in case the caller wants to log
// or surface it (it should not, normally).
func (m *Manager) Create(ctx context.Context, w http.ResponseWriter, r *http.Request, userID int64) (string, error) {
	token, err := newToken()
	if err != nil {
		return "", err
	}
	expires := time.Now().Add(m.MaxAge)
	if err := m.Store.CreateSession(ctx, token, userID, expires.Unix()); err != nil {
		return "", err
	}
	http.SetCookie(w, m.cookie(r, token, expires))
	return token, nil
}

// Destroy revokes the session identified by the cookie on r and clears the
// cookie on w. Missing or already-destroyed sessions are silently ignored.
func (m *Manager) Destroy(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	c, err := r.Cookie(CookieName)
	if err != nil {
		return nil //nolint:nilerr // missing cookie is the expected "no session to destroy" path.
	}
	if err := m.Store.DeleteSession(ctx, c.Value); err != nil {
		return err
	}
	http.SetCookie(w, m.clearCookie(r))
	return nil
}

// UserFromRequest returns the user tied to the request's session cookie,
// or nil if no valid session is present.
func (m *Manager) UserFromRequest(r *http.Request) *domain.User {
	c, err := r.Cookie(CookieName)
	if err != nil {
		return nil
	}
	u, err := m.Store.SessionUser(r.Context(), c.Value)
	if err != nil {
		if !errors.Is(err, repo.ErrNotFound) {
			// Treat every lookup failure as "not authenticated" to avoid leaking
			// row state to an attacker via error responses, but record server-side
			// since the caller's interface (*domain.User) cannot surface the error.
			log.Printf("session.UserFromRequest: lookup: %v", err)
		}
		return nil
	}
	return u
}

func (m *Manager) cookie(r *http.Request, token string, expires time.Time) *http.Cookie {
	return &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(time.Until(expires).Seconds()),
		HttpOnly: true,
		Secure:   m.secureFor(r),
		SameSite: http.SameSiteLaxMode,
	}
}

func (m *Manager) clearCookie(r *http.Request) *http.Cookie {
	return &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   m.secureFor(r),
		SameSite: http.SameSiteLaxMode,
	}
}

func (m *Manager) secureFor(r *http.Request) bool {
	if m.Secure {
		return true
	}
	if r.TLS != nil {
		return true
	}
	// Honour a reverse-proxy terminating TLS.
	if r.Header.Get("X-Forwarded-Proto") == "https" {
		return true
	}
	return false
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
