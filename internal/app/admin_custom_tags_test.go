package app_test

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/serendipitynz/serenebach/internal/auth"
)

func TestAdminCustomTagsRequiresLogin(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)

	w := authedGET(t, a.Handler(), "/admin/templates/custom-tags", nil)
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	if loc := w.Header().Get("Location"); !strings.Contains(loc, "/admin/login") {
		t.Errorf("Location = %q, want prefix /admin/login", loc)
	}
}

// TestAdminCustomTagsRejectsRegularUser confirms requireDesign blocks
// role=3 (regular) sessions even when they guess the URL directly.
func TestAdminCustomTagsRejectsRegularUser(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	hash, _ := auth.HashPassword("secret")
	if _, err := a.DB.Exec(`INSERT INTO users (wid, name, display_name, email, password_hash, role, created_at, updated_at, list_visible, description_format)
		VALUES (1, 'regular', 'Reg', '', ?, 3, strftime('%s','now'), strftime('%s','now'), 1, 'html')`, hash); err != nil {
		t.Fatal(err)
	}
	cookies := login(t, a.Handler(), "regular", "secret")

	w := authedGET(t, a.Handler(), "/admin/templates/custom-tags", cookies)
	if w.Code != http.StatusForbidden {
		t.Errorf("GET list status = %d, want 403", w.Code)
	}

	form := url.Values{"name": {"custom_x"}, "value": {"v"}}
	w2 := authedPOSTForm(t, a.Handler(), "/admin/templates/custom-tags", form, cookies)
	if w2.Code != http.StatusForbidden {
		t.Errorf("POST create status = %d, want 403", w2.Code)
	}
}

// TestAdminCustomTagsAllowsPowerUser confirms power-tier sessions can
// reach the page (requireDesign permits both admin and power roles).
func TestAdminCustomTagsAllowsPowerUser(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	hash, _ := auth.HashPassword("secret")
	if _, err := a.DB.Exec(`INSERT INTO users (wid, name, display_name, email, password_hash, role, created_at, updated_at, list_visible, description_format)
		VALUES (1, 'powerguy', 'Power', '', ?, 2, strftime('%s','now'), strftime('%s','now'), 1, 'html')`, hash); err != nil {
		t.Fatal(err)
	}
	cookies := login(t, a.Handler(), "powerguy", "secret")

	w := authedGET(t, a.Handler(), "/admin/templates/custom-tags", cookies)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// TestAdminCustomTagsCreateRoundtrip exercises the happy path:
// create → list shows the row → update changes the value → delete
// removes it.
func TestAdminCustomTagsCreateRoundtrip(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	form := url.Values{"name": {"custom_hello"}, "value": {"<b>hi</b>"}}
	w := authedPOSTForm(t, a.Handler(), "/admin/templates/custom-tags", form, cookies)
	if w.Code != http.StatusFound {
		t.Fatalf("create status = %d; body:\n%s", w.Code, w.Body.String())
	}
	if loc := w.Header().Get("Location"); !strings.Contains(loc, "ok=created") {
		t.Errorf("expected ok=created, got %q", loc)
	}

	list := authedGET(t, a.Handler(), "/admin/templates/custom-tags", cookies)
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d", list.Code)
	}
	body := list.Body.String()
	if !strings.Contains(body, "custom_hello") {
		t.Errorf("created tag missing from list; body:\n%s", body)
	}

	// Look up the row id.
	var id int64
	if err := a.DB.QueryRow(`SELECT id FROM site_custom_tags WHERE wid = 1 AND name = 'custom_hello'`).Scan(&id); err != nil {
		t.Fatalf("lookup id: %v", err)
	}

	// Update the value.
	upd := url.Values{"name": {"custom_hello"}, "value": {"<i>hello</i>"}}
	w2 := authedPOSTForm(t, a.Handler(), "/admin/templates/custom-tags/"+strconv.FormatInt(id, 10)+"/update", upd, cookies)
	if w2.Code != http.StatusFound {
		t.Fatalf("update status = %d; body:\n%s", w2.Code, w2.Body.String())
	}
	if loc := w2.Header().Get("Location"); !strings.Contains(loc, "ok=updated") {
		t.Errorf("expected ok=updated, got %q", loc)
	}

	// Delete.
	w3 := authedPOSTForm(t, a.Handler(), "/admin/templates/custom-tags/"+strconv.FormatInt(id, 10)+"/delete", url.Values{}, cookies)
	if w3.Code != http.StatusFound {
		t.Fatalf("delete status = %d", w3.Code)
	}
	if loc := w3.Header().Get("Location"); !strings.Contains(loc, "ok=deleted") {
		t.Errorf("expected ok=deleted, got %q", loc)
	}

	// Row is gone from the DB.
	var remaining int
	if err := a.DB.QueryRow(`SELECT COUNT(*) FROM site_custom_tags WHERE wid = 1 AND name = 'custom_hello'`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Errorf("expected 0 rows after delete, got %d", remaining)
	}
}

// TestAdminCustomTagsValidation covers each rejection branch a
// well-meaning user might trip into. All cases redirect with err=
// in the query string; the list page reads the param and surfaces
// a localised message.
func TestAdminCustomTagsValidation(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	cases := []struct {
		name string
		form url.Values
	}{
		// "custom_" prefix is auto-prepended; name must still match the
		// remaining-chars regex. Uppercase + dash break it.
		{"invalid name (uppercase)", url.Values{"name": {"custom_BadName"}, "value": {"v"}}},
		{"invalid name (dash)", url.Values{"name": {"custom_bad-name"}, "value": {"v"}}},
		// Over-long value (> 64 KB).
		{"value too long", url.Values{"name": {"custom_long"}, "value": {strings.Repeat("a", 65536+1)}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := authedPOSTForm(t, a.Handler(), "/admin/templates/custom-tags", tc.form, cookies)
			if w.Code != http.StatusFound {
				t.Fatalf("status = %d, want 302", w.Code)
			}
			loc := w.Header().Get("Location")
			if !strings.Contains(loc, "err=") {
				t.Errorf("expected err= in Location, got %q", loc)
			}
		})
	}
}

// TestAdminCustomTagsDuplicateBlocked confirms a second row with the
// same name fails the unique constraint and surfaces a duplicate
// error in the redirect.
func TestAdminCustomTagsDuplicateBlocked(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	form := url.Values{"name": {"custom_dup"}, "value": {"a"}}
	w := authedPOSTForm(t, a.Handler(), "/admin/templates/custom-tags", form, cookies)
	if w.Code != http.StatusFound {
		t.Fatalf("first create status = %d", w.Code)
	}
	w2 := authedPOSTForm(t, a.Handler(), "/admin/templates/custom-tags", form, cookies)
	if w2.Code != http.StatusFound {
		t.Fatalf("second create status = %d", w2.Code)
	}
	if loc := w2.Header().Get("Location"); !strings.Contains(loc, "err=") {
		t.Errorf("duplicate did not redirect with err=, got %q", loc)
	}
}

// TestAdminCustomTagsLimitEnforced confirms the 50-row cap. The DB
// is pre-loaded straight via SQL; the 51st request goes through the
// handler and must redirect with err=.
func TestAdminCustomTagsLimitEnforced(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	for i := 0; i < 50; i++ {
		if _, err := a.DB.Exec(`INSERT INTO site_custom_tags (wid, name, value) VALUES (1, ?, 'v')`,
			"custom_seed_"+strconv.Itoa(i)); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}
	form := url.Values{"name": {"custom_overflow"}, "value": {"v"}}
	w := authedPOSTForm(t, a.Handler(), "/admin/templates/custom-tags", form, cookies)
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d", w.Code)
	}
	if loc := w.Header().Get("Location"); !strings.Contains(loc, "err=") {
		t.Errorf("expected err= at limit, got %q", loc)
	}
	// Row count stayed at 50 — handler refused to insert the 51st.
	var n int
	if err := a.DB.QueryRow(`SELECT COUNT(*) FROM site_custom_tags WHERE wid = 1`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 50 {
		t.Errorf("row count = %d, want 50", n)
	}
}

// TestAdminCustomTagsUpdateMissingID confirms updating a non-existent
// id returns 404 rather than silently no-oping.
func TestAdminCustomTagsUpdateMissingID(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	upd := url.Values{"name": {"custom_ghost"}, "value": {"v"}}
	w := authedPOSTForm(t, a.Handler(), "/admin/templates/custom-tags/9999/update", upd, cookies)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// TestAdminCustomTagsDeleteMissingID confirms deleting a non-existent
// id returns 404.
func TestAdminCustomTagsDeleteMissingID(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	w := authedPOSTForm(t, a.Handler(), "/admin/templates/custom-tags/9999/delete", url.Values{}, cookies)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}
