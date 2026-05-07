package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/serendipitynz/serenebach/internal/domain"
)

// PageBySlug returns one page by its slug (including the leading "/").
func (s *Store) PageBySlug(ctx context.Context, wid int64, slug string) (*domain.Page, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, wid, author_id, title, body, format, slug, template_id, sort_order, status, og_bg_image_path, created_at, updated_at
		FROM pages WHERE wid = ? AND slug = ?`, wid, slug)
	return scanPage(row)
}

// PageByID returns one page by id.
func (s *Store) PageByID(ctx context.Context, wid, id int64) (*domain.Page, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, wid, author_id, title, body, format, slug, template_id, sort_order, status, og_bg_image_path, created_at, updated_at
		FROM pages WHERE wid = ? AND id = ?`, wid, id)
	return scanPage(row)
}

// ListPagesForAdmin returns every page for the weblog ordered by sort_order then id.
func (s *Store) ListPagesForAdmin(ctx context.Context, wid int64) ([]domain.Page, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, wid, author_id, title, body, format, slug, template_id, sort_order, status, og_bg_image_path, created_at, updated_at
		FROM pages WHERE wid = ? ORDER BY sort_order, id`, wid)
	if err != nil {
		return nil, fmt.Errorf("repo: ListPagesForAdmin: %w", err)
	}
	defer rows.Close()
	return scanPages(rows)
}

// PublishedPages returns only published pages, ordered by sort_order then id.
func (s *Store) PublishedPages(ctx context.Context, wid int64) ([]domain.Page, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, wid, author_id, title, body, format, slug, template_id, sort_order, status, og_bg_image_path, created_at, updated_at
		FROM pages WHERE wid = ? AND status = ? ORDER BY sort_order, id`, wid, domain.PagePublished)
	if err != nil {
		return nil, fmt.Errorf("repo: PublishedPages: %w", err)
	}
	defer rows.Close()
	return scanPages(rows)
}

// CreatePage inserts a new page and returns its id.
func (s *Store) CreatePage(ctx context.Context, p domain.Page) (int64, error) {
	now := time.Now().Unix()
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO pages (wid, author_id, title, body, format, slug, template_id, sort_order, status, og_bg_image_path, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.WID, p.AuthorID, p.Title, p.Body, p.Format, p.Slug, p.TemplateID, p.SortOrder, p.Status, p.OGBGImagePath, now, now)
	if err != nil {
		if isUniqueViolation(err) {
			return 0, ErrSlugInUse
		}
		return 0, fmt.Errorf("repo: CreatePage: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("repo: CreatePage lastid: %w", err)
	}
	return id, nil
}

// UpdatePage overwrites the editable fields of an existing page.
func (s *Store) UpdatePage(ctx context.Context, p domain.Page) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE pages SET
			title = ?, body = ?, format = ?, slug = ?, template_id = ?,
			sort_order = ?, status = ?, og_bg_image_path = ?, updated_at = ?
		WHERE wid = ? AND id = ?`,
		p.Title, p.Body, p.Format, p.Slug, p.TemplateID,
		p.SortOrder, p.Status, p.OGBGImagePath, time.Now().Unix(), p.WID, p.ID)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrSlugInUse
		}
		return fmt.Errorf("repo: UpdatePage: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeletePage removes a page by id.
func (s *Store) DeletePage(ctx context.Context, wid, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM pages WHERE wid = ? AND id = ?`, wid, id)
	if err != nil {
		return fmt.Errorf("repo: DeletePage: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func scanPage(row *sql.Row) (*domain.Page, error) {
	var p domain.Page
	var createdAt, updatedAt int64
	if err := row.Scan(&p.ID, &p.WID, &p.AuthorID, &p.Title, &p.Body, &p.Format, &p.Slug, &p.TemplateID, &p.SortOrder, &p.Status, &p.OGBGImagePath, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("repo: scan page: %w", err)
	}
	p.CreatedAt = time.Unix(createdAt, 0)
	p.UpdatedAt = time.Unix(updatedAt, 0)
	return &p, nil
}

func scanPages(rows *sql.Rows) ([]domain.Page, error) {
	var out []domain.Page
	for rows.Next() {
		var p domain.Page
		var createdAt, updatedAt int64
		if err := rows.Scan(&p.ID, &p.WID, &p.AuthorID, &p.Title, &p.Body, &p.Format, &p.Slug, &p.TemplateID, &p.SortOrder, &p.Status, &p.OGBGImagePath, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("repo: scan page: %w", err)
		}
		p.CreatedAt = time.Unix(createdAt, 0)
		p.UpdatedAt = time.Unix(updatedAt, 0)
		out = append(out, p)
	}
	return out, rows.Err()
}
