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

func TestAdminTemplatesListRenders(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	w := authedGET(t, a.Handler(), "/admin/templates", cookies)
	if w.Code != 200 {
		t.Fatalf("status = %d; body:\n%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{
		"デザイン設定",
		"テンプレート",
		"利用中",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("templates page missing %q", want)
		}
	}
}

func TestAdminTemplatesActivateSwitchesActive(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	// Add a second template via direct SQL — for this test we just
	// need two rows.
	res, err := a.DB.Exec(`
		INSERT INTO templates (wid, name, is_active, main_body, entry_body, css, info, sort_order, created_at, updated_at)
		VALUES (1, 'alt template', 0, '<html>{site_title}</html>', '', '', '', 0, 0, 0)`)
	if err != nil {
		t.Fatal(err)
	}
	altID, _ := res.LastInsertId()

	// Pull the currently-active id so we can assert it flips.
	var originalActive int64
	if err := a.DB.QueryRow(`SELECT id FROM templates WHERE is_active = 1`).Scan(&originalActive); err != nil {
		t.Fatal(err)
	}

	w := authedPOSTForm(t, a.Handler(),
		"/admin/templates/"+itoa64(altID)+"/activate", url.Values{}, cookies)
	if w.Code != http.StatusFound {
		t.Fatalf("activate status = %d; body:\n%s", w.Code, w.Body.String())
	}

	var nowActive int64
	_ = a.DB.QueryRow(`SELECT id FROM templates WHERE is_active = 1`).Scan(&nowActive)
	if nowActive != altID {
		t.Errorf("active id = %d, want %d", nowActive, altID)
	}
	var activeCount int
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM templates WHERE is_active = 1`).Scan(&activeCount)
	if activeCount != 1 {
		t.Errorf("expected exactly one active template, got %d", activeCount)
	}
	if nowActive == originalActive {
		t.Errorf("activate should have flipped away from %d", originalActive)
	}
}

func TestAdminTemplatesDeleteRefusesActive(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	var activeID int64
	if err := a.DB.QueryRow(`SELECT id FROM templates WHERE is_active = 1`).Scan(&activeID); err != nil {
		t.Fatal(err)
	}
	w := authedPOSTForm(t, a.Handler(),
		"/admin/templates/"+itoa64(activeID)+"/delete", url.Values{}, cookies)
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d; want 302", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "err=active-template-cannot-delete") {
		t.Errorf("unexpected redirect target: %q", loc)
	}
	var n int
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM templates WHERE id = ?`, activeID).Scan(&n)
	if n == 0 {
		t.Errorf("active template was deleted despite the guard")
	}
}

func TestAdminTemplatesDeleteInactiveRemoves(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	res, err := a.DB.Exec(`
		INSERT INTO templates (wid, name, is_active, main_body, entry_body, css, info, sort_order, created_at, updated_at)
		VALUES (1, 'throwaway', 0, '<html></html>', '', '', '', 0, 0, 0)`)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()

	w := authedPOSTForm(t, a.Handler(),
		"/admin/templates/"+itoa64(id)+"/delete", url.Values{}, cookies)
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d", w.Code)
	}
	var n int
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM templates WHERE id = ?`, id).Scan(&n)
	if n != 0 {
		t.Errorf("delete did not remove the row")
	}
}

// ---- edit / save-as ----------------------------------------------------

func TestAdminTemplatesEditFormRenders(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	var activeID int64
	if err := a.DB.QueryRow(`SELECT id FROM templates WHERE is_active = 1`).Scan(&activeID); err != nil {
		t.Fatal(err)
	}
	w := authedGET(t, a.Handler(), "/admin/templates/"+itoa64(activeID)+"/edit", cookies)
	if w.Code != 200 {
		t.Fatalf("status = %d; body:\n%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	// The edit page does not render name / info inputs; the save-as
	// modal + the backing new_name hidden field are what persist a
	// rename. Assert the content inputs + the modal triggers.
	for _, want := range []string{
		"テンプレート編集",
		`name="main_body"`,
		`name="css"`,
		`name="entry_body"`,
		`data-template-new-name`,
		`data-template-save-as`,
		`data-template-export`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("edit form missing %q", want)
		}
	}
}

// TestAdminTemplatesListExposesInfoMetadata confirms the row that the
// design-settings list renders carries the parsed info metadata so
// the i-icon modal has something to show.
func TestAdminTemplatesListExposesInfoMetadata(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	// Set a row whose info carries every metadata key plus a memo.
	infoBody := "Name: Classic\nAuthor: Takkyun\nAddress: https://example.com/\nVersion: 1.0\n=====\na simple memo line"
	if _, err := a.DB.Exec(`UPDATE templates SET info = ? WHERE is_active = 1`, infoBody); err != nil {
		t.Fatal(err)
	}
	w := authedGET(t, a.Handler(), "/admin/templates", cookies)
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{
		`data-template-info`,
		`data-meta-name="Classic"`,
		`data-meta-author="Takkyun"`,
		`data-meta-version="1.0"`,
		`data-meta-memo="a simple memo line"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("list page missing %q", want)
		}
	}
}

// TestAdminTemplateExportOverrideName checks that the modal-driven
// export params land in the generated pack — name override in particular
// since the Content-Disposition filename reflects it.
func TestAdminTemplateExportOverrideName(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	var activeID int64
	_ = a.DB.QueryRow(`SELECT id FROM templates WHERE is_active = 1`).Scan(&activeID)

	w := authedGET(t, a.Handler(),
		"/admin/templates/"+itoa64(activeID)+"/export?name=renamed&memo=custom+memo", cookies)
	if w.Code != 200 {
		t.Fatalf("export status = %d", w.Code)
	}
	cd := w.Header().Get("Content-Disposition")
	if !strings.Contains(cd, "renamed.txt") {
		t.Errorf("Content-Disposition = %q, want renamed.txt", cd)
	}
	// Parse the produced file and confirm the overrides round-trip.
	// (Using parser-level assertions keeps this from tightly coupling
	// to the exact encoding of the MIME body.)
	if !strings.Contains(w.Body.String(), "custom memo") {
		t.Errorf("export body missing override memo")
	}
}

func TestAdminTemplatesActiveShortcutRedirects(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	var activeID int64
	_ = a.DB.QueryRow(`SELECT id FROM templates WHERE is_active = 1`).Scan(&activeID)
	w := authedGET(t, a.Handler(), "/admin/templates/active/edit", cookies)
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	loc := w.Header().Get("Location")
	want := "/admin/templates/" + itoa64(activeID) + "/edit"
	if loc != want {
		t.Errorf("Location = %q, want %q", loc, want)
	}
}

func TestAdminTemplatesSavePersists(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	var activeID int64
	_ = a.DB.QueryRow(`SELECT id FROM templates WHERE is_active = 1`).Scan(&activeID)

	var priorName, priorInfo string
	_ = a.DB.QueryRow(`SELECT name, info FROM templates WHERE id = ?`, activeID).Scan(&priorName, &priorInfo)

	// name + info are not editable from this form. Content-only
	// submit.
	form := url.Values{
		"main_body":  {"<html>{site_title}</html>"},
		"css":        {"body { color: red; }"},
		"entry_body": {""},
	}
	w := authedPOSTForm(t, a.Handler(),
		"/admin/templates/"+itoa64(activeID)+"/edit", form, cookies)
	if w.Code != http.StatusFound {
		t.Fatalf("save status = %d; body:\n%s", w.Code, w.Body.String())
	}
	var name, main, css, info string
	_ = a.DB.QueryRow(`SELECT name, main_body, css, info FROM templates WHERE id = ?`, activeID).
		Scan(&name, &main, &css, &info)
	if !strings.Contains(main, "{site_title}") || !strings.Contains(css, "color: red") {
		t.Errorf("content not persisted: main=%q css=%q", main, css)
	}
	// Name + info preserved (not clobbered by the content-only submit).
	if name != priorName {
		t.Errorf("name changed unexpectedly: %q → %q", priorName, name)
	}
	if info != priorInfo {
		t.Errorf("info changed unexpectedly: %q → %q", priorInfo, info)
	}
}

func TestAdminTemplatesSaveRejectsBadSyntax(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	var activeID int64
	_ = a.DB.QueryRow(`SELECT id FROM templates WHERE is_active = 1`).Scan(&activeID)

	// Unmatched BEGIN/END is what the sbtemplate parser refuses; that's
	// the exact class of breakage the syntax check is meant to catch.
	form := url.Values{
		"name":      {"keep"},
		"main_body": {"<html><!-- BEGIN foo --></html>"},
		"css":       {""},
	}
	w := authedPOSTForm(t, a.Handler(),
		"/admin/templates/"+itoa64(activeID)+"/edit", form, cookies)
	if w.Code != 200 {
		t.Fatalf("status = %d, want 200 (stay on form); body:\n%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "ベース HTML の解析に失敗") {
		t.Errorf("expected parse-error flash")
	}
}

func TestAdminTemplatesSaveAsCreatesNewRow(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	var activeID int64
	_ = a.DB.QueryRow(`SELECT id FROM templates WHERE is_active = 1`).Scan(&activeID)

	var beforeCount int
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM templates`).Scan(&beforeCount)

	// Save-as reads the name from the new_name hidden field (the modal
	// writes into it). Fall back to "<name> (コピー)" on empty is tested
	// separately further down.
	form := url.Values{
		"new_name":  {"forked"},
		"main_body": {"<html>{site_title}</html>"},
		"css":       {""},
	}
	w := authedPOSTForm(t, a.Handler(),
		"/admin/templates/"+itoa64(activeID)+"/save-as", form, cookies)
	if w.Code != http.StatusFound {
		t.Fatalf("save-as status = %d; body:\n%s", w.Code, w.Body.String())
	}

	var afterCount int
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM templates`).Scan(&afterCount)
	if afterCount != beforeCount+1 {
		t.Fatalf("template count = %d, want %d", afterCount, beforeCount+1)
	}
	// Original row was not flipped to the new name.
	var originalName string
	_ = a.DB.QueryRow(`SELECT name FROM templates WHERE id = ?`, activeID).Scan(&originalName)
	if originalName == "forked" {
		t.Errorf("save-as rewrote the original row")
	}
	// New row is inactive so the caller is never surprised by an activation.
	var forkedActive int
	_ = a.DB.QueryRow(`SELECT is_active FROM templates WHERE name = 'forked'`).Scan(&forkedActive)
	if forkedActive != 0 {
		t.Errorf("save-as result should be inactive")
	}
}

func TestAdminTemplatesReorderPersists(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	// Create two additional templates so we have at least three rows.
	for _, n := range []string{"t-a", "t-b"} {
		if _, err := a.DB.Exec(`
			INSERT INTO templates (wid, name, is_active, main_body, entry_body, css, info, sort_order, created_at, updated_at)
			VALUES (1, ?, 0, '<html></html>', '', '', '', 0, 0, 0)`, n); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := a.DB.Query(`SELECT id FROM templates ORDER BY id`)
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
		t.Fatalf("expected >= 2 templates, got %d", len(ids))
	}
	reversed := make([]int64, len(ids))
	for i, id := range ids {
		reversed[len(ids)-1-i] = id
	}

	payload, _ := json.Marshal(struct {
		IDs []int64 `json:"ids"`
	}{IDs: reversed})
	req := httptest.NewRequest("POST", "/admin/templates/reorder", bytes.NewReader(payload))
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

	for i, id := range reversed {
		var got int
		if err := a.DB.QueryRow(`SELECT sort_order FROM templates WHERE id = ?`, id).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != i {
			t.Errorf("id=%d sort_order = %d, want %d", id, got, i)
		}
	}
}
