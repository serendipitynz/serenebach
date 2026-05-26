package repo

import (
	"context"
	"fmt"
	"time"
)

// SitemapCategoryLastMods returns the latest updated_at for each
// category that has at least one published entry. Hidden categories
// are not excluded here — the caller filters them.
func (s *Store) SitemapCategoryLastMods(ctx context.Context, wid int64) (map[int64]time.Time, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.category_id, MAX(e.updated_at) AS lastmod
		FROM entries e
		WHERE e.wid = ? AND e.status = ?
		GROUP BY e.category_id`, wid, 1)
	if err != nil {
		return nil, fmt.Errorf("repo: SitemapCategoryLastMods: %w", err)
	}
	defer rows.Close()
	out := make(map[int64]time.Time)
	for rows.Next() {
		var catID int64
		var ts int64
		if err := rows.Scan(&catID, &ts); err != nil {
			return nil, fmt.Errorf("repo: SitemapCategoryLastMods scan: %w", err)
		}
		out[catID] = time.Unix(ts, 0)
	}
	return out, rows.Err()
}

// SitemapTagLastMods returns the latest updated_at for each tag that
// has at least one published entry, keyed by tag_id.
func (s *Store) SitemapTagLastMods(ctx context.Context, wid int64) (map[int64]time.Time, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT et.tag_id, MAX(e.updated_at) AS lastmod
		FROM entry_tags et
		JOIN entries e ON e.id = et.entry_id
		WHERE e.wid = ? AND e.status = ?
		GROUP BY et.tag_id`, wid, 1)
	if err != nil {
		return nil, fmt.Errorf("repo: SitemapTagLastMods: %w", err)
	}
	defer rows.Close()
	out := make(map[int64]time.Time)
	for rows.Next() {
		var tagID int64
		var ts int64
		if err := rows.Scan(&tagID, &ts); err != nil {
			return nil, fmt.Errorf("repo: SitemapTagLastMods scan: %w", err)
		}
		out[tagID] = time.Unix(ts, 0)
	}
	return out, rows.Err()
}
