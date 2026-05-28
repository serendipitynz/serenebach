// Package sqlite opens the SQLite database file used by Serene Bach.
// The driver is modernc.org/sqlite (pure Go) so the binary builds with
// CGO_ENABLED=0 and runs on shared CGI hosts; see AGENTS.md hard
// constraints.
package sqlite

import (
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
	if _, err := db.Exec(`CREATE VIRTUAL TABLE _fts_probe USING fts5(content, tokenize='trigram')`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite: FTS5 with the trigram tokenizer is required but not available in this driver build: %w", err)
	}
	if _, err := db.Exec(`DROP TABLE _fts_probe`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite: drop FTS5 probe: %w", err)
	}
	return db, nil
}
