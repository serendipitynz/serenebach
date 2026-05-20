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
)

func seedListMessage(t *testing.T, h *Handler, author, body string, status domain.MessageStatus, ageMinutes int) int64 {
	t.Helper()
	id, err := h.Store.CreateMessage(context.Background(), domain.Message{
		WID:        1,
		EntryID:    1,
		Status:     status,
		PostedAt:   time.Now().Add(-time.Duration(ageMinutes) * time.Minute),
		AuthorName: author,
		Body:       body,
	})
	if err != nil {
		t.Fatalf("CreateMessage(%q): %v", author, err)
	}
	return id
}

func TestCommentList_DefaultShowsAllStatuses(t *testing.T) {
	h, _ := newAdminTestHandler(t)
	seedListMessage(t, h, "alpha", "a", domain.MessageWaiting, 30)
	seedListMessage(t, h, "bravo", "b", domain.MessageApproved, 20)
	seedListMessage(t, h, "charlie", "c", domain.MessageHidden, 10)

	req := withAdmin(httptest.NewRequest(http.MethodGet, "/admin/comments", nil))
	rec := httptest.NewRecorder()
	h.commentList(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"alpha", "bravo", "charlie"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing comment %q", want)
		}
	}
}

func TestCommentList_StatusFilter(t *testing.T) {
	h, _ := newAdminTestHandler(t)
	seedListMessage(t, h, "wait1", "w1", domain.MessageWaiting, 30)
	seedListMessage(t, h, "approved1", "a1", domain.MessageApproved, 20)

	req := withAdmin(httptest.NewRequest(http.MethodGet, "/admin/comments?status=waiting", nil))
	rec := httptest.NewRecorder()
	h.commentList(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "wait1") {
		t.Error("waiting filter should include waiting comments")
	}
	if strings.Contains(body, ">approved1<") {
		t.Error("waiting filter should exclude approved comments")
	}
}

func TestCommentList_SearchPreservesStatusFilter(t *testing.T) {
	h, _ := newAdminTestHandler(t)
	seedListMessage(t, h, "needle", "x", domain.MessageWaiting, 30)
	seedListMessage(t, h, "needle approved", "x", domain.MessageApproved, 20)
	seedListMessage(t, h, "miss", "x", domain.MessageWaiting, 10)

	req := withAdmin(httptest.NewRequest(http.MethodGet, "/admin/comments?status=waiting&q=needle", nil))
	rec := httptest.NewRecorder()
	h.commentList(rec, req)

	body := rec.Body.String()
	// status=waiting + q=needle: only "needle" (waiting) should remain.
	if !strings.Contains(body, ">needle<") {
		t.Error("expected waiting+needle comment to appear")
	}
	if strings.Contains(body, ">needle approved<") {
		t.Error("approved match should be filtered out by status=waiting")
	}
	if strings.Contains(body, ">miss<") {
		t.Error("non-search match should be filtered out")
	}
}

func TestCommentList_SearchHitsEmailAndIP(t *testing.T) {
	h, _ := newAdminTestHandler(t)
	id, err := h.Store.CreateMessage(context.Background(), domain.Message{
		WID:         1,
		EntryID:     1,
		Status:      domain.MessageApproved,
		PostedAt:    time.Now(),
		AuthorName:  "x",
		Body:        "x",
		AuthorEmail: "needle@example.com",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_ = id

	req := withAdmin(httptest.NewRequest(http.MethodGet, "/admin/comments?q=needle@example", nil))
	rec := httptest.NewRecorder()
	h.commentList(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, "needle@example") {
		t.Errorf("email match should appear in body; got: %s", body[:min(400, len(body))])
	}
}

func TestCommentList_SortByAuthorToggles(t *testing.T) {
	h, _ := newAdminTestHandler(t)
	seedListMessage(t, h, "alpha", "a", domain.MessageApproved, 10)

	req := withAdmin(httptest.NewRequest(http.MethodGet, "/admin/comments?sort=author&dir=asc", nil))
	rec := httptest.NewRecorder()
	h.commentList(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `sort-link active asc`) {
		t.Error(`author column should render with "active asc" class`)
	}
	if !strings.Contains(body, `sort=author&amp;dir=desc`) && !strings.Contains(body, `sort=author&dir=desc`) {
		t.Error("active author column should toggle to desc on next click")
	}
}

func TestCommentList_PaginatesOver50Comments(t *testing.T) {
	h, _ := newAdminTestHandler(t)
	for i := 0; i < 55; i++ {
		seedListMessage(t, h, "row-"+strconv.Itoa(i), "x", domain.MessageApproved, 100-i)
	}

	req := withAdmin(httptest.NewRequest(http.MethodGet, "/admin/comments", nil))
	rec := httptest.NewRecorder()
	h.commentList(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, `rel="next"`) {
		t.Error("page 1 should expose a next-page link")
	}
	if !strings.Contains(body, "1 / 2") {
		t.Error(`pager state "1 / 2" missing on page 1`)
	}
}

func TestCommentList_ClearSearchHref_NoStatus(t *testing.T) {
	h, _ := newAdminTestHandler(t)
	seedListMessage(t, h, "alpha", "a", domain.MessageApproved, 10)

	req := withAdmin(httptest.NewRequest(http.MethodGet, "/admin/comments?q=needle", nil))
	rec := httptest.NewRecorder()
	h.commentList(rec, req)

	body := rec.Body.String()
	// The clear-search anchor must have a non-empty href even when no
	// ?status= is active (regression: a template lookup keyed on the
	// empty status returned "" and made the link inert).
	if !strings.Contains(body, `href="/admin/comments" class="list-search-clear"`) {
		t.Errorf("clear-search href should drop q and land on /admin/comments; got: %s", body[:min(800, len(body))])
	}
}

func TestCommentList_ClearSearchHref_KeepsStatusAndSort(t *testing.T) {
	h, _ := newAdminTestHandler(t)
	seedListMessage(t, h, "alpha", "a", domain.MessageWaiting, 10)

	req := withAdmin(httptest.NewRequest(http.MethodGet, "/admin/comments?status=waiting&q=needle&sort=author&dir=asc", nil))
	rec := httptest.NewRecorder()
	h.commentList(rec, req)

	body := rec.Body.String()
	// Clear should drop q but keep sort + dir + status — the user
	// explicitly chose those, only the search needle is ephemeral.
	if !strings.Contains(body, `href="/admin/comments?sort=author&amp;dir=asc&amp;status=waiting" class="list-search-clear"`) &&
		!strings.Contains(body, `href="/admin/comments?sort=author&dir=asc&status=waiting" class="list-search-clear"`) {
		t.Errorf("clear-search href should preserve sort/dir/status; got: %s", body[:min(800, len(body))])
	}
}

func TestCommentList_FilterTabLinksPreserveSearchAndSort(t *testing.T) {
	h, _ := newAdminTestHandler(t)
	seedListMessage(t, h, "alpha", "a", domain.MessageApproved, 10)

	req := withAdmin(httptest.NewRequest(http.MethodGet, "/admin/comments?q=needle&sort=author&dir=asc", nil))
	rec := httptest.NewRecorder()
	h.commentList(rec, req)

	body := rec.Body.String()
	// The "waiting" tab link must carry q + sort + dir even though
	// the page itself has no ?status= active.
	if !strings.Contains(body, `q=needle&amp;sort=author&amp;dir=asc&amp;status=waiting`) &&
		!strings.Contains(body, `q=needle&sort=author&dir=asc&status=waiting`) {
		t.Errorf("filter tab link should preserve q/sort/dir alongside status=; got: %s", body[:min(800, len(body))])
	}
}
