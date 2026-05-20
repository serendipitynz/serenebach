package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/session"
)

// seedEntryListRow creates a published entry minimal-enough for the
// list page to render it. ageMinutes shifts posted_at so multiple
// rows produce a deterministic posted-DESC order.
func seedEntryListRow(t *testing.T, h *Handler, title, slug string, authorID int64, ageMinutes int) int64 {
	t.Helper()
	if authorID == 0 {
		authorID = 1
	}
	id, err := h.Store.CreateEntry(context.Background(), domain.Entry{
		WID:      1,
		AuthorID: authorID,
		Title:    title,
		Slug:     slug,
		Body:     "body of " + title,
		Format:   "html",
		Status:   domain.EntryPublished,
		PostedAt: time.Now().Add(-time.Duration(ageMinutes) * time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateEntry(%q): %v", title, err)
	}
	return id
}

func TestEntryList_DefaultRendersAllEntries(t *testing.T) {
	h, _ := newAdminTestHandler(t)
	seedEntryListRow(t, h, "alpha", "", 1, 30)
	seedEntryListRow(t, h, "bravo", "", 1, 20)
	seedEntryListRow(t, h, "charlie", "", 1, 10)

	req := withAdmin(httptest.NewRequest(http.MethodGet, "/admin/entries", nil))
	rec := httptest.NewRecorder()
	h.entryList(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"alpha", "bravo", "charlie"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing entry %q", want)
		}
	}
	// Posted-DESC default: charlie (newest) appears before alpha (oldest).
	if strings.Index(body, "charlie") > strings.Index(body, "alpha") {
		t.Error("default sort should place newer entries above older")
	}
}

func TestEntryList_SearchNarrowsResults(t *testing.T) {
	h, _ := newAdminTestHandler(t)
	seedEntryListRow(t, h, "needle in title", "", 1, 30)
	seedEntryListRow(t, h, "unrelated", "my-needle", 1, 20)
	seedEntryListRow(t, h, "miss", "", 1, 10)

	req := withAdmin(httptest.NewRequest(http.MethodGet, "/admin/entries?q=needle", nil))
	rec := httptest.NewRecorder()
	h.entryList(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "needle in title") {
		t.Error("title-match should appear in body")
	}
	if !strings.Contains(body, "my-needle") {
		t.Error("slug-match should appear in body")
	}
	if strings.Contains(body, ">miss<") {
		t.Error("non-matching entry should be filtered out")
	}
	// Clear link present when search is active.
	if !strings.Contains(body, `class="list-search-clear"`) {
		t.Error("clear-search link missing when ?q= active")
	}
}

func TestEntryList_SearchEmptyMessage(t *testing.T) {
	h, _ := newAdminTestHandler(t)
	seedEntryListRow(t, h, "alpha", "", 1, 10)

	req := withAdmin(httptest.NewRequest(http.MethodGet, "/admin/entries?q=zzz-no-match", nil))
	rec := httptest.NewRecorder()
	h.entryList(rec, req)

	body := rec.Body.String()
	// JSON value escaping: "該当する記事はありません。" comes through verbatim.
	if !strings.Contains(body, "該当する記事はありません") && !strings.Contains(body, "No entries match") {
		t.Errorf("expected search-empty message, got body: %s", body[:min(400, len(body))])
	}
}

// Go 1.21 added built-in min/max; this package already builds against
// that toolchain (see go.mod).

func TestEntryList_SortLinkTogglesDir(t *testing.T) {
	h, _ := newAdminTestHandler(t)
	seedEntryListRow(t, h, "alpha", "", 1, 10)

	// ?sort=title&dir=asc → the title header link should now point to
	// dir=desc (toggle) and carry the "active asc" class.
	req := withAdmin(httptest.NewRequest(http.MethodGet, "/admin/entries?sort=title&dir=asc", nil))
	rec := httptest.NewRecorder()
	h.entryList(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `sort=title&amp;dir=desc`) && !strings.Contains(body, `sort=title&dir=desc`) {
		t.Error("active column should toggle to opposite direction on next click")
	}
	if !strings.Contains(body, `sort-link active asc`) {
		t.Error(`title column should render with "active asc" class`)
	}
}

func TestEntryList_PaginatesOver50Entries(t *testing.T) {
	h, _ := newAdminTestHandler(t)
	// Seed 55 entries — first page shows 50, second page shows 5.
	for i := 0; i < 55; i++ {
		seedEntryListRow(t, h, "row-"+strconv.Itoa(i), "", 1, 100-i)
	}

	// Page 1 should have a "Next" pager link.
	req := withAdmin(httptest.NewRequest(http.MethodGet, "/admin/entries", nil))
	rec := httptest.NewRecorder()
	h.entryList(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, `rel="next"`) {
		t.Error("page 1 should expose a next-page link")
	}
	if !strings.Contains(body, "1 / 2") {
		t.Error(`pager state "1 / 2" missing on page 1`)
	}

	// Page 2 should have a "Prev" link and the 51st row.
	req2 := withAdmin(httptest.NewRequest(http.MethodGet, "/admin/entries?page=2", nil))
	rec2 := httptest.NewRecorder()
	h.entryList(rec2, req2)
	body2 := rec2.Body.String()
	if !strings.Contains(body2, `rel="prev"`) {
		t.Error("page 2 should expose a prev-page link")
	}
	if !strings.Contains(body2, "2 / 2") {
		t.Error(`pager state "2 / 2" missing on page 2`)
	}
}

func TestEntryList_RegularRoleOnlySeesOwnEntries(t *testing.T) {
	h, _ := newAdminTestHandler(t)
	seedEntryListRow(t, h, "mine", "", 7, 20)
	seedEntryListRow(t, h, "theirs", "", 8, 10)

	req := httptest.NewRequest(http.MethodGet, "/admin/entries", nil)
	regular := &domain.User{ID: 7, Name: "regular", Role: domain.RoleRegular}
	req = req.WithContext(session.WithUser(req.Context(), regular))
	rec := httptest.NewRecorder()
	h.entryList(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "mine") {
		t.Error("regular user should see their own entry")
	}
	if strings.Contains(body, ">theirs<") {
		t.Error("regular user should not see another author's entry")
	}
}

func TestEntryList_PageParamClampsToValidRange(t *testing.T) {
	h, _ := newAdminTestHandler(t)
	seedEntryListRow(t, h, "alpha", "", 1, 10)

	// page=99 with only 1 row → clamp to page 1, render OK.
	req := withAdmin(httptest.NewRequest(http.MethodGet, "/admin/entries?page=99", nil))
	rec := httptest.NewRecorder()
	h.entryList(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("bad ?page= should not 404; got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "alpha") {
		t.Error("clamp should still render the only available entry")
	}
}

