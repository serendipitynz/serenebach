// Package repo holds the hand-written SQL queries the public and admin layers
// call. We stay on database/sql for now; migrating to sqlc is a later call
// once the query set gets large enough to justify codegen.
package repo

import (
	"database/sql"
	"errors"
	"strings"
)

type Store struct {
	db *sql.DB
}

func New(db *sql.DB) *Store { return &Store{db: db} }

// DB exposes the underlying connection for callers that need to join
// against main-DB tables (e.g. analytics reading likes_count /
// stamps_count from the external analytics DB). Kept internal-only —
// don't hand this pointer to handlers; use it from sibling packages
// that genuinely need cross-DB queries.
func (s *Store) DB() *sql.DB { return s.db }

var ErrNotFound = errors.New("repo: not found")

// ErrSlugInUse is returned when a CreateEntry / UpdateEntry call would
// violate the partial unique index on (wid, slug). Callers (the admin
// handler) catch this and re-render the form with a validation message.
var ErrSlugInUse = errors.New("repo: slug already in use")

// ErrSlugPrefixConflict is returned when a page slug would nest inside
// or envelop an existing slug (e.g. /service and /service/pricing).
// Unlike ErrSlugInUse this is checked in Go, not the DB layer.
var ErrSlugPrefixConflict = errors.New("repo: slug prefix conflict")

// defaultDescFormat applies the "empty → html" fallback for
// description_format columns. Keeps every call site consistent so
// a missing value never lands in the DB as raw "".
func defaultDescFormat(s string) string {
	if s == "" {
		return "html"
	}
	return s
}

// isUniqueViolation reports whether err is SQLite's "UNIQUE constraint
// failed" error. modernc.org/sqlite surfaces this through error text
// rather than a typed code, so we sniff the string — the narrow match
// here means an unrelated constraint doesn't get remapped to ErrSlugInUse.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "constraint failed (unique)")
}
