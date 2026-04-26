// Package mcpaudit persists one row per MCP write-tool invocation
// so the admin UI can render "who did what when" without scraping
// the process log. Storage mirrors the analytics pattern: by default
// the audit rows live in the main app DB via the migration-managed
// mcp_audit_log table, but operators can point SB_MCP_AUDIT_DB at a
// dedicated SQLite file when they want the audit trail separated from
// content (e.g. a larger retention window, a different backup policy,
// or read-only analyst access).
//
// The Store exposed here hides which backend the MCP server is
// talking to — a single Insert / Recent surface works for both.
package mcpaudit

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Store wraps the *sql.DB audit rows live in. Construct via WrapMain
// (shares the main app DB and relies on migration 0030 for schema)
// or Open (separate file, creates the schema on demand).
type Store struct {
	db             *sql.DB
	ownsConnection bool
}

// Entry is one audited MCP mutation. TargetID / Extra are tool-specific
// — create_entry stores the new entry id, upload_image stores the
// image id, etc. Empty Extra is the normal case.
type Entry struct {
	ID        int64
	WID       int64
	TokenID   int64
	AuthorID  int64
	Tool      string
	TargetID  int64
	Extra     string
	CreatedAt time.Time
}

// WrapMain returns a Store backed by the main application database.
// Schema is assumed to exist via migrations (0030_mcp_audit_log).
func WrapMain(db *sql.DB) *Store {
	return &Store{db: db}
}

// Open opens a dedicated audit SQLite file, creating the schema if
// needed. Use this when the operator sets SB_MCP_AUDIT_DB so the log
// doesn't mix with the weblog content DB.
func Open(path string) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("mcpaudit.Open: empty path")
	}
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)", filepath.ToSlash(path))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("mcpaudit: open %q: %w", path, err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("mcpaudit: ping %q: %w", path, err)
	}
	s := &Store{db: db, ownsConnection: true}
	if err := s.ensureSchema(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the underlying *sql.DB only when the Store owns it
// (i.e. when created via Open). Main-DB mode never closes here —
// that belongs to the app.
func (s *Store) Close() error {
	if s.ownsConnection && s.db != nil {
		return s.db.Close()
	}
	return nil
}

// DB exposes the underlying connection so tests can poke at the rows.
func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) ensureSchema(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS mcp_audit_log (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			wid        INTEGER NOT NULL DEFAULT 1,
			token_id   INTEGER NOT NULL DEFAULT 0,
			author_id  INTEGER NOT NULL DEFAULT 0,
			tool       TEXT    NOT NULL,
			target_id  INTEGER NOT NULL DEFAULT 0,
			extra      TEXT    NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_mcp_audit_log_wid_created ON mcp_audit_log(wid, created_at DESC)`,
	}
	for _, q := range stmts {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("mcpaudit: ensure schema: %w", err)
		}
	}
	return nil
}

// Insert records one mutation. Returns the new row id + any DB error.
// Callers log errors but never fail the mutation — audit is
// observational.
func (s *Store) Insert(ctx context.Context, e Entry) (int64, error) {
	if s == nil || s.db == nil {
		return 0, nil
	}
	ts := e.CreatedAt
	if ts.IsZero() {
		ts = time.Now()
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO mcp_audit_log (wid, token_id, author_id, tool, target_id, extra, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		e.WID, e.TokenID, e.AuthorID, e.Tool, e.TargetID, e.Extra, ts.Unix())
	if err != nil {
		return 0, fmt.Errorf("mcpaudit: insert: %w", err)
	}
	id, _ := res.LastInsertId()
	return id, nil
}

// Recent returns up to `limit` newest-first audit rows for `wid`.
// The admin panel pages one screenful at a time — 100 fits on a
// laptop without sidescroll.
func (s *Store) Recent(ctx context.Context, wid int64, limit int) ([]Entry, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, wid, token_id, author_id, tool, target_id, extra, created_at
		FROM mcp_audit_log
		WHERE wid = ?
		ORDER BY created_at DESC, id DESC
		LIMIT ?`, wid, limit)
	if err != nil {
		return nil, fmt.Errorf("mcpaudit: recent: %w", err)
	}
	defer rows.Close()
	out := make([]Entry, 0, limit)
	for rows.Next() {
		var e Entry
		var created int64
		if err := rows.Scan(&e.ID, &e.WID, &e.TokenID, &e.AuthorID, &e.Tool, &e.TargetID, &e.Extra, &created); err != nil {
			return nil, err
		}
		e.CreatedAt = time.Unix(created, 0)
		out = append(out, e)
	}
	return out, rows.Err()
}
