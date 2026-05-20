package repo

import (
	"context"
	"testing"

	"github.com/serendipitynz/serenebach/internal/domain"
)

func seedAdminListPage(t *testing.T, s *Store, p domain.Page) int64 {
	t.Helper()
	p.WID = 1
	if p.AuthorID == 0 {
		p.AuthorID = 1
	}
	if p.Format == "" {
		p.Format = "html"
	}
	id, err := s.CreatePage(context.Background(), p)
	if err != nil {
		t.Fatalf("CreatePage(%q): %v", p.Title, err)
	}
	return id
}

func TestListPagesForAdmin_DefaultSortsBySortOrderThenID(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	c := seedAdminListPage(t, s, domain.Page{Title: "c", Slug: "/c", SortOrder: 10})
	a := seedAdminListPage(t, s, domain.Page{Title: "a", Slug: "/a", SortOrder: 1})
	b := seedAdminListPage(t, s, domain.Page{Title: "b", Slug: "/b", SortOrder: 5})

	got, err := s.ListPagesForAdmin(ctx, 1, ListPagesQuery{})
	if err != nil {
		t.Fatalf("ListPagesForAdmin: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len: got %d, want 3", len(got))
	}
	// sort_order ASC tie-broken by id.
	if got[0].ID != a || got[1].ID != b || got[2].ID != c {
		t.Errorf("default order: got %d,%d,%d; want %d,%d,%d", got[0].ID, got[1].ID, got[2].ID, a, b, c)
	}
}

func TestListPagesForAdmin_OwnerFilter(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	mine := seedAdminListPage(t, s, domain.Page{Title: "mine", Slug: "/m", AuthorID: 7})
	_ = seedAdminListPage(t, s, domain.Page{Title: "theirs", Slug: "/t", AuthorID: 8})

	owner := int64(7)
	got, err := s.ListPagesForAdmin(ctx, 1, ListPagesQuery{OwnerID: &owner})
	if err != nil {
		t.Fatalf("ListPagesForAdmin: %v", err)
	}
	if len(got) != 1 || got[0].ID != mine {
		t.Errorf("owner filter: got %v, want only id %d", got, mine)
	}
}

func TestListPagesForAdmin_SearchMatchesTitleBodySlug(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedAdminListPage(t, s, domain.Page{Title: "needle in title", Slug: "/t"})
	seedAdminListPage(t, s, domain.Page{Title: "body match", Body: "the needle hides here", Slug: "/b"})
	seedAdminListPage(t, s, domain.Page{Title: "slug match", Slug: "/needle"})
	_ = seedAdminListPage(t, s, domain.Page{Title: "miss", Slug: "/m"})

	got, err := s.ListPagesForAdmin(ctx, 1, ListPagesQuery{Search: "needle"})
	if err != nil {
		t.Fatalf("ListPagesForAdmin: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("search needle: got %d rows, want 3", len(got))
	}
}

func TestListPagesForAdmin_SortByTitleAsc(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	cherry := seedAdminListPage(t, s, domain.Page{Title: "cherry", Slug: "/c"})
	apple := seedAdminListPage(t, s, domain.Page{Title: "apple", Slug: "/a"})
	banana := seedAdminListPage(t, s, domain.Page{Title: "banana", Slug: "/b"})

	got, err := s.ListPagesForAdmin(ctx, 1, ListPagesQuery{SortBy: PageSortTitle, SortDir: SortAsc})
	if err != nil {
		t.Fatalf("ListPagesForAdmin: %v", err)
	}
	if got[0].ID != apple || got[1].ID != banana || got[2].ID != cherry {
		t.Errorf("title ASC: got %d,%d,%d; want %d,%d,%d", got[0].ID, got[1].ID, got[2].ID, apple, banana, cherry)
	}
}

func TestCountPagesForAdmin(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedAdminListPage(t, s, domain.Page{Title: "alpha", Slug: "/a"})
	seedAdminListPage(t, s, domain.Page{Title: "beta needle", Slug: "/b"})
	seedAdminListPage(t, s, domain.Page{Title: "gamma", Slug: "/needle"})

	all, err := s.CountPagesForAdmin(ctx, 1, ListPagesQuery{})
	if err != nil {
		t.Fatalf("CountPagesForAdmin: %v", err)
	}
	if all != 3 {
		t.Errorf("unfiltered count: got %d, want 3", all)
	}
	hits, err := s.CountPagesForAdmin(ctx, 1, ListPagesQuery{Search: "needle"})
	if err != nil {
		t.Fatalf("CountPagesForAdmin needle: %v", err)
	}
	if hits != 2 {
		t.Errorf("needle count: got %d, want 2", hits)
	}
}

func TestParsePageSortKey(t *testing.T) {
	cases := []struct {
		in   string
		want PageSortKey
	}{
		{"", PageSortDefault},
		{"garbage", PageSortDefault},
		{"title", PageSortTitle},
		{"slug", PageSortSlug},
		{"template", PageSortTemplate},
		{"status", PageSortStatus},
		{"updated", PageSortUpdated},
	}
	for _, tc := range cases {
		if got := ParsePageSortKey(tc.in); got != tc.want {
			t.Errorf("ParsePageSortKey(%q): got %v, want %v", tc.in, got, tc.want)
		}
	}
}
