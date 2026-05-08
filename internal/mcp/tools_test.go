package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/mcpaudit"
	"github.com/serendipitynz/serenebach/internal/storage"
	"github.com/serendipitynz/serenebach/internal/storage/repo"

	_ "modernc.org/sqlite"
)

// newTestServer creates an mcp.Server backed by an in-memory SQLite
// database with all migrations applied. The audit store is wired to
// the main DB via mcpaudit.WrapMain so audit tests can query rows
// directly through the server's Store.DB().
func newTestServer(t *testing.T) *Server {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := storage.Up(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	s := repo.New(db)
	return &Server{
		Store: s,
		Audit: mcpaudit.WrapMain(db),
		WID:   1,
	}
}

// --- requiredScope tests ---

func TestRequiredScope(t *testing.T) {
	for _, tc := range []struct {
		tool  string
		scope domain.MCPScope
	}{
		// write tools
		{"create_entry", domain.MCPScopeWrite},
		{"update_entry", domain.MCPScopeWrite},
		{"publish_entry", domain.MCPScopeWrite},
		{"upload_image", domain.MCPScopeWrite},
		// read tools
		{"list_entries", domain.MCPScopeRead},
		{"get_entry", domain.MCPScopeRead},
		{"search_entries", domain.MCPScopeRead},
		{"list_categories", domain.MCPScopeRead},
		{"list_tags", domain.MCPScopeRead},
		{"get_analytics", domain.MCPScopeRead},
		{"list_images", domain.MCPScopeRead},
		// unknown tool — defaults to read scope
		{"some_future_tool", domain.MCPScopeRead},
		{"", domain.MCPScopeRead},
	} {
		if got := requiredScope(tc.tool); got != tc.scope {
			t.Errorf("requiredScope(%q) = %q, want %q", tc.tool, got, tc.scope)
		}
	}
}

// --- toolDescriptors tests ---

func TestToolDescriptorsReadScope(t *testing.T) {
	descs := toolDescriptors(domain.MCPScopeRead)
	if len(descs) < 5 {
		t.Fatalf("expected at least 5 read tools, got %d", len(descs))
	}
	for _, d := range descs {
		if requiredScope(d.Name) == domain.MCPScopeWrite {
			t.Errorf("read-scope descriptor leaked write tool %q", d.Name)
		}
	}
}

func TestToolDescriptorsWriteScope(t *testing.T) {
	readCount := len(toolDescriptors(domain.MCPScopeRead))
	writeCount := len(toolDescriptors(domain.MCPScopeWrite))
	if writeCount <= readCount {
		t.Errorf("write-scope descriptors (%d) should be more than read-only (%d)", writeCount, readCount)
	}
}

// --- entryStatusLabel tests ---

func TestEntryStatusLabel(t *testing.T) {
	tests := []struct {
		status domain.EntryStatus
		want   string
	}{
		{domain.EntryPublished, "published"},
		{domain.EntryDraft, "draft"},
		{domain.EntryClosed, "closed"},
		{domain.EntryStatus(99), "unknown"},
	}
	for _, tc := range tests {
		if got := entryStatusLabel(tc.status); got != tc.want {
			t.Errorf("entryStatusLabel(%d) = %q, want %q", tc.status, got, tc.want)
		}
	}
}

// --- parseEntryStatus tests ---

func TestParseEntryStatus(t *testing.T) {
	s := func(v string) *string { return &v }

	tests := []struct {
		raw      *string
		fallback domain.EntryStatus
		want     domain.EntryStatus
		wantErr  bool
	}{
		{nil, domain.EntryDraft, domain.EntryDraft, false},
		{s(""), domain.EntryPublished, domain.EntryPublished, false},
		{s("draft"), domain.EntryPublished, domain.EntryDraft, false},
		{s("published"), domain.EntryDraft, domain.EntryPublished, false},
		{s("closed"), domain.EntryDraft, domain.EntryClosed, false},
		{s("invalid-status"), domain.EntryDraft, 0, true},
	}
	for _, tc := range tests {
		got, err := parseEntryStatus(tc.raw, tc.fallback)
		if tc.wantErr && err == nil {
			t.Errorf("parseEntryStatus(%v, %v) expected error", tc.raw, tc.fallback)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("parseEntryStatus(%v, %v) unexpected error: %v", tc.raw, tc.fallback, err)
		}
		if !tc.wantErr && got != tc.want {
			t.Errorf("parseEntryStatus(%v, %v) = %v, want %v", tc.raw, tc.fallback, got, tc.want)
		}
	}
}

// --- normaliseSlug tests ---

func TestNormaliseSlug(t *testing.T) {
	s := func(v string) *string { return &v }

	tests := []struct {
		raw     *string
		want    string
		wantErr bool
	}{
		{nil, "", false},
		{s(""), "", false},
		{s("valid-slug-123"), "valid-slug-123", false},
		{s("a"), "a", false},
		// Invalid slugs with uppercase or special characters
		{s("UpperCase"), "", true},
		{s("no spaces"), "", true},
		{s("double--hyphen"), "", true},
		{s("-leading-hyphen"), "", true},
		{s("trailing-hyphen-"), "", true},
	}
	for _, tc := range tests {
		got, err := normaliseSlug(tc.raw)
		if tc.wantErr && err == nil {
			t.Errorf("normaliseSlug(%v) expected error", tc.raw)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("normaliseSlug(%v) unexpected error: %v", tc.raw, err)
		}
		if !tc.wantErr && got != tc.want {
			t.Errorf("normaliseSlug(%v) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

// --- parsePostedAt tests ---

func TestParsePostedAt(t *testing.T) {
	s := func(v string) *string { return &v }

	ref := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	valid := s("2025-06-15T12:30:00Z")

	// nil / empty fall through to fallback
	got, err := parsePostedAt(nil, ref)
	if err != nil || !got.Equal(ref) {
		t.Errorf("parsePostedAt(nil, ref) = %v, %v, want %v, nil", got, err, ref)
	}
	got, err = parsePostedAt(s(""), ref)
	if err != nil || !got.Equal(ref) {
		t.Errorf("parsePostedAt('', ref) = %v, %v, want %v, nil", got, err, ref)
	}

	// valid RFC3339
	got, err = parsePostedAt(valid, ref)
	if err != nil {
		t.Errorf("parsePostedAt(valid, ref) error: %v", err)
	}
	want := time.Date(2025, 6, 15, 12, 30, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("parsePostedAt(valid, ref) = %v, want %v", got, want)
	}

	// invalid
	_, err = parsePostedAt(s("not-a-timestamp"), ref)
	if err == nil {
		t.Error("parsePostedAt(invalid, ref) expected error")
	}
}

// --- decodeBase64Flexible tests ---

func TestDecodeBase64Flexible(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"std padded", "SGVsbG8gV29ybGQ=", false},
		{"raw unpadded", "SGVsbG8gV29ybGQ", false},
		{"url encoding", "SGVsbG8gV29ybGQ", false},
		{"empty", "", false},
		{"whitespace only", "   \n  ", false},
		{"invalid chars", "!!!invalid!!!", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := decodeBase64Flexible(tc.input)
			if tc.wantErr && err == nil {
				t.Error("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}

	// Decoded content matches.
	decoded, err := decodeBase64Flexible("SGVsbG8gV29ybGQ=")
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if string(decoded) != "Hello World" {
		t.Errorf("decoded = %q, want 'Hello World'", decoded)
	}

	// Multi-line input with whitespace
	decoded, err = decodeBase64Flexible("SGVsbG8g\nV29ybGQ=")
	if err != nil {
		t.Fatalf("decode multi-line: %v", err)
	}
	if string(decoded) != "Hello World" {
		t.Errorf("multi-line decoded = %q, want 'Hello World'", decoded)
	}
}

// --- deref helpers ---

func TestDerefHelpers(t *testing.T) {
	s := func(v string) *string { return &v }
	if got := derefString(nil, "fallback"); got != "fallback" {
		t.Errorf("derefString(nil) = %q, want fallback", got)
	}
	if got := derefString(s("value"), "fallback"); got != "value" {
		t.Errorf("derefString('value') = %q, want value", got)
	}

	i := func(v int64) *int64 { return &v }
	if got := derefInt64(nil, 42); got != 42 {
		t.Errorf("derefInt64(nil) = %d, want 42", got)
	}
	if got := derefInt64(i(99), 42); got != 99 {
		t.Errorf("derefInt64(99) = %d, want 99", got)
	}
}

// --- authorIDForCtx / WithAuth tests ---

func TestAuthorIDForCtx(t *testing.T) {
	// No auth injected — falls back to 1.
	ctx := context.Background()
	if got := authorIDForCtx(ctx); got != 1 {
		t.Errorf("authorIDForCtx(no-auth) = %d, want 1", got)
	}

	// With auth + bound author.
	ctx = WithAuth(context.Background(), domain.MCPScopeWrite, 42, 99)
	if auth := authFromContext(ctx); auth.AuthorID != 99 {
		t.Errorf("AuthorID = %d, want 99", auth.AuthorID)
	}
	if got := authorIDForCtx(ctx); got != 99 {
		t.Errorf("authorIDForCtx(with-auth) = %d, want 99", got)
	}

	// AuthorID = 0 should fall back to 1 (plumbing bug guard).
	ctx = WithAuth(context.Background(), domain.MCPScopeWrite, 5, 0)
	if got := authorIDForCtx(ctx); got != 1 {
		t.Errorf("authorIDForCtx(author=0) = %d, want 1", got)
	}
}

func TestWithAuthAndAuthFromContext(t *testing.T) {
	ctx := context.Background()

	// No auth → read-only sentinel.
	auth := authFromContext(ctx)
	if auth.Scope != domain.MCPScopeRead {
		t.Errorf("default scope = %q, want read", auth.Scope)
	}
	if auth.TokenID != 0 {
		t.Errorf("default token_id = %d, want 0", auth.TokenID)
	}

	// WithAuth injects scope, token, author.
	ctx = WithAuth(ctx, domain.MCPScopeWrite, 123, 456)
	auth = authFromContext(ctx)
	if auth.Scope != domain.MCPScopeWrite {
		t.Errorf("injected scope = %q, want write", auth.Scope)
	}
	if auth.TokenID != 123 {
		t.Errorf("injected token_id = %d, want 123", auth.TokenID)
	}
	if auth.AuthorID != 456 {
		t.Errorf("injected author_id = %d, want 456", auth.AuthorID)
	}
	if !auth.Scope.CanWrite() {
		t.Error("write scope should return CanWrite() = true")
	}
}

// --- callTool scope gate tests ---

func TestCallToolReadScopeRejectsWrite(t *testing.T) {
	srv := newTestServer(t)
	ctx := WithAuth(context.Background(), domain.MCPScopeRead, 1, 1)

	// create_entry rejected with read scope.
	_, err := srv.callTool(ctx, "create_entry", json.RawMessage(`{"title":"x","body":"x"}`))
	if err == nil {
		t.Error("expected error for create_entry with read scope")
	}
	if err != nil && err.Error() != `tool "create_entry" requires write scope (token has "read")` {
		t.Errorf("unexpected error: %v", err)
	}

	// publish_entry rejected.
	_, err = srv.callTool(ctx, "publish_entry", json.RawMessage(`{"id":1}`))
	if err == nil {
		t.Error("expected error for publish_entry with read scope")
	}
	if err != nil && err.Error() != `tool "publish_entry" requires write scope (token has "read")` {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- callTool argument validation tests ---

func TestCallToolGetEntryInvalidArgs(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background() // read scope by default

	// No id and no slug.
	_, err := srv.callTool(ctx, "get_entry", json.RawMessage(`{}`))
	if err == nil {
		t.Error("expected error for get_entry without id or slug")
	}
	if err != nil && err.Error() != "either id or slug is required" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCallToolGetEntryNotFound(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()

	_, err := srv.callTool(ctx, "get_entry", json.RawMessage(`{"id":999999}`))
	if err == nil {
		t.Error("expected error for missing entry")
	}
	if err != nil && err.Error() != "entry not found" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCallToolSearchEntriesInvalidArgs(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()

	// Missing query.
	_, err := srv.callTool(ctx, "search_entries", json.RawMessage(`{}`))
	if err == nil {
		t.Error("expected error for search_entries without query")
	}
	if err != nil && err.Error() != "query is required" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCallToolUnknownTool(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()

	_, err := srv.callTool(ctx, "non_existent_tool", nil)
	if err == nil {
		t.Error("expected error for unknown tool")
	}
	if err != nil && err.Error() != "unknown tool: non_existent_tool" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCallToolUploadImageNilStore(t *testing.T) {
	srv := newTestServer(t)
	srv.ImageStore = nil // explicitly nil
	ctx := WithAuth(context.Background(), domain.MCPScopeWrite, 1, 1)

	_, err := srv.callTool(ctx, "upload_image", json.RawMessage(`{"data":"dGVzdA=="}`))
	if err == nil {
		t.Error("expected error for upload_image with nil ImageStore")
	}
	if err != nil && err.Error() != "upload_image: server has no image store configured" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCallToolGetEntryBySlug(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()

	// Slug lookup on empty DB returns "entry not found".
	_, err := srv.callTool(ctx, "get_entry", json.RawMessage(`{"slug":"no-such-slug"}`))
	if err == nil {
		t.Error("expected error for missing slug")
	}
	if err != nil && err.Error() != "entry not found" {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- toolDescriptors scope filtering edge cases ---

func TestToolDescriptorsNoWriteToolsForReadScope(t *testing.T) {
	descs := toolDescriptors(domain.MCPScopeRead)
	writeNames := map[string]bool{
		"create_entry": true, "update_entry": true,
		"publish_entry": true, "upload_image": true,
	}
	for _, d := range descs {
		if writeNames[d.Name] {
			t.Errorf("write tool %q present in read-scope descriptor list", d.Name)
		}
	}
}

// --- MCPScope type tests ---

func TestMCPScopeValid(t *testing.T) {
	if !domain.MCPScopeRead.Valid() {
		t.Error("read scope should be valid")
	}
	if !domain.MCPScopeWrite.Valid() {
		t.Error("write scope should be valid")
	}
	if domain.MCPScope("invalid").Valid() {
		t.Error("invalid scope should not be valid")
	}
}

func TestMCPScopeCanWrite(t *testing.T) {
	if domain.MCPScopeRead.CanWrite() {
		t.Error("read scope should not CanWrite()")
	}
	if !domain.MCPScopeWrite.CanWrite() {
		t.Error("write scope should CanWrite()")
	}
}
