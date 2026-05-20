package repo

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
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

// MCPTokenSortKey is a typed enum of the columns the admin token list
// can sort by. Default is created_at DESC so an operator landing on
// the page sees the most-recently-issued tokens first.
type MCPTokenSortKey int

const (
	MCPTokenSortCreatedAt MCPTokenSortKey = iota // default
	MCPTokenSortName
	MCPTokenSortScope
	MCPTokenSortAuthor
	MCPTokenSortLastUsed
	MCPTokenSortRevoked
)

func (k MCPTokenSortKey) col() string {
	switch k {
	case MCPTokenSortName:
		return "name"
	case MCPTokenSortScope:
		return "scope"
	case MCPTokenSortAuthor:
		return "author_id"
	case MCPTokenSortLastUsed:
		return "last_used_at"
	case MCPTokenSortRevoked:
		return "revoked_at"
	default:
		return "created_at"
	}
}

// String returns the URL-form name of the sort key.
func (k MCPTokenSortKey) String() string {
	switch k {
	case MCPTokenSortName:
		return "name"
	case MCPTokenSortScope:
		return "scope"
	case MCPTokenSortAuthor:
		return "author"
	case MCPTokenSortLastUsed:
		return "lastUsed"
	case MCPTokenSortRevoked:
		return "revoked"
	default:
		return "created"
	}
}

// ParseMCPTokenSortKey maps a ?sort= query value to the enum. Both
// "" and "created" land on MCPTokenSortCreatedAt so the default-
// landing URL and an explicit ?sort=created produce the same value.
func ParseMCPTokenSortKey(s string) MCPTokenSortKey {
	switch s {
	case "name":
		return MCPTokenSortName
	case "scope":
		return MCPTokenSortScope
	case "author":
		return MCPTokenSortAuthor
	case "lastUsed":
		return MCPTokenSortLastUsed
	case "revoked":
		return MCPTokenSortRevoked
	default:
		return MCPTokenSortCreatedAt
	}
}

// zeroLast reports whether this column stores 0 as "never used /
// never revoked" — those rows should always sort to the bottom
// regardless of direction. last_used_at and revoked_at do; the rest
// are real values everywhere.
func (k MCPTokenSortKey) zeroLast() bool {
	return k == MCPTokenSortLastUsed || k == MCPTokenSortRevoked
}

// ListMCPTokensQuery bundles the admin token list's sort parameters.
// No search / paging — the list is small (admin-issued tokens).
type ListMCPTokensQuery struct {
	SortBy  MCPTokenSortKey
	SortDir SortDir
}

// ListMCPTokens returns every token (active + revoked) for the weblog
// in the order requested by q. The admin UI shows both active and
// revoked rows so an operator can confirm that a previously-revoked
// token is actually dead.
func (s *Store) ListMCPTokens(ctx context.Context, wid int64, q ListMCPTokensQuery) ([]domain.MCPToken, error) {
	var b strings.Builder
	b.WriteString(`SELECT ` + mcpTokenColumns + ` FROM mcp_tokens WHERE wid = ? ORDER BY `)
	col := q.SortBy.col()
	if q.SortBy.zeroLast() {
		// Rows where the column is 0 ("never used" / "never revoked")
		// sort to the bottom both ways — those tokens have no
		// timestamp to compare against the rest meaningfully.
		fmt.Fprintf(&b, `CASE WHEN %s = 0 THEN 1 ELSE 0 END, %s %s`, col, col, q.SortDir.String())
	} else {
		b.WriteString(col)
		b.WriteByte(' ')
		b.WriteString(q.SortDir.String())
	}
	// Stable tie-breaker.
	b.WriteString(`, id DESC`)
	rows, err := s.db.QueryContext(ctx, b.String(), wid)
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
