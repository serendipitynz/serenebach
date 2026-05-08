package mcpaudit

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func newTestStoreOnMain(t *testing.T) *Store {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := ensureSchemaOn(db); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	return WrapMain(db)
}

func ensureSchemaOn(db *sql.DB) error {
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
		if _, err := db.ExecContext(context.Background(), q); err != nil {
			return err
		}
	}
	return nil
}

// --- Insert tests ---

func TestInsert(t *testing.T) {
	ctx := context.Background()
	s := newTestStoreOnMain(t)

	now := time.Now()
	id, err := s.Insert(ctx, Entry{
		WID:      1,
		TokenID:  42,
		AuthorID: 99,
		Tool:     "create_entry",
		TargetID: 123,
		Extra:    "",
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if id == 0 {
		t.Error("expected non-zero id")
	}

	// Verify field values.
	var e Entry
	var created int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT id, wid, token_id, author_id, tool, target_id, extra, created_at
		 FROM mcp_audit_log WHERE id = ?`, id).
		Scan(&e.ID, &e.WID, &e.TokenID, &e.AuthorID, &e.Tool, &e.TargetID, &e.Extra, &created); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if e.WID != 1 {
		t.Errorf("wid = %d, want 1", e.WID)
	}
	if e.TokenID != 42 {
		t.Errorf("token_id = %d, want 42", e.TokenID)
	}
	if e.AuthorID != 99 {
		t.Errorf("author_id = %d, want 99", e.AuthorID)
	}
	if e.Tool != "create_entry" {
		t.Errorf("tool = %q, want create_entry", e.Tool)
	}
	if e.TargetID != 123 {
		t.Errorf("target_id = %d, want 123", e.TargetID)
	}
	if e.Extra != "" {
		t.Errorf("extra = %q, want empty", e.Extra)
	}
	e.CreatedAt = time.Unix(created, 0)
	if e.CreatedAt.Before(now.Add(-time.Second)) || e.CreatedAt.After(now.Add(time.Second)) {
		t.Errorf("created_at = %v, want ~%v", e.CreatedAt, now)
	}
}

func TestInsertMultipleTools(t *testing.T) {
	ctx := context.Background()
	s := newTestStoreOnMain(t)

	for i, tool := range []string{"create_entry", "update_entry", "publish_entry", "upload_image"} {
		if _, err := s.Insert(ctx, Entry{
			WID:      1,
			TokenID:  int64(10 + i),
			AuthorID: int64(100 + i),
			Tool:     tool,
			TargetID: int64(1000 + i),
		}); err != nil {
			t.Fatalf("Insert %s: %v", tool, err)
		}
	}

	// Verify all tools recorded.
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mcp_audit_log`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 4 {
		t.Errorf("count = %d, want 4", count)
	}

	for _, tool := range []string{"create_entry", "update_entry", "publish_entry", "upload_image"} {
		var c int
		s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mcp_audit_log WHERE tool = ?`, tool).Scan(&c)
		if c != 1 {
			t.Errorf("tool %q count = %d, want 1", tool, c)
		}
	}
}

func TestInsertWithExtra(t *testing.T) {
	ctx := context.Background()
	s := newTestStoreOnMain(t)

	id, err := s.Insert(ctx, Entry{
		WID:      1,
		TokenID:  1,
		AuthorID: 1,
		Tool:     "upload_image",
		TargetID: 500,
		Extra:    "filename:test.png",
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if id == 0 {
		t.Error("expected non-zero id")
	}

	var extra string
	s.db.QueryRowContext(ctx, `SELECT extra FROM mcp_audit_log WHERE id = ?`, id).Scan(&extra)
	if extra != "filename:test.png" {
		t.Errorf("extra = %q, want 'filename:test.png'", extra)
	}
}

func TestInsertWithExplicitCreatedAt(t *testing.T) {
	ctx := context.Background()
	s := newTestStoreOnMain(t)

	fixed := time.Date(2025, 3, 15, 10, 30, 0, 0, time.UTC)
	id, err := s.Insert(ctx, Entry{
		WID:       1,
		TokenID:   1,
		AuthorID:  1,
		Tool:      "create_entry",
		TargetID:  1,
		CreatedAt: fixed,
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	var created int64
	s.db.QueryRowContext(ctx, `SELECT created_at FROM mcp_audit_log WHERE id = ?`, id).Scan(&created)
	if created != fixed.Unix() {
		t.Errorf("created_at = %d, want %d", created, fixed.Unix())
	}
}

// --- Recent tests ---

func TestRecentOrdering(t *testing.T) {
	ctx := context.Background()
	s := newTestStoreOnMain(t)

	// Insert entries with staggered timestamps.
	now := time.Now()
	for i := 0; i < 5; i++ {
		s.Insert(ctx, Entry{
			WID:       1,
			TokenID:   1,
			AuthorID:  1,
			Tool:      "create_entry",
			TargetID:  int64(100 + i),
			CreatedAt: now.Add(time.Duration(i) * time.Second),
		})
	}

	entries, err := s.Recent(ctx, 1, 10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(entries) != 5 {
		t.Fatalf("len = %d, want 5", len(entries))
	}

	// Newest first.
	for i := 0; i < 4; i++ {
		if entries[i].CreatedAt.Before(entries[i+1].CreatedAt) {
			t.Errorf("entries[%d].CreatedAt (%v) should be >= entries[%d].CreatedAt (%v)",
				i, entries[i].CreatedAt, i+1, entries[i+1].CreatedAt)
		}
	}
}

func TestRecentLimit(t *testing.T) {
	ctx := context.Background()
	s := newTestStoreOnMain(t)

	for i := 0; i < 10; i++ {
		s.Insert(ctx, Entry{
			WID:      1,
			TokenID:  1,
			AuthorID: 1,
			Tool:     "create_entry",
			TargetID: int64(i),
		})
	}

	// Limit to 3.
	entries, err := s.Recent(ctx, 1, 3)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("len = %d, want 3", len(entries))
	}
}

func TestRecentWIDScoping(t *testing.T) {
	ctx := context.Background()
	s := newTestStoreOnMain(t)

	s.Insert(ctx, Entry{WID: 1, TokenID: 1, AuthorID: 1, Tool: "create_entry", TargetID: 1})
	s.Insert(ctx, Entry{WID: 2, TokenID: 2, AuthorID: 2, Tool: "update_entry", TargetID: 2})
	s.Insert(ctx, Entry{WID: 1, TokenID: 3, AuthorID: 3, Tool: "publish_entry", TargetID: 3})

	// WID 1 should have 2 entries.
	entries, err := s.Recent(ctx, 1, 10)
	if err != nil {
		t.Fatalf("Recent wid=1: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("wid=1 len = %d, want 2", len(entries))
	}
	for _, e := range entries {
		if e.WID != 1 {
			t.Errorf("entry wid = %d, want 1", e.WID)
		}
	}

	// WID 2 should have 1 entry.
	entries2, err := s.Recent(ctx, 2, 10)
	if err != nil {
		t.Fatalf("Recent wid=2: %v", err)
	}
	if len(entries2) != 1 {
		t.Fatalf("wid=2 len = %d, want 1", len(entries2))
	}
	if entries2[0].Tool != "update_entry" {
		t.Errorf("wid=2 tool = %q, want update_entry", entries2[0].Tool)
	}
}

func TestRecentEmpty(t *testing.T) {
	ctx := context.Background()
	s := newTestStoreOnMain(t)

	entries, err := s.Recent(ctx, 1, 10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("len = %d, want 0 (empty DB)", len(entries))
	}
}

// --- Open tests ---

func TestOpenExternalDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Insert a row.
	id, err := s.Insert(ctx, Entry{
		WID:      1,
		TokenID:  5,
		AuthorID: 10,
		Tool:     "upload_image",
		TargetID: 999,
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if id == 0 {
		t.Error("expected non-zero id")
	}

	// Read recent.
	entries, err := s.Recent(ctx, 1, 10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len = %d, want 1", len(entries))
	}
	if entries[0].Tool != "upload_image" {
		t.Errorf("tool = %q, want upload_image", entries[0].Tool)
	}
}

func TestOpenEmptyPath(t *testing.T) {
	_, err := Open("")
	if err == nil {
		t.Error("expected error for empty path")
	}
}

// --- WrapMain tests ---

func TestWrapMain(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if err := ensureSchemaOn(db); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	s := WrapMain(db)
	ctx := context.Background()

	id, err := s.Insert(ctx, Entry{
		WID:      1,
		TokenID:  3,
		AuthorID: 7,
		Tool:     "create_entry",
		TargetID: 42,
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if id == 0 {
		t.Error("expected non-zero id")
	}

	entries, err := s.Recent(ctx, 1, 10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len = %d, want 1", len(entries))
	}

	// ownsConnection=false → Close should be a no-op (no error).
	if err := s.Close(); err != nil {
		t.Fatalf("Close on main-wrapped store: %v", err)
	}
}

// --- Close tests ---

func TestCloseOwnership(t *testing.T) {
	// WrapMain: Close does nothing; the owning code closes the DB.
	db, _ := sql.Open("sqlite", ":memory:")
	defer db.Close()
	ensureSchemaOn(db)
	s := WrapMain(db)
	if err := s.Close(); err != nil {
		t.Errorf("WrapMain.Close: unexpected error: %v", err)
	}
	// DB should still be usable (not closed by Close).
	_, err := s.Insert(context.Background(), Entry{WID: 1, TokenID: 1, AuthorID: 1, Tool: "x", TargetID: 1})
	if err != nil {
		t.Fatalf("Insert after Close: %v", err)
	}

	// Open: Close closes the owned connection.
	path := filepath.Join(t.TempDir(), "close-test.db")
	os, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := os.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Second Close should be a no-op.
	if err := os.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	// Insert after Close should fail.
	_, err = os.Insert(context.Background(), Entry{WID: 1, TokenID: 1, AuthorID: 1, Tool: "x", TargetID: 1})
	if err == nil {
		t.Errorf("expected error for Insert after Close on owned DB")
	}
}

// --- Nil store safety tests ---

func TestInsertNilStore(t *testing.T) {
	var s *Store
	id, err := s.Insert(context.Background(), Entry{Tool: "test"})
	if err != nil {
		t.Errorf("nil store Insert should not error: %v", err)
	}
	if id != 0 {
		t.Errorf("nil store Insert id = %d, want 0", id)
	}
}

func TestRecentNilStore(t *testing.T) {
	var s *Store
	entries, err := s.Recent(context.Background(), 1, 10)
	if err != nil {
		t.Errorf("nil store Recent should not error: %v", err)
	}
	if entries != nil {
		t.Errorf("nil store Recent = %v, want nil", entries)
	}
}

func TestDBExposed(t *testing.T) {
	db, _ := sql.Open("sqlite", ":memory:")
	defer db.Close()
	ensureSchemaOn(db)
	s := WrapMain(db)
	if dbPtr := s.DB(); dbPtr != db {
		t.Error("DB() should return the underlying connection")
	}
}

// --- ensureSchema is idempotent ---

func TestEnsureSchemaIdempotent(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	// Run twice.
	if err := ensureSchemaOn(db); err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	if err := ensureSchemaOn(db); err != nil {
		t.Fatalf("second ensure: %v", err)
	}

	// Verify table exists.
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM mcp_audit_log`).Scan(&count)
	// Shouldn't error.
}
