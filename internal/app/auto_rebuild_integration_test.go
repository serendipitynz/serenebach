package app_test

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAdminSettingsTogglePersistsAutoRebuild covers round-tripping the
// new "記事公開時の静的生成" radio through the basic settings form.
func TestAdminSettingsTogglePersistsAutoRebuild(t *testing.T) {
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	w := authedGET(t, a.Handler(), "/admin/settings/basic", cookies)
	if !strings.Contains(w.Body.String(), `name="auto_rebuild_on_publish"`) {
		t.Errorf("settings form missing auto_rebuild_on_publish radios")
	}

	form := url.Values{
		"title":                   {"T"},
		"description":             {"d"},
		"base_url":                {"https://example.com/"},
		"lang":                    {"ja"},
		"llms_enabled":            {"0"},
		"auto_rebuild_on_publish": {"1"},
	}
	res := authedPOSTForm(t, a.Handler(), "/admin/settings/basic", form, cookies)
	if res.Code != http.StatusFound {
		t.Fatalf("save status = %d; body:\n%s", res.Code, res.Body.String())
	}
	var v int
	_ = a.DB.QueryRow(`SELECT auto_rebuild_on_publish FROM weblogs WHERE id = 1`).Scan(&v)
	if v != 1 {
		t.Errorf("auto_rebuild_on_publish = %d after save, want 1", v)
	}

	form.Set("auto_rebuild_on_publish", "0")
	res = authedPOSTForm(t, a.Handler(), "/admin/settings/basic", form, cookies)
	if res.Code != http.StatusFound {
		t.Fatalf("save-off status = %d", res.Code)
	}
	_ = a.DB.QueryRow(`SELECT auto_rebuild_on_publish FROM weblogs WHERE id = 1`).Scan(&v)
	if v != 0 {
		t.Errorf("auto_rebuild_on_publish = %d after toggle-off, want 0", v)
	}
}

// TestAutoRebuildRunsOnEntryCreateWhenEnabled verifies the wire-up:
// flipping the toggle on and creating an entry should leave a
// freshly-rendered index.html in SB_REBUILD_OUT.
func TestAutoRebuildRunsOnEntryCreateWhenEnabled(t *testing.T) {
	out := filepath.Join(t.TempDir(), "public")
	t.Setenv("SB_REBUILD_OUT", out)

	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	// Enable the toggle.
	if _, err := a.DB.Exec(`UPDATE weblogs SET auto_rebuild_on_publish = 1 WHERE id = 1`); err != nil {
		t.Fatalf("enable toggle: %v", err)
	}

	// Confirm the output directory is empty before the save fires the
	// rebuild — otherwise a leftover file from setup would mask a
	// no-op handler.
	if _, err := os.Stat(filepath.Join(out, "index.html")); err == nil {
		t.Fatalf("output dir not empty before save")
	}

	form := url.Values{
		"title":       {"auto rebuild trigger"},
		"body":        {"<p>fired</p>"},
		"more":        {""},
		"category_id": {"-1"},
		"status":      {"1"},
		"posted_at":   {"2026-04-29T12:00"},
	}
	w := authedPOSTForm(t, a.Handler(), "/admin/entries/new", form, cookies)
	if w.Code != http.StatusFound {
		t.Fatalf("create status = %d; body:\n%s", w.Code, w.Body.String())
	}

	// The save handler should have triggered a synchronous rebuild.
	if _, err := os.Stat(filepath.Join(out, "index.html")); err != nil {
		t.Errorf("expected index.html in %s after entry create: %v", out, err)
	}
}

// TestAutoRebuildSkippedWhenDisabled is the negative case — without
// the toggle, an entry create must not produce static output even
// when SB_REBUILD_OUT is configured.
func TestAutoRebuildSkippedWhenDisabled(t *testing.T) {
	out := filepath.Join(t.TempDir(), "public")
	t.Setenv("SB_REBUILD_OUT", out)

	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	form := url.Values{
		"title":       {"no rebuild"},
		"body":        {"<p>x</p>"},
		"more":        {""},
		"category_id": {"-1"},
		"status":      {"1"},
		"posted_at":   {"2026-04-29T12:00"},
	}
	w := authedPOSTForm(t, a.Handler(), "/admin/entries/new", form, cookies)
	if w.Code != http.StatusFound {
		t.Fatalf("create status = %d; body:\n%s", w.Code, w.Body.String())
	}

	if _, err := os.Stat(filepath.Join(out, "index.html")); err == nil {
		t.Errorf("index.html appeared in %s but auto-rebuild was disabled", out)
	}
}
