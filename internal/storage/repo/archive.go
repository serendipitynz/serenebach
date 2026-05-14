package repo

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/serendipitynz/serenebach/internal/domain"
)

// ArchivePeriod is one (year, month) bucket for which entries exist.
type ArchivePeriod struct {
	Year  int
	Month int
	// Count is populated by ArchivePeriodsWithCounts; zero from the
	// older ArchivePeriods shape (it didn't need the tally).
	Count int64
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

// ArchivePeriodsWithCounts is ArchivePeriods + an entry-count per
// bucket, for the sidebar `{archives_list}` fragment. The (year,
// month) bucket is computed against loc so the sidebar agrees with
// the archive range queries that also key off the configured
// timezone — without this they diverge for entries posted within a
// few hours of midnight UTC. Pass nil for time.Local.
func (s *Store) ArchivePeriodsWithCounts(ctx context.Context, wid int64, loc *time.Location) ([]ArchivePeriod, error) {
	if loc == nil {
		loc = time.Local
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT posted_at
		FROM entries
		WHERE wid = ? AND status = ?`+
		excludeHiddenCategoryClause, wid, domain.EntryPublished)
	if err != nil {
		return nil, fmt.Errorf("repo: ArchivePeriodsWithCounts: %w", err)
	}
	defer rows.Close()
	type key struct{ y, m int }
	counts := map[key]int64{}
	for rows.Next() {
		var ts int64
		if err := rows.Scan(&ts); err != nil {
			return nil, fmt.Errorf("repo: scan period: %w", err)
		}
		t := time.Unix(ts, 0).In(loc)
		counts[key{t.Year(), int(t.Month())}]++
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]ArchivePeriod, 0, len(counts))
	for k, c := range counts {
		out = append(out, ArchivePeriod{Year: k.y, Month: k.m, Count: c})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Year != out[j].Year {
			return out[i].Year > out[j].Year
		}
		return out[i].Month > out[j].Month
	})
	return out, nil
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
// weblog has at least one published entry, newest first. The bucket is
// computed in loc — passing nil falls back to time.Local — so the
// rebuild emits the same per-month archive files the
// ArchivePeriodsWithCounts sidebar links to. Without a shared zone the
// two would disagree for posts within a few hours of UTC midnight.
func (s *Store) ArchivePeriods(ctx context.Context, wid int64, loc *time.Location) ([]ArchivePeriod, error) {
	if loc == nil {
		loc = time.Local
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT posted_at
		FROM entries
		WHERE wid = ? AND status = ?`+
		excludeHiddenCategoryClause, wid, domain.EntryPublished)
	if err != nil {
		return nil, fmt.Errorf("repo: ArchivePeriods: %w", err)
	}
	defer rows.Close()
	type key struct{ y, m int }
	seen := map[key]struct{}{}
	for rows.Next() {
		var ts int64
		if err := rows.Scan(&ts); err != nil {
			return nil, fmt.Errorf("repo: scan period: %w", err)
		}
		t := time.Unix(ts, 0).In(loc)
		seen[key{t.Year(), int(t.Month())}] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]ArchivePeriod, 0, len(seen))
	for k := range seen {
		out = append(out, ArchivePeriod{Year: k.y, Month: k.m})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Year != out[j].Year {
			return out[i].Year > out[j].Year
		}
		return out[i].Month > out[j].Month
	})
	return out, nil
}
