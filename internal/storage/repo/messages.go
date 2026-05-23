package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/serendipitynz/serenebach/internal/domain"
)

// messageColumns is the canonical column list for the messages table.
// Order must match the Scan argument order in scanMessages.
const messageColumns = `id, wid, entry_id, status, posted_at, author_name, author_email, author_url, body, ip_address, user_agent`

// messageColumnsM is messageColumns qualified with the `m.` alias for
// the admin list query (used so additional WHERE clauses can reference
// the alias unambiguously).
const messageColumnsM = `m.id, m.wid, m.entry_id, m.status, m.posted_at, m.author_name, m.author_email, m.author_url, m.body, m.ip_address, m.user_agent`

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

// ApprovedMessagesByEntries batches ApprovedMessagesByEntry across many
// entries so the full-site rebuild can fetch comments for every entry
// with a bounded number of queries. The map always has an entry for every
// input id (empty slice when none approved).
//
// Requests are chunked to stay under SQLite's bind-variable limit
// (32766), accounting for the two fixed placeholders (wid, status).
func (s *Store) ApprovedMessagesByEntries(ctx context.Context, wid int64, entryIDs []int64) (map[int64][]domain.Message, error) {
	out := make(map[int64][]domain.Message, len(entryIDs))
	for _, id := range entryIDs {
		out[id] = []domain.Message{}
	}
	if len(entryIDs) == 0 {
		return out, nil
	}

	const maxVars = 32766 // SQLite bind-variable limit; wid + status consume 2
	for start := 0; start < len(entryIDs); start += maxVars - 2 {
		end := start + maxVars - 2
		if end > len(entryIDs) {
			end = len(entryIDs)
		}
		chunk := entryIDs[start:end]

		placeholders := strings.Repeat("?,", len(chunk))
		placeholders = placeholders[:len(placeholders)-1]
		args := make([]interface{}, 0, len(chunk)+2)
		args = append(args, wid, domain.MessageApproved)
		for _, id := range chunk {
			args = append(args, id)
		}

		rows, err := s.db.QueryContext(ctx, `
			SELECT `+messageColumns+`
			FROM messages
			WHERE wid = ? AND status = ? AND entry_id IN (`+placeholders+`)
			ORDER BY entry_id ASC, posted_at ASC, id ASC`, args...)
		if err != nil {
			return nil, fmt.Errorf("repo: ApprovedMessagesByEntries: %w", err)
		}
		for rows.Next() {
			var m domain.Message
			var postedAt int64
			if err := rows.Scan(&m.ID, &m.WID, &m.EntryID, &m.Status, &postedAt,
				&m.AuthorName, &m.AuthorEmail, &m.AuthorURL, &m.Body,
				&m.IPAddress, &m.UserAgent); err != nil {
				rows.Close()
				return nil, fmt.Errorf("repo: scan message row: %w", err)
			}
			m.PostedAt = time.Unix(postedAt, 0)
			out[m.EntryID] = append(out[m.EntryID], m)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("repo: ApprovedMessagesByEntries: %w", err)
		}
		rows.Close()
	}

	return out, nil
}

// MessageSortKey is a typed enum of the columns the admin comment list
// can sort by. Default is posted_at DESC — the moderation queue's
// natural order.
type MessageSortKey int

const (
	MessageSortPostedAt MessageSortKey = iota // default
	MessageSortID
	MessageSortAuthor
	MessageSortStatus
	MessageSortEntry
	MessageSortBody
)

func (k MessageSortKey) orderClause() string {
	switch k {
	case MessageSortID:
		return "m.id"
	case MessageSortAuthor:
		return "m.author_name"
	case MessageSortStatus:
		return "m.status"
	case MessageSortEntry:
		return "m.entry_id"
	case MessageSortBody:
		return "m.body"
	default:
		return "m.posted_at"
	}
}

func (k MessageSortKey) String() string {
	switch k {
	case MessageSortID:
		return "id"
	case MessageSortAuthor:
		return "author"
	case MessageSortStatus:
		return "status"
	case MessageSortEntry:
		return "entry"
	case MessageSortBody:
		return "body"
	default:
		return "posted"
	}
}

// ParseMessageSortKey maps a ?sort= query value to the enum.
func ParseMessageSortKey(s string) MessageSortKey {
	switch s {
	case "id":
		return MessageSortID
	case "author":
		return MessageSortAuthor
	case "status":
		return MessageSortStatus
	case "entry":
		return MessageSortEntry
	case "body":
		return MessageSortBody
	default:
		return MessageSortPostedAt
	}
}

// ListMessagesQuery bundles the admin comment list's filter / search /
// sort / paging parameters. Filter is a pointer so the zero-value
// query (every field zero) means "no filter" — needed because
// MessageWaiting's underlying int is 0, which would otherwise collide
// with the natural "unset" default.
type ListMessagesQuery struct {
	// Filter, when non-nil, restricts to one MessageStatus (waiting /
	// approved / hidden). nil = no status filter (all rows).
	Filter  *domain.MessageStatus
	Search  string // matches author_name, body, author_email, ip_address
	SortBy  MessageSortKey
	SortDir SortDir
	Limit   int
	Offset  int
}

// ListMessagesForAdmin returns comments matching q. The Filter field
// is interpreted with the same sentinel rules as the previous
// signature (anything outside waiting/approved/hidden = no filter).
func (s *Store) ListMessagesForAdmin(ctx context.Context, wid int64, q ListMessagesQuery) ([]domain.Message, error) {
	sqlText, args := buildMessagesListSQL(wid, q)
	rows, err := s.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, fmt.Errorf("repo: ListMessagesForAdmin: %w", err)
	}
	defer rows.Close()
	return scanMessages(rows)
}

// CountMessagesForAdmin returns how many rows ListMessagesForAdmin
// would produce ignoring Limit / Offset.
func (s *Store) CountMessagesForAdmin(ctx context.Context, wid int64, q ListMessagesQuery) (int64, error) {
	sqlText, args := buildMessagesCountSQL(wid, q)
	var n int64
	if err := s.db.QueryRowContext(ctx, sqlText, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("repo: CountMessagesForAdmin: %w", err)
	}
	return n, nil
}

func buildMessagesListSQL(wid int64, q ListMessagesQuery) (string, []any) {
	var b strings.Builder
	b.WriteString(`SELECT ` + messageColumnsM + `
		FROM messages m
		WHERE m.wid = ?`)
	args := []any{wid}
	appendMessagesFilters(&b, &args, q)
	b.WriteString(` ORDER BY `)
	b.WriteString(q.SortBy.orderClause())
	b.WriteByte(' ')
	b.WriteString(q.SortDir.String())
	b.WriteString(`, m.id DESC`)
	if q.Limit > 0 {
		b.WriteString(` LIMIT ?`)
		args = append(args, q.Limit)
		if q.Offset > 0 {
			b.WriteString(` OFFSET ?`)
			args = append(args, q.Offset)
		}
	}
	return b.String(), args
}

func buildMessagesCountSQL(wid int64, q ListMessagesQuery) (string, []any) {
	var b strings.Builder
	b.WriteString(`SELECT COUNT(*) FROM messages m WHERE m.wid = ?`)
	args := []any{wid}
	appendMessagesFilters(&b, &args, q)
	return b.String(), args
}

func appendMessagesFilters(b *strings.Builder, args *[]any, q ListMessagesQuery) {
	if q.Filter != nil {
		switch *q.Filter {
		case domain.MessageWaiting, domain.MessageApproved, domain.MessageHidden:
			b.WriteString(` AND m.status = ?`)
			*args = append(*args, *q.Filter)
		}
	}
	if q.Search != "" {
		needle := "%" + escapeLike(q.Search) + "%"
		// Search hits author_name + body (the spec's mandatory pair)
		// plus author_email + ip_address (the §12-2 add — useful for
		// moderation; not exposed to readers).
		b.WriteString(` AND (m.author_name LIKE ? ESCAPE '\'
			OR m.body LIKE ? ESCAPE '\'
			OR m.author_email LIKE ? ESCAPE '\'
			OR m.ip_address LIKE ? ESCAPE '\')`)
		*args = append(*args, needle, needle, needle, needle)
	}
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

// MessageByID fetches one comment row by primary key. ErrNotFound on miss.
// Used by callers (webhook dispatch, future audit hooks) that need the
// full row after an UpdateMessageStatus / CreateMessage.
func (s *Store) MessageByID(ctx context.Context, wid, id int64) (*domain.Message, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT `+messageColumns+`
		FROM messages
		WHERE wid = ? AND id = ?`, wid, id)
	var m domain.Message
	var postedAt int64
	if err := row.Scan(&m.ID, &m.WID, &m.EntryID, &m.Status, &postedAt,
		&m.AuthorName, &m.AuthorEmail, &m.AuthorURL, &m.Body,
		&m.IPAddress, &m.UserAgent); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("repo: MessageByID: %w", err)
	}
	m.PostedAt = time.Unix(postedAt, 0)
	return &m, nil
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
