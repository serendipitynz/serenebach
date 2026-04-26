package app_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/serendipitynz/serenebach/internal/app"
)

// countMessages returns the total comment count so a silent-drop test
// can assert "nothing was persisted".
func countMessages(t *testing.T, a *app.App) int64 {
	t.Helper()
	var n int64
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&n)
	return n
}

func TestCommentBlacklistExactIPSilentlyDropped(t *testing.T) {
	a := newTestApp(t)
	// Block the submitComment helper's hard-coded RemoteAddr (127.0.0.1).
	if _, err := a.DB.Exec(`UPDATE weblogs SET ip_blacklist = '127.0.0.1' WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	before := countMessages(t, a)

	w := submitComment(t, a.Handler(), 1, url.Values{
		"name":        {"banned"},
		"description": {"should never persist"},
	})
	// Silent drop: same UX as honeypot trip — 303 redirect with no err.
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303 (silent drop)", w.Code)
	}

	if after := countMessages(t, a); after != before {
		t.Errorf("message count changed from %d to %d — blocked IP should not persist", before, after)
	}
}

func TestCommentBlacklistCIDRSilentlyDropped(t *testing.T) {
	a := newTestApp(t)
	// /24 covers 127.0.0.x.
	if _, err := a.DB.Exec(`UPDATE weblogs SET ip_blacklist = '127.0.0.0/24' WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	before := countMessages(t, a)

	w := submitComment(t, a.Handler(), 1, url.Values{
		"name":        {"banned-by-cidr"},
		"description": {"should never persist"},
	})
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", w.Code)
	}
	if countMessages(t, a) != before {
		t.Errorf("CIDR-blocked comment should not persist")
	}
}

func TestCommentBlacklistOutOfRangeAllowed(t *testing.T) {
	a := newTestApp(t)
	// Block a different range so the test IP (127.0.0.1) slips through.
	if _, err := a.DB.Exec(`UPDATE weblogs SET ip_blacklist = '198.51.100.0/24' WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	before := countMessages(t, a)

	w := submitComment(t, a.Handler(), 1, url.Values{
		"name":        {"not-blocked"},
		"description": {"comment should persist"},
	})
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", w.Code)
	}
	if countMessages(t, a) != before+1 {
		t.Errorf("non-matching IP should have created a message row (before=%d)", before)
	}
}

func TestAdminCommentSettingsShowsAndSavesIPBlacklist(t *testing.T) {
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	// Form renders with the new textarea.
	w := authedGET(t, a.Handler(), "/admin/comments/settings", cookies)
	if w.Code != 200 {
		t.Fatalf("form status = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `name="ip_blacklist"`) {
		t.Errorf("ip_blacklist textarea missing from comment-settings form")
	}

	// Save populates the column.
	form := url.Values{
		"comment_mode": {"moderated"},
		"spam_words":   {""},
		"ip_blacklist": {"198.51.100.5\n# note\n198.51.100.0/24"},
	}
	save := authedPOSTForm(t, a.Handler(), "/admin/comments/settings", form, cookies)
	if save.Code != http.StatusFound {
		t.Fatalf("save status = %d, body:\n%s", save.Code, save.Body.String())
	}
	var stored string
	_ = a.DB.QueryRow(`SELECT ip_blacklist FROM weblogs WHERE id = 1`).Scan(&stored)
	if !strings.Contains(stored, "198.51.100.5") || !strings.Contains(stored, "198.51.100.0/24") {
		t.Errorf("persisted ip_blacklist missing entries; got %q", stored)
	}
}
