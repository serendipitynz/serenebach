package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/serendipitynz/serenebach/internal/domain"
)

// CreateSession persists a new session and returns its db id.
func (s *Store) CreateSession(ctx context.Context, token string, userID, expiresAt int64) error {
	now := time.Now().Unix()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sessions (token, user_id, expires_at, created_at)
		VALUES (?, ?, ?, ?)`, token, userID, expiresAt, now)
	if err != nil {
		return fmt.Errorf("repo: CreateSession: %w", err)
	}
	return nil
}

// SessionUser returns the user behind a session token if the session exists
// and is not expired. ErrNotFound on miss / expiry.
func (s *Store) SessionUser(ctx context.Context, token string) (*domain.User, error) {
	var u domain.User
	var expiresAt int64
	var listVis, autoAlt int
	err := s.db.QueryRowContext(ctx, `
		SELECT u.id, u.wid, u.name, u.display_name, u.email, u.role,
		       u.description, u.description_format, u.list_visible, u.sort_order,
		       u.ai_kind, u.ai_base_url, u.ai_model, u.ai_api_key_enc, u.ai_auto_alt, u.ai_timeout_seconds,
		       s.expires_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token = ?`, token).Scan(
		&u.ID, &u.WID, &u.Name, &u.DisplayName, &u.Email, &u.Role,
		&u.Description, &u.DescriptionFormat, &listVis, &u.SortOrder,
		&u.AIKind, &u.AIBaseURL, &u.AIModel, &u.AIAPIKeyEnc, &autoAlt, &u.AITimeoutSeconds,
		&expiresAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("repo: SessionUser: %w", err)
	}
	if expiresAt <= time.Now().Unix() {
		return nil, ErrNotFound
	}
	u.ListVisible = listVis != 0
	u.AIAutoAlt = autoAlt != 0
	return &u, nil
}

// DeleteSession removes the session row for a given token (idempotent).
func (s *Store) DeleteSession(ctx context.Context, token string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token = ?`, token); err != nil {
		return fmt.Errorf("repo: DeleteSession: %w", err)
	}
	return nil
}

// DeleteExpiredSessions sweeps expired rows so the table stays small. Called
// on a cadence (e.g. from a scheduled cleanup) — safe to call eagerly.
func (s *Store) DeleteExpiredSessions(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, time.Now().Unix())
	if err != nil {
		return 0, fmt.Errorf("repo: DeleteExpiredSessions: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
