package app_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestAdminCategoriesListShowsSeeded(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	w := authedGET(t, a.Handler(), "/admin/categories", cookies)
	if w.Code != 200 {
		t.Fatalf("status = %d, body:\n%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{
		"カテゴリー",
		"お知らせ", // seeded in seed.go
		"新規カテゴリー",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("list page missing %q", want)
		}
	}
}

func TestAdminCategoryCreateAndEdit(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	// Create
	form := url.Values{
		"name":       {"ジャーナル"},
		"slug":       {"journal"},
		"parent_id":  {"0"},
		"sort_order": {"10"},
	}
	w := authedPOSTForm(t, a.Handler(), "/admin/categories/new", form, cookies)
	if w.Code != http.StatusFound {
		t.Fatalf("create status = %d; body:\n%s", w.Code, w.Body.String())
	}

	// Listing now includes the new row.
	list := authedGET(t, a.Handler(), "/admin/categories", cookies).Body.String()
	if !strings.Contains(list, "ジャーナル") {
		t.Errorf("listing missing new category; body:\n%s", list)
	}

	// Find the new id via DB so we can edit it.
	var id int64
	if err := a.DB.QueryRow(`SELECT id FROM categories WHERE name = ?`, "ジャーナル").Scan(&id); err != nil {
		t.Fatal(err)
	}

	// Edit → rename
	update := url.Values{
		"name":       {"日記"},
		"slug":       {"diary"},
		"parent_id":  {"0"},
		"sort_order": {"20"},
	}
	editPath := "/admin/categories/" + itoa64(id) + "/edit"
	w2 := authedPOSTForm(t, a.Handler(), editPath, update, cookies)
	if w2.Code != http.StatusFound {
		t.Fatalf("update status = %d; body:\n%s", w2.Code, w2.Body.String())
	}
	var name, slug string
	var sortOrder int
	_ = a.DB.QueryRow(`SELECT name, slug, sort_order FROM categories WHERE id = ?`, id).Scan(&name, &slug, &sortOrder)
	if name != "日記" || slug != "diary" || sortOrder != 20 {
		t.Errorf("persisted row = (%q, %q, %d); want (日記, diary, 20)", name, slug, sortOrder)
	}
}

func TestAdminCategoryRejectsBlankName(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	form := url.Values{
		"name":      {"   "},
		"parent_id": {"0"},
	}
	w := authedPOSTForm(t, a.Handler(), "/admin/categories/new", form, cookies)
	if w.Code != 200 {
		t.Fatalf("blank-name status = %d, want 200 (stay on form)", w.Code)
	}
	if !strings.Contains(w.Body.String(), "カテゴリー名を入力してください") {
		t.Errorf("missing validation message")
	}
}

// TestAdminCategoryDeleteReassignsEntries confirms the "delete reassigns
// entries to uncategorised" guarantee the repo makes. Without it the
// admin listing would silently show entries pointing at a dead id.
func TestAdminCategoryDeleteReassignsEntries(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	// The seed creates a category with one or more entries attached.
	var catID int64
	if err := a.DB.QueryRow(`SELECT id FROM categories LIMIT 1`).Scan(&catID); err != nil {
		t.Fatal(err)
	}
	var entryCount int64
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM entries WHERE category_id = ?`, catID).Scan(&entryCount)
	if entryCount == 0 {
		t.Fatalf("seed expected at least one entry attached to category %d", catID)
	}

	del := authedPOSTForm(t, a.Handler(), "/admin/categories/"+itoa64(catID)+"/delete",
		url.Values{}, cookies)
	if del.Code != http.StatusFound {
		t.Fatalf("delete status = %d", del.Code)
	}

	var stillThere int
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM categories WHERE id = ?`, catID).Scan(&stillThere)
	if stillThere != 0 {
		t.Errorf("category row should be gone")
	}
	var leftover int
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM entries WHERE category_id = ?`, catID).Scan(&leftover)
	if leftover != 0 {
		t.Errorf("entries still reference dead category id")
	}
	var uncat int
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM entries WHERE category_id = -1`).Scan(&uncat)
	if int64(uncat) < entryCount {
		t.Errorf("uncategorised count = %d, want >= %d", uncat, entryCount)
	}
}

// TestAdminCategoryReorderPersistsOrder sends the JSON order the drag-and-
// drop UI would send and confirms sort_order gets rewritten so the
// AllCategories listing reflects the new positions.
func TestAdminCategoryReorderPersistsOrder(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	// Create two categories so the initial listing has at least three rows
	// (one seeded + two fresh).
	for _, name := range []string{"A-first", "B-second"} {
		f := url.Values{"name": {name}, "parent_id": {"0"}, "sort_order": {"0"}}
		_ = authedPOSTForm(t, a.Handler(), "/admin/categories/new", f, cookies)
	}

	// Grab the current id order newest-first so we can ask for a reversed
	// order — any permutation works, this one is easy to assert on.
	rows, err := a.DB.Query(`SELECT id FROM categories ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		_ = rows.Scan(&id)
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(ids) < 2 {
		t.Fatalf("need at least 2 categories to reorder, got %d", len(ids))
	}
	reversed := make([]int64, len(ids))
	for i, id := range ids {
		reversed[len(ids)-1-i] = id
	}

	payload, _ := json.Marshal(struct {
		IDs []int64 `json:"ids"`
	}{IDs: reversed})
	req := httptest.NewRequest("POST", "/admin/categories/reorder", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrfTokenFromJar(cookies))
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("reorder status = %d; body:\n%s", w.Code, w.Body.String())
	}

	// Confirm sort_order was rewritten to index positions.
	for i, id := range reversed {
		var got int
		if err := a.DB.QueryRow(`SELECT sort_order FROM categories WHERE id = ?`, id).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != i {
			t.Errorf("id=%d sort_order = %d, want %d", id, got, i)
		}
	}
}

// TestAdminCategoryReorderRequiresCSRF confirms the header check in the
// CSRF middleware actually refuses a JSON POST that doesn't echo the
// token. Otherwise the whole reorder endpoint would be an open relay.
func TestAdminCategoryReorderRequiresCSRF(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	body := bytes.NewReader([]byte(`{"ids":[1]}`))
	req := httptest.NewRequest("POST", "/admin/categories/reorder", body)
	req.Header.Set("Content-Type", "application/json")
	// deliberately NO X-CSRF-Token
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without CSRF token, got %d; body:\n%s", w.Code, w.Body.String())
	}
}

func TestAdminCategoryParentCycleRejected(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	// Create A.
	w := authedPOSTForm(t, a.Handler(), "/admin/categories/new", url.Values{
		"name":      {"A"},
		"parent_id": {"0"},
	}, cookies)
	if w.Code != http.StatusFound {
		t.Fatalf("create A status = %d", w.Code)
	}
	var aID int64
	_ = a.DB.QueryRow(`SELECT id FROM categories WHERE name = 'A'`).Scan(&aID)

	// Create B under A.
	w = authedPOSTForm(t, a.Handler(), "/admin/categories/new", url.Values{
		"name":      {"B"},
		"parent_id": {itoa64(aID)},
	}, cookies)
	if w.Code != http.StatusFound {
		t.Fatalf("create B status = %d", w.Code)
	}
	var bID int64
	_ = a.DB.QueryRow(`SELECT id FROM categories WHERE name = 'B'`).Scan(&bID)

	// Editing A: the parent <select> must not offer B (descendant → cycle).
	editForm := authedGET(t, a.Handler(), "/admin/categories/"+itoa64(aID)+"/edit", cookies).Body.String()
	if strings.Contains(editForm, `<option value="`+itoa64(bID)+`"`) {
		t.Errorf("descendant B should not appear as a parent candidate for A")
	}
}
