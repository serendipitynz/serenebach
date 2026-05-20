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

// pageColumns is the canonical column list for the pages table. Order
// must match the Scan argument order in scanPage / scanPages.
const pageColumns = `id, wid, author_id, title, body, format, slug, template_id, sort_order, status, og_bg_image_path, created_at, updated_at`

// pageColumnsP is pageColumns qualified with the `p.` alias so the
// admin list query can join templates without ambiguity.
const pageColumnsP = `p.id, p.wid, p.author_id, p.title, p.body, p.format, p.slug, p.template_id, p.sort_order, p.status, p.og_bg_image_path, p.created_at, p.updated_at`

// PageBySlug returns one page by its slug (including the leading "/").
func (s *Store) PageBySlug(ctx context.Context, wid int64, slug string) (*domain.Page, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT `+pageColumns+`
		FROM pages WHERE wid = ? AND slug = ?`, wid, slug)
	return scanPage(row)
}

// PageByID returns one page by id.
func (s *Store) PageByID(ctx context.Context, wid, id int64) (*domain.Page, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT `+pageColumns+`
		FROM pages WHERE wid = ? AND id = ?`, wid, id)
	return scanPage(row)
}

// PageSortKey is a typed enum of the columns the admin page list can
// sort by. Default is the manual sort_order column so an admin who
// hand-ordered their pages doesn't lose that arrangement on landing.
type PageSortKey int

const (
	PageSortDefault  PageSortKey = iota // sort_order ASC, id ASC
	PageSortTitle
	PageSortSlug
	PageSortTemplate
	PageSortStatus
	PageSortUpdated
)

// orderClause returns the SQL fragment for ORDER BY. Always qualified
// with the pages alias `p` (or `t` for the JOINed templates table).
func (k PageSortKey) orderClause() string {
	switch k {
	case PageSortTitle:
		return "p.title"
	case PageSortSlug:
		return "p.slug"
	case PageSortTemplate:
		// NULL template (page uses site default) sorts as empty string.
		return "COALESCE(t.name, '')"
	case PageSortStatus:
		return "p.status"
	case PageSortUpdated:
		return "p.updated_at"
	default:
		return "p.sort_order"
	}
}

// String returns the URL-form name of the sort key.
func (k PageSortKey) String() string {
	switch k {
	case PageSortTitle:
		return "title"
	case PageSortSlug:
		return "slug"
	case PageSortTemplate:
		return "template"
	case PageSortStatus:
		return "status"
	case PageSortUpdated:
		return "updated"
	default:
		return ""
	}
}

// ParsePageSortKey maps a ?sort= query value to the enum. Unknown /
// empty values fall back to PageSortDefault (sort_order).
func ParsePageSortKey(s string) PageSortKey {
	switch s {
	case "title":
		return PageSortTitle
	case "slug":
		return PageSortSlug
	case "template":
		return PageSortTemplate
	case "status":
		return PageSortStatus
	case "updated":
		return PageSortUpdated
	default:
		return PageSortDefault
	}
}

// ListPagesQuery bundles the admin page list's filter / sort
// parameters. The zero value reproduces the legacy behaviour: every
// page, ordered by sort_order then id. Limit <= 0 disables LIMIT.
type ListPagesQuery struct {
	// OwnerID, when non-nil, restricts results to pages authored by
	// that user. Lets regular-tier author scoping happen in SQL.
	OwnerID *int64
	// Search matches LIKE-needle against title, body, and slug.
	Search  string
	SortBy  PageSortKey
	SortDir SortDir
	Limit   int
	Offset  int
}

// ListPagesForAdmin returns pages matching q. Default (zero-value
// query) preserves the historical "ORDER BY sort_order, id" so
// callers like validatePageSlug keep working unchanged.
func (s *Store) ListPagesForAdmin(ctx context.Context, wid int64, q ListPagesQuery) ([]domain.Page, error) {
	sqlText, args := buildPagesListSQL(wid, q)
	rows, err := s.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, fmt.Errorf("repo: ListPagesForAdmin: %w", err)
	}
	defer rows.Close()
	return scanPages(rows)
}

// CountPagesForAdmin returns how many rows ListPagesForAdmin would
// produce ignoring Limit / Offset.
func (s *Store) CountPagesForAdmin(ctx context.Context, wid int64, q ListPagesQuery) (int64, error) {
	sqlText, args := buildPagesCountSQL(wid, q)
	var n int64
	if err := s.db.QueryRowContext(ctx, sqlText, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("repo: CountPagesForAdmin: %w", err)
	}
	return n, nil
}

func buildPagesListSQL(wid int64, q ListPagesQuery) (string, []any) {
	var b strings.Builder
	b.WriteString(`SELECT ` + pageColumnsP + `
		FROM pages p
		LEFT JOIN templates t ON t.id = p.template_id
		WHERE p.wid = ?`)
	args := []any{wid}
	appendPagesFilters(&b, &args, q)
	b.WriteString(` ORDER BY `)
	if q.SortBy == PageSortDefault {
		// The historical default is two columns — keep both so manual
		// reorderings render the way the admin set them up.
		b.WriteString(`p.sort_order ASC, p.id ASC`)
	} else {
		b.WriteString(q.SortBy.orderClause())
		b.WriteByte(' ')
		b.WriteString(q.SortDir.String())
		// Stable tie-breaker.
		b.WriteString(`, p.id DESC`)
	}
	if q.Limit > 0 {
		b.WriteString(` LIMIT ?`)
		args = append(args, q.Limit)
		if q.Offset > 0 {
			b.WriteString(` OFFSET ?`)
			args = append(args, q.Offset)
		}
	}
	return b.String(), args
}

func buildPagesCountSQL(wid int64, q ListPagesQuery) (string, []any) {
	var b strings.Builder
	b.WriteString(`SELECT COUNT(*) FROM pages p WHERE p.wid = ?`)
	args := []any{wid}
	appendPagesFilters(&b, &args, q)
	return b.String(), args
}

func appendPagesFilters(b *strings.Builder, args *[]any, q ListPagesQuery) {
	if q.OwnerID != nil {
		b.WriteString(` AND p.author_id = ?`)
		*args = append(*args, *q.OwnerID)
	}
	if q.Search != "" {
		needle := "%" + escapeLike(q.Search) + "%"
		b.WriteString(` AND (p.title LIKE ? ESCAPE '\'
			OR p.body LIKE ? ESCAPE '\'
			OR p.slug LIKE ? ESCAPE '\')`)
		*args = append(*args, needle, needle, needle)
	}
}

// PublishedPages returns only published pages, ordered by sort_order then id.
func (s *Store) PublishedPages(ctx context.Context, wid int64) ([]domain.Page, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+pageColumns+`
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
