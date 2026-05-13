package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/serendipitynz/serenebach/internal/domain"
)

// categoryColumns is the canonical column list for the categories table.
// Order must match the inline Scan call sites.
const categoryColumns = `id, wid, parent_id, name, slug, sort_order, description, description_format, template_id`

// AllCategories returns every category row for the weblog, ordered by
// sort_order then id.
func (s *Store) AllCategories(ctx context.Context, wid int64) ([]domain.Category, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+categoryColumns+`
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

// CategoryByID fetches one category row. ErrNotFound on miss.
func (s *Store) CategoryByID(ctx context.Context, wid, id int64) (*domain.Category, error) {
	var c domain.Category
	err := s.db.QueryRowContext(ctx, `
		SELECT `+categoryColumns+`
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
	q := "SELECT " + categoryColumns + " FROM categories WHERE id IN (" + string(placeholders) + ")"
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
