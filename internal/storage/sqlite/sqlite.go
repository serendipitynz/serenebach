// Package sqlite opens the SQLite database file used by Serene Bach.
// The driver is modernc.org/sqlite (pure Go) so the binary builds with
// CGO_ENABLED=0 and runs on shared CGI hosts; see AGENTS.md hard
// constraints.
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Open opens (creating if needed) the SQLite database at path and applies
// pragmas appropriate for a single-node, low-to-mid traffic weblog: WAL journal
// mode, a generous busy timeout, foreign keys on, and normal synchronous.
func Open(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("sqlite: mkdir data dir: %w", err)
	}

	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)&_pragma=synchronous(NORMAL)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("sqlite: open %q: %w", path, err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite: ping %q: %w", path, err)
	}
	// FTS5 + trigram tokenizer are required by the entry full-text search
	// migration (see migrations/0052_entry_fts.sql). modernc.org/sqlite
	// ships with both enabled, but we assert that here before any
	// migration runs so a hypothetical driver swap fails loudly with a
	// clear message rather than corrupting the migrations table.
	//
	// The probe runs on a single dedicated connection against the temp
	// schema. CGI hosts call Open on every request, so a persistent probe
	// table on the main schema would (a) collide with concurrent opens as
	// "table already exists" and (b) permanently break startup if the
	// process dies between CREATE and DROP (e.g. shared-host OOM kill).
	// temp.* tables live only for the connection's lifetime and never
	// touch the DB file, so neither failure mode is possible. Pinning one
	// connection guarantees the CREATE and DROP hit the same connection
	// rather than two different ones from the pool.
	conn, err := db.Conn(context.Background())
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite: acquire FTS5 probe connection: %w", err)
	}
	if _, err := conn.ExecContext(context.Background(), `CREATE VIRTUAL TABLE temp._fts_probe USING fts5(content, tokenize='trigram')`); err != nil {
		_ = conn.Close()
		_ = db.Close()
		return nil, fmt.Errorf("sqlite: FTS5 with the trigram tokenizer is required but not available in this driver build: %w", err)
	}
	// temp.* tables are dropped automatically when the connection closes;
	// this explicit drop keeps the pinned connection clean before it is
	// returned to the pool. Best-effort — a failure here is harmless.
	_, _ = conn.ExecContext(context.Background(), `DROP TABLE IF EXISTS temp._fts_probe`)
	if err := conn.Close(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite: release FTS5 probe connection: %w", err)
	}
	return db, nil
}
