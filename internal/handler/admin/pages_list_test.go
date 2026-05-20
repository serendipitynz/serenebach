package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/session"
)

func seedListPage(t *testing.T, h *Handler, title, slug string, authorID int64, sortOrder int) int64 {
	t.Helper()
	if authorID == 0 {
		authorID = 1
	}
	id, err := h.Store.CreatePage(context.Background(), domain.Page{
		WID:       1,
		AuthorID:  authorID,
		Title:     title,
		Body:      "body of " + title,
		Slug:      slug,
		Format:    "html",
		SortOrder: sortOrder,
		Status:    domain.PagePublished,
	})
	if err != nil {
		t.Fatalf("CreatePage(%q): %v", title, err)
	}
	return id
}

func TestPageList_DefaultRendersAllPages(t *testing.T) {
	h, _ := newAdminTestHandler(t)
	seedListPage(t, h, "alpha", "/alpha", 1, 2)
	seedListPage(t, h, "beta", "/beta", 1, 1)
	seedListPage(t, h, "gamma", "/gamma", 1, 3)

	req := withAdmin(httptest.NewRequest(http.MethodGet, "/admin/pages", nil))
	rec := httptest.NewRecorder()
	h.pageList(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	// sort_order 1 (beta) should come before sort_order 2 (alpha).
	if strings.Index(body, ">beta<") > strings.Index(body, ">alpha<") {
		t.Error("default sort should follow sort_order ASC")
	}
}

func TestPageList_SearchNarrowsResults(t *testing.T) {
	h, _ := newAdminTestHandler(t)
	seedListPage(t, h, "needle in title", "/a", 1, 1)
	seedListPage(t, h, "slug match", "/needle", 1, 2)
	seedListPage(t, h, "miss", "/m", 1, 3)

	req := withAdmin(httptest.NewRequest(http.MethodGet, "/admin/pages?q=needle", nil))
	rec := httptest.NewRecorder()
	h.pageList(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "needle in title") {
		t.Error("title-match should appear")
	}
	if !strings.Contains(body, "slug match") {
		t.Error("slug-match should appear")
	}
	if strings.Contains(body, ">miss<") {
		t.Error("non-matching page should be filtered out")
	}
	if !strings.Contains(body, `class="list-search-clear"`) {
		t.Error("clear-search link missing when ?q= active")
	}
}

func TestPageList_SortByTitleToggles(t *testing.T) {
	h, _ := newAdminTestHandler(t)
	seedListPage(t, h, "alpha", "/a", 1, 1)

	req := withAdmin(httptest.NewRequest(http.MethodGet, "/admin/pages?sort=title&dir=asc", nil))
	rec := httptest.NewRecorder()
	h.pageList(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `sort-link active asc`) {
		t.Error(`title column should render with "active asc" class`)
	}
	if !strings.Contains(body, `sort=title&amp;dir=desc`) && !strings.Contains(body, `sort=title&dir=desc`) {
		t.Error("active title column should toggle to desc on next click")
	}
}

func TestPageList_RegularRoleOnlySeesOwnPages(t *testing.T) {
	h, _ := newAdminTestHandler(t)
	seedListPage(t, h, "mine", "/m", 7, 1)
	seedListPage(t, h, "theirs", "/t", 8, 2)

	req := httptest.NewRequest(http.MethodGet, "/admin/pages", nil)
	regular := &domain.User{ID: 7, Name: "regular", Role: domain.RoleRegular}
	req = req.WithContext(session.WithUser(req.Context(), regular))
	rec := httptest.NewRecorder()
	h.pageList(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "mine") {
		t.Error("regular user should see their own page")
	}
	if strings.Contains(body, ">theirs<") {
		t.Error("regular user should not see another author's page")
	}
}

func TestPageList_SearchEmptyMessage(t *testing.T) {
	h, _ := newAdminTestHandler(t)
	seedListPage(t, h, "alpha", "/a", 1, 1)

	req := withAdmin(httptest.NewRequest(http.MethodGet, "/admin/pages?q=zzz-no-match", nil))
	rec := httptest.NewRecorder()
	h.pageList(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "該当する固定ページ") && !strings.Contains(body, "No pages match") {
		t.Errorf("expected search-empty message, got body: %s", body[:min(400, len(body))])
	}
}
