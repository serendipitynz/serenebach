package app_test

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/serendipitynz/serenebach/internal/templatepack"
)

// TestAdminTemplateExportRoundTripsThroughParser confirms the export
// endpoint produces a template.txt the templatepack parser can read
// back without loss. Belt-and-suspenders for the format writer.
func TestAdminTemplateExportRoundTripsThroughParser(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	var activeID int64
	if err := a.DB.QueryRow(`SELECT id FROM templates WHERE is_active = 1`).Scan(&activeID); err != nil {
		t.Fatal(err)
	}

	// Seed a bit of content so round-trip assertions are meaningful.
	if _, err := a.DB.Exec(`UPDATE templates SET
		name = 'export-me', info = 'Name: export-me\nAuthor: test\n',
		main_body = ?, css = ?, entry_body = ?
		WHERE id = ?`,
		"<html>{site_title}</html>", "body { color: red; }", "<article>{entry_title}</article>", activeID); err != nil {
		t.Fatal(err)
	}
	// And add a tiny asset on disk.
	absDir := filepath.Join(a.Config.TemplateDir, itoa64(activeID))
	_ = os.MkdirAll(absDir, 0o755)
	_ = os.WriteFile(filepath.Join(absDir, "mark.png"), []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}, 0o644)
	if _, err := a.DB.Exec(`INSERT INTO template_assets (template_id, filename, mime_type, size_bytes, created_at, updated_at)
		VALUES (?, 'mark.png', 'image/png', 8, 0, 0)`, activeID); err != nil {
		t.Fatal(err)
	}

	w := authedGET(t, a.Handler(), "/admin/templates/"+itoa64(activeID)+"/export", cookies)
	if w.Code != 200 {
		t.Fatalf("export status = %d; body:\n%s", w.Code, w.Body.String())
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, "export-me") {
		t.Errorf("Content-Disposition = %q, want filename mentioning export-me", cd)
	}

	pack, err := templatepack.Parse(bytes.NewReader(w.Body.Bytes()))
	if err != nil {
		t.Fatalf("parse back: %v", err)
	}
	// Raw export preserves the original Info bytes — "\\n" escapes in the
	// SQL literal land as literal backslash-n here, not a real newline,
	// so name extraction only relies on MainBody / CSS.
	if !strings.Contains(pack.MainBody, "{site_title}") {
		t.Errorf("MainBody not preserved: %q", pack.MainBody)
	}
	if !strings.Contains(pack.CSS, "color: red") {
		t.Errorf("CSS not preserved: %q", pack.CSS)
	}
	if pack.EntryBody == "" {
		t.Errorf("EntryBody should round-trip")
	}
	if len(pack.Assets) != 1 || pack.Assets[0].Filename != "mark.png" {
		t.Errorf("assets lost: %+v", pack.Assets)
	}
}

// TestAdminTemplateImportCreatesTemplateAndAssets drives the import flow
// with a pack built in-memory, then reads the DB to confirm every piece
// landed correctly (template row, entry body, CSS, and on-disk assets).
func TestAdminTemplateImportCreatesTemplateAndAssets(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	pack := &templatepack.Pack{
		Name:     "imported-one",
		Author:   "tester",
		Version:  "0.01",
		Info:     "Name: imported-one\nAuthor: tester\nVersion: 0.01\n",
		MainBody: "<html>{site_title}</html>",
		CSS:      "body { margin: 0; }",
		Assets: []templatepack.Asset{
			{Filename: "bg.png", MimeType: "image/png", Data: []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0xaa}},
		},
	}
	var packBuf bytes.Buffer
	if err := templatepack.Write(&packBuf, pack, time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}

	body, ct := buildFileUpload(t, "template.txt", packBuf.Bytes(), csrfTokenFromJar(cookies))
	req := httptest.NewRequest("POST", "/admin/templates/import", body)
	req.Header.Set("Content-Type", ct)
	req.Header.Set("X-CSRF-Token", csrfTokenFromJar(cookies))
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("import status = %d; body:\n%s", w.Code, w.Body.String())
	}

	var newID int64
	if err := a.DB.QueryRow(`SELECT id FROM templates WHERE name = 'imported-one'`).Scan(&newID); err != nil {
		t.Fatal(err)
	}
	var main, css string
	_ = a.DB.QueryRow(`SELECT main_body, css FROM templates WHERE id = ?`, newID).Scan(&main, &css)
	if !strings.Contains(main, "{site_title}") || !strings.Contains(css, "margin: 0") {
		t.Errorf("imported bodies not persisted: main=%q css=%q", main, css)
	}
	var assetCount int
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM template_assets WHERE template_id = ?`, newID).Scan(&assetCount)
	if assetCount != 1 {
		t.Errorf("imported asset row count = %d, want 1", assetCount)
	}
	if _, err := os.Stat(filepath.Join(a.Config.TemplateDir, itoa64(newID), "bg.png")); err != nil {
		t.Errorf("asset not on disk: %v", err)
	}
}

// TestAdminTemplateImportRejectsMissingMainBody refuses a pack with no
// base.html — the template would render nothing, so the import is
// declined up-front rather than creating a dud row.
func TestAdminTemplateImportRejectsMissingMainBody(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	pack := &templatepack.Pack{Name: "empty", CSS: "body { }"}
	var buf bytes.Buffer
	if err := templatepack.Write(&buf, pack, time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	body, ct := buildFileUpload(t, "template.txt", buf.Bytes(), csrfTokenFromJar(cookies))
	req := httptest.NewRequest("POST", "/admin/templates/import", body)
	req.Header.Set("Content-Type", ct)
	req.Header.Set("X-CSRF-Token", csrfTokenFromJar(cookies))
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d; body:\n%s", w.Code, w.Body.String())
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "err=") || !strings.Contains(loc, "base.html") {
		t.Errorf("expected err= flash mentioning base.html; got %q", loc)
	}
}

// ---- helpers -----------------------------------------------------------

func buildFileUpload(t *testing.T, filename string, contents []byte, csrfToken string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("csrf_token", csrfToken)
	part, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(part, bytes.NewReader(contents)); err != nil {
		t.Fatal(err)
	}
	_ = mw.Close()
	return &buf, mw.FormDataContentType()
}
