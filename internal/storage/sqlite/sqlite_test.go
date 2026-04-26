package sqlite

import (
	"path/filepath"
	"testing"
)

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
