package app_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildAssetMultipart composes a multipart POST body for a template
// asset upload, mirroring what the drop-zone sends.
func buildAssetMultipart(t *testing.T, filename string, contents []byte, csrfToken string) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("csrf_token", csrfToken)
	part, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(part, bytes.NewReader(contents)); err != nil {
		t.Fatal(err)
	}
	_ = mw.Close()
	return &body, mw.FormDataContentType()
}

func postAssetUpload(t *testing.T, h http.Handler, cookies []*http.Cookie, tplID int64, filename string, contents []byte) *httptest.ResponseRecorder {
	t.Helper()
	token := csrfTokenFromJar(cookies)
	body, ct := buildAssetMultipart(t, filename, contents, token)
	req := httptest.NewRequest("POST", "/admin/templates/"+itoa64(tplID)+"/assets", body)
	req.Header.Set("Content-Type", ct)
	req.Header.Set("X-CSRF-Token", token)
	req.Header.Set("Accept", "application/json")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// tinyPNG is a minimal valid PNG built in-memory so tests don't rely on
// fixtures on disk.
func tinyPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestTemplateAssetUploadPersistsFileAndRow(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	var activeID int64
	if err := a.DB.QueryRow(`SELECT id FROM templates WHERE is_active = 1`).Scan(&activeID); err != nil {
		t.Fatal(err)
	}

	w := postAssetUpload(t, a.Handler(), cookies, activeID, "logo.png", tinyPNG(t))
	if w.Code != http.StatusCreated {
		t.Fatalf("upload status = %d; body:\n%s", w.Code, w.Body.String())
	}

	// Disk: under <TemplateDir>/<id>/logo.png
	absPath := filepath.Join(a.Config.TemplateDir, itoa64(activeID), "logo.png")
	if _, err := os.Stat(absPath); err != nil {
		t.Errorf("expected file on disk at %s: %v", absPath, err)
	}

	// DB row exists
	var n int
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM template_assets WHERE template_id = ?`, activeID).Scan(&n)
	if n != 1 {
		t.Errorf("template_assets count = %d, want 1", n)
	}

	// Public URL resolves
	resp := authedGET(t, a.Handler(), "/template/"+itoa64(activeID)+"/logo.png", cookies)
	if resp.Code != 200 {
		t.Errorf("public /template GET = %d", resp.Code)
	}
}

func TestTemplateAssetUploadRejectsBadMIME(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	var activeID int64
	_ = a.DB.QueryRow(`SELECT id FROM templates WHERE is_active = 1`).Scan(&activeID)

	// .exe binary-looking bytes — definitely not in the allowlist.
	w := postAssetUpload(t, a.Handler(), cookies, activeID, "trojan.exe", []byte("MZ\x90\x00\x03\x00\x00\x00"))
	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected 415, got %d; body:\n%s", w.Code, w.Body.String())
	}
}

func TestTemplateAssetReuploadReplaces(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	var activeID int64
	_ = a.DB.QueryRow(`SELECT id FROM templates WHERE is_active = 1`).Scan(&activeID)

	_ = postAssetUpload(t, a.Handler(), cookies, activeID, "logo.png", tinyPNG(t))

	// Re-upload with different bytes; the unique (template_id, filename)
	// index should drive an update rather than a second row.
	differentImg := image.NewRGBA(image.Rect(0, 0, 8, 8))
	var buf bytes.Buffer
	_ = png.Encode(&buf, differentImg)
	_ = postAssetUpload(t, a.Handler(), cookies, activeID, "logo.png", buf.Bytes())

	var n int
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM template_assets WHERE template_id = ?`, activeID).Scan(&n)
	if n != 1 {
		t.Errorf("row count after re-upload = %d, want 1 (upsert)", n)
	}
}

func TestTemplateAssetDeleteRemovesRowAndFile(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	var activeID int64
	_ = a.DB.QueryRow(`SELECT id FROM templates WHERE is_active = 1`).Scan(&activeID)

	_ = postAssetUpload(t, a.Handler(), cookies, activeID, "gone.png", tinyPNG(t))
	var assetID int64
	_ = a.DB.QueryRow(`SELECT id FROM template_assets WHERE template_id = ? AND filename = 'gone.png'`, activeID).Scan(&assetID)
	if assetID == 0 {
		t.Fatalf("asset row not found")
	}

	w := authedPOSTForm(t, a.Handler(),
		"/admin/templates/"+itoa64(activeID)+"/assets/"+itoa64(assetID)+"/delete",
		url.Values{}, cookies)
	if w.Code != http.StatusFound {
		t.Fatalf("delete status = %d", w.Code)
	}

	var n int
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM template_assets WHERE id = ?`, assetID).Scan(&n)
	if n != 0 {
		t.Errorf("row still present after delete")
	}
	absPath := filepath.Join(a.Config.TemplateDir, itoa64(activeID), "gone.png")
	if _, err := os.Stat(absPath); !os.IsNotExist(err) {
		t.Errorf("file still on disk after delete")
	}
}

func TestSiteParsesSitePartsTag(t *testing.T) {
	t.Parallel()
	// Updates the active template so {site_parts} appears in the rendered
	// home page. The tag should resolve to "/template/<active_id>/".
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	var activeID int64
	_ = a.DB.QueryRow(`SELECT id FROM templates WHERE is_active = 1`).Scan(&activeID)

	body := `<!doctype html><html><head><title>{site_title}</title></head>
<body><img src="{site_parts}logo.png" alt="">
<!-- BEGIN entry --><div>{entry_title}</div><!-- END entry -->
</body></html>`

	form := url.Values{
		"name":      {"with-parts"},
		"main_body": {body},
		"css":       {""},
	}
	w := authedPOSTForm(t, a.Handler(),
		"/admin/templates/"+itoa64(activeID)+"/edit", form, cookies)
	if w.Code != http.StatusFound {
		t.Fatalf("save status = %d; body:\n%s", w.Code, w.Body.String())
	}

	pub := authedGET(t, a.Handler(), "/", cookies).Body.String()
	want := `src="/template/` + itoa64(activeID) + `/logo.png"`
	if !strings.Contains(pub, want) {
		t.Errorf("expected %s in rendered home; got:\n%s", want, pub)
	}
}
