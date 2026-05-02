package app_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCategoryPageShowsFilteredEntries(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)

	// seed places both sample entries into category id=1 ("お知らせ")
	req := httptest.NewRequest("GET", "/category/1", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{
		`<title>Category: お知らせ</title>`,
		`ようこそ Serene Bach へ`,
		`カテゴリとテンプレートについて`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q\nbody:\n%s", want, body)
			return
		}
	}
}

func TestCategoryPage404ForUnknownID(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	req := httptest.NewRequest("GET", "/category/99999", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != 404 {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestCategoryPageHidesEntriesInOtherCategories(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)

	// Insert a second category and a published entry belonging to it, so
	// the /category/1 page must not mention the new entry's title.
	ctx := context.Background()
	now := time.Now().Unix()
	res, err := a.DB.ExecContext(ctx, `
		INSERT INTO categories (wid, parent_id, name, slug, sort_order, created_at, updated_at)
		VALUES (1, 0, 'other', 'other', 0, ?, ?)`, now, now)
	if err != nil {
		t.Fatal(err)
	}
	otherCatID, _ := res.LastInsertId()
	if _, err := a.DB.ExecContext(ctx, `
		INSERT INTO entries (wid, author_id, category_id, title, body, more, format, status, posted_at, created_at, updated_at)
		VALUES (1, 1, ?, 'ELSEWHERE', '<p>x</p>', '', '', 1, ?, ?, ?)`,
		otherCatID, now, now, now); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/category/1", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	// Check only the main content area — the default template's sidebar
	// has a {latest_entry_list} widget that legitimately surfaces
	// entries from every category.
	if strings.Contains(mainArea(w.Body.String()), "ELSEWHERE") {
		t.Errorf("entry from other category leaked into /category/1")
	}

	// But the other category's own page must show it.
	req2 := httptest.NewRequest("GET", "/category/"+strings.TrimSpace(strings.ReplaceAll("  ", " ", " "))+"", nil)
	_ = req2 // silence unused in some refactor paths
	req3 := httptest.NewRequest("GET", "/category/2", nil)
	w3 := httptest.NewRecorder()
	a.Handler().ServeHTTP(w3, req3)
	if !strings.Contains(w3.Body.String(), "ELSEWHERE") {
		t.Errorf("/category/%d missing its own entry; body:\n%s", otherCatID, w3.Body.String())
	}
}

func TestArchiveYearFiltersByRange(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)

	year := time.Now().Year()
	// seeded entries are posted at "now" and "now-24h", both in current year
	url := "/archive/" + itoa(year)
	req := httptest.NewRequest("GET", url, nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Archive: "+itoa(year)) {
		t.Errorf("title missing; body:\n%s", body)
	}
	if !strings.Contains(body, "ようこそ Serene Bach へ") {
		t.Errorf("expected current-year entry to appear; body:\n%s", body)
	}

	// Earlier year must produce an empty entry list (no ようこそ).
	req2 := httptest.NewRequest("GET", "/archive/2000", nil)
	w2 := httptest.NewRecorder()
	a.Handler().ServeHTTP(w2, req2)
	if w2.Code != 200 {
		t.Fatalf("status = %d, want 200", w2.Code)
	}
	if strings.Contains(mainArea(w2.Body.String()), "ようこそ Serene Bach へ") {
		t.Errorf("old-year archive should have no entries")
	}
}

func TestArchiveMonth404ForBadParams(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)

	cases := []string{
		"/archive/abc",
		"/archive/2026/13",
		"/archive/2026/00",
		"/archive/abc/01",
	}
	for _, path := range cases {
		req := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, req)
		if w.Code != 404 {
			t.Errorf("%s: status = %d, want 404", path, w.Code)
		}
	}
}

// itoa is a tiny strconv-free helper used so the test file stays independent
// of the production strconv imports.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
