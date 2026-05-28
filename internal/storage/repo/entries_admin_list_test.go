package repo

import (
	"context"
	"testing"
	"time"

	"github.com/serendipitynz/serenebach/internal/domain"
)

// seedAdminListEntry creates a minimal published entry with the given
// fields, returning its id. Posted_at is offset by `ageMinutes` so
// later seeds sort newer-first naturally.
func seedAdminListEntry(t *testing.T, s *Store, e domain.Entry, ageMinutes int) int64 {
	t.Helper()
	e.WID = 1
	if e.AuthorID == 0 {
		e.AuthorID = 1
	}
	if e.Format == "" {
		e.Format = "html"
	}
	e.PostedAt = time.Now().Add(-time.Duration(ageMinutes) * time.Minute)
	id, err := s.CreateEntry(context.Background(), e)
	if err != nil {
		t.Fatalf("CreateEntry(%q): %v", e.Title, err)
	}
	return id
}

func TestListEntriesForAdmin_DefaultsToPostedDesc(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	a := seedAdminListEntry(t, s, domain.Entry{Title: "oldest"}, 30)
	b := seedAdminListEntry(t, s, domain.Entry{Title: "middle"}, 20)
	c := seedAdminListEntry(t, s, domain.Entry{Title: "newest"}, 10)

	got, err := s.ListEntriesForAdmin(ctx, 1, ListEntriesQuery{})
	if err != nil {
		t.Fatalf("ListEntriesForAdmin: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len: got %d, want 3", len(got))
	}
	if got[0].ID != c || got[1].ID != b || got[2].ID != a {
		t.Errorf("default order should be posted DESC; got ids %d,%d,%d", got[0].ID, got[1].ID, got[2].ID)
	}
}

func TestListEntriesForAdmin_OwnerFilter(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	a := seedAdminListEntry(t, s, domain.Entry{Title: "by-1", AuthorID: 1}, 20)
	_ = seedAdminListEntry(t, s, domain.Entry{Title: "by-2", AuthorID: 2}, 10)

	owner := int64(1)
	got, err := s.ListEntriesForAdmin(ctx, 1, ListEntriesQuery{OwnerID: &owner})
	if err != nil {
		t.Fatalf("ListEntriesForAdmin: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len: got %d, want 1", len(got))
	}
	if got[0].ID != a {
		t.Errorf("expected entry id %d, got %d", a, got[0].ID)
	}
}

func TestListEntriesForAdmin_Search(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedAdminListEntry(t, s, domain.Entry{Title: "hello world", Body: "alpha"}, 50)
	bID := seedAdminListEntry(t, s, domain.Entry{Title: "goodbye", Body: "needle in body", Slug: "bye"}, 40)
	cID := seedAdminListEntry(t, s, domain.Entry{Title: "needle in title", Body: "nothing"}, 30)
	// Slug-only matches are no longer searchable after the move from
	// the SB-era LIKE-on-five-columns query to the trigram FTS path
	// (entries_fts indexes title/body/more/keywords, not slug). Keep
	// the row in fixtures so the absence is documented.
	_ = seedAdminListEntry(t, s, domain.Entry{Title: "slug match", Slug: "my-needle"}, 20)
	_ = seedAdminListEntry(t, s, domain.Entry{Title: "miss"}, 10)

	got, err := s.ListEntriesForAdmin(ctx, 1, ListEntriesQuery{Search: "needle"})
	if err != nil {
		t.Fatalf("ListEntriesForAdmin: %v", err)
	}
	wantIDs := []int64{cID, bID}
	if len(got) != len(wantIDs) {
		t.Fatalf("len: got %d, want %d", len(got), len(wantIDs))
	}
	for i, w := range wantIDs {
		if got[i].ID != w {
			t.Errorf("row %d: got id %d, want %d", i, got[i].ID, w)
		}
	}
}

func TestListEntriesForAdmin_SearchEscapesLikeMetachars(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	a := seedAdminListEntry(t, s, domain.Entry{Title: "100% complete"}, 20)
	_ = seedAdminListEntry(t, s, domain.Entry{Title: "fifty done"}, 10)

	got, err := s.ListEntriesForAdmin(ctx, 1, ListEntriesQuery{Search: "100%"})
	if err != nil {
		t.Fatalf("ListEntriesForAdmin: %v", err)
	}
	if len(got) != 1 || got[0].ID != a {
		t.Fatalf("expected literal '%%' match only the first entry, got %d rows (first id=%d)", len(got), firstID(got))
	}
}

func TestListEntriesForAdmin_SortByTitleAsc(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	a := seedAdminListEntry(t, s, domain.Entry{Title: "banana"}, 30)
	b := seedAdminListEntry(t, s, domain.Entry{Title: "apple"}, 20)
	c := seedAdminListEntry(t, s, domain.Entry{Title: "cherry"}, 10)

	got, err := s.ListEntriesForAdmin(ctx, 1, ListEntriesQuery{SortBy: EntrySortTitle, SortDir: SortAsc})
	if err != nil {
		t.Fatalf("ListEntriesForAdmin: %v", err)
	}
	if got[0].ID != b || got[1].ID != a || got[2].ID != c {
		t.Errorf("title ASC: got %d,%d,%d; want %d,%d,%d", got[0].ID, got[1].ID, got[2].ID, b, a, c)
	}
}

func TestListEntriesForAdmin_LimitOffset(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	for i := 0; i < 5; i++ {
		seedAdminListEntry(t, s, domain.Entry{Title: "row"}, 50-i)
	}
	got, err := s.ListEntriesForAdmin(ctx, 1, ListEntriesQuery{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("ListEntriesForAdmin: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("len: got %d, want 2", len(got))
	}
}

func TestCountEntriesForAdmin(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedAdminListEntry(t, s, domain.Entry{Title: "one"}, 30)
	seedAdminListEntry(t, s, domain.Entry{Title: "two needle"}, 20)
	seedAdminListEntry(t, s, domain.Entry{Title: "needle three"}, 10)

	all, err := s.CountEntriesForAdmin(ctx, 1, ListEntriesQuery{})
	if err != nil {
		t.Fatalf("CountEntriesForAdmin: %v", err)
	}
	if all != 3 {
		t.Errorf("unfiltered count: got %d, want 3", all)
	}
	hits, err := s.CountEntriesForAdmin(ctx, 1, ListEntriesQuery{Search: "needle"})
	if err != nil {
		t.Fatalf("CountEntriesForAdmin needle: %v", err)
	}
	if hits != 2 {
		t.Errorf("needle count: got %d, want 2", hits)
	}
}

func TestParseEntrySortKey(t *testing.T) {
	cases := []struct {
		in   string
		want EntrySortKey
	}{
		{"", EntrySortPostedAt},
		{"unknown", EntrySortPostedAt},
		{"posted", EntrySortPostedAt},
		{"updated", EntrySortUpdatedAt},
		{"id", EntrySortID},
		{"title", EntrySortTitle},
		{"slug", EntrySortSlug},
		{"category", EntrySortCategory},
		{"status", EntrySortStatus},
	}
	for _, tc := range cases {
		got := ParseEntrySortKey(tc.in)
		if got != tc.want {
			t.Errorf("ParseEntrySortKey(%q): got %v, want %v", tc.in, got, tc.want)
		}
		if got.String() != "" && got != EntrySortPostedAt && tc.in != "" && tc.in != "unknown" {
			if got.String() != tc.in {
				t.Errorf("roundtrip mismatch: parsed %q → String=%q", tc.in, got.String())
			}
		}
	}
}

func firstID(es []domain.Entry) int64 {
	if len(es) == 0 {
		return 0
	}
	return es[0].ID
}
