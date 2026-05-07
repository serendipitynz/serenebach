// Package repo holds the hand-written SQL queries the public and admin layers
// call. We stay on database/sql for now; migrating to sqlc is a later call
// once the query set gets large enough to justify codegen.
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

// WeblogByID returns the weblog with the given id; ErrNotFound if missing.
func (s *Store) WeblogByID(ctx context.Context, id int64) (*domain.Weblog, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, title, description, base_url, lang, comment_mode, spam_words, ip_blacklist, llms_enabled,
		       auto_rebuild_on_publish,
		       og_bg_image_path, og_text_color,
		       archive_template_id, profile_template_id,
		       date_format_entry, time_format_entry, date_format_comment,
		       date_format_list, date_format_archive,
		       entries_per_page, entry_sort_order, comment_sort_order
		FROM weblogs WHERE id = ?`, id)
	w := &domain.Weblog{}
	var mode string
	var llmsEnabled, autoRebuild int
	if err := row.Scan(&w.ID, &w.Title, &w.Description, &w.BaseURL, &w.Lang, &mode, &w.SpamWords, &w.IPBlacklist, &llmsEnabled,
		&autoRebuild,
		&w.OGBGImagePath, &w.OGTextColor,
		&w.ArchiveTemplateID, &w.ProfileTemplateID,
		&w.DateFormatEntry, &w.TimeFormatEntry, &w.DateFormatComment,
		&w.DateFormatList, &w.DateFormatArchive,
		&w.EntriesPerPage, &w.EntrySortOrder, &w.CommentSortOrder); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("repo: WeblogByID: %w", err)
	}
	w.CommentMode = domain.CommentMode(mode)
	if !w.CommentMode.Valid() {
		w.CommentMode = domain.CommentModerated
	}
	w.LLMSEnabled = llmsEnabled != 0
	w.AutoRebuildOnPublish = autoRebuild != 0
	return w, nil
}

// UpdateWeblogDesign sets just the design-settings columns so the
// settings form can stay narrow and the archive/profile tabs don't need
// to re-submit every weblog field. Leaves the rest untouched.
func (s *Store) UpdateWeblogDesign(ctx context.Context, wid, archiveID, profileID int64) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE weblogs SET archive_template_id = ?, profile_template_id = ?
		WHERE id = ?`, archiveID, profileID, wid)
	if err != nil {
		return fmt.Errorf("repo: UpdateWeblogDesign: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateWeblogDisplaySettings persists the count + sort knobs edited on
// the design-settings 表示件数 / 並び順 section. Callers normalise the
// sort-order strings before calling so "desc" / "asc" are the only
// values that ever land here.
func (s *Store) UpdateWeblogDisplaySettings(ctx context.Context, wid int64, entriesPerPage int, entrySort, commentSort string) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE weblogs SET
			entries_per_page   = ?,
			entry_sort_order   = ?,
			comment_sort_order = ?
		WHERE id = ?`, entriesPerPage, entrySort, commentSort, wid)
	if err != nil {
		return fmt.Errorf("repo: UpdateWeblogDisplaySettings: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateWeblogDateFormats persists the five date-format pattern strings
// edited on the design-settings 時刻表記 tab. Empty strings are stored
// as-is so the reader path can resolve "unset" to the package defaults
// without a second flag column.
func (s *Store) UpdateWeblogDateFormats(ctx context.Context, wid int64, entry, entryTime, comment, list, archive string) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE weblogs SET
			date_format_entry   = ?,
			time_format_entry   = ?,
			date_format_comment = ?,
			date_format_list    = ?,
			date_format_archive = ?
		WHERE id = ?`, entry, entryTime, comment, list, archive, wid)
	if err != nil {
		return fmt.Errorf("repo: UpdateWeblogDateFormats: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateWeblog rewrites every user-editable field on the weblog row. The
// caller hands over a full Weblog struct because the admin settings form
// always submits every field together — no partial updates to worry about.
func (s *Store) UpdateWeblog(ctx context.Context, w domain.Weblog) error {
	llms := 0
	if w.LLMSEnabled {
		llms = 1
	}
	autoRebuild := 0
	if w.AutoRebuildOnPublish {
		autoRebuild = 1
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE weblogs SET
			title = ?, description = ?, base_url = ?, lang = ?,
			comment_mode = ?, spam_words = ?, ip_blacklist = ?, llms_enabled = ?,
			auto_rebuild_on_publish = ?,
			og_bg_image_path = ?, og_text_color = ?
		WHERE id = ?`,
		w.Title, w.Description, w.BaseURL, w.Lang,
		string(w.CommentMode), w.SpamWords, w.IPBlacklist, llms,
		autoRebuild,
		w.OGBGImagePath, w.OGTextColor,
		w.ID)
	if err != nil {
		return fmt.Errorf("repo: UpdateWeblog: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ActiveTemplate returns the weblog's active template. ErrNotFound if none
// is marked active, which means the site has nothing to render with.
func (s *Store) ActiveTemplate(ctx context.Context, wid int64) (*domain.Template, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, wid, name, is_active, main_body, entry_body, css, info
		FROM templates
		WHERE wid = ? AND is_active = 1
		ORDER BY id DESC
		LIMIT 1`, wid)
	t := &domain.Template{}
	var active int
	if err := row.Scan(&t.ID, &t.WID, &t.Name, &active, &t.MainBody, &t.EntryBody, &t.CSS, &t.Info); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("repo: ActiveTemplate: %w", err)
	}
	t.IsActive = active == 1
	return t, nil
}

// TemplateByID fetches one template row by id. ErrNotFound on miss.
func (s *Store) TemplateByID(ctx context.Context, wid, id int64) (*domain.Template, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, wid, name, is_active, main_body, entry_body, css, info, sort_order, created_at, updated_at
		FROM templates WHERE wid = ? AND id = ?`, wid, id)
	t := &domain.Template{}
	var active int
	var createdAt, updatedAt int64
	if err := row.Scan(&t.ID, &t.WID, &t.Name, &active, &t.MainBody, &t.EntryBody, &t.CSS, &t.Info,
		&t.SortOrder, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("repo: TemplateByID: %w", err)
	}
	t.IsActive = active == 1
	t.CreatedAt = time.Unix(createdAt, 0)
	t.UpdatedAt = time.Unix(updatedAt, 0)
	return t, nil
}

// ListTemplatesForAdmin returns every template for the weblog ordered by
// sort_order then id. The admin design-settings page shows this list and
// lets the admin activate / reorder / delete rows.
func (s *Store) ListTemplatesForAdmin(ctx context.Context, wid int64) ([]domain.Template, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, wid, name, is_active, main_body, entry_body, css, info, sort_order, created_at, updated_at
		FROM templates
		WHERE wid = ?
		ORDER BY sort_order, id`, wid)
	if err != nil {
		return nil, fmt.Errorf("repo: ListTemplatesForAdmin: %w", err)
	}
	defer rows.Close()
	var out []domain.Template
	for rows.Next() {
		var t domain.Template
		var active int
		var createdAt, updatedAt int64
		if err := rows.Scan(&t.ID, &t.WID, &t.Name, &active, &t.MainBody, &t.EntryBody, &t.CSS, &t.Info,
			&t.SortOrder, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("repo: scan template: %w", err)
		}
		t.IsActive = active == 1
		t.CreatedAt = time.Unix(createdAt, 0)
		t.UpdatedAt = time.Unix(updatedAt, 0)
		out = append(out, t)
	}
	return out, rows.Err()
}

// ---- template assets ---------------------------------------------------

// CreateOrReplaceTemplateAsset upserts the metadata row for one template
// asset. The unique index on (template_id, filename) means a re-upload
// of the same filename overwrites the existing entry so there's no stale
// duplicate.
func (s *Store) CreateOrReplaceTemplateAsset(ctx context.Context, a domain.TemplateAsset) (int64, error) {
	now := time.Now().Unix()
	// Fast path: try insert. If the unique constraint kicks in, update
	// the existing row (size / mime may have changed on re-upload).
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO template_assets (template_id, filename, mime_type, size_bytes, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(template_id, filename) DO UPDATE SET
			mime_type = excluded.mime_type,
			size_bytes = excluded.size_bytes,
			updated_at = excluded.updated_at`,
		a.TemplateID, a.Filename, a.MimeType, a.SizeBytes, now, now)
	if err != nil {
		return 0, fmt.Errorf("repo: CreateOrReplaceTemplateAsset: %w", err)
	}
	// With ON CONFLICT DO UPDATE, LastInsertId may refer to the existing
	// row (SQLite returns the rowid of the updated row). That's fine for
	// our purposes — the admin only needs to know "it's there".
	id, _ := res.LastInsertId()
	if id == 0 {
		// Fetch the id explicitly for the upsert case.
		_ = s.db.QueryRowContext(ctx,
			`SELECT id FROM template_assets WHERE template_id = ? AND filename = ?`,
			a.TemplateID, a.Filename).Scan(&id)
	}
	return id, nil
}

// ListTemplateAssets returns every asset row for the given template,
// newest first so re-uploads surface at the top of the admin panel.
func (s *Store) ListTemplateAssets(ctx context.Context, templateID int64) ([]domain.TemplateAsset, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, template_id, filename, mime_type, size_bytes, created_at, updated_at
		FROM template_assets
		WHERE template_id = ?
		ORDER BY created_at DESC, id DESC`, templateID)
	if err != nil {
		return nil, fmt.Errorf("repo: ListTemplateAssets: %w", err)
	}
	defer rows.Close()
	var out []domain.TemplateAsset
	for rows.Next() {
		var a domain.TemplateAsset
		var createdAt, updatedAt int64
		if err := rows.Scan(&a.ID, &a.TemplateID, &a.Filename, &a.MimeType, &a.SizeBytes, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("repo: scan template asset: %w", err)
		}
		a.CreatedAt = time.Unix(createdAt, 0)
		a.UpdatedAt = time.Unix(updatedAt, 0)
		out = append(out, a)
	}
	return out, rows.Err()
}

// TemplateAssetByID fetches one row. ErrNotFound on miss.
func (s *Store) TemplateAssetByID(ctx context.Context, id int64) (*domain.TemplateAsset, error) {
	var a domain.TemplateAsset
	var createdAt, updatedAt int64
	err := s.db.QueryRowContext(ctx, `
		SELECT id, template_id, filename, mime_type, size_bytes, created_at, updated_at
		FROM template_assets WHERE id = ?`, id).
		Scan(&a.ID, &a.TemplateID, &a.Filename, &a.MimeType, &a.SizeBytes, &createdAt, &updatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("repo: TemplateAssetByID: %w", err)
	}
	a.CreatedAt = time.Unix(createdAt, 0)
	a.UpdatedAt = time.Unix(updatedAt, 0)
	return &a, nil
}

// DeleteTemplateAsset removes the metadata row; on-disk file cleanup is
// the caller's responsibility (kept out of repo for the same reason
// DeleteImage is).
func (s *Store) DeleteTemplateAsset(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM template_assets WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("repo: DeleteTemplateAsset: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// CreateTemplate inserts a new template row (inactive by default; callers
// flip is_active via ActivateTemplate separately). Returns the new id.
func (s *Store) CreateTemplate(ctx context.Context, t domain.Template) (int64, error) {
	now := time.Now().Unix()
	active := 0
	if t.IsActive {
		active = 1
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO templates (wid, name, is_active, main_body, entry_body, css, info, sort_order, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.WID, t.Name, active, t.MainBody, t.EntryBody, t.CSS, t.Info, t.SortOrder, now, now)
	if err != nil {
		return 0, fmt.Errorf("repo: CreateTemplate: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("repo: CreateTemplate lastid: %w", err)
	}
	return id, nil
}

// UpdateTemplate overwrites the editable fields of an existing row. The
// is_active flag is left alone — callers route through ActivateTemplate
// so the single-active invariant stays centralised.
func (s *Store) UpdateTemplate(ctx context.Context, t domain.Template) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE templates SET
			name = ?, main_body = ?, entry_body = ?, css = ?, info = ?,
			updated_at = ?
		WHERE wid = ? AND id = ?`,
		t.Name, t.MainBody, t.EntryBody, t.CSS, t.Info,
		time.Now().Unix(),
		t.WID, t.ID)
	if err != nil {
		return fmt.Errorf("repo: UpdateTemplate: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ActivateTemplate flips the single-active row for the weblog. Every other
// template row is cleared in the same transaction so the constraint "at
// most one is_active per weblog" stays honoured without needing a unique
// partial index (SQLite doesn't gracefully enforce that).
func (s *Store) ActivateTemplate(ctx context.Context, wid, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("repo: ActivateTemplate begin: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	now := time.Now().Unix()
	if _, err := tx.ExecContext(ctx,
		`UPDATE templates SET is_active = 0, updated_at = ? WHERE wid = ? AND is_active = 1`,
		now, wid); err != nil {
		return fmt.Errorf("repo: ActivateTemplate clear: %w", err)
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE templates SET is_active = 1, updated_at = ? WHERE wid = ? AND id = ?`,
		now, wid, id)
	if err != nil {
		return fmt.Errorf("repo: ActivateTemplate set: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("repo: ActivateTemplate commit: %w", err)
	}
	tx = nil
	return nil
}

// DeleteTemplate removes a template row. The active template cannot be
// deleted — callers must activate a different row first so the site
// always has something to render with.
var ErrTemplateActive = errors.New("repo: cannot delete the active template")

func (s *Store) DeleteTemplate(ctx context.Context, wid, id int64) error {
	var active int
	err := s.db.QueryRowContext(ctx,
		`SELECT is_active FROM templates WHERE wid = ? AND id = ?`, wid, id).Scan(&active)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("repo: DeleteTemplate load: %w", err)
	}
	if active == 1 {
		return ErrTemplateActive
	}
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM templates WHERE wid = ? AND id = ?`, wid, id); err != nil {
		return fmt.Errorf("repo: DeleteTemplate: %w", err)
	}
	return nil
}

// ReorderTemplates rewrites sort_order for the given ids so list order
// matches the input slice. Missing ids stay untouched. Mirrors the
// ReorderCategories pattern so the admin JS can treat both tables the
// same way.
func (s *Store) ReorderTemplates(ctx context.Context, wid int64, orderedIDs []int64) error {
	if len(orderedIDs) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("repo: ReorderTemplates begin: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	stmt, err := tx.PrepareContext(ctx,
		`UPDATE templates SET sort_order = ?, updated_at = ? WHERE wid = ? AND id = ?`)
	if err != nil {
		return fmt.Errorf("repo: ReorderTemplates prepare: %w", err)
	}
	defer stmt.Close()
	now := time.Now().Unix()
	for i, id := range orderedIDs {
		if _, err := stmt.ExecContext(ctx, i, now, wid, id); err != nil {
			return fmt.Errorf("repo: ReorderTemplates id=%d: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("repo: ReorderTemplates commit: %w", err)
	}
	tx = nil
	return nil
}

// EntryByID returns one entry by id and weblog id. ErrNotFound when missing.
// The caller decides how to treat the entry's status (e.g. 410 vs 200) —
// this layer returns closed/draft rows exactly as stored.
func (s *Store) EntryByID(ctx context.Context, wid, id int64) (*domain.Entry, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, wid, author_id, category_id, title, slug, keywords, body, more, format, status, posted_at, updated_at, likes_count, stamps_count, comments_count, og_bg_image_path
		FROM entries WHERE wid = ? AND id = ?`, wid, id)
	e := &domain.Entry{}
	var postedAt, updatedAt int64
	if err := row.Scan(&e.ID, &e.WID, &e.AuthorID, &e.CategoryID, &e.Title, &e.Slug, &e.Keywords, &e.Body, &e.More, &e.Format, &e.Status, &postedAt, &updatedAt, &e.LikesCount, &e.StampsCount, &e.CommentsCount, &e.OGBGImagePath); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("repo: EntryByID: %w", err)
	}
	e.PostedAt = time.Unix(postedAt, 0)
	e.UpdatedAt = time.Unix(updatedAt, 0)
	return e, nil
}

// PrevPublishedEntry returns the most recent published entry strictly older
// than the anchor (by posted_at, tie-broken by id). ErrNotFound at the edge.
func (s *Store) PrevPublishedEntry(ctx context.Context, wid int64, anchor domain.Entry) (*domain.Entry, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, wid, author_id, category_id, title, slug, keywords, body, more, format, status, posted_at, updated_at, likes_count, stamps_count, comments_count, og_bg_image_path
		FROM entries
		WHERE wid = ? AND status = ?
		  AND (posted_at < ? OR (posted_at = ? AND id < ?))
		ORDER BY posted_at DESC, id DESC
		LIMIT 1`,
		wid, domain.EntryPublished, anchor.PostedAt.Unix(), anchor.PostedAt.Unix(), anchor.ID)
	return scanEntryOrNotFound(row)
}

// NextPublishedEntry returns the earliest published entry strictly newer than
// the anchor. ErrNotFound at the edge.
func (s *Store) NextPublishedEntry(ctx context.Context, wid int64, anchor domain.Entry) (*domain.Entry, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, wid, author_id, category_id, title, slug, keywords, body, more, format, status, posted_at, updated_at, likes_count, stamps_count, comments_count, og_bg_image_path
		FROM entries
		WHERE wid = ? AND status = ?
		  AND (posted_at > ? OR (posted_at = ? AND id > ?))
		ORDER BY posted_at ASC, id ASC
		LIMIT 1`,
		wid, domain.EntryPublished, anchor.PostedAt.Unix(), anchor.PostedAt.Unix(), anchor.ID)
	return scanEntryOrNotFound(row)
}

func scanEntryOrNotFound(row *sql.Row) (*domain.Entry, error) {
	e := &domain.Entry{}
	var postedAt, updatedAt int64
	if err := row.Scan(&e.ID, &e.WID, &e.AuthorID, &e.CategoryID, &e.Title, &e.Slug, &e.Keywords, &e.Body, &e.More, &e.Format, &e.Status, &postedAt, &updatedAt, &e.LikesCount, &e.StampsCount, &e.CommentsCount, &e.OGBGImagePath); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("repo: scan entry: %w", err)
	}
	e.PostedAt = time.Unix(postedAt, 0)
	e.UpdatedAt = time.Unix(updatedAt, 0)
	return e, nil
}

// RecentPublishedEntries returns the N most recent published entries for the
// weblog, newest first.
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

// CountMessagesByStatus returns how many comments the weblog has at the
// given status. Used to surface the moderation queue size on the dashboard.
func (s *Store) CountMessagesByStatus(ctx context.Context, wid int64, status domain.MessageStatus) (int64, error) {
	var n int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM messages WHERE wid = ? AND status = ?`, wid, status).Scan(&n); err != nil {
		return 0, fmt.Errorf("repo: CountMessagesByStatus: %w", err)
	}
	return n, nil
}

// ListEntriesForAdmin returns every entry regardless of status, newest first,
// for the admin entries table. Status filtering is handled client-side.
func (s *Store) ListEntriesForAdmin(ctx context.Context, wid int64, limit int) ([]domain.Entry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, wid, author_id, category_id, title, slug, keywords, body, more, format, status, posted_at, updated_at, likes_count, stamps_count, comments_count, og_bg_image_path
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
		INSERT INTO entries (wid, author_id, category_id, title, slug, keywords, body, more, format, status, posted_at, created_at, updated_at, og_bg_image_path)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.WID, e.AuthorID, e.CategoryID, e.Title, e.Slug, e.Keywords, e.Body, e.More, e.Format, e.Status,
		e.PostedAt.Unix(), now, now, e.OGBGImagePath)
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
			format = ?, status = ?, posted_at = ?, updated_at = ?, og_bg_image_path = ?
		WHERE wid = ? AND id = ?`,
		e.CategoryID, e.Title, e.Slug, e.Keywords, e.Body, e.More, e.Format, e.Status,
		e.PostedAt.Unix(), time.Now().Unix(), e.OGBGImagePath,
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
		SELECT id, wid, author_id, category_id, title, slug, keywords, body, more, format, status, posted_at, updated_at, likes_count, stamps_count, comments_count, og_bg_image_path
		FROM entries WHERE wid = ? AND slug = ? AND slug != ''`, wid, slug)
	e := &domain.Entry{}
	var postedAt, updatedAt int64
	if err := row.Scan(&e.ID, &e.WID, &e.AuthorID, &e.CategoryID, &e.Title, &e.Slug, &e.Keywords, &e.Body, &e.More, &e.Format, &e.Status, &postedAt, &updatedAt, &e.LikesCount, &e.StampsCount, &e.CommentsCount, &e.OGBGImagePath); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("repo: EntryBySlug: %w", err)
	}
	e.PostedAt = time.Unix(postedAt, 0)
	e.UpdatedAt = time.Unix(updatedAt, 0)
	return e, nil
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

// ---- likes --------------------------------------------------------------

// LikeEntry atomically records a like from the given fingerprint and bumps
// the denormalised counter. Returns `true` when the like was new (and the
// counter actually advanced), `false` when this fingerprint had already
// liked the entry — the caller uses this to decide whether to set a
// "already liked" cookie on the browser.
func (s *Store) LikeEntry(ctx context.Context, entryID int64, fingerprint string) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("repo: LikeEntry begin: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	res, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO entry_likes (entry_id, fingerprint, created_at)
		VALUES (?, ?, ?)`, entryID, fingerprint, time.Now().Unix())
	if err != nil {
		return false, fmt.Errorf("repo: LikeEntry insert: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		// Fingerprint already on file — commit the read-only tx and tell the
		// caller nothing changed.
		if err := tx.Commit(); err != nil {
			return false, fmt.Errorf("repo: LikeEntry commit: %w", err)
		}
		tx = nil
		return false, nil
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE entries SET likes_count = likes_count + 1 WHERE id = ?`, entryID); err != nil {
		return false, fmt.Errorf("repo: LikeEntry bump: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("repo: LikeEntry commit: %w", err)
	}
	tx = nil
	return true, nil
}

// ---- stamps -------------------------------------------------------------

// StampEntry atomically records a stamp of the given kind from the
// supplied fingerprint and bumps the denormalised total counter.
// Returns true when the stamp was new (counter moved); false when the
// same (entry, kind, fingerprint) triple was already on file. Mirrors
// LikeEntry's contract so the HTTP handler can decide whether to set
// an "already reacted" cookie.
func (s *Store) StampEntry(ctx context.Context, entryID int64, kind domain.StampKind, fingerprint string) (bool, error) {
	if !kind.Valid() {
		return false, fmt.Errorf("repo: StampEntry: invalid kind %q", kind)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("repo: StampEntry begin: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	res, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO entry_stamps (entry_id, stamp_kind, fingerprint, created_at)
		VALUES (?, ?, ?, ?)`, entryID, string(kind), fingerprint, time.Now().Unix())
	if err != nil {
		return false, fmt.Errorf("repo: StampEntry insert: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		if err := tx.Commit(); err != nil {
			return false, fmt.Errorf("repo: StampEntry commit: %w", err)
		}
		tx = nil
		return false, nil
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE entries SET stamps_count = stamps_count + 1 WHERE id = ?`, entryID); err != nil {
		return false, fmt.Errorf("repo: StampEntry bump: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("repo: StampEntry commit: %w", err)
	}
	tx = nil
	return true, nil
}

// StampCountsByEntry returns a kind → count map for the given entry,
// covering every kind even when zero so callers can render a uniform
// set of reaction buttons.
func (s *Store) StampCountsByEntry(ctx context.Context, entryID int64) (map[domain.StampKind]int64, error) {
	out := make(map[domain.StampKind]int64, len(domain.StampKinds))
	for _, k := range domain.StampKinds {
		out[k] = 0
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT stamp_kind, COUNT(*) FROM entry_stamps
		WHERE entry_id = ? GROUP BY stamp_kind`, entryID)
	if err != nil {
		return nil, fmt.Errorf("repo: StampCountsByEntry: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var kind string
		var count int64
		if err := rows.Scan(&kind, &count); err != nil {
			return nil, fmt.Errorf("repo: scan stamp count: %w", err)
		}
		out[domain.StampKind(kind)] = count
	}
	return out, rows.Err()
}

// ---- messages (comments) -----------------------------------------------

// CreateMessage inserts a new comment row and returns its id. The status is
// taken from the caller so an `open` weblog stores approved comments while
// `moderated` stores waiting ones. When the message is approved on creation,
// the entry's comments_count is bumped +1 in the same transaction.
func (s *Store) CreateMessage(ctx context.Context, m domain.Message) (int64, error) {
	now := time.Now().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("repo: CreateMessage: begin: %w", err)
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `
		INSERT INTO messages (wid, entry_id, status, posted_at, author_name, author_email, author_url, body, ip_address, user_agent, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.WID, m.EntryID, m.Status, m.PostedAt.Unix(),
		m.AuthorName, m.AuthorEmail, m.AuthorURL, m.Body,
		m.IPAddress, m.UserAgent, now, now)
	if err != nil {
		return 0, fmt.Errorf("repo: CreateMessage: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("repo: CreateMessage lastid: %w", err)
	}
	if m.Status == domain.MessageApproved {
		if _, err := tx.ExecContext(ctx, `
			UPDATE entries SET comments_count = comments_count + 1
			WHERE wid = ? AND id = ?`, m.WID, m.EntryID); err != nil {
			return 0, fmt.Errorf("repo: CreateMessage: bump: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("repo: CreateMessage: commit: %w", err)
	}
	return id, nil
}

// ApprovedMessagesByEntry returns the approved comments for an entry in
// posting order (oldest first — readers usually follow threads top-down).
func (s *Store) ApprovedMessagesByEntry(ctx context.Context, wid, entryID int64) ([]domain.Message, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, wid, entry_id, status, posted_at, author_name, author_email, author_url, body, ip_address, user_agent
		FROM messages
		WHERE wid = ? AND entry_id = ? AND status = ?
		ORDER BY posted_at ASC, id ASC`,
		wid, entryID, domain.MessageApproved)
	if err != nil {
		return nil, fmt.Errorf("repo: ApprovedMessagesByEntry: %w", err)
	}
	defer rows.Close()
	return scanMessages(rows)
}

// ListMessagesForAdmin returns every comment (optionally filtered by status)
// newest first. Pass -99 or any non-valid MessageStatus for "no filter".
func (s *Store) ListMessagesForAdmin(ctx context.Context, wid int64, filter domain.MessageStatus, limit int) ([]domain.Message, error) {
	var (
		rows *sql.Rows
		err  error
	)
	switch filter {
	case domain.MessageWaiting, domain.MessageApproved, domain.MessageHidden:
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, wid, entry_id, status, posted_at, author_name, author_email, author_url, body, ip_address, user_agent
			FROM messages
			WHERE wid = ? AND status = ?
			ORDER BY posted_at DESC
			LIMIT ?`, wid, filter, limit)
	default:
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, wid, entry_id, status, posted_at, author_name, author_email, author_url, body, ip_address, user_agent
			FROM messages
			WHERE wid = ?
			ORDER BY posted_at DESC
			LIMIT ?`, wid, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("repo: ListMessagesForAdmin: %w", err)
	}
	defer rows.Close()
	return scanMessages(rows)
}

// UpdateMessageStatus flips a comment between waiting / approved / hidden.
// The entry's comments_count is adjusted based on the transition: approved↔
// non-approved changes bump or decrement the counter in the same transaction.
func (s *Store) UpdateMessageStatus(ctx context.Context, wid, id int64, status domain.MessageStatus) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("repo: UpdateMessageStatus: begin: %w", err)
	}
	defer tx.Rollback()
	// Read the old status so we know whether the entry counter needs adjusting.
	var oldStatus domain.MessageStatus
	var entryID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT status, entry_id FROM messages WHERE wid = ? AND id = ?`, wid, id).Scan(&oldStatus, &entryID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("repo: UpdateMessageStatus: select: %w", err)
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE messages SET status = ?, updated_at = ?
		WHERE wid = ? AND id = ?`, status, time.Now().Unix(), wid, id)
	if err != nil {
		return fmt.Errorf("repo: UpdateMessageStatus: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	// Bump or decrement the entry's comments_count depending on the transition.
	wasApproved := oldStatus == domain.MessageApproved
	nowApproved := status == domain.MessageApproved
	switch {
	case !wasApproved && nowApproved:
		if _, err := tx.ExecContext(ctx, `
			UPDATE entries SET comments_count = comments_count + 1
			WHERE wid = ? AND id = ?`, wid, entryID); err != nil {
			return fmt.Errorf("repo: UpdateMessageStatus: bump: %w", err)
		}
	case wasApproved && !nowApproved:
		if _, err := tx.ExecContext(ctx, `
			UPDATE entries SET comments_count = comments_count - 1
			WHERE wid = ? AND id = ?`, wid, entryID); err != nil {
			return fmt.Errorf("repo: UpdateMessageStatus: debump: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("repo: UpdateMessageStatus: commit: %w", err)
	}
	return nil
}

// DeleteMessage removes a comment. Used by admin hard-delete (distinct from
// the soft hide that UpdateMessageStatus(.., MessageHidden) performs).
// If the removed comment was approved, the entry's comments_count is
// decremented in the same transaction.
func (s *Store) DeleteMessage(ctx context.Context, wid, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("repo: DeleteMessage: begin: %w", err)
	}
	defer tx.Rollback()
	// Read the old status and entry_id so we can adjust the counter.
	var oldStatus domain.MessageStatus
	var entryID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT status, entry_id FROM messages WHERE wid = ? AND id = ?`, wid, id).Scan(&oldStatus, &entryID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("repo: DeleteMessage: select: %w", err)
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM messages WHERE wid = ? AND id = ?`, wid, id)
	if err != nil {
		return fmt.Errorf("repo: DeleteMessage: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	if oldStatus == domain.MessageApproved {
		if _, err := tx.ExecContext(ctx, `
			UPDATE entries SET comments_count = comments_count - 1
			WHERE wid = ? AND id = ?`, wid, entryID); err != nil {
			return fmt.Errorf("repo: DeleteMessage: debump: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("repo: DeleteMessage: commit: %w", err)
	}
	return nil
}

// HasApprovedCommentFromEmail reports whether the weblog has ever published
// an approved comment from the given email address. Used to auto-approve
// repeat commenters who have already been vetted — a lightweight "trust
// memory" so moderation doesn't burn out the admin.
func (s *Store) HasApprovedCommentFromEmail(ctx context.Context, wid int64, email string) (bool, error) {
	if email == "" {
		return false, nil
	}
	var n int
	if err := s.db.QueryRowContext(ctx, `
		SELECT 1 FROM messages
		WHERE wid = ? AND status = ? AND author_email = ?
		LIMIT 1`, wid, domain.MessageApproved, email).Scan(&n); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("repo: HasApprovedCommentFromEmail: %w", err)
	}
	return true, nil
}

// CountRecentCommentsFromIP returns how many comments the given IP posted in
// the last `since` seconds. Used as a lightweight rate-limit signal by the
// public POST handler.
func (s *Store) CountRecentCommentsFromIP(ctx context.Context, ip string, since time.Duration) (int, error) {
	cutoff := time.Now().Add(-since).Unix()
	var n int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM messages WHERE ip_address = ? AND created_at >= ?`,
		ip, cutoff).Scan(&n); err != nil {
		return 0, fmt.Errorf("repo: CountRecentCommentsFromIP: %w", err)
	}
	return n, nil
}

func scanMessages(rows *sql.Rows) ([]domain.Message, error) {
	var out []domain.Message
	for rows.Next() {
		var m domain.Message
		var postedAt int64
		if err := rows.Scan(&m.ID, &m.WID, &m.EntryID, &m.Status, &postedAt,
			&m.AuthorName, &m.AuthorEmail, &m.AuthorURL, &m.Body,
			&m.IPAddress, &m.UserAgent); err != nil {
			return nil, fmt.Errorf("repo: scan message: %w", err)
		}
		m.PostedAt = time.Unix(postedAt, 0)
		out = append(out, m)
	}
	return out, rows.Err()
}

// AllPublishedEntries returns every published entry, newest first. Intended
// for full-site rebuilds rather than request-path rendering.
func (s *Store) AllPublishedEntries(ctx context.Context, wid int64) ([]domain.Entry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, wid, author_id, category_id, title, slug, keywords, body, more, format, status, posted_at, updated_at, likes_count, stamps_count, comments_count, og_bg_image_path
		FROM entries
		WHERE wid = ? AND status = ?
		ORDER BY posted_at DESC`, wid, domain.EntryPublished)
	if err != nil {
		return nil, fmt.Errorf("repo: AllPublishedEntries: %w", err)
	}
	defer rows.Close()
	return scanEntries(rows)
}

// AllCategories returns every category row for the weblog, ordered by
// sort_order then id.
func (s *Store) AllCategories(ctx context.Context, wid int64) ([]domain.Category, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, wid, parent_id, name, slug, sort_order, description, description_format, template_id
		FROM categories
		WHERE wid = ?
		ORDER BY sort_order, id`, wid)
	if err != nil {
		return nil, fmt.Errorf("repo: AllCategories: %w", err)
	}
	defer rows.Close()
	var out []domain.Category
	for rows.Next() {
		var c domain.Category
		if err := rows.Scan(&c.ID, &c.WID, &c.ParentID, &c.Name, &c.Slug, &c.SortOrder, &c.Description, &c.DescriptionFormat, &c.TemplateID); err != nil {
			return nil, fmt.Errorf("repo: scan category: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ArchivePeriod is one (year, month) bucket for which entries exist.
type ArchivePeriod struct {
	Year  int
	Month int
	// Count is populated by ArchivePeriodsWithCounts; zero from the
	// older ArchivePeriods shape (it didn't need the tally).
	Count int64
}

// ArchivePeriodsWithCounts is ArchivePeriods + an entry-count per
// bucket, for the sidebar `{archives_list}` fragment. One GROUP BY
// keeps it to a single scan.
func (s *Store) ArchivePeriodsWithCounts(ctx context.Context, wid int64) ([]ArchivePeriod, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			CAST(strftime('%Y', datetime(posted_at, 'unixepoch')) AS INTEGER) AS y,
			CAST(strftime('%m', datetime(posted_at, 'unixepoch')) AS INTEGER) AS m,
			COUNT(*) AS c
		FROM entries
		WHERE wid = ? AND status = ?
		GROUP BY y, m
		ORDER BY y DESC, m DESC`, wid, domain.EntryPublished)
	if err != nil {
		return nil, fmt.Errorf("repo: ArchivePeriodsWithCounts: %w", err)
	}
	defer rows.Close()
	var out []ArchivePeriod
	for rows.Next() {
		var p ArchivePeriod
		if err := rows.Scan(&p.Year, &p.Month, &p.Count); err != nil {
			return nil, fmt.Errorf("repo: scan period: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// RecentApprovedMessage bundles one comment + its entry's title so the
// sidebar recent-comments block can link back to the entry without a
// second fetch.
type RecentApprovedMessage struct {
	EntryID    int64
	EntryTitle string
	EntrySlug  string
	AuthorName string
	PostedAt   time.Time
}

// RecentApprovedMessages fetches the N most recent approved comments
// across every entry on the weblog — powers the SB3
// `{recent_comment_list}` sidebar block.
func (s *Store) RecentApprovedMessages(ctx context.Context, wid int64, limit int) ([]RecentApprovedMessage, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.entry_id, e.title, e.slug, m.author_name, m.posted_at
		FROM messages m
		JOIN entries e ON e.id = m.entry_id
		WHERE m.wid = ? AND m.status = ? AND e.status = ?
		ORDER BY m.posted_at DESC
		LIMIT ?`, wid, domain.MessageApproved, domain.EntryPublished, limit)
	if err != nil {
		return nil, fmt.Errorf("repo: RecentApprovedMessages: %w", err)
	}
	defer rows.Close()
	out := []RecentApprovedMessage{}
	for rows.Next() {
		var rm RecentApprovedMessage
		var postedAt int64
		if err := rows.Scan(&rm.EntryID, &rm.EntryTitle, &rm.EntrySlug, &rm.AuthorName, &postedAt); err != nil {
			return nil, fmt.Errorf("repo: scan recent message: %w", err)
		}
		rm.PostedAt = time.Unix(postedAt, 0)
		out = append(out, rm)
	}
	return out, rows.Err()
}

// ArchivePeriods returns every distinct (year, month) pair for which the
// weblog has at least one published entry, newest first. Uses SQLite's
// strftime to extract fields from the integer unix timestamp.
func (s *Store) ArchivePeriods(ctx context.Context, wid int64) ([]ArchivePeriod, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT
			CAST(strftime('%Y', datetime(posted_at, 'unixepoch')) AS INTEGER) AS y,
			CAST(strftime('%m', datetime(posted_at, 'unixepoch')) AS INTEGER) AS m
		FROM entries
		WHERE wid = ? AND status = ?
		ORDER BY y DESC, m DESC`, wid, domain.EntryPublished)
	if err != nil {
		return nil, fmt.Errorf("repo: ArchivePeriods: %w", err)
	}
	defer rows.Close()
	var out []ArchivePeriod
	for rows.Next() {
		var p ArchivePeriod
		if err := rows.Scan(&p.Year, &p.Month); err != nil {
			return nil, fmt.Errorf("repo: scan period: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
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
		SELECT id, wid, author_id, category_id, title, slug, keywords, body, more, format, status, posted_at, updated_at, likes_count, stamps_count, comments_count, og_bg_image_path
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
		SELECT id, wid, author_id, category_id, title, slug, keywords, body, more, format, status, posted_at, updated_at, likes_count, stamps_count, comments_count, og_bg_image_path
		FROM entries
		WHERE wid = ? AND status = ?
		ORDER BY posted_at DESC
		LIMIT ?`, wid, domain.EntryPublished, limit)
	if err != nil {
		return nil, fmt.Errorf("repo: RecentPublishedEntries: %w", err)
	}
	defer rows.Close()
	return scanEntries(rows)
}

// CategoryByID fetches one category row. ErrNotFound on miss.
func (s *Store) CategoryByID(ctx context.Context, wid, id int64) (*domain.Category, error) {
	var c domain.Category
	err := s.db.QueryRowContext(ctx, `
		SELECT id, wid, parent_id, name, slug, sort_order, description, description_format, template_id
		FROM categories WHERE wid = ? AND id = ?`, wid, id).Scan(
		&c.ID, &c.WID, &c.ParentID, &c.Name, &c.Slug, &c.SortOrder, &c.Description, &c.DescriptionFormat, &c.TemplateID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("repo: CategoryByID: %w", err)
	}
	return &c, nil
}

// PublishedEntriesByCategory returns published entries in the given category,
// newest first. SB v3 also supported additional categories via `entry_add`;
// when that becomes relevant we can widen the filter here.
func (s *Store) PublishedEntriesByCategory(ctx context.Context, wid, catID int64, limit int) ([]domain.Entry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, wid, author_id, category_id, title, slug, keywords, body, more, format, status, posted_at, updated_at, likes_count, stamps_count, comments_count, og_bg_image_path
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
func (s *Store) PublishedEntriesInRange(ctx context.Context, wid int64, from, to time.Time, limit int) ([]domain.Entry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, wid, author_id, category_id, title, slug, keywords, body, more, format, status, posted_at, updated_at, likes_count, stamps_count, comments_count, og_bg_image_path
		FROM entries
		WHERE wid = ? AND status = ? AND posted_at >= ? AND posted_at < ?
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
		if err := rows.Scan(&e.ID, &e.WID, &e.AuthorID, &e.CategoryID, &e.Title, &e.Slug, &e.Keywords, &e.Body, &e.More, &e.Format, &e.Status, &postedAt, &updatedAt, &e.LikesCount, &e.StampsCount, &e.CommentsCount, &e.OGBGImagePath); err != nil {
			return nil, fmt.Errorf("repo: scan entry: %w", err)
		}
		e.PostedAt = time.Unix(postedAt, 0)
		e.UpdatedAt = time.Unix(updatedAt, 0)
		out = append(out, e)
	}
	return out, rows.Err()
}

// CategoriesByIDs returns the categories matching the given ids as a map
// keyed by id, so a caller rendering a list of entries can look up each
// entry's category without N+1 queries.
func (s *Store) CategoriesByIDs(ctx context.Context, ids []int64) (map[int64]domain.Category, error) {
	if len(ids) == 0 {
		return map[int64]domain.Category{}, nil
	}
	// Build `?,?,?` placeholders.
	args := make([]any, 0, len(ids))
	placeholders := make([]byte, 0, 2*len(ids))
	for i, id := range ids {
		if i > 0 {
			placeholders = append(placeholders, ',')
		}
		placeholders = append(placeholders, '?')
		args = append(args, id)
	}
	q := "SELECT id, wid, parent_id, name, slug, sort_order, description, description_format, template_id FROM categories WHERE id IN (" + string(placeholders) + ")"
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("repo: CategoriesByIDs: %w", err)
	}
	defer rows.Close()

	out := make(map[int64]domain.Category, len(ids))
	for rows.Next() {
		var c domain.Category
		if err := rows.Scan(&c.ID, &c.WID, &c.ParentID, &c.Name, &c.Slug, &c.SortOrder, &c.Description, &c.DescriptionFormat, &c.TemplateID); err != nil {
			return nil, fmt.Errorf("repo: scan category: %w", err)
		}
		out[c.ID] = c
	}
	return out, rows.Err()
}

// ---- categories (admin CRUD) -------------------------------------------

// CreateCategory inserts a new category and returns its id.
func (s *Store) CreateCategory(ctx context.Context, c domain.Category, sortOrder int) (int64, error) {
	now := time.Now().Unix()
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO categories (wid, parent_id, name, slug, sort_order, description, description_format, template_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.WID, c.ParentID, c.Name, c.Slug, sortOrder, c.Description, defaultDescFormat(c.DescriptionFormat), c.TemplateID, now, now)
	if err != nil {
		return 0, fmt.Errorf("repo: CreateCategory: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("repo: CreateCategory lastid: %w", err)
	}
	return id, nil
}

// UpdateCategory overwrites name, slug, parent, and sort order. created_at
// stays put; updated_at advances.
func (s *Store) UpdateCategory(ctx context.Context, c domain.Category, sortOrder int) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE categories
		SET parent_id = ?, name = ?, slug = ?, sort_order = ?,
		    description = ?, description_format = ?, template_id = ?, updated_at = ?
		WHERE wid = ? AND id = ?`,
		c.ParentID, c.Name, c.Slug, sortOrder,
		c.Description, defaultDescFormat(c.DescriptionFormat), c.TemplateID, time.Now().Unix(), c.WID, c.ID)
	if err != nil {
		return fmt.Errorf("repo: UpdateCategory: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteCategory removes a category. Any entry that referenced it is
// reassigned to "uncategorised" (category_id = -1) in the same transaction
// so the admin listing never leaves an entry pointing at a dead id.
func (s *Store) DeleteCategory(ctx context.Context, wid, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("repo: DeleteCategory begin: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err := tx.ExecContext(ctx,
		`UPDATE entries SET category_id = ?, updated_at = ?
		 WHERE wid = ? AND category_id = ?`,
		domain.Uncategorized, time.Now().Unix(), wid, id); err != nil {
		return fmt.Errorf("repo: DeleteCategory reassign: %w", err)
	}

	res, err := tx.ExecContext(ctx,
		`DELETE FROM categories WHERE wid = ? AND id = ?`, wid, id)
	if err != nil {
		return fmt.Errorf("repo: DeleteCategory: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("repo: DeleteCategory commit: %w", err)
	}
	tx = nil
	return nil
}

// ReorderCategories rewrites sort_order for the given ids so the list
// order matches the input slice. Used by the admin drag-and-drop reorder
// endpoint. Missing ids are left untouched (no error) so a stale client
// can't blank-out the whole table. All writes happen in one transaction
// so a concurrent edit doesn't leave the list half-reordered.
func (s *Store) ReorderCategories(ctx context.Context, wid int64, orderedIDs []int64) error {
	if len(orderedIDs) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("repo: ReorderCategories begin: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	stmt, err := tx.PrepareContext(ctx,
		`UPDATE categories SET sort_order = ?, updated_at = ? WHERE wid = ? AND id = ?`)
	if err != nil {
		return fmt.Errorf("repo: ReorderCategories prepare: %w", err)
	}
	defer stmt.Close()
	now := time.Now().Unix()
	for i, id := range orderedIDs {
		if _, err := stmt.ExecContext(ctx, i, now, wid, id); err != nil {
			return fmt.Errorf("repo: ReorderCategories update id=%d: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("repo: ReorderCategories commit: %w", err)
	}
	tx = nil
	return nil
}

// CountEntriesByCategory returns how many entries currently reference the
// given category id (any status — the admin wants the full count, not just
// the public one). Used to warn before a destructive delete.
func (s *Store) CountEntriesByCategory(ctx context.Context, wid, catID int64) (int64, error) {
	var n int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM entries WHERE wid = ? AND category_id = ?`,
		wid, catID).Scan(&n); err != nil {
		return 0, fmt.Errorf("repo: CountEntriesByCategory: %w", err)
	}
	return n, nil
}

// ---- pagination count queries -----------------------------------------
//
// Each list route pairs with a count: handlers need `total_entries` to
// compute `page_num` for the SB3 `{page_num}` tag. Filters mirror the
// corresponding PublishedEntries* query so count + page slice never
// disagree.

// CountPublishedEntries returns the total number of published entries
// for the weblog — the denominator for the home page's pagination.
func (s *Store) CountPublishedEntries(ctx context.Context, wid int64) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM entries WHERE wid = ? AND status = ?`,
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
// time slice the handler fetches.
func (s *Store) CountPublishedEntriesByTag(ctx context.Context, wid, tagID int64) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM entries e
		JOIN entry_tags et ON et.entry_id = e.id
		WHERE e.wid = ? AND e.status = ? AND et.tag_id = ?`,
		wid, domain.EntryPublished, tagID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("repo: CountPublishedEntriesByTag: %w", err)
	}
	return n, nil
}

// CountPublishedEntriesInRange mirrors PublishedEntriesInRange.
func (s *Store) CountPublishedEntriesInRange(ctx context.Context, wid int64, from, to time.Time) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM entries WHERE wid = ? AND status = ? AND posted_at >= ? AND posted_at < ?`,
		wid, domain.EntryPublished, from.Unix(), to.Unix()).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("repo: CountPublishedEntriesInRange: %w", err)
	}
	return n, nil
}

// RecentPublishedEntriesPage is RecentPublishedEntries + an OFFSET —
// the same shape SB3 implements via `LIMIT disp OFFSET (page*disp)`.
// Caller computes offset = (page-1) * limit.
func (s *Store) RecentPublishedEntriesPage(ctx context.Context, wid int64, limit, offset int) ([]domain.Entry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, wid, author_id, category_id, title, slug, keywords, body, more, format, status, posted_at, updated_at, likes_count, stamps_count, comments_count, og_bg_image_path
		FROM entries
		WHERE wid = ? AND status = ?
		ORDER BY posted_at DESC
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
		SELECT id, wid, author_id, category_id, title, slug, keywords, body, more, format, status, posted_at, updated_at, likes_count, stamps_count, comments_count, og_bg_image_path
		FROM entries
		WHERE wid = ? AND status = ? AND category_id = ?
		ORDER BY posted_at DESC
		LIMIT ? OFFSET ?`, wid, domain.EntryPublished, catID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("repo: PublishedEntriesByCategoryPage: %w", err)
	}
	defer rows.Close()
	return scanEntries(rows)
}

// PublishedEntriesInRangePage is the paginated sibling of
// PublishedEntriesInRange. Used by year/month archive pagination.
func (s *Store) PublishedEntriesInRangePage(ctx context.Context, wid int64, from, to time.Time, limit, offset int) ([]domain.Entry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, wid, author_id, category_id, title, slug, keywords, body, more, format, status, posted_at, updated_at, likes_count, stamps_count, comments_count, og_bg_image_path
		FROM entries
		WHERE wid = ? AND status = ? AND posted_at >= ? AND posted_at < ?
		ORDER BY posted_at DESC
		LIMIT ? OFFSET ?`, wid, domain.EntryPublished, from.Unix(), to.Unix(), limit, offset)
	if err != nil {
		return nil, fmt.Errorf("repo: PublishedEntriesInRangePage: %w", err)
	}
	defer rows.Close()
	return scanEntries(rows)
}

// ---- images -------------------------------------------------------------

// CreateImage inserts a new image row and returns its id. Timestamps default
// to now. Callers write the file + thumbnail to disk before calling this so
// the DB row is a pointer to bytes that already exist.
func (s *Store) CreateImage(ctx context.Context, img domain.Image) (int64, error) {
	now := time.Now().Unix()
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO images (wid, uploaded_by, filename, stored_path, thumb_path, mime_type, size_bytes, width, height, alt_text, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		img.WID, img.UploadedBy, img.Filename, img.StoredPath, img.ThumbPath,
		img.MimeType, img.SizeBytes, img.Width, img.Height, img.AltText, now, now)
	if err != nil {
		return 0, fmt.Errorf("repo: CreateImage: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("repo: CreateImage lastid: %w", err)
	}
	return id, nil
}

// ListImagesForAdmin returns the weblog's images newest first, with basic
// pagination. limit<=0 defaults to 60.
func (s *Store) ListImagesForAdmin(ctx context.Context, wid int64, limit, offset int) ([]domain.Image, error) {
	if limit <= 0 {
		limit = 60
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, wid, uploaded_by, filename, stored_path, thumb_path, mime_type, size_bytes, width, height, alt_text, created_at, updated_at
		FROM images
		WHERE wid = ?
		ORDER BY created_at DESC, id DESC
		LIMIT ? OFFSET ?`, wid, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("repo: ListImagesForAdmin: %w", err)
	}
	defer rows.Close()
	return scanImages(rows)
}

// CountImages returns the total number of image rows for the weblog.
// Used to paginate the admin gallery.
func (s *Store) CountImages(ctx context.Context, wid int64) (int64, error) {
	var n int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM images WHERE wid = ?`, wid).Scan(&n); err != nil {
		return 0, fmt.Errorf("repo: CountImages: %w", err)
	}
	return n, nil
}

// ImageByID returns one image row. ErrNotFound on miss.
func (s *Store) ImageByID(ctx context.Context, wid, id int64) (*domain.Image, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, wid, uploaded_by, filename, stored_path, thumb_path, mime_type, size_bytes, width, height, alt_text, created_at, updated_at
		FROM images WHERE wid = ? AND id = ?`, wid, id)
	var img domain.Image
	var createdAt, updatedAt int64
	if err := row.Scan(&img.ID, &img.WID, &img.UploadedBy, &img.Filename, &img.StoredPath,
		&img.ThumbPath, &img.MimeType, &img.SizeBytes, &img.Width, &img.Height,
		&img.AltText, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("repo: ImageByID: %w", err)
	}
	img.CreatedAt = time.Unix(createdAt, 0)
	img.UpdatedAt = time.Unix(updatedAt, 0)
	return &img, nil
}

// UpdateImageAltText overwrites the alt text for one image. Used by
// the AI alt generator (auto on upload) + the future manual edit
// path. Silent no-op if id doesn't exist — the caller already
// knows what it uploaded, a missing row means something else is broken
// and the error would just be noise in the goroutine.
func (s *Store) UpdateImageAltText(ctx context.Context, wid, id int64, alt string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE images SET alt_text = ?, updated_at = ? WHERE wid = ? AND id = ?`,
		alt, time.Now().Unix(), wid, id)
	if err != nil {
		return fmt.Errorf("repo: UpdateImageAltText: %w", err)
	}
	return nil
}

// DeleteImage removes an image row. The on-disk file/thumbnail cleanup is
// the caller's responsibility (best-effort unlink) — we keep repo pure SQL.
func (s *Store) DeleteImage(ctx context.Context, wid, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM images WHERE wid = ? AND id = ?`, wid, id)
	if err != nil {
		return fmt.Errorf("repo: DeleteImage: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func scanImages(rows *sql.Rows) ([]domain.Image, error) {
	var out []domain.Image
	for rows.Next() {
		var img domain.Image
		var createdAt, updatedAt int64
		if err := rows.Scan(&img.ID, &img.WID, &img.UploadedBy, &img.Filename, &img.StoredPath,
			&img.ThumbPath, &img.MimeType, &img.SizeBytes, &img.Width, &img.Height,
			&img.AltText, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("repo: scan image: %w", err)
		}
		img.CreatedAt = time.Unix(createdAt, 0)
		img.UpdatedAt = time.Unix(updatedAt, 0)
		out = append(out, img)
	}
	return out, rows.Err()
}

// UserByName looks up one user by login name. Used on login.
func (s *Store) UserByName(ctx context.Context, name string) (*domain.User, string, error) {
	var u domain.User
	var hash string
	var listVis int
	var autoAlt int
	err := s.db.QueryRowContext(ctx, `
		SELECT id, wid, name, display_name, email, role, description, description_format, list_visible, sort_order,
		       ai_kind, ai_base_url, ai_model, ai_api_key_enc, ai_auto_alt, ai_timeout_seconds,
		       password_hash
		FROM users WHERE name = ?`, name).Scan(
		&u.ID, &u.WID, &u.Name, &u.DisplayName, &u.Email, &u.Role, &u.Description, &u.DescriptionFormat, &listVis, &u.SortOrder,
		&u.AIKind, &u.AIBaseURL, &u.AIModel, &u.AIAPIKeyEnc, &autoAlt, &u.AITimeoutSeconds,
		&hash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, "", ErrNotFound
		}
		return nil, "", fmt.Errorf("repo: UserByName: %w", err)
	}
	u.ListVisible = listVis != 0
	u.AIAutoAlt = autoAlt != 0
	return &u, hash, nil
}

// UserByID looks up one user by primary key. Used by session middleware.
func (s *Store) UserByID(ctx context.Context, id int64) (*domain.User, error) {
	var u domain.User
	var listVis, autoAlt int
	err := s.db.QueryRowContext(ctx, `
		SELECT id, wid, name, display_name, email, role, description, description_format, list_visible, sort_order,
		       ai_kind, ai_base_url, ai_model, ai_api_key_enc, ai_auto_alt, ai_timeout_seconds
		FROM users WHERE id = ?`, id).Scan(
		&u.ID, &u.WID, &u.Name, &u.DisplayName, &u.Email, &u.Role, &u.Description, &u.DescriptionFormat, &listVis, &u.SortOrder,
		&u.AIKind, &u.AIBaseURL, &u.AIModel, &u.AIAPIKeyEnc, &autoAlt, &u.AITimeoutSeconds)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("repo: UserByID: %w", err)
	}
	u.ListVisible = listVis != 0
	u.AIAutoAlt = autoAlt != 0
	return &u, nil
}

// CreateSession persists a new session and returns its db id.
func (s *Store) CreateSession(ctx context.Context, token string, userID, expiresAt int64) error {
	now := time.Now().Unix()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sessions (token, user_id, expires_at, created_at)
		VALUES (?, ?, ?, ?)`, token, userID, expiresAt, now)
	if err != nil {
		return fmt.Errorf("repo: CreateSession: %w", err)
	}
	return nil
}

// SessionUser returns the user behind a session token if the session exists
// and is not expired. ErrNotFound on miss / expiry.
func (s *Store) SessionUser(ctx context.Context, token string) (*domain.User, error) {
	var u domain.User
	var expiresAt int64
	var listVis, autoAlt int
	err := s.db.QueryRowContext(ctx, `
		SELECT u.id, u.wid, u.name, u.display_name, u.email, u.role,
		       u.description, u.description_format, u.list_visible, u.sort_order,
		       u.ai_kind, u.ai_base_url, u.ai_model, u.ai_api_key_enc, u.ai_auto_alt, u.ai_timeout_seconds,
		       s.expires_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token = ?`, token).Scan(
		&u.ID, &u.WID, &u.Name, &u.DisplayName, &u.Email, &u.Role,
		&u.Description, &u.DescriptionFormat, &listVis, &u.SortOrder,
		&u.AIKind, &u.AIBaseURL, &u.AIModel, &u.AIAPIKeyEnc, &autoAlt, &u.AITimeoutSeconds,
		&expiresAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("repo: SessionUser: %w", err)
	}
	if expiresAt <= time.Now().Unix() {
		return nil, ErrNotFound
	}
	u.ListVisible = listVis != 0
	u.AIAutoAlt = autoAlt != 0
	return &u, nil
}

// DeleteSession removes the session row for a given token (idempotent).
func (s *Store) DeleteSession(ctx context.Context, token string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token = ?`, token); err != nil {
		return fmt.Errorf("repo: DeleteSession: %w", err)
	}
	return nil
}

// DeleteExpiredSessions sweeps expired rows so the table stays small. Called
// on a cadence (e.g. from a scheduled cleanup) — safe to call eagerly.
func (s *Store) DeleteExpiredSessions(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, time.Now().Unix())
	if err != nil {
		return 0, fmt.Errorf("repo: DeleteExpiredSessions: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// UsersByIDs returns the users matching the given ids as a map keyed by id.
func (s *Store) UsersByIDs(ctx context.Context, ids []int64) (map[int64]domain.User, error) {
	if len(ids) == 0 {
		return map[int64]domain.User{}, nil
	}
	args := make([]any, 0, len(ids))
	placeholders := make([]byte, 0, 2*len(ids))
	for i, id := range ids {
		if i > 0 {
			placeholders = append(placeholders, ',')
		}
		placeholders = append(placeholders, '?')
		args = append(args, id)
	}
	q := "SELECT id, wid, name, display_name, email, role, description, description_format, list_visible, sort_order FROM users WHERE id IN (" + string(placeholders) + ")"
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("repo: UsersByIDs: %w", err)
	}
	defer rows.Close()

	out := make(map[int64]domain.User, len(ids))
	for rows.Next() {
		var u domain.User
		var listVis int
		if err := rows.Scan(&u.ID, &u.WID, &u.Name, &u.DisplayName, &u.Email, &u.Role, &u.Description, &u.DescriptionFormat, &listVis, &u.SortOrder); err != nil {
			return nil, fmt.Errorf("repo: scan user: %w", err)
		}
		u.ListVisible = listVis != 0
		out[u.ID] = u
	}
	return out, rows.Err()
}
