package analytics

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestEntryIDFromPath(t *testing.T) {
	cases := []struct {
		path string
		want int64
	}{
		{"/entry/42/", 42},
		{"/entry/42", 42},
		{"/entry/42/comment", 42},
		{"/entry/42/like", 42},
		{"/entry/abc/", 0},
		{"/", 0},
		{"/category/1/", 0},
		{"/archive/2026/04/", 0},
	}
	for _, c := range cases {
		if got := EntryIDFromPath(c.path); got != c.want {
			t.Errorf("EntryIDFromPath(%q) = %d, want %d", c.path, got, c.want)
		}
	}
}

func freshMainDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open main db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`
		CREATE TABLE entries (
			id INTEGER PRIMARY KEY,
			likes_count INTEGER NOT NULL DEFAULT 0,
			stamps_count INTEGER NOT NULL DEFAULT 0
		)
	`); err != nil {
		t.Fatalf("create entries table: %v", err)
	}
	return db
}

func freshStore(t *testing.T, retentionDays int) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "analytics.db")
	s, err := Open(path, retentionDays)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestRecordAndSummarise(t *testing.T) {
	s := freshStore(t, 0)
	ctx := context.Background()

	if err := s.Record(ctx, "v1", "/", 0); err != nil {
		t.Fatal(err)
	}
	if err := s.Record(ctx, "v1", "/entry/1/", 1); err != nil {
		t.Fatal(err)
	}
	if err := s.Record(ctx, "v2", "/entry/1/", 1); err != nil {
		t.Fatal(err)
	}
	if err := s.Record(ctx, "v3", "/entry/2/", 2); err != nil {
		t.Fatal(err)
	}

	sum, err := s.Summarise(ctx, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if sum.PageViews != 4 {
		t.Errorf("PageViews = %d, want 4", sum.PageViews)
	}
	if sum.UniqueVisitors != 3 {
		t.Errorf("UniqueVisitors = %d, want 3", sum.UniqueVisitors)
	}
}

func TestTopEntries(t *testing.T) {
	s := freshStore(t, 0)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		_ = s.Record(ctx, "v"+itoa(i), "/entry/1/", 1)
	}
	for i := 0; i < 2; i++ {
		_ = s.Record(ctx, "v"+itoa(i+10), "/entry/2/", 2)
	}
	_ = s.Record(ctx, "other", "/", 0) // home hits must not appear

	top, err := s.TopEntries(ctx, nil, time.Now().Add(-time.Hour), 10, SortByViews)
	if err != nil {
		t.Fatal(err)
	}
	if len(top) != 2 {
		t.Fatalf("expected 2 entries in top, got %d", len(top))
	}
	if top[0].EntryID != 1 || top[0].Views != 5 {
		t.Errorf("top[0] = %+v, want EntryID=1 Views=5", top[0])
	}
	if top[1].EntryID != 2 || top[1].Views != 2 {
		t.Errorf("top[1] = %+v, want EntryID=2 Views=2", top[1])
	}
}

func TestTopEntriesViewsSortLimit(t *testing.T) {
	s := freshStore(t, 0)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		_ = s.Record(ctx, "v"+itoa(i), "/entry/1/", 1)
	}
	for i := 0; i < 4; i++ {
		_ = s.Record(ctx, "v"+itoa(i+10), "/entry/2/", 2)
	}
	for i := 0; i < 3; i++ {
		_ = s.Record(ctx, "v"+itoa(i+20), "/entry/3/", 3)
	}

	top, err := s.TopEntries(ctx, nil, time.Now().Add(-time.Hour), 2, SortByViews)
	if err != nil {
		t.Fatal(err)
	}
	if len(top) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(top))
	}
	if top[0].EntryID != 1 || top[0].Views != 5 {
		t.Errorf("top[0] = %+v, want EntryID=1 Views=5", top[0])
	}
	if top[1].EntryID != 2 || top[1].Views != 4 {
		t.Errorf("top[1] = %+v, want EntryID=2 Views=4", top[1])
	}
}

func TestTopEntriesViewsSortTieBreak(t *testing.T) {
	s := freshStore(t, 0)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		_ = s.Record(ctx, "v"+itoa(i), "/entry/1/", 1)
	}
	for i := 0; i < 3; i++ {
		_ = s.Record(ctx, "v"+itoa(i+10), "/entry/2/", 2)
	}
	_ = s.Record(ctx, "other", "/entry/3/", 3)

	top, err := s.TopEntries(ctx, nil, time.Now().Add(-time.Hour), 2, SortByViews)
	if err != nil {
		t.Fatal(err)
	}
	if len(top) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(top))
	}
	// tie break: entry_id desc, so 2 > 1
	if top[0].EntryID != 2 {
		t.Errorf("top[0].EntryID = %d, want 2 (tie break id desc)", top[0].EntryID)
	}
	if top[1].EntryID != 1 {
		t.Errorf("top[1].EntryID = %d, want 1 (tie break id desc)", top[1].EntryID)
	}
}

func TestTopEntriesViewsSortWithEngagement(t *testing.T) {
	s := freshStore(t, 0)
	mainDB := freshMainDB(t)
	ctx := context.Background()

	if _, err := mainDB.Exec(`
		INSERT INTO entries (id, likes_count, stamps_count) VALUES (1, 5, 10), (2, 3, 7)
	`); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 5; i++ {
		_ = s.Record(ctx, "v"+itoa(i), "/entry/1/", 1)
	}
	for i := 0; i < 2; i++ {
		_ = s.Record(ctx, "v"+itoa(i+10), "/entry/2/", 2)
	}

	top, err := s.TopEntries(ctx, mainDB, time.Now().Add(-time.Hour), 10, SortByViews)
	if err != nil {
		t.Fatal(err)
	}
	if len(top) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(top))
	}
	if top[0].EntryID != 1 || top[0].Views != 5 || top[0].Likes != 5 || top[0].Stamps != 10 {
		t.Errorf("top[0] = %+v, want EntryID=1 Views=5 Likes=5 Stamps=10", top[0])
	}
	if top[1].EntryID != 2 || top[1].Views != 2 || top[1].Likes != 3 || top[1].Stamps != 7 {
		t.Errorf("top[1] = %+v, want EntryID=2 Views=2 Likes=3 Stamps=7", top[1])
	}
}

func TestReturningVisitorsDetected(t *testing.T) {
	s := freshStore(t, 0)
	ctx := context.Background()

	now := time.Now()
	// Pre-window view from v1
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO page_views (visitor_id, path, entry_id, created_at) VALUES (?, ?, 0, ?)`,
		"v1", "/", now.Add(-8*24*time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	// Window views
	_ = s.Record(ctx, "v1", "/", 0) // returning
	_ = s.Record(ctx, "v2", "/", 0) // new

	since := now.Add(-24 * time.Hour)
	sum, err := s.Summarise(ctx, since)
	if err != nil {
		t.Fatal(err)
	}
	if sum.ReturnVisitors != 1 {
		t.Errorf("ReturnVisitors = %d, want 1 (v1 seen before window)", sum.ReturnVisitors)
	}
}

func TestRetentionDropsOldRows(t *testing.T) {
	s := freshStore(t, 7)
	ctx := context.Background()

	// Insert rows older than retention by directly touching the DB so the
	// created_at lands where we want.
	old := time.Now().Add(-30 * 24 * time.Hour).Unix()
	for i := 0; i < 5; i++ {
		if _, err := s.db.ExecContext(ctx,
			`INSERT INTO page_views (visitor_id, path, entry_id, created_at) VALUES (?, ?, 0, ?)`,
			"old", "/", old); err != nil {
			t.Fatal(err)
		}
	}
	// Plus one fresh row so the table isn't empty after cleanup.
	_ = s.Record(ctx, "fresh", "/", 0)

	if err := s.CleanupNow(ctx); err != nil {
		t.Fatal(err)
	}

	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM page_views`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("post-cleanup row count = %d, want 1 (retention=7d, old rows should be gone)", n)
	}
}

func TestRetentionZeroMeansKeepEverything(t *testing.T) {
	s := freshStore(t, 0)
	ctx := context.Background()

	old := time.Now().Add(-365 * 24 * time.Hour).Unix()
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO page_views (visitor_id, path, entry_id, created_at) VALUES (?, ?, 0, ?)`,
		"ancient", "/", old); err != nil {
		t.Fatal(err)
	}
	if err := s.CleanupNow(ctx); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM page_views`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("retention=0 should not delete anything; count = %d", n)
	}
}

func TestEntryIDForRequestWithResolver(t *testing.T) {
	s := freshStore(t, 0)
	resolverCalled := false
	s.WithEntryResolver(func(_ context.Context, path string) int64 {
		resolverCalled = true
		if path == "/entry/my-slug/" {
			return 42
		}
		return 0
	})

	ctx := context.Background()
	if got := s.entryIDForRequest(ctx, "/entry/my-slug/"); got != 42 {
		t.Errorf("entryIDForRequest(/entry/my-slug/) = %d, want 42", got)
	}
	if !resolverCalled {
		t.Error("expected resolver to be called")
	}
}

func TestEntryIDForRequestResolverZeroFallsBack(t *testing.T) {
	s := freshStore(t, 0)
	s.WithEntryResolver(func(_ context.Context, _ string) int64 { return 0 })

	ctx := context.Background()
	if got := s.entryIDForRequest(ctx, "/entry/7/"); got != 7 {
		t.Errorf("entryIDForRequest(/entry/7/) = %d, want 7 (fallback)", got)
	}
}

func TestEntryIDForRequestNoResolver(t *testing.T) {
	s := freshStore(t, 0)
	ctx := context.Background()
	if got := s.entryIDForRequest(ctx, "/entry/3/"); got != 3 {
		t.Errorf("entryIDForRequest(/entry/3/) = %d, want 3", got)
	}
	if got := s.entryIDForRequest(ctx, "/entry/abc/"); got != 0 {
		t.Errorf("entryIDForRequest(/entry/abc/) = %d, want 0", got)
	}
}

// itoa is intentionally re-implemented here so the test file is independent
// of strconv, matching the style used in the app-level tests.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
