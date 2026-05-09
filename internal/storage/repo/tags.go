package repo

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/serendipitynz/serenebach/internal/domain"
)

// tagSlugPattern matches valid tag slug values. Same shape as entry
// slug so URL rules stay uniform across the site.
var tagSlugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// IsValidTagSlug reports whether s is an acceptable tag slug.
func IsValidTagSlug(s string) bool {
	if len(s) == 0 || len(s) > 100 {
		return false
	}
	return tagSlugPattern.MatchString(s)
}

// nonAlnum matches any run of characters outside [a-z0-9] for use by
// DeriveTagSlug — those runs collapse to a single hyphen.
var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// DeriveTagSlug turns a freeform tag name into a URL-safe slug. ASCII
// names produce a lowercase-hyphenated form ("Go Lang!" → "go-lang");
// names that contain no ASCII alphanumerics at all (pure Japanese,
// emoji, etc.) fall back to a short sha1-based identifier ("t-<8 hex>")
// so tag creation never needs a manual slug input just because the
// name isn't Latin.
func DeriveTagSlug(name string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	cleaned := nonAlnum.ReplaceAllString(lower, "-")
	cleaned = strings.Trim(cleaned, "-")
	if cleaned != "" {
		if len(cleaned) > 100 {
			cleaned = cleaned[:100]
		}
		return cleaned
	}
	// Non-ASCII fallback: deterministic hash so the same name always
	// resolves to the same URL.
	h := sha1.Sum([]byte(strings.TrimSpace(name)))
	return "t-" + hex.EncodeToString(h[:4])
}

// AllTags returns every tag for the weblog, ordered alphabetically by
// name — matches user expectation in the admin list and is stable for
// the static-rebuild tag loop.
func (s *Store) AllTags(ctx context.Context, wid int64) ([]domain.Tag, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, wid, name, slug, created_at, updated_at
		FROM tags WHERE wid = ?
		ORDER BY name ASC`, wid)
	if err != nil {
		return nil, fmt.Errorf("repo: AllTags: %w", err)
	}
	defer rows.Close()
	return scanTags(rows)
}

// TagBySlug fetches one tag row. ErrNotFound when missing — the public
// /tag/<slug>/ handler maps that straight to a 404.
func (s *Store) TagBySlug(ctx context.Context, wid int64, slug string) (*domain.Tag, error) {
	if slug == "" {
		return nil, ErrNotFound
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, wid, name, slug, created_at, updated_at
		FROM tags WHERE wid = ? AND slug = ?`, wid, slug)
	return scanTag(row, "TagBySlug")
}

// TagByID is the admin-side counterpart used by edit / delete handlers.
func (s *Store) TagByID(ctx context.Context, wid, id int64) (*domain.Tag, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, wid, name, slug, created_at, updated_at
		FROM tags WHERE wid = ? AND id = ?`, wid, id)
	return scanTag(row, "TagByID")
}

// CreateTag inserts a new tag row. Slug is required and must be unique
// per weblog; callers derive one via DeriveTagSlug when the user didn't
// supply one explicitly. Returns ErrSlugInUse on a collision.
func (s *Store) CreateTag(ctx context.Context, t domain.Tag) (int64, error) {
	if t.Name == "" || t.Slug == "" {
		return 0, errors.New("repo: CreateTag: name and slug required")
	}
	now := time.Now().Unix()
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO tags (wid, name, slug, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)`,
		t.WID, t.Name, t.Slug, now, now)
	if err != nil {
		if isUniqueViolation(err) {
			return 0, ErrSlugInUse
		}
		return 0, fmt.Errorf("repo: CreateTag: %w", err)
	}
	return res.LastInsertId()
}

// UpdateTag rewrites name + slug. Used by the admin edit page. Returns
// ErrSlugInUse on a collision with another tag.
func (s *Store) UpdateTag(ctx context.Context, t domain.Tag) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE tags SET name = ?, slug = ?, updated_at = ?
		WHERE wid = ? AND id = ?`,
		t.Name, t.Slug, time.Now().Unix(), t.WID, t.ID)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrSlugInUse
		}
		return fmt.Errorf("repo: UpdateTag: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteTag removes a tag and every entry_tags row that references it.
// Wrapped in a transaction so a failure mid-way doesn't orphan the
// join rows.
func (s *Store) DeleteTag(ctx context.Context, wid, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("repo: DeleteTag begin: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	if _, err := tx.ExecContext(ctx, `DELETE FROM entry_tags WHERE tag_id = ?`, id); err != nil {
		return fmt.Errorf("repo: DeleteTag join: %w", err)
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM tags WHERE wid = ? AND id = ?`, wid, id)
	if err != nil {
		return fmt.Errorf("repo: DeleteTag row: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("repo: DeleteTag commit: %w", err)
	}
	tx = nil
	return nil
}

// EnsureTagsByName looks up tags by name (case-sensitive, trimmed),
// creating any that don't exist yet. Returns the resolved tag slice in
// the same order as the input so the caller can persist the entry's
// tag assignment deterministically.
//
// Duplicates in the input are silently collapsed so "go, Go, go"
// resolves to one tag. Empty items are dropped.
func (s *Store) EnsureTagsByName(ctx context.Context, wid int64, names []string) ([]domain.Tag, error) {
	seen := make(map[string]struct{}, len(names))
	out := make([]domain.Tag, 0, len(names))
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		existing, err := s.tagByName(ctx, wid, name)
		if err == nil {
			out = append(out, *existing)
			continue
		}
		if !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		// Not found — create. Slug collisions (extremely rare with
		// the sha1 fallback) bubble up; the admin handler shows the
		// user an error rather than silently dropping the tag.
		t := domain.Tag{WID: wid, Name: name, Slug: DeriveTagSlug(name)}
		id, err := s.CreateTag(ctx, t)
		if err != nil {
			return nil, err
		}
		t.ID = id
		out = append(out, t)
	}
	return out, nil
}

// SetEntryTags replaces the entry's tag assignment with exactly the ids
// in tagIDs. Runs as a single transaction so a failure mid-way doesn't
// leave the join half-rewritten.
func (s *Store) SetEntryTags(ctx context.Context, entryID int64, tagIDs []int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("repo: SetEntryTags begin: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	if _, err := tx.ExecContext(ctx, `DELETE FROM entry_tags WHERE entry_id = ?`, entryID); err != nil {
		return fmt.Errorf("repo: SetEntryTags clear: %w", err)
	}
	if len(tagIDs) > 0 {
		stmt, err := tx.PrepareContext(ctx,
			`INSERT OR IGNORE INTO entry_tags (entry_id, tag_id) VALUES (?, ?)`)
		if err != nil {
			return fmt.Errorf("repo: SetEntryTags prep: %w", err)
		}
		defer stmt.Close()
		for _, tid := range tagIDs {
			if _, err := stmt.ExecContext(ctx, entryID, tid); err != nil {
				return fmt.Errorf("repo: SetEntryTags insert: %w", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("repo: SetEntryTags commit: %w", err)
	}
	tx = nil
	return nil
}

// TagsByEntry returns the tag slice attached to one entry, name-sorted.
// Empty slice (not nil) when the entry has no tags — simpler for the
// template layer than checking for nil.
func (s *Store) TagsByEntry(ctx context.Context, entryID int64) ([]domain.Tag, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, t.wid, t.name, t.slug, t.created_at, t.updated_at
		FROM tags t
		JOIN entry_tags et ON et.tag_id = t.id
		WHERE et.entry_id = ?
		ORDER BY t.name ASC`, entryID)
	if err != nil {
		return nil, fmt.Errorf("repo: TagsByEntry: %w", err)
	}
	defer rows.Close()
	return scanTags(rows)
}

// TagsByEntries batches TagsByEntry across many entries so list views
// can render the tag chip row per entry with one query. The return
// map always has an entry for every input id (empty slice when none).
func (s *Store) TagsByEntries(ctx context.Context, entryIDs []int64) (map[int64][]domain.Tag, error) {
	out := make(map[int64][]domain.Tag, len(entryIDs))
	for _, id := range entryIDs {
		out[id] = []domain.Tag{}
	}
	if len(entryIDs) == 0 {
		return out, nil
	}
	placeholders := strings.Repeat("?,", len(entryIDs))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]interface{}, 0, len(entryIDs))
	for _, id := range entryIDs {
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT et.entry_id, t.id, t.wid, t.name, t.slug, t.created_at, t.updated_at
		FROM entry_tags et
		JOIN tags t ON t.id = et.tag_id
		WHERE et.entry_id IN (`+placeholders+`)
		ORDER BY t.name ASC`, args...)
	if err != nil {
		return nil, fmt.Errorf("repo: TagsByEntries: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var entryID int64
		var t domain.Tag
		var createdAt, updatedAt int64
		if err := rows.Scan(&entryID, &t.ID, &t.WID, &t.Name, &t.Slug, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("repo: scan tag row: %w", err)
		}
		t.CreatedAt = time.Unix(createdAt, 0)
		t.UpdatedAt = time.Unix(updatedAt, 0)
		out[entryID] = append(out[entryID], t)
	}
	return out, rows.Err()
}

// PublishedEntriesByTag returns published entries carrying the given
// tag, newest first, capped at limit. Mirrors
// PublishedEntriesByCategory's shape so the list-page handlers look
// identical on both routes.
func (s *Store) PublishedEntriesByTag(ctx context.Context, wid, tagID int64, limit int) ([]domain.Entry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.id, e.wid, e.author_id, e.category_id, e.title, e.slug, e.keywords, e.body, e.more, e.format, e.status, e.posted_at, e.updated_at, e.likes_count, e.stamps_count, e.comments_count, e.og_bg_image_path, e.pinned
		FROM entries e
		JOIN entry_tags et ON et.entry_id = e.id
		WHERE e.wid = ? AND e.status = ? AND et.tag_id = ?
		ORDER BY e.posted_at DESC
		LIMIT ?`, wid, domain.EntryPublished, tagID, limit)
	if err != nil {
		return nil, fmt.Errorf("repo: PublishedEntriesByTag: %w", err)
	}
	defer rows.Close()
	return scanEntries(rows)
}

// PublishedEntriesByTagPage is the paginated sibling of
// PublishedEntriesByTag.
func (s *Store) PublishedEntriesByTagPage(ctx context.Context, wid, tagID int64, limit, offset int) ([]domain.Entry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.id, e.wid, e.author_id, e.category_id, e.title, e.slug, e.keywords, e.body, e.more, e.format, e.status, e.posted_at, e.updated_at, e.likes_count, e.stamps_count, e.comments_count, e.og_bg_image_path, e.pinned
		FROM entries e
		JOIN entry_tags et ON et.entry_id = e.id
		WHERE e.wid = ? AND e.status = ? AND et.tag_id = ?
		ORDER BY e.posted_at DESC
		LIMIT ? OFFSET ?`, wid, domain.EntryPublished, tagID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("repo: PublishedEntriesByTagPage: %w", err)
	}
	defer rows.Close()
	return scanEntries(rows)
}

// TagEntryCount returns how many entries carry a given tag. Used by the
// admin tag list so editors know what's in use before they rename or
// delete. A count-per-tag batch variant isn't wired yet — add it when
// the tag list gets pagination.
func (s *Store) TagEntryCount(ctx context.Context, tagID int64) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM entry_tags WHERE tag_id = ?`, tagID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("repo: TagEntryCount: %w", err)
	}
	return n, nil
}

// tagByName is the lookup EnsureTagsByName uses. Package-private because
// external callers should go through EnsureTagsByName (which also
// creates-if-missing); exposing a bare "by name" on a table that's
// usually addressed by slug would invite inconsistent call sites.
func (s *Store) tagByName(ctx context.Context, wid int64, name string) (*domain.Tag, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, wid, name, slug, created_at, updated_at
		FROM tags WHERE wid = ? AND name = ?`, wid, name)
	return scanTag(row, "tagByName")
}

func scanTag(row *sql.Row, op string) (*domain.Tag, error) {
	var t domain.Tag
	var createdAt, updatedAt int64
	if err := row.Scan(&t.ID, &t.WID, &t.Name, &t.Slug, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("repo: %s: %w", op, err)
	}
	t.CreatedAt = time.Unix(createdAt, 0)
	t.UpdatedAt = time.Unix(updatedAt, 0)
	return &t, nil
}

func scanTags(rows *sql.Rows) ([]domain.Tag, error) {
	out := []domain.Tag{}
	for rows.Next() {
		var t domain.Tag
		var createdAt, updatedAt int64
		if err := rows.Scan(&t.ID, &t.WID, &t.Name, &t.Slug, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("repo: scan tag: %w", err)
		}
		t.CreatedAt = time.Unix(createdAt, 0)
		t.UpdatedAt = time.Unix(updatedAt, 0)
		out = append(out, t)
	}
	return out, rows.Err()
}
