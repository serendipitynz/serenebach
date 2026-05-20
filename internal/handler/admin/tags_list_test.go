package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func seedListTag(t *testing.T, h *Handler, name string) {
	t.Helper()
	if _, err := h.Store.EnsureTagsByName(context.Background(), 1, []string{name}); err != nil {
		t.Fatalf("EnsureTagsByName(%q): %v", name, err)
	}
}

func TestTagList_DefaultSortsByNameAsc(t *testing.T) {
	h, _ := newAdminTestHandler(t)
	seedListTag(t, h, "cherry")
	seedListTag(t, h, "apple")
	seedListTag(t, h, "banana")

	req := withAdmin(httptest.NewRequest(http.MethodGet, "/admin/tags", nil))
	rec := httptest.NewRecorder()
	h.tagList(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	// apple < banana < cherry
	apple := strings.Index(body, "apple")
	banana := strings.Index(body, "banana")
	cherry := strings.Index(body, "cherry")
	if !(apple < banana && banana < cherry) {
		t.Errorf("name ASC: positions apple=%d banana=%d cherry=%d", apple, banana, cherry)
	}
}

func TestTagList_SortByIDToggles(t *testing.T) {
	h, _ := newAdminTestHandler(t)
	seedListTag(t, h, "alpha")

	req := withAdmin(httptest.NewRequest(http.MethodGet, "/admin/tags?sort=id&dir=desc", nil))
	rec := httptest.NewRecorder()
	h.tagList(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `sort-link active desc`) {
		t.Error(`id column should render with "active desc" class`)
	}
	if !strings.Contains(body, `sort=id&amp;dir=asc`) && !strings.Contains(body, `sort=id&dir=asc`) {
		t.Error("active id column should toggle to asc on next click")
	}
}

