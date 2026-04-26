package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// WeblogLegacyURL holds the SB3 URL-shaping settings recorded by the
// importer. The redirect layer reads this to reconstruct old Perl URL
// patterns; nothing else in the system should consume these fields.
type WeblogLegacyURL struct {
	ArchiveType string
	LogPath     string
	BasePath    string
	CgiName     string
	IDPrefix    string
	Suffix      string
}

// HasAny reports whether at least one field is populated. The redirect
// middleware uses this to decide if the cached config is worth checking
// against incoming requests.
func (l WeblogLegacyURL) HasAny() bool {
	return l.ArchiveType != "" || l.LogPath != "" || l.BasePath != "" ||
		l.CgiName != "" || l.IDPrefix != "" || l.Suffix != ""
}

// LegacyEntryRef is what the redirect layer needs to build a canonical
// entry URL: the new id and slug. Slug wins when non-empty so the redirect
// lands on the same surface the rest of the site links to.
type LegacyEntryRef struct {
	ID   int64
	Slug string
}

// WeblogLegacyURLByID loads the legacy URL settings for one weblog. An
// all-zero result is normal (the weblog was never imported from SB3) and
// not an error.
func (s *Store) WeblogLegacyURLByID(ctx context.Context, wid int64) (WeblogLegacyURL, error) {
	var l WeblogLegacyURL
	err := s.db.QueryRowContext(ctx, `
		SELECT legacy_archive_type, legacy_log_path, legacy_base_path,
		       legacy_cgi_name, legacy_id_prefix, legacy_suffix
		FROM weblogs WHERE id = ?`, wid).Scan(
		&l.ArchiveType, &l.LogPath, &l.BasePath, &l.CgiName, &l.IDPrefix, &l.Suffix)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return l, ErrNotFound
		}
		return l, fmt.Errorf("repo: WeblogLegacyURLByID: %w", err)
	}
	return l, nil
}

// EntryByLegacyID resolves an SB3 entry_id to the destination entry's
// id + slug. ErrNotFound when no row carries the requested legacy_id —
// the redirect handler then 404s rather than guessing.
func (s *Store) EntryByLegacyID(ctx context.Context, wid, legacyID int64) (LegacyEntryRef, error) {
	var ref LegacyEntryRef
	err := s.db.QueryRowContext(ctx, `
		SELECT id, slug FROM entries
		WHERE wid = ? AND legacy_id = ?
		LIMIT 1`, wid, legacyID).Scan(&ref.ID, &ref.Slug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ref, ErrNotFound
		}
		return ref, fmt.Errorf("repo: EntryByLegacyID: %w", err)
	}
	return ref, nil
}

// EntryByLegacyFile resolves an SB3 entry_file (custom save name) to the
// destination entry. The empty-string path is rejected up-front so a
// stray request like /log/.html cannot match every entry that left
// legacy_file blank.
func (s *Store) EntryByLegacyFile(ctx context.Context, wid int64, file string) (LegacyEntryRef, error) {
	var ref LegacyEntryRef
	if file == "" {
		return ref, ErrNotFound
	}
	err := s.db.QueryRowContext(ctx, `
		SELECT id, slug FROM entries
		WHERE wid = ? AND legacy_file = ?
		LIMIT 1`, wid, file).Scan(&ref.ID, &ref.Slug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ref, ErrNotFound
		}
		return ref, fmt.Errorf("repo: EntryByLegacyFile: %w", err)
	}
	return ref, nil
}

// CategoryIDByLegacyID resolves an SB3 category_id to the destination
// category id.
func (s *Store) CategoryIDByLegacyID(ctx context.Context, wid, legacyID int64) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `
		SELECT id FROM categories
		WHERE wid = ? AND legacy_id = ?
		LIMIT 1`, wid, legacyID).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, fmt.Errorf("repo: CategoryIDByLegacyID: %w", err)
	}
	return id, nil
}

// CategoryIDByLegacyDir resolves an SB3 category_dir to the destination
// category id. Empty input is rejected for the same reason as
// EntryByLegacyFile — a default-only-dir blog leaves every row with the
// global log_path, and we don't want a bare hit at the log root to map
// to an arbitrary category.
func (s *Store) CategoryIDByLegacyDir(ctx context.Context, wid int64, dir string) (int64, error) {
	if dir == "" {
		return 0, ErrNotFound
	}
	var id int64
	err := s.db.QueryRowContext(ctx, `
		SELECT id FROM categories
		WHERE wid = ? AND legacy_dir = ?
		LIMIT 1`, wid, dir).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, fmt.Errorf("repo: CategoryIDByLegacyDir: %w", err)
	}
	return id, nil
}
