package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/serendipitynz/serenebach/internal/domain"
)

// customTagColumns is the canonical column list for the site_custom_tags
// table. Order must match the inline Scan call sites.
const customTagColumns = `id, wid, name, value, created_at, updated_at`

// ---- custom tags ---------------------------------------------------------

// ListCustomTags returns every custom tag for the weblog, ordered by name.
func (s *Store) ListCustomTags(ctx context.Context, wid int64) ([]domain.CustomTag, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+customTagColumns+`
		FROM site_custom_tags
		WHERE wid = ?
		ORDER BY name`, wid)
	if err != nil {
		return nil, fmt.Errorf("repo: ListCustomTags: %w", err)
	}
	defer rows.Close()

	var out []domain.CustomTag
	for rows.Next() {
		var ct domain.CustomTag
		var createdAt, updatedAt int64
		if err := rows.Scan(&ct.ID, &ct.WID, &ct.Name, &ct.Value, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("repo: scan custom tag: %w", err)
		}
		ct.CreatedAt = time.Unix(createdAt, 0)
		ct.UpdatedAt = time.Unix(updatedAt, 0)
		out = append(out, ct)
	}
	return out, rows.Err()
}

// CustomTagByID fetches one custom tag row. ErrNotFound on miss.
func (s *Store) CustomTagByID(ctx context.Context, wid, id int64) (*domain.CustomTag, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT `+customTagColumns+`
		FROM site_custom_tags
		WHERE wid = ? AND id = ?`, wid, id)
	var ct domain.CustomTag
	var createdAt, updatedAt int64
	if err := row.Scan(&ct.ID, &ct.WID, &ct.Name, &ct.Value, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("repo: CustomTagByID: %w", err)
	}
	ct.CreatedAt = time.Unix(createdAt, 0)
	ct.UpdatedAt = time.Unix(updatedAt, 0)
	return &ct, nil
}

// CreateCustomTag inserts a new custom tag and returns its id.
func (s *Store) CreateCustomTag(ctx context.Context, ct domain.CustomTag) (int64, error) {
	now := time.Now().Unix()
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO site_custom_tags (wid, name, value, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)`,
		ct.WID, ct.Name, ct.Value, now, now)
	if err != nil {
		if isUniqueViolation(err) {
			return 0, ErrSlugInUse // reuse slug-in-use as name collision
		}
		return 0, fmt.Errorf("repo: CreateCustomTag: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("repo: CreateCustomTag lastid: %w", err)
	}
	return id, nil
}

// UpdateCustomTag overwrites name and value of an existing row.
func (s *Store) UpdateCustomTag(ctx context.Context, ct domain.CustomTag) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE site_custom_tags SET
			name = ?, value = ?, updated_at = ?
		WHERE wid = ? AND id = ?`,
		ct.Name, ct.Value, time.Now().Unix(), ct.WID, ct.ID)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrSlugInUse
		}
		return fmt.Errorf("repo: UpdateCustomTag: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteCustomTag removes a custom tag row. ErrNotFound when missing.
func (s *Store) DeleteCustomTag(ctx context.Context, wid, id int64) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM site_custom_tags WHERE wid = ? AND id = ?`, wid, id)
	if err != nil {
		return fmt.Errorf("repo: DeleteCustomTag: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// CountCustomTags returns how many custom tags the weblog has.
func (s *Store) CountCustomTags(ctx context.Context, wid int64) (int64, error) {
	var n int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM site_custom_tags WHERE wid = ?`, wid).Scan(&n); err != nil {
		return 0, fmt.Errorf("repo: CountCustomTags: %w", err)
	}
	return n, nil
}
