package app_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestAdminTemplateSettingsFormRenders(t *testing.T) {
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	w := authedGET(t, a.Handler(), "/admin/templates/settings", cookies)
	if w.Code != 200 {
		t.Fatalf("status = %d; body:\n%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{
		`name="archive_template_id"`,
		`name="profile_template_id"`,
		"デザイン設定",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("settings page missing %q", want)
		}
	}
	// Each of the three tab links should be present on any tab page.
	for _, href := range []string{
		`href="/admin/templates"`,
		`href="/admin/templates/settings"`,
		`href="/admin/templates/import"`,
	} {
		if !strings.Contains(body, href) {
			t.Errorf("tab link missing: %s", href)
		}
	}
}

func TestAdminTemplateSettingsPersists(t *testing.T) {
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	// Add a second template so we have a non-active id to point at.
	body := "<html><body>archive:{site_title}\n<!-- BEGIN entry -->\n{entry_title}\n<!-- END entry -->\n</body></html>"
	res, err := a.DB.Exec(`
		INSERT INTO templates (wid, name, is_active, main_body, entry_body, css, info, sort_order, created_at, updated_at)
		VALUES (1, 'archive-tmpl', 0, ?, '', '', '', 0, 0, 0)`, body)
	if err != nil {
		t.Fatal(err)
	}
	archiveID, _ := res.LastInsertId()

	form := url.Values{
		"archive_template_id": {itoa64(archiveID)},
		"profile_template_id": {"0"},
	}
	w := authedPOSTForm(t, a.Handler(), "/admin/templates/settings", form, cookies)
	if w.Code != http.StatusFound {
		t.Fatalf("settings save status = %d; body:\n%s", w.Code, w.Body.String())
	}

	var gotArchive, gotProfile int64
	_ = a.DB.QueryRow(`SELECT archive_template_id, profile_template_id FROM weblogs WHERE id = 1`).Scan(&gotArchive, &gotProfile)
	if gotArchive != archiveID {
		t.Errorf("archive_template_id = %d, want %d", gotArchive, archiveID)
	}
	if gotProfile != 0 {
		t.Errorf("profile_template_id = %d, want 0", gotProfile)
	}
}

// TestAdminTemplateSettingsArchivePinAffectsCategoryPage confirms that
// pinning an archive template actually changes what gets rendered on
// /category/{id}/ (one of the archive-family routes).
func TestAdminTemplateSettingsArchivePinAffectsCategoryPage(t *testing.T) {
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	// Create a distinctive archive template so we can detect it in output.
	body := "<html><body>ARCHIVE-MARKER {site_title}\n<!-- BEGIN entry -->\n{entry_title}\n<!-- END entry -->\n</body></html>"
	res, err := a.DB.Exec(`
		INSERT INTO templates (wid, name, is_active, main_body, entry_body, css, info, sort_order, created_at, updated_at)
		VALUES (1, 'archive-distinct', 0, ?, '', '', '', 0, 0, 0)`, body)
	if err != nil {
		t.Fatal(err)
	}
	archiveID, _ := res.LastInsertId()

	// Find an existing seeded category for the GET target.
	var catID int64
	if err := a.DB.QueryRow(`SELECT id FROM categories LIMIT 1`).Scan(&catID); err != nil {
		t.Fatal(err)
	}

	// Before the pin: category page renders with the active template
	// (no ARCHIVE-MARKER substring).
	before := authedGET(t, a.Handler(), "/category/"+itoa64(catID)+"/", cookies).Body.String()
	if strings.Contains(before, "ARCHIVE-MARKER") {
		t.Fatalf("ARCHIVE-MARKER leaked into category page before pin")
	}

	// Apply the pin.
	form := url.Values{
		"archive_template_id": {itoa64(archiveID)},
		"profile_template_id": {"0"},
	}
	if w := authedPOSTForm(t, a.Handler(), "/admin/templates/settings", form, cookies); w.Code != http.StatusFound {
		t.Fatalf("pin save status = %d", w.Code)
	}

	after := authedGET(t, a.Handler(), "/category/"+itoa64(catID)+"/", cookies).Body.String()
	if !strings.Contains(after, "ARCHIVE-MARKER") {
		t.Errorf("category page should use the pinned archive template after settings save; body:\n%s", after)
	}

	// Home page should stay on the active template (no marker there).
	home := authedGET(t, a.Handler(), "/", cookies).Body.String()
	if strings.Contains(home, "ARCHIVE-MARKER") {
		t.Errorf("home should keep using the active template, not the archive pin")
	}
}

func TestAdminTemplateSettingsRejectsNegativeID(t *testing.T) {
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	form := url.Values{
		"archive_template_id": {"-5"},
		"profile_template_id": {"0"},
	}
	w := authedPOSTForm(t, a.Handler(), "/admin/templates/settings", form, cookies)
	if w.Code != 200 {
		t.Fatalf("negative id status = %d, want 200 (stay on form); body:\n%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "アーカイブ用テンプレートの値が不正です") {
		t.Errorf("missing validation message")
	}
}
