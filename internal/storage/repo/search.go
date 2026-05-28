package repo

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/serendipitynz/serenebach/internal/domain"
)

// trigramMinLen is FTS5 trigram tokenizer's minimum substring length.
// Tokens shorter than this can't be expressed as a MATCH phrase and are
// instead routed through a bounded LIKE on the base columns (see
// LikeNeedles).
const trigramMinLen = 3

// ToFTSQuery turns a normalized user query into an FTS5 MATCH expression
// for the trigram tokenizer. Each whitespace-separated token of length
// >= trigramMinLen is wrapped in a double-quote phrase and AND-joined
// (FTS5 default conjunction). Phrase wrapping makes every reserved
// operator (`-` `:` `(` `)` `*` `+` `#` `AND` `OR` `NOT`) literal, so
// `node:fs`, `foo(bar)`, `A*B`, `C++`, `max-age`, and `cats AND dogs`
// all become substring searches. The only character that can break out
// of a phrase is `"`, which is doubled per FTS5 escaping rules.
//
// Tokens shorter than trigramMinLen are dropped here — they cannot be
// expressed in a trigram MATCH. LikeNeedles is the caller's recourse
// for those. Returns "" when no token qualifies, signalling "no MATCH
// clause needed".
func ToFTSQuery(s string) string {
	s = NormalizeSearch(s)
	if s == "" {
		return ""
	}
	out := make([]string, 0, 4)
	for _, raw := range strings.Fields(s) {
		if utf8.RuneCountInString(raw) < trigramMinLen {
			continue
		}
		t := strings.ReplaceAll(raw, `"`, `""`)
		out = append(out, `"`+t+`"`)
	}
	return strings.Join(out, " ")
}

// LikeNeedles turns the 1..trigramMinLen-1 character tokens of a query
// into `%token%` LIKE needles. Used to recover 1- or 2-character tokens
// (typical of Japanese: 2-character compounds like 東京 / 大阪) that the
// trigram MATCH path can't express. Wildcards inside the user input
// (`%` `_` `\`) are escaped so the needle searches literally; pair with
// `LIKE ? ESCAPE '\'` in the SQL fragment. Tokens of length >= trigramMinLen
// are NOT returned — the MATCH path covers those — and a query whose
// every token is long enough therefore returns nil.
func LikeNeedles(s string) []string {
	s = NormalizeSearch(s)
	if s == "" {
		return nil
	}
	var out []string
	for _, raw := range strings.Fields(s) {
		if utf8.RuneCountInString(raw) >= trigramMinLen {
			continue
		}
		out = append(out, "%"+escapeLike(raw)+"%")
	}
	return out
}

// HasSearchTerms reports whether the query has any usable token —
// either a >=3-character MATCH token or a 1..2-character LIKE token.
// The public /search handler uses this to distinguish "no query
// entered (show guidance)" from "query entered but matched nothing
// (show 0-results)".
func HasSearchTerms(s string) bool {
	return ToFTSQuery(s) != "" || len(LikeNeedles(s)) > 0
}

// SearchPublicOptions configures SearchEntriesPublic. The Query is
// taken as the user's normalised raw input; the repo decides whether
// to drive the search from the FTS index, base-column LIKE, or both,
// based on the token-length breakdown.
type SearchPublicOptions struct {
	WID      int64
	Query    string
	Page     int
	PageSize int
}

// SearchResult is the page slice + pagination context for a public
// search query.
type SearchResult struct {
	Entries  []domain.Entry
	Total    int
	Page     int
	PageSize int
}

// SearchEntriesPublic runs the user's query against the published-entry
// surface — status = 1, hidden categories excluded — and returns the
// requested page plus the total match count. Tokens >= trigramMinLen
// drive an FTS5 MATCH on entries_fts; shorter tokens AND in a bounded
// LIKE on the base columns (see LikeNeedles). Returns an empty result
// (not an error) when the query produces no usable tokens.
func (s *Store) SearchEntriesPublic(ctx context.Context, opts SearchPublicOptions) (*SearchResult, error) {
	page := opts.Page
	if page < 1 {
		page = 1
	}
	pageSize := opts.PageSize
	if pageSize <= 0 {
		pageSize = 10
	}
	result := &SearchResult{
		Page:     page,
		PageSize: pageSize,
	}

	ftsQuery := ToFTSQuery(opts.Query)
	likeNeedles := LikeNeedles(opts.Query)
	if ftsQuery == "" && len(likeNeedles) == 0 {
		return result, nil
	}

	whereSQL, whereArgs := buildPublicSearchWhere(opts.WID, ftsQuery, likeNeedles)
	from := buildPublicSearchFrom(ftsQuery)

	var total int
	countSQL := "SELECT COUNT(*) " + from + whereSQL
	if err := s.db.QueryRowContext(ctx, countSQL, whereArgs...).Scan(&total); err != nil {
		return nil, fmt.Errorf("repo: SearchEntriesPublic count: %w", err)
	}
	result.Total = total

	listArgs := append([]any{}, whereArgs...)
	listSQL := "SELECT " + entryColumnsE + " " + from + whereSQL +
		" ORDER BY e.posted_at DESC, e.id DESC LIMIT ? OFFSET ?"
	listArgs = append(listArgs, pageSize, (page-1)*pageSize)

	rows, err := s.db.QueryContext(ctx, listSQL, listArgs...)
	if err != nil {
		return nil, fmt.Errorf("repo: SearchEntriesPublic list: %w", err)
	}
	defer rows.Close()
	entries, err := scanEntries(rows)
	if err != nil {
		return nil, fmt.Errorf("repo: SearchEntriesPublic scan: %w", err)
	}
	result.Entries = entries
	return result, nil
}

// buildPublicSearchFrom picks the FROM/JOIN shape based on whether an
// FTS MATCH clause is active. When MATCH is in play, drive from
// entries_fts so the planner uses the trigram index; otherwise scan
// the base entries table.
func buildPublicSearchFrom(ftsQuery string) string {
	if ftsQuery != "" {
		return `FROM entries_fts f
JOIN entries e ON e.id = f.rowid
LEFT JOIN categories c ON c.id = e.category_id `
	}
	return `FROM entries e
LEFT JOIN categories c ON c.id = e.category_id `
}

// buildPublicSearchWhere assembles the WHERE fragment + args for the
// public search query. wid / status / hidden-category filters always
// apply; the MATCH and LIKE clauses gate on whether the caller has
// any tokens of the corresponding length class.
func buildPublicSearchWhere(wid int64, ftsQuery string, likeNeedles []string) (string, []any) {
	var b strings.Builder
	b.WriteString(`WHERE e.wid = ? AND e.status = 1 AND (c.hidden IS NULL OR c.hidden = 0)`)
	args := []any{wid}
	if ftsQuery != "" {
		b.WriteString(` AND f.entries_fts MATCH ?`)
		args = append(args, ftsQuery)
	}
	for _, n := range likeNeedles {
		b.WriteString(` AND (e.title LIKE ? ESCAPE '\' OR e.body LIKE ? ESCAPE '\' OR e.more LIKE ? ESCAPE '\' OR e.keywords LIKE ? ESCAPE '\')`)
		args = append(args, n, n, n, n)
	}
	return b.String(), args
}

// RebuildFTSIndex tears down and reinstalls every row in entries_fts
// from the base entries table. Normal INSERT / UPDATE / DELETE keep
// the index in sync via triggers, so this is a repair CLI for when an
// operator suspects drift (manual DB edits, trigger drop, or a
// tokenizer change). No wid argument because entries_fts is unscoped —
// all weblogs share one index, matching the single-weblog footprint
// of the rest of the codebase.
func (s *Store) RebuildFTSIndex(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM entries_fts`); err != nil {
		return fmt.Errorf("repo: RebuildFTSIndex clear: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO entries_fts (rowid, title, body, more, keywords)
		SELECT id, title, body, COALESCE(more,''), COALESCE(keywords,'') FROM entries`); err != nil {
		return fmt.Errorf("repo: RebuildFTSIndex insert: %w", err)
	}
	return nil
}
