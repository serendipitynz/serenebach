// Package analytics records pageviews for the public side of a weblog and
// exposes the queries the admin dashboard needs on top of them. Everything
// is kept first-party: no third-party beacons, no IP / User-Agent logging,
// just a per-visitor random cookie and the path / entry id being viewed.
//
// Storage is either the main application SQLite database (the default — a
// single file is still the simplest thing to back up) or a user-configured
// separate analytics file. The Store exposed by this package hides which
// one the handler is talking to.
package analytics

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Store wraps the *sql.DB analytics rows live in. Construct one with Open
// (separate file) or WrapMain (main-DB mode).
type Store struct {
	db             *sql.DB
	retentionDays  int
	cleanupEvery   int // attempt cleanup roughly once every N writes
	writeCount     int64
	ownsConnection bool
}

// WrapMain returns a Store backed by the main application database. This is
// the default analytics storage and assumes the page_views table already
// exists via migrations.
func WrapMain(db *sql.DB, retentionDays int) *Store {
	return &Store{
		db:            db,
		retentionDays: retentionDays,
		cleanupEvery:  100,
	}
}

// Open opens a dedicated analytics SQLite file, creating the schema if
// needed. Use this when the operator set SB_ANALYTICS_DB so the page_views
// table doesn't mix with the weblog content DB.
func Open(path string, retentionDays int) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("analytics.Open: empty path")
	}
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)", filepath.ToSlash(path))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("analytics: open %q: %w", path, err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("analytics: ping %q: %w", path, err)
	}
	s := &Store{
		db:             db,
		retentionDays:  retentionDays,
		cleanupEvery:   100,
		ownsConnection: true,
	}
	if err := s.ensureSchema(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the underlying *sql.DB only when the Store owns it (i.e.
// when created via Open). Main-DB mode never closes here — that belongs to
// the app.
func (s *Store) Close() error {
	if s.ownsConnection && s.db != nil {
		return s.db.Close()
	}
	return nil
}

// DB exposes the underlying connection so tests can poke at the rows.
func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) ensureSchema(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS page_views (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			visitor_id TEXT    NOT NULL,
			path       TEXT    NOT NULL,
			entry_id   INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_page_views_created ON page_views(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_page_views_entry   ON page_views(entry_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_page_views_visitor ON page_views(visitor_id, created_at)`,
	}
	for _, q := range stmts {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("analytics: ensure schema: %w", err)
		}
	}
	return nil
}

// Record inserts one pageview. Also triggers a probabilistic cleanup so
// retention is enforced without a separate cron job. Errors here are never
// user-visible — callers log them but return 200 to the browser anyway.
func (s *Store) Record(ctx context.Context, visitorID, path string, entryID int64) error {
	if s == nil || s.db == nil {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO page_views (visitor_id, path, entry_id, created_at)
		VALUES (?, ?, ?, ?)`, visitorID, path, entryID, time.Now().Unix()); err != nil {
		return fmt.Errorf("analytics: record: %w", err)
	}
	s.writeCount++
	if s.shouldCleanup() {
		// Best-effort — never block a request on retention maintenance.
		_ = s.cleanupOld(ctx)
	}
	return nil
}

func (s *Store) shouldCleanup() bool {
	if s.retentionDays <= 0 || s.cleanupEvery <= 0 {
		return false
	}
	// Deterministic modulo is fine here: we want the cleanup to run on
	// roughly 1 in N requests, and spreading it by writeCount keeps a slow
	// blog from ever running cleanup while a busy one runs it often.
	return s.writeCount%int64(s.cleanupEvery) == 0
}

func (s *Store) cleanupOld(ctx context.Context) error {
	if s.retentionDays <= 0 {
		return nil
	}
	cutoff := time.Now().Add(-time.Duration(s.retentionDays) * 24 * time.Hour).Unix()
	if _, err := s.db.ExecContext(ctx, `DELETE FROM page_views WHERE created_at < ?`, cutoff); err != nil {
		return fmt.Errorf("analytics: cleanup: %w", err)
	}
	return nil
}

// CleanupNow forces a retention sweep — useful in tests and for an admin
// "tidy up" button we haven't built yet.
func (s *Store) CleanupNow(ctx context.Context) error {
	return s.cleanupOld(ctx)
}

// ---- aggregates --------------------------------------------------------

// Summary captures the top-line numbers shown on the admin dashboard.
type Summary struct {
	Since          time.Time
	PageViews      int64
	UniqueVisitors int64
	ReturnVisitors int64 // visitor_ids seen before Since
}

// Summarise returns a Summary for the window [since, now].
func (s *Store) Summarise(ctx context.Context, since time.Time) (*Summary, error) {
	if s == nil || s.db == nil {
		return &Summary{Since: since}, nil
	}
	sum := &Summary{Since: since}

	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM page_views WHERE created_at >= ?`,
		since.Unix()).Scan(&sum.PageViews); err != nil {
		return nil, fmt.Errorf("analytics: total: %w", err)
	}

	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(DISTINCT visitor_id) FROM page_views WHERE created_at >= ?`,
		since.Unix()).Scan(&sum.UniqueVisitors); err != nil {
		return nil, fmt.Errorf("analytics: uniques: %w", err)
	}

	// Return visitors: distinct visitor_ids seen in the window whose
	// first-ever visit predates the window. The subquery captures every
	// visitor with at least one view before `since`, and the outer query
	// restricts to those that also appear in the window.
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT visitor_id) FROM page_views
		WHERE created_at >= ?
		  AND visitor_id IN (
		      SELECT visitor_id FROM page_views WHERE created_at < ?
		  )`, since.Unix(), since.Unix()).Scan(&sum.ReturnVisitors); err != nil {
		return nil, fmt.Errorf("analytics: returning: %w", err)
	}

	return sum, nil
}

// EntryHit is one row of the "top entries" listing on the dashboard.
// Carries every signal the admin wants to rank by — PV, likes, and
// stamps — so the template can render all three columns side-by-side.
type EntryHit struct {
	EntryID int64
	Views   int64
	Likes   int64
	Stamps  int64
}

// TopEntrySort selects which column the listing orders by.
type TopEntrySort string

const (
	SortByViews  TopEntrySort = "views"
	SortByLikes  TopEntrySort = "likes"
	SortByStamps TopEntrySort = "stamps"
)

// TopEntries returns the N most-<sort> entries in the window. When
// sort is empty or unrecognised it falls back to SortByViews so the
// existing dashboard call keeps working without a change. Analytics
// lives in its own (optionally external) DB, so likes/stamps counters
// are sourced through the main repo via the Store's contentDB shim.
func (s *Store) TopEntries(ctx context.Context, mainDB *sql.DB, since time.Time, limit int, sort TopEntrySort) ([]EntryHit, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	// Step 1: views per entry in the window. Always computed — likes /
	// stamps sort modes also display the current view count.
	rows, err := s.db.QueryContext(ctx, `
		SELECT entry_id, COUNT(*) AS c
		FROM page_views
		WHERE entry_id > 0 AND created_at >= ?
		GROUP BY entry_id`, since.Unix())
	if err != nil {
		return nil, fmt.Errorf("analytics: top entries: %w", err)
	}
	defer rows.Close()
	viewsByID := map[int64]int64{}
	for rows.Next() {
		var id, c int64
		if err := rows.Scan(&id, &c); err != nil {
			return nil, fmt.Errorf("analytics: scan top: %w", err)
		}
		viewsByID[id] = c
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Step 2: build the ordered list. For views-sort we just read from
	// the map; for likes/stamps we need the full set of entries so an
	// entry with zero views but many reactions still appears.
	if sort != SortByLikes && sort != SortByStamps {
		sort = SortByViews
	}

	var orderClause string
	switch sort {
	case SortByLikes:
		orderClause = "likes_count DESC, id DESC"
	case SortByStamps:
		orderClause = "stamps_count DESC, id DESC"
	default:
		orderClause = "id DESC" // views sort is applied client-side below
	}

	// For likes/stamps sort, pull the candidate pool via the main DB.
	// For views sort we only need entries present in viewsByID.
	var ids []int64
	likesByID := map[int64]int64{}
	stampsByID := map[int64]int64{}

	if sort == SortByLikes || sort == SortByStamps {
		if mainDB == nil {
			// Engagement sorts need the main DB to read likes_count /
			// stamps_count. Without it, fall back to a views-only listing
			// so the caller still sees something useful.
			sort = SortByViews
		}
	}

	if sort == SortByLikes || sort == SortByStamps {
		if err := loadEngagementByOrder(ctx, mainDB, orderClause, limit, &ids, likesByID, stampsByID); err != nil {
			return nil, err
		}
	} else {
		var err error
		ids, err = collectViewsSortedIDs(ctx, mainDB, viewsByID, limit, likesByID, stampsByID)
		if err != nil {
			return nil, err
		}
		if len(ids) == 0 {
			return nil, nil
		}
	}

	out := make([]EntryHit, 0, len(ids))
	for _, id := range ids {
		out = append(out, EntryHit{
			EntryID: id,
			Views:   viewsByID[id],
			Likes:   likesByID[id],
			Stamps:  stampsByID[id],
		})
	}
	return out, nil
}

// sortByValueDesc sorts ids in place by descending values[id]; ties
// break on id descending so the ordering is deterministic.
func sortByValueDesc(ids []int64, values map[int64]int64) {
	// Simple insertion sort — top-entries limit is small (default ~10).
	for i := 1; i < len(ids); i++ {
		for j := i; j > 0; j-- {
			a, b := ids[j-1], ids[j]
			if values[b] > values[a] || (values[b] == values[a] && b > a) {
				ids[j-1], ids[j] = b, a
			} else {
				break
			}
		}
	}
}

// DayPoint is one bucket for a daily PV chart (date formatted YYYY-MM-DD).
type DayPoint struct {
	Day   string
	Views int64
}

// DailyViews returns PV counts bucketed by day, oldest first, for the given
// window.
func (s *Store) DailyViews(ctx context.Context, since time.Time) ([]DayPoint, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT date(created_at, 'unixepoch', 'localtime') AS day, COUNT(*) AS c
		FROM page_views
		WHERE created_at >= ?
		GROUP BY day
		ORDER BY day ASC`, since.Unix())
	if err != nil {
		return nil, fmt.Errorf("analytics: daily: %w", err)
	}
	defer rows.Close()
	var out []DayPoint
	for rows.Next() {
		var p DayPoint
		if err := rows.Scan(&p.Day, &p.Views); err != nil {
			return nil, fmt.Errorf("analytics: scan daily: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ---- visitor id + path helpers ----------------------------------------

// NewVisitorID returns a fresh random identifier suitable for the
// sb_visitor_id cookie. 16 bytes base64url keeps the cookie short.
func NewVisitorID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// collectViewsSortedIDs returns the entry ids visible in viewsByID, sorted
// by view count descending and capped at `limit`. Views-sort still wants
// likes_count / stamps_count populated so the table can show those
// columns; when mainDB is nil (tests / analytics-only callers) the
// engagement columns stay at zero and the views ranking is unaffected.
func collectViewsSortedIDs(ctx context.Context, mainDB *sql.DB, viewsByID map[int64]int64, limit int, likesByID, stampsByID map[int64]int64) ([]int64, error) {
	ids := make([]int64, 0, len(viewsByID))
	for id := range viewsByID {
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	if mainDB != nil {
		if err := loadEngagementByIDs(ctx, mainDB, ids, likesByID, stampsByID); err != nil {
			return nil, err
		}
	}
	sortByValueDesc(ids, viewsByID)
	if len(ids) > limit {
		ids = ids[:limit]
	}
	return ids, nil
}

// loadEngagementByOrder reads ids + likes_count + stamps_count from the
// entries table ordered by `orderClause` (caller-provided; trusted), used
// by the engagement-sorted TopEntries paths. The ids slice is grown via
// the pointer so the caller's final ordering reflects the SQL order.
func loadEngagementByOrder(ctx context.Context, mainDB *sql.DB, orderClause string, limit int, ids *[]int64, likesByID, stampsByID map[int64]int64) error {
	q := `SELECT id, likes_count, stamps_count FROM entries ORDER BY ` + orderClause + ` LIMIT ?`
	rows, err := mainDB.QueryContext(ctx, q, limit)
	if err != nil {
		return fmt.Errorf("analytics: top entries engagement: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, l, st int64
		if err := rows.Scan(&id, &l, &st); err != nil {
			return err
		}
		*ids = append(*ids, id)
		likesByID[id] = l
		stampsByID[id] = st
	}
	return rows.Err()
}

// loadEngagementByIDs fills likesByID / stampsByID for the specific id set,
// used by the views-sorted TopEntries path so the table can show the
// engagement columns even when the sort is not by them.
func loadEngagementByIDs(ctx context.Context, mainDB *sql.DB, ids []int64, likesByID, stampsByID map[int64]int64) error {
	placeholders := make([]byte, 0, 2*len(ids))
	args := make([]any, 0, len(ids))
	for i, id := range ids {
		if i > 0 {
			placeholders = append(placeholders, ',')
		}
		placeholders = append(placeholders, '?')
		args = append(args, id)
	}
	q := `SELECT id, likes_count, stamps_count FROM entries WHERE id IN (` + string(placeholders) + `)`
	rows, err := mainDB.QueryContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("analytics: top entries engagement: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, l, st int64
		if err := rows.Scan(&id, &l, &st); err != nil {
			return err
		}
		likesByID[id] = l
		stampsByID[id] = st
	}
	return rows.Err()
}

// EntryIDFromPath extracts the numeric entry id from /entry/<id>/ paths so
// the middleware can attribute pageviews to specific entries. Returns 0
// when the path isn't a single-entry page (home, archive, category, etc.).
func EntryIDFromPath(path string) int64 {
	const prefix = "/entry/"
	if !strings.HasPrefix(path, prefix) {
		return 0
	}
	rest := strings.TrimPrefix(path, prefix)
	// rest might be "123/" or "123" or "123/comment" or "123/like"
	idStr := rest
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		idStr = rest[:i]
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		return 0
	}
	return id
}
