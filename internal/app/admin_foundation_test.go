package app_test

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAdminLayoutIncludesThemeAndVersion verifies that every rendered
// admin page has the theme pre-init script, the logo swap pair, and
// the footer build stamp so the user can tell at a glance which
// version they're on.
func TestAdminLayoutIncludesThemeAndVersion(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	w := authedGET(t, a.Handler(), "/admin/", cookies)
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{
		`data-theme="auto"`,
		`sb_admin_appearance`,
		`class="brand-logo brand-logo-light"`,
		`class="brand-logo brand-logo-dark"`,
		`build-stamp`,
		`v4.0`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("layout missing %q", want)
		}
	}
}

func TestAdminStaticLogosServe(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	for _, path := range []string{
		"/admin/static/sb_logo_dark.svg",
		"/admin/static/sb_logo_light.svg",
		"/admin/static/sb_logo_gray.svg",
		"/admin/static/favicon.png",
	} {
		req := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, req)
		if w.Code != 200 {
			t.Errorf("GET %s = %d", path, w.Code)
		}
	}
}

func TestAdminSettingsIncludesAppearanceUI(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	// /admin/settings 302s admins to /settings/basic; the appearance
	// UI lives on the personal 画面設定 tab at /settings/screen.
	w := authedGET(t, a.Handler(), "/admin/settings/screen", cookies)
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{
		`data-appearance-select`,
		`data-language-select`,
		"System (OS の設定に従う)",
		"日本語",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("settings page missing %q", want)
		}
	}
}
