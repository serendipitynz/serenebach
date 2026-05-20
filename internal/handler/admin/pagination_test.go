package admin

import (
	"testing"
)

func TestListPagination(t *testing.T) {
	cases := []struct {
		name       string
		rawPage    string
		total      int64
		pageSize   int
		wantPage   int
		wantPages  int
		wantOffset int
	}{
		{"empty zero rows", "", 0, 50, 1, 1, 0},
		{"empty single page", "", 30, 50, 1, 1, 0},
		{"exact fit two pages", "2", 100, 50, 2, 2, 50},
		{"partial last page", "3", 110, 50, 3, 3, 100},
		{"page=0 clamps to 1", "0", 110, 50, 1, 3, 0},
		{"negative page clamps to 1", "-2", 110, 50, 1, 3, 0},
		{"non-numeric clamps to 1", "abc", 110, 50, 1, 3, 0},
		{"page past end clamps to last", "99", 110, 50, 3, 3, 100},
		{"pageSize<=0 defaults to 1", "1", 5, 0, 1, 5, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			page, totalPages, offset := listPagination(tc.rawPage, tc.total, tc.pageSize)
			if page != tc.wantPage {
				t.Errorf("page: got %d, want %d", page, tc.wantPage)
			}
			if totalPages != tc.wantPages {
				t.Errorf("totalPages: got %d, want %d", totalPages, tc.wantPages)
			}
			if offset != tc.wantOffset {
				t.Errorf("offset: got %d, want %d", offset, tc.wantOffset)
			}
		})
	}
}

func TestListURLState_HrefSort(t *testing.T) {
	base := listURLState{BasePath: "/admin/entries", Search: "foo", SortKey: "posted", SortDir: "desc", Page: 3}

	// Clicking a different column uses that column's default dir,
	// resets page to 1, preserves q.
	if got := base.hrefSort("title", "asc"); got != "/admin/entries?q=foo&sort=title&dir=asc" {
		t.Errorf("hrefSort(title, asc): got %q", got)
	}

	// Clicking the active column toggles dir.
	if got := base.hrefSort("posted", "desc"); got != "/admin/entries?q=foo&sort=posted&dir=asc" {
		t.Errorf("hrefSort(active toggle): got %q", got)
	}
	asc := listURLState{BasePath: "/admin/entries", SortKey: "title", SortDir: "asc"}
	if got := asc.hrefSort("title", "asc"); got != "/admin/entries?sort=title&dir=desc" {
		t.Errorf("hrefSort(asc->desc): got %q", got)
	}
}

func TestListURLState_HrefPage(t *testing.T) {
	base := listURLState{BasePath: "/admin/entries", Search: "foo", SortKey: "title", SortDir: "asc", Page: 2}
	if got := base.hrefPage(3); got != "/admin/entries?q=foo&sort=title&dir=asc&page=3" {
		t.Errorf("hrefPage(3): got %q", got)
	}
	// page=1 is omitted for clean canonical URL.
	if got := base.hrefPage(1); got != "/admin/entries?q=foo&sort=title&dir=asc" {
		t.Errorf("hrefPage(1) should omit page=: got %q", got)
	}
	// 0 means "no link in this direction".
	if got := base.hrefPage(0); got != "" {
		t.Errorf("hrefPage(0): got %q, want empty", got)
	}
}

func TestListURLState_HrefPage_EncodesSearch(t *testing.T) {
	base := listURLState{BasePath: "/admin/entries", Search: "a b&c", SortKey: "title", SortDir: "asc"}
	if got := base.hrefPage(2); got != "/admin/entries?q=a+b%26c&sort=title&dir=asc&page=2" {
		t.Errorf("hrefPage with special chars: got %q", got)
	}
}

func TestListURLState_ExtrasPreservedAcrossLinks(t *testing.T) {
	base := listURLState{
		BasePath: "/admin/comments",
		Search:   "foo",
		SortKey:  "posted",
		SortDir:  "desc",
		Page:     2,
		Extras:   map[string]string{"status": "waiting"},
	}
	// Pager and sort links must keep status= attached.
	if got := base.hrefPage(3); got != "/admin/comments?q=foo&sort=posted&dir=desc&page=3&status=waiting" {
		t.Errorf("hrefPage extras: got %q", got)
	}
	if got := base.hrefSort("author", "asc"); got != "/admin/comments?q=foo&sort=author&dir=asc&status=waiting" {
		t.Errorf("hrefSort extras: got %q", got)
	}
}

func TestListURLState_ExtrasEmptyValueOmitted(t *testing.T) {
	base := listURLState{
		BasePath: "/admin/comments",
		Extras:   map[string]string{"status": ""},
	}
	if got := base.hrefPage(2); got != "/admin/comments?page=2" {
		t.Errorf("empty extras value should be omitted: got %q", got)
	}
}

func TestListURLState_ClassFor(t *testing.T) {
	state := listURLState{SortKey: "title", SortDir: "asc"}
	if got := state.classFor("title"); got != "active asc" {
		t.Errorf("classFor(active asc): got %q", got)
	}
	state.SortDir = "desc"
	if got := state.classFor("title"); got != "active desc" {
		t.Errorf("classFor(active desc): got %q", got)
	}
	if got := state.classFor("posted"); got != "" {
		t.Errorf("classFor(inactive): got %q", got)
	}
}

func TestPagerNeighbours(t *testing.T) {
	cases := []struct {
		name       string
		page       int
		totalPages int
		wantPrev   int
		wantNext   int
	}{
		{"single page", 1, 1, 0, 0},
		{"first of many", 1, 5, 0, 2},
		{"middle", 3, 5, 2, 4},
		{"last", 5, 5, 4, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prev, next := pagerNeighbours(tc.page, tc.totalPages)
			if prev != tc.wantPrev {
				t.Errorf("prev: got %d, want %d", prev, tc.wantPrev)
			}
			if next != tc.wantNext {
				t.Errorf("next: got %d, want %d", next, tc.wantNext)
			}
		})
	}
}
