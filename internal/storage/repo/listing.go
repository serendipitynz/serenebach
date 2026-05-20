package repo

import "strings"

// SortDir is a typed sort direction so handlers can't smuggle raw SQL
// fragments into ORDER BY. The zero value is descending — the most
// common default for time-series admin lists (newest first).
type SortDir int

const (
	SortDesc SortDir = iota
	SortAsc
)

// String renders the direction as an uppercase SQL keyword. Safe to
// concatenate into a query string because the value space is closed.
func (d SortDir) String() string {
	if d == SortAsc {
		return "ASC"
	}
	return "DESC"
}

// ParseSortDir maps a user-supplied "asc" / "desc" (case-insensitive)
// to SortDir. Anything else falls back to SortDesc — admin lists must
// never 404 on a malformed query string.
func ParseSortDir(s string) SortDir {
	if strings.EqualFold(s, "asc") {
		return SortAsc
	}
	return SortDesc
}

// escapeLike escapes the SQL LIKE metacharacters %, _, and \ so a
// user-supplied search term matches literally. Pair with `ESCAPE '\'`
// in the SQL fragment, e.g.
//
//	... LIKE '%' || ? || '%' ESCAPE '\'
//
// Without this, a query like "100%" would match every row that
// contained "100" instead of literally "100%".
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// NormalizeSearch trims leading/trailing whitespace and collapses any
// internal runs of whitespace into single spaces. A blank-only query
// becomes the empty string so the repo layer can treat "no filter"
// uniformly.
func NormalizeSearch(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return strings.Join(strings.Fields(s), " ")
}
