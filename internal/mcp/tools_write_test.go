package mcp

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"testing"
	"time"

	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/images"
	"github.com/serendipitynz/serenebach/internal/mcpaudit"
	"github.com/serendipitynz/serenebach/internal/storage"
	"github.com/serendipitynz/serenebach/internal/storage/repo"

	_ "modernc.org/sqlite"
)

// newTestServerWithSeed is like newTestServer but also inserts a minimal
// weblog row so that create/update/publish entry operations work without
// FK or wid-scoping issues.
func newTestServerWithSeed(t *testing.T) *Server {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := storage.Up(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	now := time.Now().Unix()
	if _, err := db.ExecContext(context.Background(), `
		INSERT INTO weblogs (id, title, description, base_url, lang, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO NOTHING`,
		1, "Test", "test", "", "en", now, now); err != nil {
		t.Fatalf("seed weblog: %v", err)
	}

	s := repo.New(db)
	return &Server{
		Store: s,
		Audit: mcpaudit.WrapMain(db),
		WID:   1,
	}
}

// --- toolCreateEntry tests ---

func TestToolCreateEntryMissingTitle(t *testing.T) {
	srv := newTestServerWithSeed(t)
	ctx := WithAuth(context.Background(), domain.MCPScopeWrite, 1, 1)

	_, err := srv.toolCreateEntry(ctx, json.RawMessage(`{}`))
	if err == nil {
		t.Error("expected error for missing title")
	}
	if err != nil && err.Error() != "title is required" {
		t.Errorf("unexpected error: %v", err)
	}

	// Title with only whitespace.
	_, err = srv.toolCreateEntry(ctx, json.RawMessage(`{"title":"   ","body":"x"}`))
	if err == nil {
		t.Error("expected error for whitespace-only title")
	}
	if err != nil && err.Error() != "title is required" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestToolCreateEntrySuccessAndAudit(t *testing.T) {
	srv := newTestServerWithSeed(t)
	ctx := WithAuth(context.Background(), domain.MCPScopeWrite, 42, 99)

	result, err := srv.toolCreateEntry(ctx, json.RawMessage(`{"title":"Audit Test","body":"content","tags":["test"]}`))
	if err != nil {
		t.Fatalf("toolCreateEntry: %v", err)
	}

	// Parse the result to get the entry id.
	var payload map[string]any
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	entryID := int64(payload["id"].(float64))
	if entryID == 0 {
		t.Fatal("expected non-zero entry id")
	}

	// Verify audit row exists for create_entry.
	var auditCount int
	db := srv.Store.DB()
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mcp_audit_log WHERE tool = 'create_entry' AND target_id = ?`, entryID).Scan(&auditCount); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	if auditCount != 1 {
		t.Errorf("audit count = %d, want 1", auditCount)
	}

	// Verify audit captures tool name, target id, acting user (author), token id, and timestamp.
	var tool string
	var targetID, tokenID, authorID int64
	var createdAt int64
	if err := db.QueryRowContext(ctx,
		`SELECT tool, target_id, token_id, author_id, created_at FROM mcp_audit_log WHERE tool = 'create_entry' AND target_id = ?`,
		entryID).Scan(&tool, &targetID, &tokenID, &authorID, &createdAt); err != nil {
		t.Fatalf("scan audit row: %v", err)
	}
	if tool != "create_entry" {
		t.Errorf("tool = %q, want create_entry", tool)
	}
	if targetID != entryID {
		t.Errorf("target_id = %d, want %d", targetID, entryID)
	}
	if authorID != 99 {
		t.Errorf("author_id = %d, want 99", authorID)
	}
	if tokenID != 42 {
		t.Errorf("token_id = %d, want 42", tokenID)
	}
	if createdAt == 0 {
		t.Error("created_at should be non-zero")
	}
}

func TestToolCreateEntryInvalidStatus(t *testing.T) {
	srv := newTestServerWithSeed(t)
	ctx := WithAuth(context.Background(), domain.MCPScopeWrite, 1, 1)

	_, err := srv.toolCreateEntry(ctx, json.RawMessage(`{"title":"x","body":"x","status":"nonexistent"}`))
	if err == nil {
		t.Error("expected error for invalid status")
	}
	if err != nil && err.Error() != `invalid status "nonexistent" (expected draft / published / closed)` {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestToolCreateEntryInvalidSlug(t *testing.T) {
	srv := newTestServerWithSeed(t)
	ctx := WithAuth(context.Background(), domain.MCPScopeWrite, 1, 1)

	_, err := srv.toolCreateEntry(ctx, json.RawMessage(`{"title":"x","body":"x","slug":"BAD SLUG"}`))
	if err == nil {
		t.Error("expected error for invalid slug")
	}
	if err != nil && err.Error() != `invalid slug "BAD SLUG" (lowercase alphanum + single hyphens only)` {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- toolUpdateEntry tests ---

func TestToolUpdateEntryMissingID(t *testing.T) {
	srv := newTestServerWithSeed(t)
	ctx := WithAuth(context.Background(), domain.MCPScopeWrite, 1, 1)

	_, err := srv.toolUpdateEntry(ctx, json.RawMessage(`{}`))
	if err == nil {
		t.Error("expected error for missing id")
	}
	if err != nil && err.Error() != "id is required" {
		t.Errorf("unexpected error: %v", err)
	}

	// id = 0.
	_, err = srv.toolUpdateEntry(ctx, json.RawMessage(`{"id":0,"title":"x"}`))
	if err == nil {
		t.Error("expected error for id=0")
	}
	if err != nil && err.Error() != "id is required" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestToolUpdateEntryNotFound(t *testing.T) {
	srv := newTestServerWithSeed(t)
	ctx := WithAuth(context.Background(), domain.MCPScopeWrite, 1, 1)

	_, err := srv.toolUpdateEntry(ctx, json.RawMessage(`{"id":999999,"title":"ghost"}`))
	if err == nil {
		t.Error("expected error for non-existent entry")
	}
	if err != nil && err.Error() != "entry 999999 not found" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestToolUpdateEntrySuccessAndAudit(t *testing.T) {
	srv := newTestServerWithSeed(t)
	ctx := WithAuth(context.Background(), domain.MCPScopeWrite, 10, 20)

	// First create an entry.
	createResult, err := srv.toolCreateEntry(ctx, json.RawMessage(`{"title":"Original","body":"content"}`))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	var payload map[string]any
	json.Unmarshal([]byte(createResult), &payload)
	entryID := int64(payload["id"].(float64))

	// Update it.
	_, err = srv.toolUpdateEntry(ctx, json.RawMessage(fmt.Sprintf(`{"id":%d,"title":"Updated"}`, entryID)))
	if err != nil {
		t.Fatalf("update: %v", err)
	}

	// Verify audit row for update_entry.
	var auditCount int
	db := srv.Store.DB()
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM mcp_audit_log WHERE tool = 'update_entry' AND target_id = ?`, entryID).Scan(&auditCount); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	if auditCount != 1 {
		t.Errorf("audit count for update_entry = %d, want 1", auditCount)
	}

	// Verify update audit captures correct fields.
	var tokenID, authorID int64
	if err := db.QueryRowContext(ctx,
		`SELECT token_id, author_id FROM mcp_audit_log WHERE tool = 'update_entry' AND target_id = ?`,
		entryID).Scan(&tokenID, &authorID); err != nil {
		t.Fatalf("scan update audit: %v", err)
	}
	if tokenID != 10 {
		t.Errorf("update token_id = %d, want 10", tokenID)
	}
	if authorID != 20 {
		t.Errorf("update author_id = %d, want 20", authorID)
	}
}

// --- toolPublishEntry tests ---

func TestToolPublishEntryMissingID(t *testing.T) {
	srv := newTestServerWithSeed(t)
	ctx := WithAuth(context.Background(), domain.MCPScopeWrite, 1, 1)

	_, err := srv.toolPublishEntry(ctx, json.RawMessage(`{}`))
	if err == nil {
		t.Error("expected error for missing id")
	}
	if err != nil && err.Error() != "id is required" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestToolPublishEntryNotFound(t *testing.T) {
	srv := newTestServerWithSeed(t)
	ctx := WithAuth(context.Background(), domain.MCPScopeWrite, 1, 1)

	_, err := srv.toolPublishEntry(ctx, json.RawMessage(`{"id":999999}`))
	if err == nil {
		t.Error("expected error for non-existent entry")
	}
	if err != nil && err.Error() != "entry 999999 not found" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestToolPublishEntrySuccessAndAudit(t *testing.T) {
	srv := newTestServerWithSeed(t)
	ctx := WithAuth(context.Background(), domain.MCPScopeWrite, 5, 7)

	// Create a draft entry first.
	createResult, err := srv.toolCreateEntry(ctx, json.RawMessage(`{"title":"Draft","body":"content"}`))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	var payload map[string]any
	json.Unmarshal([]byte(createResult), &payload)
	entryID := int64(payload["id"].(float64))

	// Publish it.
	result, err := srv.toolPublishEntry(ctx, json.RawMessage(fmt.Sprintf(`{"id":%d}`, entryID)))
	if err != nil {
		t.Fatalf("publish: %v", err)
	}

	// Verify status flipped to published.
	json.Unmarshal([]byte(result), &payload)
	if status, _ := payload["status"].(string); status != "published" {
		t.Errorf("status = %q, want published", status)
	}

	// Verify audit rows: one for create_entry, one for publish_entry.
	var createCount, publishCount int
	db := srv.Store.DB()
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mcp_audit_log WHERE tool = 'create_entry'`).Scan(&createCount)
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mcp_audit_log WHERE tool = 'publish_entry'`).Scan(&publishCount)
	if createCount != 1 {
		t.Errorf("create audit count = %d, want 1", createCount)
	}
	if publishCount != 1 {
		t.Errorf("publish audit count = %d, want 1", publishCount)
	}

	// Verify publish audit target_id matches entry id.
	var targetID int64
	if err := db.QueryRowContext(ctx,
		`SELECT target_id FROM mcp_audit_log WHERE tool = 'publish_entry'`,
	).Scan(&targetID); err != nil {
		t.Fatalf("scan publish audit: %v", err)
	}
	if targetID != entryID {
		t.Errorf("publish target_id = %d, want %d", targetID, entryID)
	}
}

// --- auditWrite behavior tests ---

func TestAuditWriteSucceedsEvenWhenAuditDBClosed(t *testing.T) {
	// Use a separate audit DB file, then close it to trigger audit insert failure.
	// The write tool should still succeed (log-and-continue).
	mainDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open main db: %v", err)
	}
	defer mainDB.Close()
	if err := storage.Up(mainDB); err != nil {
		t.Fatalf("migrate main: %v", err)
	}

	// Seed weblog.
	now := time.Now().Unix()
	mainDB.ExecContext(context.Background(), `
		INSERT INTO weblogs (id, title, description, base_url, lang, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO NOTHING`,
		1, "Test", "test", "", "en", now, now)

	// Create a separate audit DB.
	auditDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open audit db: %v", err)
	}
	auditStore := mcpaudit.WrapMain(auditDB)
	// Manually create the schema since WrapMain expects migration-created table.
	if _, err := auditDB.ExecContext(context.Background(), `
		CREATE TABLE IF NOT EXISTS mcp_audit_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			wid INTEGER NOT NULL DEFAULT 1,
			token_id INTEGER NOT NULL DEFAULT 0,
			author_id INTEGER NOT NULL DEFAULT 0,
			tool TEXT NOT NULL,
			target_id INTEGER NOT NULL DEFAULT 0,
			extra TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL
		)`); err != nil {
		t.Fatalf("create audit schema: %v", err)
	}

	store := repo.New(mainDB)
	srv := &Server{
		Store: store,
		Audit: auditStore,
		WID:   1,
	}
	ctx := WithAuth(context.Background(), domain.MCPScopeWrite, 1, 1)

	// Create an entry — audit insert succeeds.
	result, err := srv.toolCreateEntry(ctx, json.RawMessage(`{"title":"Before Audit Fail","body":"content"}`))
	if err != nil {
		t.Fatalf("create before audit fail: %v", err)
	}
	var payload map[string]any
	json.Unmarshal([]byte(result), &payload)
	entryID := int64(payload["id"].(float64))

	// Verify audit row exists.
	var count int
	_ = auditDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM mcp_audit_log WHERE tool = 'create_entry'`).Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 audit row, got %d", count)
	}

	// Now close the audit DB so the next audit INSERT fails.
	auditDB.Close()

	ctx2 := WithAuth(context.Background(), domain.MCPScopeWrite, 1, 1)
	// Create another entry — must still succeed (audit failure is log-only).
	result2, err := srv.toolCreateEntry(ctx2, json.RawMessage(`{"title":"After Audit Fail","body":"content"}`))
	if err != nil {
		t.Fatalf("create with audit db closed: %v (audit failure should not gate the write)", err)
	}

	// Verify the entry was actually created in the main DB.
	json.Unmarshal([]byte(result2), &payload)
	entryID2 := int64(payload["id"].(float64))
	if entryID2 == 0 || entryID2 == entryID {
		t.Fatalf("expected new entry id, got %d", entryID2)
	}
	var dbTitle string
	mainDB.QueryRowContext(ctx, `SELECT title FROM entries WHERE id = ?`, entryID2).Scan(&dbTitle)
	if dbTitle != "After Audit Fail" {
		t.Fatalf("entry title = %q, want 'After Audit Fail'", dbTitle)
	}
}

func TestEntryPayloadReturnsTags(t *testing.T) {
	srv := newTestServerWithSeed(t)
	ctx := WithAuth(context.Background(), domain.MCPScopeWrite, 1, 1)

	result, err := srv.toolCreateEntry(ctx, json.RawMessage(`{"title":"Tagged","body":"content","tags":["alpha","beta"]}`))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatalf("decode result: %v", err)
	}

	tags, ok := payload["tags"].([]any)
	if !ok {
		t.Fatal("expected tags field")
	}
	if len(tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(tags))
	}
}

// TestCallToolWriteWithValidScope exercises the full scope gate path.
func TestCallToolWriteAllowedWithWriteScope(t *testing.T) {
	srv := newTestServerWithSeed(t)
	ctx := WithAuth(context.Background(), domain.MCPScopeWrite, 1, 1)

	result, err := srv.callTool(ctx, "create_entry", json.RawMessage(`{"title":"Write Scope Test","body":"body"}`))
	if err != nil {
		t.Fatalf("write-scoped create should succeed, got: %v", err)
	}
	if result == "" {
		t.Fatal("expected non-empty result from create_entry")
	}
}

func TestParsePostedAtErrorOnInvalidTimestamp(t *testing.T) {
	bad := "not-a-timestamp"
	_, err := parsePostedAt(&bad, time.Now())
	if err == nil {
		t.Error("expected error for invalid timestamp")
	}
}

func TestToolUploadImageSuccessAndAudit(t *testing.T) {
	mainDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open main db: %v", err)
	}
	defer mainDB.Close()
	if err := storage.Up(mainDB); err != nil {
		t.Fatalf("migrate main: %v", err)
	}

	// Seed weblog.
	now := time.Now().Unix()
	mainDB.ExecContext(context.Background(), `
		INSERT INTO weblogs (id, title, description, base_url, lang, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO NOTHING`,
		1, "Test", "test", "", "en", now, now)

	imgDir := t.TempDir()
	srv := &Server{
		Store:      repo.New(mainDB),
		Audit:      mcpaudit.WrapMain(mainDB),
		ImageStore: images.NewStore(imgDir),
		WID:        1,
	}

	ctx := WithAuth(context.Background(), domain.MCPScopeWrite, 7, 13)

	// Build a minimal valid PNG.
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for x := 0; x < 2; x++ {
		for y := 0; y < 2; y++ {
			img.Set(x, y, color.RGBA{R: 100, G: 100, B: 100, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}

	result, err := srv.toolUploadImage(ctx, json.RawMessage(fmt.Sprintf(
		`{"data":"%s","filename":"probe.png"}`, base64.StdEncoding.EncodeToString(buf.Bytes()))))
	if err != nil {
		t.Fatalf("toolUploadImage: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	imageID := int64(resp["id"].(float64))
	if imageID == 0 {
		t.Fatal("expected non-zero image id")
	}

	// Verify audit row for upload_image.
	var auditCount int
	if err := mainDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM mcp_audit_log WHERE tool = 'upload_image' AND target_id = ?`, imageID).Scan(&auditCount); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	if auditCount != 1 {
		t.Errorf("audit count for upload_image = %d, want 1", auditCount)
	}

	var tokenID, authorID, targetID int64
	var tool string
	if err := mainDB.QueryRowContext(ctx,
		`SELECT tool, target_id, token_id, author_id FROM mcp_audit_log WHERE tool = 'upload_image'`,
	).Scan(&tool, &targetID, &tokenID, &authorID); err != nil {
		t.Fatalf("scan audit: %v", err)
	}
	if tool != "upload_image" {
		t.Errorf("tool = %q, want upload_image", tool)
	}
	if targetID != imageID {
		t.Errorf("target_id = %d, want %d", targetID, imageID)
	}
	if tokenID != 7 {
		t.Errorf("token_id = %d, want 7", tokenID)
	}
	if authorID != 13 {
		t.Errorf("author_id = %d, want 13", authorID)
	}
}
