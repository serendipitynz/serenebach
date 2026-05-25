package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/serendipitynz/serenebach/internal/domain"
)

// imageColumns is the canonical column list for the images table.
// Order must match scanImages and the inline Scan in ImageByID.
const imageColumns = `id, wid, uploaded_by, kind, filename, stored_path, thumb_path, mime_type, size_bytes, width, height, alt_text, created_at, updated_at`

// CreateImage inserts a new image row and returns its id. Timestamps default
// to now. Callers write the file + thumbnail to disk before calling this so
// the DB row is a pointer to bytes that already exist.
func (s *Store) CreateImage(ctx context.Context, img domain.Image) (int64, error) {
	now := time.Now().Unix()
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO images (wid, uploaded_by, kind, filename, stored_path, thumb_path, mime_type, size_bytes, width, height, alt_text, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		img.WID, img.UploadedBy, img.Kind, img.Filename, img.StoredPath, img.ThumbPath,
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

// ListImagesForAdmin returns the weblog's uploads newest first, with basic
// pagination. limit<=0 defaults to 60. kind=="" returns all kinds.
func (s *Store) ListImagesForAdmin(ctx context.Context, wid int64, kind string, limit, offset int) ([]domain.Image, error) {
	if limit <= 0 {
		limit = 60
	}
	if offset < 0 {
		offset = 0
	}
	var rows *sql.Rows
	var err error
	if kind != "" {
		rows, err = s.db.QueryContext(ctx, `
			SELECT `+imageColumns+`
			FROM images
			WHERE wid = ? AND kind = ?
			ORDER BY created_at DESC, id DESC
			LIMIT ? OFFSET ?`, wid, kind, limit, offset)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT `+imageColumns+`
			FROM images
			WHERE wid = ?
			ORDER BY created_at DESC, id DESC
			LIMIT ? OFFSET ?`, wid, limit, offset)
	}
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
		SELECT `+imageColumns+`
		FROM images WHERE wid = ? AND id = ?`, wid, id)
	var img domain.Image
	var createdAt, updatedAt int64
	if err := row.Scan(&img.ID, &img.WID, &img.UploadedBy, &img.Kind, &img.Filename, &img.StoredPath,
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

// UpdateImageFilename overwrites the display filename for one upload.
func (s *Store) UpdateImageFilename(ctx context.Context, wid, id int64, filename string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE images SET filename = ?, updated_at = ? WHERE wid = ? AND id = ?`,
		filename, time.Now().Unix(), wid, id)
	if err != nil {
		return fmt.Errorf("repo: UpdateImageFilename: %w", err)
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
		if err := rows.Scan(&img.ID, &img.WID, &img.UploadedBy, &img.Kind, &img.Filename, &img.StoredPath,
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
