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

// LegacyCategoryRef mirrors LegacyEntryRef for the category dir lookup.
// The redirect layer prefers slug when set so a redirect from an SB3
// category dir lands on the canonical /category/<slug>/ surface.
type LegacyCategoryRef struct {
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

// CategoryByLegacyID resolves an SB3 category_id to the destination
// category's id + slug so the redirect layer can prefer the slug URL
// when one is set.
func (s *Store) CategoryByLegacyID(ctx context.Context, wid, legacyID int64) (LegacyCategoryRef, error) {
	var ref LegacyCategoryRef
	err := s.db.QueryRowContext(ctx, `
		SELECT id, slug FROM categories
		WHERE wid = ? AND legacy_id = ?
		LIMIT 1`, wid, legacyID).Scan(&ref.ID, &ref.Slug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ref, ErrNotFound
		}
		return ref, fmt.Errorf("repo: CategoryByLegacyID: %w", err)
	}
	return ref, nil
}

// CategoryIDByLegacyID is the id-only wrapper around CategoryByLegacyID,
// retained for callers that don't need the slug.
func (s *Store) CategoryIDByLegacyID(ctx context.Context, wid, legacyID int64) (int64, error) {
	ref, err := s.CategoryByLegacyID(ctx, wid, legacyID)
	if err != nil {
		return 0, err
	}
	return ref.ID, nil
}

// CategoryByLegacyDir resolves an SB3 category_dir to the destination
// category's id + slug. Empty input is rejected for the same reason as
// EntryByLegacyFile — a default-only-dir blog leaves every row with the
// global log_path, and we don't want a bare hit at the log root to map
// to an arbitrary category.
func (s *Store) CategoryByLegacyDir(ctx context.Context, wid int64, dir string) (LegacyCategoryRef, error) {
	var ref LegacyCategoryRef
	if dir == "" {
		return ref, ErrNotFound
	}
	err := s.db.QueryRowContext(ctx, `
		SELECT id, slug FROM categories
		WHERE wid = ? AND legacy_dir = ?
		LIMIT 1`, wid, dir).Scan(&ref.ID, &ref.Slug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ref, ErrNotFound
		}
		return ref, fmt.Errorf("repo: CategoryByLegacyDir: %w", err)
	}
	return ref, nil
}

// CategoryIDByLegacyDir is a thin wrapper around CategoryByLegacyDir
// that drops the slug. Retained for callers that only need the id (e.g.
// the legacy_cgi.go redirect that emits id-form URLs unchanged).
func (s *Store) CategoryIDByLegacyDir(ctx context.Context, wid int64, dir string) (int64, error) {
	ref, err := s.CategoryByLegacyDir(ctx, wid, dir)
	if err != nil {
		return 0, err
	}
	return ref.ID, nil
}
