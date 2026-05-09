package repo

import (
	"context"
	"testing"
	"time"

	"github.com/serendipitynz/serenebach/internal/domain"
)

func seedEntry(t *testing.T, s *Store, title string, pinned bool) int64 {
	t.Helper()
	id, err := s.CreateEntry(context.Background(), domain.Entry{
		WID:      1,
		AuthorID: 1,
		Title:    title,
		Body:     "body",
		Format:   "html",
		Status:   domain.EntryPublished,
		PostedAt: time.Now(),
		Pinned:   pinned,
	})
	if err != nil {
		t.Fatalf("CreateEntry(%q): %v", title, err)
	}
	return id
}

func TestSetEntryPinnedRoundtrip(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	id := seedEntry(t, s, "Hello", false)

	// Verify initial state.
	e, err := s.EntryByID(ctx, 1, id)
	if err != nil {
		t.Fatalf("EntryByID: %v", err)
	}
	if e.Pinned {
		t.Error("newly created entry should not be pinned")
	}

	// Pin it.
	if err := s.SetEntryPinned(ctx, 1, id, true); err != nil {
		t.Fatalf("SetEntryPinned(true): %v", err)
	}
	e, err = s.EntryByID(ctx, 1, id)
	if err != nil {
		t.Fatalf("EntryByID after pin: %v", err)
	}
	if !e.Pinned {
		t.Error("entry should be pinned after SetEntryPinned(true)")
	}

	// Unpin it.
	if err := s.SetEntryPinned(ctx, 1, id, false); err != nil {
		t.Fatalf("SetEntryPinned(false): %v", err)
	}
	e, err = s.EntryByID(ctx, 1, id)
	if err != nil {
		t.Fatalf("EntryByID after unpin: %v", err)
	}
	if e.Pinned {
		t.Error("entry should not be pinned after SetEntryPinned(false)")
	}
}

func TestSetEntryPinnedNotFound(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	err := s.SetEntryPinned(ctx, 1, 9999, true)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound for missing entry, got %v", err)
	}
}

func TestUpdateEntryPreservesPinned(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	id := seedEntry(t, s, "First", true)
	e, _ := s.EntryByID(ctx, 1, id)

	// UpdateEntry must persist the Pinned flag.
	e.Title = "Updated"
	if err := s.UpdateEntry(ctx, *e); err != nil {
		t.Fatalf("UpdateEntry: %v", err)
	}
	got, _ := s.EntryByID(ctx, 1, id)
	if !got.Pinned {
		t.Error("Pinned flag was lost after UpdateEntry")
	}
	if got.Title != "Updated" {
		t.Errorf("title = %q, want Updated", got.Title)
	}
}

func TestRecentPublishedEntriesPagePinnedFirst(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// Insert entries with deliberate gaps in PostedAt so sorting is
	// deterministic without relying on wall-clock ordering.
	now := time.Now()
	seedEntryAt := func(title string, pinned bool, offset time.Duration) {
		t.Helper()
		_, err := s.CreateEntry(ctx, domain.Entry{
			WID:      1,
			AuthorID: 1,
			Title:    title,
			Body:     "b",
			Format:   "html",
			Status:   domain.EntryPublished,
			PostedAt: now.Add(offset),
			Pinned:   pinned,
		})
		if err != nil {
			t.Fatalf("CreateEntry(%q): %v", title, err)
		}
	}

	// Older pinned entry and newer regular entries so that without the
	// pinned sort the pinned one would NOT appear first.
	seedEntryAt("regular-newest", false, 3*time.Hour)
	seedEntryAt("regular-mid", false, 2*time.Hour)
	seedEntryAt("pinned-old", true, 1*time.Hour) // oldest, but pinned

	// Page 1 (offset=0): pinned entry must come first.
	page1, err := s.RecentPublishedEntriesPage(ctx, 1, 10, 0)
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if len(page1) != 3 {
		t.Fatalf("page 1 len = %d, want 3", len(page1))
	}
	if page1[0].Title != "pinned-old" {
		t.Errorf("page 1[0] = %q, want pinned-old (pinned entries must float to top)", page1[0].Title)
	}

	// Page 2 (offset=10, larger than total): plain date order, no
	// special pinned treatment. Use a fresh store with just two entries
	// so the offset-based switch is exercised with a real offset > 0.
	s2 := newTestStore(t)
	seedEntryAt2 := func(title string, pinned bool, offset time.Duration) {
		t.Helper()
		_, err := s2.CreateEntry(ctx, domain.Entry{
			WID:      1,
			AuthorID: 1,
			Title:    title,
			Body:     "b",
			Format:   "html",
			Status:   domain.EntryPublished,
			PostedAt: now.Add(offset),
			Pinned:   pinned,
		})
		if err != nil {
			t.Fatalf("CreateEntry(%q): %v", title, err)
		}
	}
	seedEntryAt2("r-new", false, 2*time.Hour)
	seedEntryAt2("p-old", true, time.Hour)

	// With offset=1 (page 2 of a page-size=1 view), the ordering must
	// be plain posted_at DESC — pinned sorting no longer applies.
	page2, err := s2.RecentPublishedEntriesPage(ctx, 1, 10, 1)
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	// Only one entry after offset=1; it should be p-old regardless of pin.
	if len(page2) != 1 {
		t.Fatalf("page 2 len = %d, want 1", len(page2))
	}
	if page2[0].Title != "p-old" {
		t.Errorf("page 2[0] = %q, want p-old", page2[0].Title)
	}
}

func TestPublishedEntriesByCategoryPagePinnedFirst(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	now := time.Now()
	catID := int64(10)
	seedCat := func(title string, pinned bool, offset time.Duration) {
		t.Helper()
		_, err := s.CreateEntry(ctx, domain.Entry{
			WID:        1,
			AuthorID:   1,
			CategoryID: catID,
			Title:      title,
			Body:       "b",
			Format:     "html",
			Status:     domain.EntryPublished,
			PostedAt:   now.Add(offset),
			Pinned:     pinned,
		})
		if err != nil {
			t.Fatalf("CreateEntry(%q): %v", title, err)
		}
	}

	seedCat("cat-regular", false, 2*time.Hour)
	seedCat("cat-pinned", true, time.Hour) // older but pinned

	page1, err := s.PublishedEntriesByCategoryPage(ctx, 1, catID, 10, 0)
	if err != nil {
		t.Fatalf("category page 1: %v", err)
	}
	if len(page1) != 2 {
		t.Fatalf("len = %d, want 2", len(page1))
	}
	if page1[0].Title != "cat-pinned" {
		t.Errorf("page1[0] = %q, want cat-pinned", page1[0].Title)
	}
}
