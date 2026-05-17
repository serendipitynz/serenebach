package app_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/serendipitynz/serenebach/internal/auth"
)

// TestAdminTemplatesDesignBlocksRegularUser walks the public surface of
// the "デザイン" group (URL: /admin/templates/*) and confirms the
// requireDesign middleware rejects role=3 (regular) sessions. Each
// entry point is a separate subtest so a regression on a single
// route is easy to localise.
func TestAdminTemplatesDesignBlocksRegularUser(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	hash, _ := auth.HashPassword("secret")
	if _, err := a.DB.Exec(`INSERT INTO users (wid, name, display_name, email, password_hash, role, created_at, updated_at, list_visible, description_format)
		VALUES (1, 'regular', 'Reg', '', ?, 3, strftime('%s','now'), strftime('%s','now'), 1, 'html')`, hash); err != nil {
		t.Fatal(err)
	}
	cookies := login(t, a.Handler(), "regular", "secret")

	gets := []string{
		"/admin/templates",
		"/admin/templates/settings",
		"/admin/templates/og",
		"/admin/templates/custom-tags",
		"/admin/templates/active/edit",
		"/admin/templates/1/edit",
	}
	for _, p := range gets {
		t.Run("GET "+p, func(t *testing.T) {
			w := authedGET(t, a.Handler(), p, cookies)
			if w.Code != http.StatusForbidden {
				t.Errorf("status = %d, want 403", w.Code)
			}
		})
	}

	posts := []string{
		"/admin/templates/settings",
		"/admin/templates/og",
		"/admin/templates/custom-tags",
		"/admin/templates/1/edit",
		"/admin/templates/1/activate",
		"/admin/templates/1/delete",
	}
	for _, p := range posts {
		t.Run("POST "+p, func(t *testing.T) {
			w := authedPOSTForm(t, a.Handler(), p, url.Values{}, cookies)
			if w.Code != http.StatusForbidden {
				t.Errorf("status = %d, want 403", w.Code)
			}
		})
	}
}

// TestAdminTemplatesDesignAllowsAdmin sanity-checks the same routes
// open up for an admin session. Asserting the index page returns 200
// and labels the URL as "デザイン" pins the surprising-but-
// intentional URL/label mismatch the project documents.
func TestAdminTemplatesDesignAllowsAdmin(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	w := authedGET(t, a.Handler(), "/admin/templates", cookies)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	// "デザイン" is the user-facing label even though the URL
	// keeps the historical /templates path. Catching a rename here
	// alerts us to update CLAUDE.md / docs together.
	if !strings.Contains(body, "デザイン") {
		t.Errorf("expected デザイン label on /admin/templates; body did not contain it")
	}
}
