package app_test

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/serendipitynz/serenebach/internal/auth"
)

func TestAdminWebhooksRequiresLogin(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)

	w := authedGET(t, a.Handler(), "/admin/settings/webhooks", nil)
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	if loc := w.Header().Get("Location"); !strings.Contains(loc, "/admin/login") {
		t.Errorf("Location = %q, want prefix /admin/login", loc)
	}
}

// TestAdminWebhooksRejectsRegularUser confirms requireDesign blocks
// role=3 (regular) sessions even when they guess the URL directly.
func TestAdminWebhooksRejectsRegularUser(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	hash, _ := auth.HashPassword("secret")
	if _, err := a.DB.Exec(`INSERT INTO users (wid, name, display_name, email, password_hash, role, created_at, updated_at, list_visible, description_format)
		VALUES (1, 'regular', 'Reg', '', ?, 3, strftime('%s','now'), strftime('%s','now'), 1, 'html')`, hash); err != nil {
		t.Fatal(err)
	}
	cookies := login(t, a.Handler(), "regular", "secret")

	w := authedGET(t, a.Handler(), "/admin/settings/webhooks", cookies)
	if w.Code != http.StatusForbidden {
		t.Errorf("GET list status = %d, want 403", w.Code)
	}
}

// TestAdminWebhooksCreateRoundtrip exercises the happy path:
// create → list shows the row → toggle flips active → delete removes it.
func TestAdminWebhooksCreateRoundtrip(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	form := url.Values{
		"url":                    {"https://hooks.example.com/sb"},
		"secret":                 {"shh"},
		"event_entry.published":  {"1"},
		"event_comment.received": {"1"},
		"active":                 {"1"},
	}
	w := authedPOSTForm(t, a.Handler(), "/admin/settings/webhooks", form, cookies)
	if w.Code != http.StatusFound {
		t.Fatalf("create status = %d; body:\n%s", w.Code, w.Body.String())
	}
	if loc := w.Header().Get("Location"); !strings.Contains(loc, "?ok=created") {
		t.Errorf("Location = %q, want ?ok=created", loc)
	}

	listResp := authedGET(t, a.Handler(), "/admin/settings/webhooks", cookies)
	if listResp.Code != http.StatusOK {
		t.Fatalf("list status = %d", listResp.Code)
	}
	body := listResp.Body.String()
	if !strings.Contains(body, "hooks.example.com/sb") {
		t.Errorf("list HTML missing URL; body:\n%s", body)
	}

	// Find the new row's id from the DB (deterministic — only row).
	var id int64
	if err := a.DB.QueryRow(`SELECT id FROM webhooks WHERE wid = 1`).Scan(&id); err != nil {
		t.Fatalf("SELECT id: %v", err)
	}

	// Toggle: active → inactive.
	tog := authedPOSTForm(t, a.Handler(), "/admin/settings/webhooks/"+strconv.FormatInt(id, 10)+"/toggle", url.Values{}, cookies)
	if tog.Code != http.StatusSeeOther {
		t.Errorf("toggle status = %d, want 303", tog.Code)
	}
	var activeInt int64
	if err := a.DB.QueryRow(`SELECT active FROM webhooks WHERE id = ?`, id).Scan(&activeInt); err != nil {
		t.Fatalf("SELECT active: %v", err)
	}
	if activeInt != 0 {
		t.Errorf("active = %d after toggle, want 0", activeInt)
	}

	// Delete.
	del := authedPOSTForm(t, a.Handler(), "/admin/settings/webhooks/"+strconv.FormatInt(id, 10)+"/delete", url.Values{}, cookies)
	if del.Code != http.StatusFound {
		t.Errorf("delete status = %d, want 302", del.Code)
	}
	var n int
	if err := a.DB.QueryRow(`SELECT COUNT(*) FROM webhooks WHERE id = ?`, id).Scan(&n); err != nil {
		t.Fatalf("SELECT count: %v", err)
	}
	if n != 0 {
		t.Errorf("expected webhook row to be gone, count = %d", n)
	}
}

// TestAdminWebhooksRejectsSSRFTargets ensures the URL validator wired
// into the form refuses private-network destinations on create.
func TestAdminWebhooksRejectsSSRFTargets(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	for _, bad := range []string{"http://127.0.0.1/", "http://localhost/", "http://10.0.0.1/", "ftp://example.com/"} {
		form := url.Values{
			"url":                   {bad},
			"event_entry.published": {"1"},
			"active":                {"1"},
		}
		w := authedPOSTForm(t, a.Handler(), "/admin/settings/webhooks", form, cookies)
		if w.Code != http.StatusOK { // re-renders the form with error
			t.Errorf("POST %q status = %d, want 200 (form re-render)", bad, w.Code)
			continue
		}
		if !strings.Contains(w.Body.String(), "許可されていないホスト") &&
			!strings.Contains(w.Body.String(), "not allowed") &&
			!strings.Contains(w.Body.String(), "形式が正しくない") &&
			!strings.Contains(w.Body.String(), "malformed") {
			t.Errorf("POST %q: expected validation error message in response, got body:\n%s", bad, w.Body.String())
		}
	}
	var n int
	if err := a.DB.QueryRow(`SELECT COUNT(*) FROM webhooks`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("no rows should have been created, got %d", n)
	}
}

// TestAdminWebhooksRequiresAtLeastOneEvent confirms the empty-events
// branch of the validator.
func TestAdminWebhooksRequiresAtLeastOneEvent(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	form := url.Values{"url": {"https://hooks.example.com/sb"}, "active": {"1"}}
	w := authedPOSTForm(t, a.Handler(), "/admin/settings/webhooks", form, cookies)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (form re-render)", w.Code)
	}
	var n int
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM webhooks`).Scan(&n)
	if n != 0 {
		t.Errorf("expected zero rows, got %d", n)
	}
}
