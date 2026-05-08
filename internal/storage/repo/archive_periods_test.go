package repo

import (
	"context"
	"testing"
	"time"

	"github.com/serendipitynz/serenebach/internal/domain"
)

// TestArchivePeriodsTZBucket verifies that an entry posted within a
// few hours of UTC midnight is bucketed into the same (year, month)
// pair as the archive range query for the same loc. Without TZ-aware
// bucketing the sidebar links to a month whose archive page is empty
// (or that the static rebuild never emits at all).
func TestArchivePeriodsTZBucket(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}

	// 2026-01-01 00:30 JST → 2025-12-31 15:30 UTC. UTC bucketing
	// puts this in 2025/12; JST bucketing puts it in 2026/01.
	postedAt := time.Date(2026, time.January, 1, 0, 30, 0, 0, tokyo)
	if _, err := s.CreateEntry(ctx, domain.Entry{
		WID: 1, AuthorID: 1, CategoryID: domain.Uncategorized,
		Title: "January", Status: domain.EntryPublished, PostedAt: postedAt,
	}); err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}

	t.Run("ArchivePeriods", func(t *testing.T) {
		got, err := s.ArchivePeriods(ctx, 1, tokyo)
		if err != nil {
			t.Fatalf("ArchivePeriods: %v", err)
		}
		if len(got) != 1 || got[0].Year != 2026 || got[0].Month != 1 {
			t.Fatalf("ArchivePeriods = %+v, want one bucket for 2026/01", got)
		}
	})

	t.Run("ArchivePeriodsWithCounts", func(t *testing.T) {
		got, err := s.ArchivePeriodsWithCounts(ctx, 1, tokyo)
		if err != nil {
			t.Fatalf("ArchivePeriodsWithCounts: %v", err)
		}
		if len(got) != 1 || got[0].Year != 2026 || got[0].Month != 1 || got[0].Count != 1 {
			t.Fatalf("ArchivePeriodsWithCounts = %+v, want one bucket 2026/01 count=1", got)
		}
	})

	t.Run("UTCWouldDisagree", func(t *testing.T) {
		// Sanity: the same data bucketed in UTC ends up in 2025/12,
		// confirming the fixture sits across the boundary the sidebar
		// vs range-query divergence used to live on.
		got, err := s.ArchivePeriods(ctx, 1, time.UTC)
		if err != nil {
			t.Fatalf("ArchivePeriods UTC: %v", err)
		}
		if len(got) != 1 || got[0].Year != 2025 || got[0].Month != 12 {
			t.Fatalf("UTC bucket = %+v, want 2025/12", got)
		}
	})
}

// TestArchivePeriodsOrdering covers the newest-first contract that the
// sidebar relies on so the topmost link is the current month.
func TestArchivePeriodsOrdering(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	for _, ts := range []time.Time{
		time.Date(2025, time.January, 15, 12, 0, 0, 0, time.UTC),
		time.Date(2026, time.March, 20, 12, 0, 0, 0, time.UTC),
		time.Date(2025, time.December, 5, 12, 0, 0, 0, time.UTC),
	} {
		if _, err := s.CreateEntry(ctx, domain.Entry{
			WID: 1, AuthorID: 1, CategoryID: domain.Uncategorized,
			Title: ts.Format("2006-01"), Status: domain.EntryPublished, PostedAt: ts,
		}); err != nil {
			t.Fatalf("CreateEntry: %v", err)
		}
	}

	got, err := s.ArchivePeriods(ctx, 1, time.UTC)
	if err != nil {
		t.Fatalf("ArchivePeriods: %v", err)
	}
	want := []ArchivePeriod{
		{Year: 2026, Month: 3},
		{Year: 2025, Month: 12},
		{Year: 2025, Month: 1},
	}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d (%+v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i].Year != want[i].Year || got[i].Month != want[i].Month {
			t.Errorf("[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}
