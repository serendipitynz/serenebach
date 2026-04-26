package app_test

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestPaginationHomeTagsAndOffset seeds enough entries to span three
// pages at size=3 and confirms each page:
//   - GET /?page=N returns 200 with the right entries by ID
//   - {page_num} / {page_now} / {prev_page_*} / {next_page_*} tags
//     resolve correctly
//   - the `page` block renders (count=1) when total > 1 pages
func TestPaginationHomeTagsAndOffset(t *testing.T) {
	a := newTestApp(t)
	// Clean slate so only the deterministic entries below contribute.
	if _, err := a.DB.Exec(`DELETE FROM entries`); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 7; i++ {
		ts := time.Date(2026, 3, i, 9, 0, 0, 0, time.UTC).Unix()
		if _, err := a.DB.Exec(`INSERT INTO entries (wid, author_id, category_id, title, body, more, format, status, posted_at, created_at, updated_at)
			VALUES (1, 1, -1, ?, '<p>body</p>', '', 'html', 1, ?, ?, ?)`,
			fmt.Sprintf("Entry %02d", i), ts, ts, ts); err != nil {
			t.Fatal(err)
		}
	}
	// Set page size to 3 via the weblog row (admin UI would do this).
	if _, err := a.DB.Exec(`UPDATE weblogs SET entries_per_page = 3 WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	// Swap the active template to include the page block + tag family.
	main := "<!doctype html><html><body>\n" +
		"<!-- BEGIN entry -->\n<article>{entry_title}</article>\n<!-- END entry -->\n" +
		"<!-- BEGIN page -->\n" +
		`<nav data-now="{page_now}" data-num="{page_num}" data-prev="{prev_page_url}" data-next="{next_page_url}"></nav>` + "\n" +
		"<!-- END page -->\n" +
		"</body></html>\n"
	if _, err := a.DB.Exec(`UPDATE templates SET main_body = ? WHERE is_active = 1`, main); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		url      string
		wantNow  string
		wantNum  string
		wantPrev string
		wantNext string
		// the first entry title visible on this page — newest-first
		firstTitle string
	}{
		// 7 entries at size 3 → 3 pages, pages 1/2/3 hold 3/3/1 items.
		{"/", "1", "3", "", "/?page=2", "Entry 07"},
		{"/?page=2", "2", "3", "/?page=1", "/?page=3", "Entry 04"},
		{"/?page=3", "3", "3", "/?page=2", "", "Entry 01"},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, httptest.NewRequest("GET", tc.url, nil))
		if w.Code != 200 {
			t.Errorf("%s: status = %d", tc.url, w.Code)
			continue
		}
		body := w.Body.String()
		for _, want := range []string{
			`data-now="` + tc.wantNow + `"`,
			`data-num="` + tc.wantNum + `"`,
			`data-prev="` + tc.wantPrev + `"`,
			`data-next="` + tc.wantNext + `"`,
			`<article>` + tc.firstTitle + `</article>`,
		} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: missing %q", tc.url, want)
			}
		}
	}
}

// TestPaginationOutOfRange404 confirms ?page=N past the last page
// returns 404 instead of rendering an empty-but-200 list.
func TestPaginationOutOfRange404(t *testing.T) {
	a := newTestApp(t)
	w := httptest.NewRecorder()
	// Seed has 2 entries with size=10 → only page 1 exists.
	a.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/?page=99", nil))
	if w.Code != 404 {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// TestPaginationNonNumeric404 confirms ?page=abc returns 404 — the
// parser rejects non-numeric values rather than silently defaulting.
func TestPaginationNonNumeric404(t *testing.T) {
	a := newTestApp(t)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/?page=abc", nil))
	if w.Code != 404 {
		t.Errorf("status = %d, want 404", w.Code)
	}
	// page=0 and negative values also 404 (1-indexed).
	w = httptest.NewRecorder()
	a.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/?page=0", nil))
	if w.Code != 404 {
		t.Errorf("page=0 status = %d, want 404", w.Code)
	}
}

// TestPaginationPageBlockStripsOnSinglePage confirms the block
// collapses to 0 when there's only one page — imported templates
// shouldn't show empty pager UI on a short blog.
func TestPaginationPageBlockStripsOnSinglePage(t *testing.T) {
	a := newTestApp(t)
	main := "<!doctype html><html><body>\n" +
		"<!-- BEGIN entry -->\n<article></article>\n<!-- END entry -->\n" +
		"<!-- BEGIN page -->\n<nav class=\"pager\"></nav>\n<!-- END page -->\n" +
		"</body></html>\n"
	if _, err := a.DB.Exec(`UPDATE templates SET main_body = ? WHERE is_active = 1`, main); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	if strings.Contains(w.Body.String(), `class="pager"`) {
		t.Errorf("pager block rendered on single-page home")
	}
}
