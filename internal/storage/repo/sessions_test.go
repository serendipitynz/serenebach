package repo

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/serendipitynz/serenebach/internal/domain"
)

// assertNoRawToken fails if any sessions row carries token verbatim — the
// leak the digest exists to prevent — or if no row carries its digest.
func assertNoRawToken(t *testing.T, s *Store, token string) {
	t.Helper()
	var raw, hashed int
	if err := s.db.QueryRowContext(context.Background(), `
		SELECT
			COALESCE(SUM(token_hash = ?), 0),
			COALESCE(SUM(token_hash = ?), 0)
		FROM sessions`, token, sessionDigest(token)).Scan(&raw, &hashed); err != nil {
		t.Fatalf("scan sessions: %v", err)
	}
	if raw != 0 {
		t.Errorf("sessions stores the cookie value %q verbatim", token)
	}
	if hashed != 1 {
		t.Errorf("rows holding the digest of %q = %d, want 1", token, hashed)
	}
}

func TestSessionsStoreDigestOnly(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	id, _ := s.CreateUser(ctx, domain.User{WID: 1, Name: "u", DisplayName: "U", Email: "u@u.com", Role: domain.RoleRegular}, "hash")
	exp := time.Now().Add(time.Hour).Unix()

	if err := s.CreateSession(ctx, "login-token", id, exp); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	assertNoRawToken(t, s, "login-token")
	if u, err := s.SessionUser(ctx, "login-token"); err != nil || u.ID != id {
		t.Fatalf("SessionUser = %v, %v; want user %d", u, err, id)
	}
	// The digest itself is not a credential.
	if _, err := s.SessionUser(ctx, sessionDigest("login-token")); !errors.Is(err, ErrNotFound) {
		t.Errorf("SessionUser(digest) err = %v, want ErrNotFound", err)
	}

	if err := s.UpdateUserPassword(ctx, id, "new", &ReplacementSession{Token: "replacement-token", ExpiresAt: exp}); err != nil {
		t.Fatalf("UpdateUserPassword: %v", err)
	}
	assertNoRawToken(t, s, "replacement-token")
	if u, err := s.SessionUser(ctx, "replacement-token"); err != nil || u.ID != id {
		t.Fatalf("SessionUser(replacement) = %v, %v; want user %d", u, err, id)
	}

	if err := s.DeleteSession(ctx, "replacement-token"); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, err := s.SessionUser(ctx, "replacement-token"); !errors.Is(err, ErrNotFound) {
		t.Errorf("SessionUser after logout err = %v, want ErrNotFound", err)
	}
}

func TestSessionsExpireByDigest(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	id, _ := s.CreateUser(ctx, domain.User{WID: 1, Name: "u", DisplayName: "U", Email: "u@u.com", Role: domain.RoleRegular}, "hash")

	if err := s.CreateSession(ctx, "stale", id, time.Now().Add(-time.Minute).Unix()); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession(ctx, "fresh", id, time.Now().Add(time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionUser(ctx, "stale"); !errors.Is(err, ErrNotFound) {
		t.Errorf("expired session err = %v, want ErrNotFound", err)
	}
	if n, err := s.DeleteExpiredSessions(ctx); err != nil || n != 1 {
		t.Errorf("DeleteExpiredSessions = %d, %v; want 1", n, err)
	}
	if _, err := s.SessionUser(ctx, "fresh"); err != nil {
		t.Errorf("unexpired session lost in the sweep: %v", err)
	}
}
