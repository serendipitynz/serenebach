package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/serendipitynz/serenebach/internal/domain"
)

// templateColumns is the canonical column list for the templates table
// when callers need the full row (sort_order + timestamps included).
// Used by TemplateByID and ListTemplatesForAdmin. ActiveTemplate runs
// on every public render and intentionally pulls a narrower 8-column
// subset, so it does not share this constant.
const templateColumns = `id, wid, name, is_active, main_body, entry_body, css, info, sort_order, created_at, updated_at`

// templateAssetColumns is the canonical column list for the
// template_assets table. Order must match the inline Scan call sites
// in ListTemplateAssets and TemplateAssetByID.
const templateAssetColumns = `id, template_id, filename, mime_type, size_bytes, created_at, updated_at`

// ErrTemplateActive is returned when DeleteTemplate is called on the row
// currently flagged is_active. The site needs at least one active row to
// render with, so callers must activate a different template first.
var ErrTemplateActive = errors.New("repo: cannot delete the active template")

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
		SELECT `+templateColumns+`
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
		SELECT `+templateColumns+`
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
		SELECT `+templateAssetColumns+`
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
		SELECT `+templateAssetColumns+`
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
