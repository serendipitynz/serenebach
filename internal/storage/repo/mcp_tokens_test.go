package repo

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/serendipitynz/serenebach/internal/domain"
)

func TestMCPTokenCRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	rawToken := "sb_mcp_secret_token_123"

	id := mcpTokenCRUDCreate(t, ctx, s, rawToken)
	mcpTokenCRUDList(t, ctx, s)
	mcpTokenCRUDByHash(t, ctx, s, rawToken, id)
	mcpTokenCRUDTouch(t, ctx, s, id)
	mcpTokenCRUDRevoke(t, ctx, s, rawToken, id)
}

func mcpTokenCRUDCreate(t *testing.T, ctx context.Context, s *Store, rawToken string) int64 {
	t.Helper()
	id, err := s.CreateMCPToken(ctx, 1, "Test Token", rawToken, domain.MCPScopeRead, 1)
	if err != nil {
		t.Fatalf("CreateMCPToken: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero id")
	}
	return id
}

func mcpTokenCRUDList(t *testing.T, ctx context.Context, s *Store) {
	t.Helper()
	tokens, err := s.ListMCPTokens(ctx, 1, ListMCPTokensQuery{})
	if err != nil {
		t.Fatalf("ListMCPTokens: %v", err)
	}
	if len(tokens) != 1 {
		t.Fatalf("ListMCPTokens len = %d, want 1", len(tokens))
	}
	assertMCPTokenFields(t, tokens[0])
}

func assertMCPTokenFields(t *testing.T, tok domain.MCPToken) {
	t.Helper()
	if tok.Name != "Test Token" {
		t.Errorf("name = %q, want Test Token", tok.Name)
	}
	if tok.Scope != domain.MCPScopeRead {
		t.Errorf("scope = %q, want read", tok.Scope)
	}
	if tok.AuthorID != 1 {
		t.Errorf("author_id = %d, want 1", tok.AuthorID)
	}
	if tok.Prefix == "" || len(tok.Prefix) > 12 {
		t.Errorf("prefix = %q, want 1-12 chars", tok.Prefix)
	}
	if !tok.Active() {
		t.Error("expected token to be Active()")
	}
}

func mcpTokenCRUDByHash(t *testing.T, ctx context.Context, s *Store, rawToken string, wantID int64) {
	t.Helper()
	hash := HashMCPToken(rawToken)
	found, err := s.MCPTokenByHash(ctx, hash)
	if err != nil {
		t.Fatalf("MCPTokenByHash: %v", err)
	}
	if found.ID != wantID {
		t.Errorf("MCPTokenByHash id = %d, want %d", found.ID, wantID)
	}
}

func mcpTokenCRUDTouch(t *testing.T, ctx context.Context, s *Store, id int64) {
	t.Helper()
	if err := s.TouchMCPToken(ctx, id); err != nil {
		t.Fatalf("TouchMCPToken: %v", err)
	}
	tokens, _ := s.ListMCPTokens(ctx, 1, ListMCPTokensQuery{})
	if tokens[0].LastUsedAt == 0 {
		t.Error("expected LastUsedAt > 0 after TouchMCPToken")
	}
}

func mcpTokenCRUDRevoke(t *testing.T, ctx context.Context, s *Store, rawToken string, id int64) {
	t.Helper()
	if err := s.RevokeMCPToken(ctx, 1, id); err != nil {
		t.Fatalf("RevokeMCPToken: %v", err)
	}
	tokens, _ := s.ListMCPTokens(ctx, 1, ListMCPTokensQuery{})
	if tokens[0].Active() {
		t.Error("expected token to be inactive after revoke")
	}
	if _, err := s.MCPTokenByHash(ctx, HashMCPToken(rawToken)); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound for revoked token, got %v", err)
	}
}

func TestMCPTokenWriteScope(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	rawToken := "sb_mcp_write_token"
	_, err := s.CreateMCPToken(ctx, 1, "Write Token", rawToken, domain.MCPScopeWrite, 1)
	if err != nil {
		t.Fatalf("CreateMCPToken write: %v", err)
	}

	hash := HashMCPToken(rawToken)
	tok, err := s.MCPTokenByHash(ctx, hash)
	if err != nil {
		t.Fatalf("MCPTokenByHash: %v", err)
	}
	if tok.Scope != domain.MCPScopeWrite {
		t.Errorf("scope = %q, want write", tok.Scope)
	}
	if !tok.Scope.CanWrite() {
		t.Error("expected CanWrite() = true")
	}
	if !tok.Scope.Valid() {
		t.Error("expected Valid() = true")
	}
}

func TestMCPTokenReadScopeCannotWrite(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	rawToken := "sb_mcp_read_token"
	_, err := s.CreateMCPToken(ctx, 1, "Read Token", rawToken, domain.MCPScopeRead, 1)
	if err != nil {
		t.Fatalf("CreateMCPToken read: %v", err)
	}

	hash := HashMCPToken(rawToken)
	tok, err := s.MCPTokenByHash(ctx, hash)
	if err != nil {
		t.Fatalf("MCPTokenByHash: %v", err)
	}
	if tok.Scope.CanWrite() {
		t.Error("expected CanWrite() = false for read scope")
	}
}

func TestMCPTokenWIDScoping(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	raw1 := "sb_mcp_token_wid1"
	_, err := s.CreateMCPToken(ctx, 1, "W1 Token", raw1, domain.MCPScopeRead, 1)
	if err != nil {
		t.Fatalf("CreateMCPToken wid=1: %v", err)
	}
	raw2 := "sb_mcp_token_wid2"
	_, err = s.CreateMCPToken(ctx, 2, "W2 Token", raw2, domain.MCPScopeRead, 1)
	if err != nil {
		t.Fatalf("CreateMCPToken wid=2: %v", err)
	}

	// ListMCPTokens should be scoped
	tokens1, _ := s.ListMCPTokens(ctx, 1, ListMCPTokensQuery{})
	if len(tokens1) != 1 || tokens1[0].Name != "W1 Token" {
		t.Errorf("ListMCPTokens wid=1 invalid")
	}
	tokens2, _ := s.ListMCPTokens(ctx, 2, ListMCPTokensQuery{})
	if len(tokens2) != 1 || tokens2[0].Name != "W2 Token" {
		t.Errorf("ListMCPTokens wid=2 invalid")
	}
}

func TestMCPTokenNotFoundErrors(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	_, err := s.MCPTokenByHash(ctx, HashMCPToken("nonexistent-token"))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound for MCPTokenByHash, got %v", err)
	}

	err = s.RevokeMCPToken(ctx, 1, 9999)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound for RevokeMCPToken, got %v", err)
	}
}

func TestMCPTokenCreateValidation(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// Empty name
	_, err := s.CreateMCPToken(ctx, 1, "", "token", domain.MCPScopeRead, 1)
	if err == nil {
		t.Error("expected error for empty name")
	}

	// Empty raw token
	_, err = s.CreateMCPToken(ctx, 1, "Name", "", domain.MCPScopeRead, 1)
	if err == nil {
		t.Error("expected error for empty token")
	}

	// Invalid scope
	_, err = s.CreateMCPToken(ctx, 1, "Name", "token", "invalid-scope", 1)
	if err == nil {
		t.Error("expected error for invalid scope")
	}

	// Invalid author_id
	_, err = s.CreateMCPToken(ctx, 1, "Name", "token", domain.MCPScopeRead, 0)
	if err == nil {
		t.Error("expected error for author_id <= 0")
	}
}

func TestMCPTokenHashDeterminism(t *testing.T) {
	raw := "test-token-for-hash"
	h1 := HashMCPToken(raw)
	h2 := HashMCPToken(raw)
	if h1 != h2 {
		t.Errorf("hash is not deterministic: %q vs %q", h1, h2)
	}
	if h1 == "" {
		t.Error("hash should not be empty")
	}
}

func TestMCPTokenPrefix(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	rawToken := "abcdefghijklmnop" // 16 chars
	_, err := s.CreateMCPToken(ctx, 1, "Prefix Test", rawToken, domain.MCPScopeRead, 1)
	if err != nil {
		t.Fatalf("CreateMCPToken: %v", err)
	}

	tokens, _ := s.ListMCPTokens(ctx, 1, ListMCPTokensQuery{})
	if tokens[0].Prefix != "abcdefghijkl" {
		t.Errorf("prefix = %q, want abcdefghijkl (first 12 chars)", tokens[0].Prefix)
	}
}

func TestMCPTokenListOrdering(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	ids := make([]int64, 3)
	for i := 0; i < 3; i++ {
		id, err := s.CreateMCPToken(ctx, 1, fmt.Sprintf("Token %d", i),
			fmt.Sprintf("sb_order_%d", i), domain.MCPScopeRead, 1)
		if err != nil {
			t.Fatalf("CreateMCPToken %d: %v", i, err)
		}
		ids[i] = id
	}

	tokens, err := s.ListMCPTokens(ctx, 1, ListMCPTokensQuery{})
	if err != nil {
		t.Fatalf("ListMCPTokens: %v", err)
	}
	if len(tokens) != 3 {
		t.Fatalf("len = %d, want 3", len(tokens))
	}

	// newest-first: created_at DESC, id DESC. Within the same second (same
	// created_at), ids should appear in descending order — the most recently
	// created token (highest id) comes first.
	if tokens[0].ID != ids[2] || tokens[1].ID != ids[1] || tokens[2].ID != ids[0] {
		t.Errorf("order = %d %d %d, want %d %d %d (newest first)",
			tokens[0].ID, tokens[1].ID, tokens[2].ID, ids[2], ids[1], ids[0])
	}
}

func TestMCPTokenScopeReadList(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// Create both read and write tokens
	_, err := s.CreateMCPToken(ctx, 1, "Read Token", "sb_read_1", domain.MCPScopeRead, 1)
	if err != nil {
		t.Fatalf("CreateMCPToken read: %v", err)
	}
	_, err = s.CreateMCPToken(ctx, 1, "Write Token", "sb_write_1", domain.MCPScopeWrite, 1)
	if err != nil {
		t.Fatalf("CreateMCPToken write: %v", err)
	}

	tokens, _ := s.ListMCPTokens(ctx, 1, ListMCPTokensQuery{})
	if len(tokens) != 2 {
		t.Fatalf("len = %d, want 2", len(tokens))
	}

	foundRead := false
	foundWrite := false
	for _, tok := range tokens {
		switch tok.Scope {
		case domain.MCPScopeRead:
			foundRead = true
		case domain.MCPScopeWrite:
			foundWrite = true
		}
	}
	if !foundRead {
		t.Error("read-scoped token not found in list")
	}
	if !foundWrite {
		t.Error("write-scoped token not found in list")
	}
}

func TestMCPTokenRevokedListStillShowsIt(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	id, _ := s.CreateMCPToken(ctx, 1, "Revoked Token", "sb_revoked", domain.MCPScopeRead, 1)
	s.RevokeMCPToken(ctx, 1, id)

	// Revoked tokens still appear in ListMCPTokens so admin can audit
	tokens, _ := s.ListMCPTokens(ctx, 1, ListMCPTokensQuery{})
	if len(tokens) != 1 {
		t.Fatalf("expected 1 token in list even after revoke, got %d", len(tokens))
	}
	if tokens[0].Active() {
		t.Error("revoked token should not be Active()")
	}
	if tokens[0].RevokedAt == 0 {
		t.Error("expected non-zero RevokedAt")
	}
}
