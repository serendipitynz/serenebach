package admin

import "testing"

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
