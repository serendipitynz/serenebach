package repo

import (
	"context"
	"fmt"
	"time"

	"github.com/serendipitynz/serenebach/internal/domain"
)

// ---- likes --------------------------------------------------------------

// LikeEntry atomically records a like from the given fingerprint and bumps
// the denormalised counter. Returns `true` when the like was new (and the
// counter actually advanced), `false` when this fingerprint had already
// liked the entry — the caller uses this to decide whether to set a
// "already liked" cookie on the browser.
func (s *Store) LikeEntry(ctx context.Context, entryID int64, fingerprint string) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("repo: LikeEntry begin: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	res, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO entry_likes (entry_id, fingerprint, created_at)
		VALUES (?, ?, ?)`, entryID, fingerprint, time.Now().Unix())
	if err != nil {
		return false, fmt.Errorf("repo: LikeEntry insert: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		// Fingerprint already on file — commit the read-only tx and tell the
		// caller nothing changed.
		if err := tx.Commit(); err != nil {
			return false, fmt.Errorf("repo: LikeEntry commit: %w", err)
		}
		tx = nil
		return false, nil
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE entries SET likes_count = likes_count + 1 WHERE id = ?`, entryID); err != nil {
		return false, fmt.Errorf("repo: LikeEntry bump: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("repo: LikeEntry commit: %w", err)
	}
	tx = nil
	return true, nil
}

// ---- stamps -------------------------------------------------------------

// StampEntry atomically records a stamp of the given kind from the
// supplied fingerprint and bumps the denormalised total counter.
// Returns true when the stamp was new (counter moved); false when the
// same (entry, kind, fingerprint) triple was already on file. Mirrors
// LikeEntry's contract so the HTTP handler can decide whether to set
// an "already reacted" cookie.
func (s *Store) StampEntry(ctx context.Context, entryID int64, kind domain.StampKind, fingerprint string) (bool, error) {
	if !kind.Valid() {
		return false, fmt.Errorf("repo: StampEntry: invalid kind %q", kind)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("repo: StampEntry begin: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	res, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO entry_stamps (entry_id, stamp_kind, fingerprint, created_at)
		VALUES (?, ?, ?, ?)`, entryID, string(kind), fingerprint, time.Now().Unix())
	if err != nil {
		return false, fmt.Errorf("repo: StampEntry insert: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		if err := tx.Commit(); err != nil {
			return false, fmt.Errorf("repo: StampEntry commit: %w", err)
		}
		tx = nil
		return false, nil
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE entries SET stamps_count = stamps_count + 1 WHERE id = ?`, entryID); err != nil {
		return false, fmt.Errorf("repo: StampEntry bump: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("repo: StampEntry commit: %w", err)
	}
	tx = nil
	return true, nil
}

// StampCountsByEntry returns a kind → count map for the given entry,
// covering every kind even when zero so callers can render a uniform
// set of reaction buttons.
func (s *Store) StampCountsByEntry(ctx context.Context, entryID int64) (map[domain.StampKind]int64, error) {
	out := make(map[domain.StampKind]int64, len(domain.StampKinds))
	for _, k := range domain.StampKinds {
		out[k] = 0
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT stamp_kind, COUNT(*) FROM entry_stamps
		WHERE entry_id = ? GROUP BY stamp_kind`, entryID)
	if err != nil {
		return nil, fmt.Errorf("repo: StampCountsByEntry: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var kind string
		var count int64
		if err := rows.Scan(&kind, &count); err != nil {
			return nil, fmt.Errorf("repo: scan stamp count: %w", err)
		}
		out[domain.StampKind(kind)] = count
	}
	return out, rows.Err()
}
