package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/session"
)

// withChiID injects a chi URL param "id" into the request context.
func withChiID(r *http.Request, id int64) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", strconv.FormatInt(id, 10))
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

// withAdmin attaches an admin-role user to the request context so
// handlers that call session.UserFrom see an authenticated user.
func withAdmin(r *http.Request) *http.Request {
	u := &domain.User{ID: 1, Name: "admin", Role: domain.RoleAdmin}
	return r.WithContext(session.WithUser(r.Context(), u))
}

func seedTestEntry(t *testing.T, h *Handler, pinned bool) int64 {
	t.Helper()
	id, err := h.Store.CreateEntry(context.Background(), domain.Entry{
		WID:      1,
		AuthorID: 1,
		Title:    "Test entry",
		Body:     "body",
		Format:   "html",
		Status:   domain.EntryPublished,
		PostedAt: time.Now(),
		Pinned:   pinned,
	})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	return id
}

func TestEntryPinSetsFlag(t *testing.T) {
	h, _ := newAdminTestHandler(t)
	id := seedTestEntry(t, h, false)

	req := httptest.NewRequest(http.MethodPost, "/admin/entries/"+strconv.FormatInt(id, 10)+"/pin", nil)
	req = withAdmin(withChiID(req, id))
	rec := httptest.NewRecorder()

	h.entryPin(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, rec.Body.String())
	}
	if resp["ok"] != true {
		t.Errorf("resp[ok] = %v, want true", resp["ok"])
	}
	if resp["pinned"] != true {
		t.Errorf("resp[pinned] = %v, want true", resp["pinned"])
	}

	e, err := h.Store.EntryByID(context.Background(), 1, id)
	if err != nil {
		t.Fatalf("EntryByID: %v", err)
	}
	if !e.Pinned {
		t.Error("entry should be pinned in DB after POST /pin")
	}
}

func TestEntryPinDeleteClearsFlag(t *testing.T) {
	h, _ := newAdminTestHandler(t)
	id := seedTestEntry(t, h, true)

	req := httptest.NewRequest(http.MethodDelete, "/admin/entries/"+strconv.FormatInt(id, 10)+"/pin", nil)
	req = withAdmin(withChiID(req, id))
	rec := httptest.NewRecorder()

	h.entryPin(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, rec.Body.String())
	}
	if resp["pinned"] != false {
		t.Errorf("resp[pinned] = %v, want false", resp["pinned"])
	}

	e, err := h.Store.EntryByID(context.Background(), 1, id)
	if err != nil {
		t.Fatalf("EntryByID: %v", err)
	}
	if e.Pinned {
		t.Error("entry should not be pinned in DB after DELETE /pin")
	}
}

func TestEntryPinReturns404ForMissingEntry(t *testing.T) {
	h, _ := newAdminTestHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/admin/entries/9999/pin", nil)
	req = withAdmin(withChiID(req, 9999))
	rec := httptest.NewRecorder()

	h.entryPin(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestEntryPinReturns403WithoutUser(t *testing.T) {
	h, _ := newAdminTestHandler(t)
	id := seedTestEntry(t, h, false)

	// No user in context — no withAdmin call.
	req := httptest.NewRequest(http.MethodPost, "/admin/entries/"+strconv.FormatInt(id, 10)+"/pin", nil)
	req = withChiID(req, id)
	rec := httptest.NewRecorder()

	h.entryPin(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestEntryPinReturnsBadRequestForInvalidID(t *testing.T) {
	h, _ := newAdminTestHandler(t)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "not-a-number")
	req := httptest.NewRequest(http.MethodPost, "/admin/entries/not-a-number/pin",
		strings.NewReader(""))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req = withAdmin(req)
	rec := httptest.NewRecorder()

	h.entryPin(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}
