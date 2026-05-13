package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/serendipitynz/serenebach/internal/domain"
)

// messageColumns is the canonical column list for the messages table.
// Order must match the Scan argument order in scanMessages.
const messageColumns = `id, wid, entry_id, status, posted_at, author_name, author_email, author_url, body, ip_address, user_agent`

// CountMessagesByStatus returns how many comments the weblog has at the
// given status. Used to surface the moderation queue size on the dashboard.
func (s *Store) CountMessagesByStatus(ctx context.Context, wid int64, status domain.MessageStatus) (int64, error) {
	var n int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM messages WHERE wid = ? AND status = ?`, wid, status).Scan(&n); err != nil {
		return 0, fmt.Errorf("repo: CountMessagesByStatus: %w", err)
	}
	return n, nil
}

// CreateMessage inserts a new comment row and returns its id. The status is
// taken from the caller so an `open` weblog stores approved comments while
// `moderated` stores waiting ones. When the message is approved on creation,
// the entry's comments_count is bumped +1 in the same transaction.
func (s *Store) CreateMessage(ctx context.Context, m domain.Message) (int64, error) {
	now := time.Now().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("repo: CreateMessage: begin: %w", err)
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `
		INSERT INTO messages (wid, entry_id, status, posted_at, author_name, author_email, author_url, body, ip_address, user_agent, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.WID, m.EntryID, m.Status, m.PostedAt.Unix(),
		m.AuthorName, m.AuthorEmail, m.AuthorURL, m.Body,
		m.IPAddress, m.UserAgent, now, now)
	if err != nil {
		return 0, fmt.Errorf("repo: CreateMessage: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("repo: CreateMessage lastid: %w", err)
	}
	if m.Status == domain.MessageApproved {
		if _, err := tx.ExecContext(ctx, `
			UPDATE entries SET comments_count = comments_count + 1
			WHERE wid = ? AND id = ?`, m.WID, m.EntryID); err != nil {
			return 0, fmt.Errorf("repo: CreateMessage: bump: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("repo: CreateMessage: commit: %w", err)
	}
	return id, nil
}

// ApprovedMessagesByEntry returns the approved comments for an entry in
// posting order (oldest first — readers usually follow threads top-down).
func (s *Store) ApprovedMessagesByEntry(ctx context.Context, wid, entryID int64) ([]domain.Message, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+messageColumns+`
		FROM messages
		WHERE wid = ? AND entry_id = ? AND status = ?
		ORDER BY posted_at ASC, id ASC`,
		wid, entryID, domain.MessageApproved)
	if err != nil {
		return nil, fmt.Errorf("repo: ApprovedMessagesByEntry: %w", err)
	}
	defer rows.Close()
	return scanMessages(rows)
}

// ListMessagesForAdmin returns every comment (optionally filtered by status)
// newest first. Pass -99 or any non-valid MessageStatus for "no filter".
func (s *Store) ListMessagesForAdmin(ctx context.Context, wid int64, filter domain.MessageStatus, limit int) ([]domain.Message, error) {
	var (
		rows *sql.Rows
		err  error
	)
	switch filter {
	case domain.MessageWaiting, domain.MessageApproved, domain.MessageHidden:
		rows, err = s.db.QueryContext(ctx, `
			SELECT `+messageColumns+`
			FROM messages
			WHERE wid = ? AND status = ?
			ORDER BY posted_at DESC
			LIMIT ?`, wid, filter, limit)
	default:
		rows, err = s.db.QueryContext(ctx, `
			SELECT `+messageColumns+`
			FROM messages
			WHERE wid = ?
			ORDER BY posted_at DESC
			LIMIT ?`, wid, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("repo: ListMessagesForAdmin: %w", err)
	}
	defer rows.Close()
	return scanMessages(rows)
}

// UpdateMessageStatus flips a comment between waiting / approved / hidden.
// The entry's comments_count is adjusted based on the transition: approved↔
// non-approved changes bump or decrement the counter in the same transaction.
func (s *Store) UpdateMessageStatus(ctx context.Context, wid, id int64, status domain.MessageStatus) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("repo: UpdateMessageStatus: begin: %w", err)
	}
	defer tx.Rollback()
	// Read the old status so we know whether the entry counter needs adjusting.
	var oldStatus domain.MessageStatus
	var entryID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT status, entry_id FROM messages WHERE wid = ? AND id = ?`, wid, id).Scan(&oldStatus, &entryID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("repo: UpdateMessageStatus: select: %w", err)
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE messages SET status = ?, updated_at = ?
		WHERE wid = ? AND id = ?`, status, time.Now().Unix(), wid, id)
	if err != nil {
		return fmt.Errorf("repo: UpdateMessageStatus: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	// Bump or decrement the entry's comments_count depending on the transition.
	wasApproved := oldStatus == domain.MessageApproved
	nowApproved := status == domain.MessageApproved
	switch {
	case !wasApproved && nowApproved:
		if _, err := tx.ExecContext(ctx, `
			UPDATE entries SET comments_count = comments_count + 1
			WHERE wid = ? AND id = ?`, wid, entryID); err != nil {
			return fmt.Errorf("repo: UpdateMessageStatus: bump: %w", err)
		}
	case wasApproved && !nowApproved:
		if _, err := tx.ExecContext(ctx, `
			UPDATE entries SET comments_count = comments_count - 1
			WHERE wid = ? AND id = ?`, wid, entryID); err != nil {
			return fmt.Errorf("repo: UpdateMessageStatus: debump: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("repo: UpdateMessageStatus: commit: %w", err)
	}
	return nil
}

// DeleteMessage removes a comment. Used by admin hard-delete (distinct from
// the soft hide that UpdateMessageStatus(.., MessageHidden) performs).
// If the removed comment was approved, the entry's comments_count is
// decremented in the same transaction.
func (s *Store) DeleteMessage(ctx context.Context, wid, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("repo: DeleteMessage: begin: %w", err)
	}
	defer tx.Rollback()
	// Read the old status and entry_id so we can adjust the counter.
	var oldStatus domain.MessageStatus
	var entryID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT status, entry_id FROM messages WHERE wid = ? AND id = ?`, wid, id).Scan(&oldStatus, &entryID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("repo: DeleteMessage: select: %w", err)
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM messages WHERE wid = ? AND id = ?`, wid, id)
	if err != nil {
		return fmt.Errorf("repo: DeleteMessage: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	if oldStatus == domain.MessageApproved {
		if _, err := tx.ExecContext(ctx, `
			UPDATE entries SET comments_count = comments_count - 1
			WHERE wid = ? AND id = ?`, wid, entryID); err != nil {
			return fmt.Errorf("repo: DeleteMessage: debump: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("repo: DeleteMessage: commit: %w", err)
	}
	return nil
}

// HasApprovedCommentFromEmail reports whether the weblog has ever published
// an approved comment from the given email address. Used to auto-approve
// repeat commenters who have already been vetted — a lightweight "trust
// memory" so moderation doesn't burn out the admin.
func (s *Store) HasApprovedCommentFromEmail(ctx context.Context, wid int64, email string) (bool, error) {
	if email == "" {
		return false, nil
	}
	var n int
	if err := s.db.QueryRowContext(ctx, `
		SELECT 1 FROM messages
		WHERE wid = ? AND status = ? AND author_email = ?
		LIMIT 1`, wid, domain.MessageApproved, email).Scan(&n); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("repo: HasApprovedCommentFromEmail: %w", err)
	}
	return true, nil
}

// CountRecentCommentsFromIP returns how many comments the given IP posted in
// the last `since` seconds. Used as a lightweight rate-limit signal by the
// public POST handler.
func (s *Store) CountRecentCommentsFromIP(ctx context.Context, ip string, since time.Duration) (int, error) {
	cutoff := time.Now().Add(-since).Unix()
	var n int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM messages WHERE ip_address = ? AND created_at >= ?`,
		ip, cutoff).Scan(&n); err != nil {
		return 0, fmt.Errorf("repo: CountRecentCommentsFromIP: %w", err)
	}
	return n, nil
}

func scanMessages(rows *sql.Rows) ([]domain.Message, error) {
	var out []domain.Message
	for rows.Next() {
		var m domain.Message
		var postedAt int64
		if err := rows.Scan(&m.ID, &m.WID, &m.EntryID, &m.Status, &postedAt,
			&m.AuthorName, &m.AuthorEmail, &m.AuthorURL, &m.Body,
			&m.IPAddress, &m.UserAgent); err != nil {
			return nil, fmt.Errorf("repo: scan message: %w", err)
		}
		m.PostedAt = time.Unix(postedAt, 0)
		out = append(out, m)
	}
	return out, rows.Err()
}
