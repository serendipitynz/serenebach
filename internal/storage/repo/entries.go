package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/serendipitynz/serenebach/internal/domain"
)

// entryColumns is the canonical column list for the entries table.
// Kept in one place so a new column added to the schema only has to
// be threaded through the corresponding Scan call sites, not every
// query string in this file. Order must match the Scan argument
// order in scanEntryOrNotFound / scanEntries and the inline Scans.
const entryColumns = `id, wid, author_id, category_id, title, slug, keywords, body, more, format, status, posted_at, updated_at, likes_count, stamps_count, comments_count, og_bg_image_path, pinned, accept_comments`

// EntryByID returns one entry by id and weblog id. ErrNotFound when missing.
// The caller decides how to treat the entry's status (e.g. 410 vs 200) —
// this layer returns closed/draft rows exactly as stored.
func (s *Store) EntryByID(ctx context.Context, wid, id int64) (*domain.Entry, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT `+entryColumns+`
		FROM entries WHERE wid = ? AND id = ?`, wid, id)
	e := &domain.Entry{}
	var postedAt, updatedAt int64
	if err := row.Scan(&e.ID, &e.WID, &e.AuthorID, &e.CategoryID, &e.Title, &e.Slug, &e.Keywords, &e.Body, &e.More, &e.Format, &e.Status, &postedAt, &updatedAt, &e.LikesCount, &e.StampsCount, &e.CommentsCount, &e.OGBGImagePath, &e.Pinned, &e.AcceptComments); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("repo: EntryByID: %w", err)
	}
	e.PostedAt = time.Unix(postedAt, 0)
	e.UpdatedAt = time.Unix(updatedAt, 0)
	return e, nil
}

// excludeHiddenCategoryClause is the WHERE-clause fragment list, feed,
// and prev/next queries append so an entry whose category was flipped
// hidden drops out of every public surface. The semantic is
// "default-visible": a row is kept unless there is an explicit
// categories row marked hidden = 1. That covers both the uncategorised
// bucket (category_id = -1, no row in categories at all) and any
// historical CategoryID = 0 left over from earlier code paths. Use
// the `entries` form when the query has no alias and the `e.` form
// (excludeHiddenCategoryClauseE) when entries is aliased to `e`.
const excludeHiddenCategoryClause = ` AND NOT EXISTS (SELECT 1 FROM categories WHERE id = entries.category_id AND hidden = 1)`
const excludeHiddenCategoryClauseE = ` AND NOT EXISTS (SELECT 1 FROM categories WHERE id = e.category_id AND hidden = 1)`

// PrevPublishedEntry returns the most recent published entry strictly older
// than the anchor (by posted_at, tie-broken by id). ErrNotFound at the edge.
// Entries belonging to a hidden category are skipped so the prev/next
// chain on a visible entry's permalink does not point into the hidden
// subtree.
func (s *Store) PrevPublishedEntry(ctx context.Context, wid int64, anchor domain.Entry) (*domain.Entry, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT `+entryColumns+`
		FROM entries
		WHERE wid = ? AND status = ?
		  AND (posted_at < ? OR (posted_at = ? AND id < ?))`+
		excludeHiddenCategoryClause+`
		ORDER BY posted_at DESC, id DESC
		LIMIT 1`,
		wid, domain.EntryPublished, anchor.PostedAt.Unix(), anchor.PostedAt.Unix(), anchor.ID)
	return scanEntryOrNotFound(row)
}

// NextPublishedEntry returns the earliest published entry strictly newer than
// the anchor. ErrNotFound at the edge. Hidden-category entries are skipped
// for the same reason PrevPublishedEntry skips them.
func (s *Store) NextPublishedEntry(ctx context.Context, wid int64, anchor domain.Entry) (*domain.Entry, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT `+entryColumns+`
		FROM entries
		WHERE wid = ? AND status = ?
		  AND (posted_at > ? OR (posted_at = ? AND id > ?))`+
		excludeHiddenCategoryClause+`
		ORDER BY posted_at ASC, id ASC
		LIMIT 1`,
		wid, domain.EntryPublished, anchor.PostedAt.Unix(), anchor.PostedAt.Unix(), anchor.ID)
	return scanEntryOrNotFound(row)
}

func scanEntryOrNotFound(row *sql.Row) (*domain.Entry, error) {
	e := &domain.Entry{}
	var postedAt, updatedAt int64
	if err := row.Scan(&e.ID, &e.WID, &e.AuthorID, &e.CategoryID, &e.Title, &e.Slug, &e.Keywords, &e.Body, &e.More, &e.Format, &e.Status, &postedAt, &updatedAt, &e.LikesCount, &e.StampsCount, &e.CommentsCount, &e.OGBGImagePath, &e.Pinned, &e.AcceptComments); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("repo: scan entry: %w", err)
	}
	e.PostedAt = time.Unix(postedAt, 0)
	e.UpdatedAt = time.Unix(updatedAt, 0)
	return e, nil
}

// CountEntriesByStatus returns how many entries the weblog has at the given
// status. Cheap enough to call from the dashboard on every page load.
func (s *Store) CountEntriesByStatus(ctx context.Context, wid int64, status domain.EntryStatus) (int64, error) {
	var n int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM entries WHERE wid = ? AND status = ?`, wid, status).Scan(&n); err != nil {
		return 0, fmt.Errorf("repo: CountEntriesByStatus: %w", err)
	}
	return n, nil
}

// ListEntriesForAdmin returns every entry regardless of status, newest first,
// for the admin entries table. Status filtering is handled client-side.
func (s *Store) ListEntriesForAdmin(ctx context.Context, wid int64, limit int) ([]domain.Entry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+entryColumns+`
		FROM entries
		WHERE wid = ?
		ORDER BY posted_at DESC
		LIMIT ?`, wid, limit)
	if err != nil {
		return nil, fmt.Errorf("repo: ListEntriesForAdmin: %w", err)
	}
	defer rows.Close()
	return scanEntries(rows)
}

// CreateEntry inserts a new entry and returns its id. Timestamps created_at
// and updated_at default to now; posted_at is taken from the caller so draft
// vs scheduled vs backdated posts all work via the same path. Returns
// ErrSlugInUse when e.Slug collides with an existing row for the same wid.
func (s *Store) CreateEntry(ctx context.Context, e domain.Entry) (int64, error) {
	now := time.Now().Unix()
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO entries (wid, author_id, category_id, title, slug, keywords, body, more, format, status, posted_at, created_at, updated_at, og_bg_image_path, pinned, accept_comments)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.WID, e.AuthorID, e.CategoryID, e.Title, e.Slug, e.Keywords, e.Body, e.More, e.Format, e.Status,
		e.PostedAt.Unix(), now, now, e.OGBGImagePath, e.Pinned, e.AcceptComments)
	if err != nil {
		if isUniqueViolation(err) {
			return 0, ErrSlugInUse
		}
		return 0, fmt.Errorf("repo: CreateEntry: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("repo: CreateEntry lastid: %w", err)
	}
	return id, nil
}

// UpdateEntry overwrites the content + metadata of an existing entry.
// created_at is not touched; updated_at advances to now. Returns
// ErrSlugInUse when the new slug collides with another row.
func (s *Store) UpdateEntry(ctx context.Context, e domain.Entry) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE entries SET
			category_id = ?, title = ?, slug = ?, keywords = ?, body = ?, more = ?,
			format = ?, status = ?, posted_at = ?, updated_at = ?, og_bg_image_path = ?, pinned = ?, accept_comments = ?
		WHERE wid = ? AND id = ?`,
		e.CategoryID, e.Title, e.Slug, e.Keywords, e.Body, e.More, e.Format, e.Status,
		e.PostedAt.Unix(), time.Now().Unix(), e.OGBGImagePath, e.Pinned, e.AcceptComments,
		e.WID, e.ID)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrSlugInUse
		}
		return fmt.Errorf("repo: UpdateEntry: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// EntryBySlug looks up an entry by its custom slug. ErrNotFound when
// no row matches — callers serve a 404. The partial unique index
// guarantees at most one row per (wid, non-empty slug).
func (s *Store) EntryBySlug(ctx context.Context, wid int64, slug string) (*domain.Entry, error) {
	if slug == "" {
		return nil, ErrNotFound
	}
	// The `slug != ''` predicate is redundant given the guard above,
	// but SQLite's planner only considers the partial unique index
	// `idx_entries_wid_slug_unique` (WHERE slug != '') when the query
	// mentions it — otherwise it falls back to a range scan on
	// `idx_entries_wid_posted`. Keep it for the planner hint.
	row := s.db.QueryRowContext(ctx, `
		SELECT `+entryColumns+`
		FROM entries WHERE wid = ? AND slug = ? AND slug != ''`, wid, slug)
	e := &domain.Entry{}
	var postedAt, updatedAt int64
	if err := row.Scan(&e.ID, &e.WID, &e.AuthorID, &e.CategoryID, &e.Title, &e.Slug, &e.Keywords, &e.Body, &e.More, &e.Format, &e.Status, &postedAt, &updatedAt, &e.LikesCount, &e.StampsCount, &e.CommentsCount, &e.OGBGImagePath, &e.Pinned, &e.AcceptComments); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("repo: EntryBySlug: %w", err)
	}
	e.PostedAt = time.Unix(postedAt, 0)
	e.UpdatedAt = time.Unix(updatedAt, 0)
	return e, nil
}

// DeleteEntry removes an entry by id. Returns ErrNotFound when the row
// didn't exist so callers can emit 404.
func (s *Store) DeleteEntry(ctx context.Context, wid, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM entries WHERE wid = ? AND id = ?`, wid, id)
	if err != nil {
		return fmt.Errorf("repo: DeleteEntry: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// AllPublishedEntries returns every published entry, newest first. Intended
// for full-site rebuilds rather than request-path rendering.
func (s *Store) AllPublishedEntries(ctx context.Context, wid int64) ([]domain.Entry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+entryColumns+`
		FROM entries
		WHERE wid = ? AND status = ?
		ORDER BY posted_at DESC`, wid, domain.EntryPublished)
	if err != nil {
		return nil, fmt.Errorf("repo: AllPublishedEntries: %w", err)
	}
	defer rows.Close()
	return scanEntries(rows)
}

// SearchPublishedEntries returns published entries whose title, body,
// or more field contains the needle (case-insensitive LIKE). Ordered
// newest-first. Used by the MCP server's search_entries tool and, in
// future, a site-search UI — no full-text index yet, plain LIKE is
// fine for the typical single-author weblog scale.
func (s *Store) SearchPublishedEntries(ctx context.Context, wid int64, query string, limit int) ([]domain.Entry, error) {
	if query == "" || limit <= 0 {
		return nil, nil
	}
	needle := "%" + strings.ReplaceAll(strings.ReplaceAll(query, `\`, `\\`), "%", `\%`) + "%"
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+entryColumns+`
		FROM entries
		WHERE wid = ? AND status = ?
		  AND (title LIKE ? ESCAPE '\' OR body LIKE ? ESCAPE '\' OR more LIKE ? ESCAPE '\' OR keywords LIKE ? ESCAPE '\')
		ORDER BY posted_at DESC
		LIMIT ?`, wid, domain.EntryPublished, needle, needle, needle, needle, limit)
	if err != nil {
		return nil, fmt.Errorf("repo: SearchPublishedEntries: %w", err)
	}
	defer rows.Close()
	return scanEntries(rows)
}

func (s *Store) RecentPublishedEntries(ctx context.Context, wid int64, limit int) ([]domain.Entry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+entryColumns+`
		FROM entries
		WHERE wid = ? AND status = ?`+
		excludeHiddenCategoryClause+`
		ORDER BY posted_at DESC
		LIMIT ?`, wid, domain.EntryPublished, limit)
	if err != nil {
		return nil, fmt.Errorf("repo: RecentPublishedEntries: %w", err)
	}
	defer rows.Close()
	return scanEntries(rows)
}

// PublishedEntriesByCategory returns published entries in the given category,
// newest first. SB v3 also supported additional categories via `entry_add`;
// when that becomes relevant we can widen the filter here.
func (s *Store) PublishedEntriesByCategory(ctx context.Context, wid, catID int64, limit int) ([]domain.Entry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+entryColumns+`
		FROM entries
		WHERE wid = ? AND status = ? AND category_id = ?
		ORDER BY posted_at DESC
		LIMIT ?`, wid, domain.EntryPublished, catID, limit)
	if err != nil {
		return nil, fmt.Errorf("repo: PublishedEntriesByCategory: %w", err)
	}
	defer rows.Close()
	return scanEntries(rows)
}

// PublishedEntriesInRange returns published entries whose posted_at falls in
// [from, to) (both in unix seconds), newest first. Used by archive handlers.
// Hidden-category entries are dropped so the date archive stays consistent
// with home and feed.
func (s *Store) PublishedEntriesInRange(ctx context.Context, wid int64, from, to time.Time, limit int) ([]domain.Entry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+entryColumns+`
		FROM entries
		WHERE wid = ? AND status = ? AND posted_at >= ? AND posted_at < ?`+
		excludeHiddenCategoryClause+`
		ORDER BY posted_at DESC
		LIMIT ?`, wid, domain.EntryPublished, from.Unix(), to.Unix(), limit)
	if err != nil {
		return nil, fmt.Errorf("repo: PublishedEntriesInRange: %w", err)
	}
	defer rows.Close()
	return scanEntries(rows)
}

func scanEntries(rows *sql.Rows) ([]domain.Entry, error) {
	var out []domain.Entry
	for rows.Next() {
		var e domain.Entry
		var postedAt, updatedAt int64
		if err := rows.Scan(&e.ID, &e.WID, &e.AuthorID, &e.CategoryID, &e.Title, &e.Slug, &e.Keywords, &e.Body, &e.More, &e.Format, &e.Status, &postedAt, &updatedAt, &e.LikesCount, &e.StampsCount, &e.CommentsCount, &e.OGBGImagePath, &e.Pinned, &e.AcceptComments); err != nil {
			return nil, fmt.Errorf("repo: scan entry: %w", err)
		}
		e.PostedAt = time.Unix(postedAt, 0)
		e.UpdatedAt = time.Unix(updatedAt, 0)
		out = append(out, e)
	}
	return out, rows.Err()
}

// ---- pagination count queries -----------------------------------------
//
// Each list route pairs with a count: handlers need `total_entries` to
// compute `page_num` for the SB3 `{page_num}` tag. Filters mirror the
// corresponding PublishedEntries* query so count + page slice never
// disagree.

// CountPublishedEntries returns the total number of published entries
// for the weblog — the denominator for the home page's pagination.
// Hidden-category entries are dropped to stay consistent with the
// page slice.
func (s *Store) CountPublishedEntries(ctx context.Context, wid int64) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM entries WHERE wid = ? AND status = ?`+
			excludeHiddenCategoryClause,
		wid, domain.EntryPublished).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("repo: CountPublishedEntries: %w", err)
	}
	return n, nil
}

// CountPublishedEntriesByCategory is the pagination counterpart to
// PublishedEntriesByCategory: published-only, filtered to the given
// category id.
func (s *Store) CountPublishedEntriesByCategory(ctx context.Context, wid, catID int64) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM entries WHERE wid = ? AND status = ? AND category_id = ?`,
		wid, domain.EntryPublished, catID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("repo: CountPublishedEntriesByCategory: %w", err)
	}
	return n, nil
}

// CountPublishedEntriesByTag mirrors PublishedEntriesByTag. Tag pages
// need the total so paginator markup lines up with the one-page-at-a-
// time slice the handler fetches. Hidden-category entries are dropped
// to stay consistent with the page slice.
func (s *Store) CountPublishedEntriesByTag(ctx context.Context, wid, tagID int64) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM entries e
		JOIN entry_tags et ON et.entry_id = e.id
		WHERE e.wid = ? AND e.status = ? AND et.tag_id = ?`+
		excludeHiddenCategoryClauseE,
		wid, domain.EntryPublished, tagID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("repo: CountPublishedEntriesByTag: %w", err)
	}
	return n, nil
}

// CountPublishedEntriesInRange mirrors PublishedEntriesInRange. Hidden
// categories are excluded to match the archive page slice.
func (s *Store) CountPublishedEntriesInRange(ctx context.Context, wid int64, from, to time.Time) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM entries WHERE wid = ? AND status = ? AND posted_at >= ? AND posted_at < ?`+
			excludeHiddenCategoryClause,
		wid, domain.EntryPublished, from.Unix(), to.Unix()).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("repo: CountPublishedEntriesInRange: %w", err)
	}
	return n, nil
}

// RecentPublishedEntriesPage is RecentPublishedEntries + an OFFSET —
// the same shape SB3 implements via `LIMIT disp OFFSET (page*disp)`.
// Caller computes offset = (page-1) * limit. Hidden-category entries
// are excluded so the home pagination stays consistent with the count.
func (s *Store) RecentPublishedEntriesPage(ctx context.Context, wid int64, limit, offset int) ([]domain.Entry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+entryColumns+`
		FROM entries
		WHERE wid = ? AND status = ?`+
		excludeHiddenCategoryClause+`
		ORDER BY pinned DESC, posted_at DESC
		LIMIT ? OFFSET ?`, wid, domain.EntryPublished, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("repo: RecentPublishedEntriesPage: %w", err)
	}
	defer rows.Close()
	return scanEntries(rows)
}

// PublishedEntriesByCategoryPage is the paginated sibling of
// PublishedEntriesByCategory.
func (s *Store) PublishedEntriesByCategoryPage(ctx context.Context, wid, catID int64, limit, offset int) ([]domain.Entry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+entryColumns+`
		FROM entries
		WHERE wid = ? AND status = ? AND category_id = ?
		ORDER BY pinned DESC, posted_at DESC
		LIMIT ? OFFSET ?`, wid, domain.EntryPublished, catID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("repo: PublishedEntriesByCategoryPage: %w", err)
	}
	defer rows.Close()
	return scanEntries(rows)
}

// PublishedEntriesInRangePage is the paginated sibling of
// PublishedEntriesInRange. Used by year/month archive pagination.
// Hidden-category entries are excluded to keep the archive page slice
// aligned with CountPublishedEntriesInRange.
func (s *Store) PublishedEntriesInRangePage(ctx context.Context, wid int64, from, to time.Time, limit, offset int) ([]domain.Entry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+entryColumns+`
		FROM entries
		WHERE wid = ? AND status = ? AND posted_at >= ? AND posted_at < ?`+
		excludeHiddenCategoryClause+`
		ORDER BY posted_at DESC
		LIMIT ? OFFSET ?`, wid, domain.EntryPublished, from.Unix(), to.Unix(), limit, offset)
	if err != nil {
		return nil, fmt.Errorf("repo: PublishedEntriesInRangePage: %w", err)
	}
	defer rows.Close()
	return scanEntries(rows)
}

// SetEntryPinned sets or clears the pinned flag on an entry.
func (s *Store) SetEntryPinned(ctx context.Context, wid, id int64, pinned bool) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE entries SET pinned = ? WHERE wid = ? AND id = ?`, pinned, wid, id)
	if err != nil {
		return fmt.Errorf("repo: SetEntryPinned: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
