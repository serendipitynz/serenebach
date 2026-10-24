package repo

import (
	"context"
	"fmt"
)

// LoginFailuresSince counts the failures recorded under key after since
// (unix seconds) and returns the oldest of them, 0 when there are none.
func (s *Store) LoginFailuresSince(ctx context.Context, key string, since int64) (count int, oldest int64, err error) {
	err = s.db.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(MIN(failed_at), 0)
		FROM login_failures WHERE key_hash = ? AND failed_at > ?`, key, since).Scan(&count, &oldest)
	if err != nil {
		return 0, 0, fmt.Errorf("repo: LoginFailuresSince: %w", err)
	}
	return count, oldest, nil
}

// RecordLoginFailure stores one failure under key at the given time and,
// in the same transaction, drops every failure at or before pruneBefore —
// for all keys, so the table stays bounded without a separate sweep.
func (s *Store) RecordLoginFailure(ctx context.Context, key string, at, pruneBefore int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("repo: RecordLoginFailure begin: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	if _, err := tx.ExecContext(ctx, `DELETE FROM login_failures WHERE failed_at <= ?`, pruneBefore); err != nil {
		return fmt.Errorf("repo: RecordLoginFailure prune: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO login_failures (key_hash, failed_at) VALUES (?, ?)`, key, at); err != nil {
		return fmt.Errorf("repo: RecordLoginFailure: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("repo: RecordLoginFailure commit: %w", err)
	}
	tx = nil
	return nil
}

// Idempotent: a key with no failures is not an error.
func (s *Store) ClearLoginFailures(ctx context.Context, key string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM login_failures WHERE key_hash = ?`, key); err != nil {
		return fmt.Errorf("repo: ClearLoginFailures: %w", err)
	}
	return nil
}
