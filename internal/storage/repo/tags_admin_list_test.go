package repo

import (
	"context"
	"testing"

	"github.com/serendipitynz/serenebach/internal/domain"
)

func seedTagWithEntries(t *testing.T, s *Store, name string, entryCount int) int64 {
	t.Helper()
	tag, err := s.EnsureTagsByName(context.Background(), 1, []string{name})
	if err != nil {
		t.Fatalf("EnsureTagsByName(%q): %v", name, err)
	}
	if len(tag) != 1 {
		t.Fatalf("EnsureTagsByName(%q): want 1, got %d", name, len(tag))
	}
	id := tag[0].ID
	// Attach `entryCount` synthetic entries to this tag so the
	// JOIN-count surfaces the right number.
	for i := 0; i < entryCount; i++ {
		entryID, err := s.CreateEntry(context.Background(), domain.Entry{
			WID: 1, AuthorID: 1, Title: name + "-" + name, Body: "b",
			Format: "html", Status: domain.EntryPublished,
		})
		if err != nil {
			t.Fatalf("CreateEntry: %v", err)
		}
		if err := s.SetEntryTags(context.Background(), entryID, []int64{id}); err != nil {
			t.Fatalf("SetEntryTags: %v", err)
		}
	}
	return id
}

func TestListTagsForAdmin_NameAsc(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	cherry := seedTagWithEntries(t, s, "cherry", 0)
	apple := seedTagWithEntries(t, s, "apple", 0)
	banana := seedTagWithEntries(t, s, "banana", 0)

	// The handler forces SortAsc for the alphabetical default landing;
	// the repo treats SortDir literally. Pass SortAsc explicitly so
	// this test exercises the alphabetical "glossary" ordering.
	got, err := s.ListTagsForAdmin(ctx, 1, ListTagsQuery{SortBy: TagSortName, SortDir: SortAsc})
	if err != nil {
		t.Fatalf("ListTagsForAdmin: %v", err)
	}
	if len(got) != 3 || got[0].ID != apple || got[1].ID != banana || got[2].ID != cherry {
		t.Errorf("name ASC: got %v", []int64{got[0].ID, got[1].ID, got[2].ID})
	}
}

func TestListTagsForAdmin_NameDescRespected(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	apple := seedTagWithEntries(t, s, "apple", 0)
	cherry := seedTagWithEntries(t, s, "cherry", 0)
	banana := seedTagWithEntries(t, s, "banana", 0)

	// Regression for the previous behaviour where ListTagsForAdmin
	// silently forced name ASC regardless of SortDir: now DESC must
	// actually flip the order.
	got, err := s.ListTagsForAdmin(ctx, 1, ListTagsQuery{SortBy: TagSortName, SortDir: SortDesc})
	if err != nil {
		t.Fatalf("ListTagsForAdmin: %v", err)
	}
	if got[0].ID != cherry || got[1].ID != banana || got[2].ID != apple {
		t.Errorf("name DESC: got %v", []int64{got[0].ID, got[1].ID, got[2].ID})
	}
}

func TestListTagsForAdmin_SortByCount(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	zero := seedTagWithEntries(t, s, "zero", 0)
	two := seedTagWithEntries(t, s, "two", 2)
	five := seedTagWithEntries(t, s, "five", 5)

	got, err := s.ListTagsForAdmin(ctx, 1, ListTagsQuery{SortBy: TagSortCount, SortDir: SortDesc})
	if err != nil {
		t.Fatalf("ListTagsForAdmin: %v", err)
	}
	if got[0].ID != five || got[1].ID != two || got[2].ID != zero {
		t.Errorf("count DESC: got %v", []int64{got[0].ID, got[1].ID, got[2].ID})
	}
	if got[0].EntryCount != 5 || got[2].EntryCount != 0 {
		t.Errorf("entry counts: got %d/%d/%d", got[0].EntryCount, got[1].EntryCount, got[2].EntryCount)
	}
}

func TestListTagsForAdmin_SortBySlugAsc(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	z := seedTagWithEntries(t, s, "zebra", 0)
	a := seedTagWithEntries(t, s, "alpha", 0)

	got, err := s.ListTagsForAdmin(ctx, 1, ListTagsQuery{SortBy: TagSortSlug, SortDir: SortAsc})
	if err != nil {
		t.Fatalf("ListTagsForAdmin: %v", err)
	}
	if got[0].ID != a || got[1].ID != z {
		t.Errorf("slug ASC: got %v", []int64{got[0].ID, got[1].ID})
	}
}

func TestParseTagSortKey(t *testing.T) {
	cases := []struct {
		in   string
		want TagSortKey
	}{
		{"", TagSortName},
		{"garbage", TagSortName},
		{"id", TagSortID},
		{"slug", TagSortSlug},
		{"count", TagSortCount},
	}
	for _, tc := range cases {
		if got := ParseTagSortKey(tc.in); got != tc.want {
			t.Errorf("ParseTagSortKey(%q): got %v, want %v", tc.in, got, tc.want)
		}
	}
}
