package repo

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/serendipitynz/serenebach/internal/domain"
)

// mcpTokenColumns is the canonical column list for the mcp_tokens table.
// Order must match the inline Scan call sites in ListMCPTokens and
// MCPTokenByHash.
const mcpTokenColumns = `id, wid, name, token_hash, prefix, scope, author_id, created_at, last_used_at, revoked_at`

// HashMCPToken returns the canonical sha256 hex digest the repo uses to
// look up an MCP token. Exposed so HTTP middleware can hash incoming
// Authorization headers before querying.
func HashMCPToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// CreateMCPToken persists a new token row. Caller supplies the raw
// token string (already generated), the scope ("read" / "write"),
// and the author id the token is bound to; the repo hashes the raw
// value and stores only the digest + a 12-char display prefix.
func (s *Store) CreateMCPToken(ctx context.Context, wid int64, name, rawToken string, scope domain.MCPScope, authorID int64) (int64, error) {
	if name == "" || rawToken == "" {
		return 0, fmt.Errorf("repo: CreateMCPToken: name and token required")
	}
	if !scope.Valid() {
		return 0, fmt.Errorf("repo: CreateMCPToken: invalid scope %q", scope)
	}
	if authorID <= 0 {
		return 0, fmt.Errorf("repo: CreateMCPToken: author_id required")
	}
	hash := HashMCPToken(rawToken)
	prefix := rawToken
	if len(prefix) > 12 {
		prefix = prefix[:12]
	}
	now := time.Now().Unix()
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO mcp_tokens (wid, name, token_hash, prefix, scope, author_id, created_at, last_used_at, revoked_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 0, 0)`,
		wid, name, hash, prefix, string(scope), authorID, now)
	if err != nil {
		return 0, fmt.Errorf("repo: CreateMCPToken: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("repo: CreateMCPToken lastid: %w", err)
	}
	return id, nil
}

// ListMCPTokens returns every token (active + revoked) for the weblog,
// newest-first. The admin UI shows both so an operator can confirm
// that a previously-revoked token is actually dead.
func (s *Store) ListMCPTokens(ctx context.Context, wid int64) ([]domain.MCPToken, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+mcpTokenColumns+`
		FROM mcp_tokens WHERE wid = ?
		ORDER BY created_at DESC, id DESC`, wid)
	if err != nil {
		return nil, fmt.Errorf("repo: ListMCPTokens: %w", err)
	}
	defer rows.Close()
	var out []domain.MCPToken
	for rows.Next() {
		var t domain.MCPToken
		var scope string
		if err := rows.Scan(&t.ID, &t.WID, &t.Name, &t.TokenHash, &t.Prefix, &scope, &t.AuthorID,
			&t.CreatedAt, &t.LastUsedAt, &t.RevokedAt); err != nil {
			return nil, fmt.Errorf("repo: ListMCPTokens scan: %w", err)
		}
		t.Scope = domain.MCPScope(scope)
		if !t.Scope.Valid() {
			t.Scope = domain.MCPScopeRead
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// MCPTokenByHash looks up an active token by its sha256 digest. Returns
// ErrNotFound on miss or if the token is revoked, so the /mcp handler
// can treat both cases uniformly as "401 unauthorized".
func (s *Store) MCPTokenByHash(ctx context.Context, hash string) (*domain.MCPToken, error) {
	var t domain.MCPToken
	var scope string
	err := s.db.QueryRowContext(ctx, `
		SELECT `+mcpTokenColumns+`
		FROM mcp_tokens WHERE token_hash = ? AND revoked_at = 0`, hash).
		Scan(&t.ID, &t.WID, &t.Name, &t.TokenHash, &t.Prefix, &scope, &t.AuthorID,
			&t.CreatedAt, &t.LastUsedAt, &t.RevokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("repo: MCPTokenByHash: %w", err)
	}
	t.Scope = domain.MCPScope(scope)
	if !t.Scope.Valid() {
		t.Scope = domain.MCPScopeRead
	}
	return &t, nil
}

// TouchMCPToken advances last_used_at to the current time. Best-effort:
// an error here is logged by the caller but doesn't fail the request —
// "last used" is observational data, not a correctness invariant.
func (s *Store) TouchMCPToken(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE mcp_tokens SET last_used_at = ? WHERE id = ?`,
		time.Now().Unix(), id)
	if err != nil {
		return fmt.Errorf("repo: TouchMCPToken: %w", err)
	}
	return nil
}

// RevokeMCPToken marks the token as no longer usable. We keep the row
// around rather than deleting so the admin list can still show what
// names were in play. To permanently clean up, call DeleteMCPToken.
func (s *Store) RevokeMCPToken(ctx context.Context, wid, id int64) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE mcp_tokens SET revoked_at = ? WHERE wid = ? AND id = ? AND revoked_at = 0`,
		time.Now().Unix(), wid, id)
	if err != nil {
		return fmt.Errorf("repo: RevokeMCPToken: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
