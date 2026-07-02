package sqlite

import (
	"path/filepath"
	"testing"
)

// TestOpenTwiceOnSameFile guards the FTS5 probe against regressing to a
// persistent table: the probe must live in the temp schema so opening the
// same DB file a second time (as CGI does per request) still succeeds and
// leaves no _fts_probe behind in the file.
func TestOpenTwiceOnSameFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "test.db")

	db1, err := Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if err := db1.Close(); err != nil {
		t.Fatalf("close first: %v", err)
	}

	db2, err := Open(path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	t.Cleanup(func() { _ = db2.Close() })

	// The probe table must not have been persisted to the DB file.
	var n int
	if err := db2.QueryRow(
		`SELECT count(*) FROM sqlite_master WHERE name = '_fts_probe'`,
	).Scan(&n); err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	if n != 0 {
		t.Fatalf("_fts_probe persisted in DB file (count=%d), want 0", n)
	}
}

func TestOpenPing(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	var got int
	if err := db.QueryRow(`SELECT 1`).Scan(&got); err != nil {
		t.Fatalf("SELECT 1: %v", err)
	}
	if got != 1 {
		t.Fatalf("SELECT 1 = %d, want 1", got)
	}
}
