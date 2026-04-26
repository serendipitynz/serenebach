package app_test

import (
	"bytes"
	"encoding/json"
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

	"github.com/serendipitynz/serenebach/internal/csrf"
)

// pngBytes returns an in-memory PNG of the given size so the upload tests
// don't need a fixture on disk.
func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{10, 20, 30, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// buildMultipart composes a multipart/form-data body with the csrf_token
// field + a file part, returning the request body and its content-type.
func buildMultipart(t *testing.T, filename string, contents []byte, csrfToken string) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if csrfToken != "" {
		_ = mw.WriteField("csrf_token", csrfToken)
	}
	part, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(part, bytes.NewReader(contents)); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return &body, mw.FormDataContentType()
}

func postUpload(t *testing.T, h http.Handler, cookies []*http.Cookie, filename string, contents []byte, acceptJSON bool) *httptest.ResponseRecorder {
	t.Helper()
	_, token := csrfFromJar(cookies)
	body, ct := buildMultipart(t, filename, contents, token)
	req := httptest.NewRequest("POST", "/admin/images", body)
	req.Header.Set("Content-Type", ct)
	if acceptJSON {
		req.Header.Set("Accept", "application/json")
		req.Header.Set("X-CSRF-Token", token)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestImageUploadCreatesFilesAndRow(t *testing.T) {
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	w := postUpload(t, a.Handler(), cookies, "hello.png", pngBytes(t, 400, 300), true)
	if w.Code != http.StatusCreated {
		t.Fatalf("upload status = %d; body:\n%s", w.Code, w.Body.String())
	}

	var payload struct {
		ID       int64  `json:"id"`
		URL      string `json:"url"`
		ThumbURL string `json:"thumb_url"`
		Filename string `json:"filename"`
	}
	if err := json.NewDecoder(w.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.ID == 0 {
		t.Errorf("expected non-zero id")
	}
	if !strings.HasPrefix(payload.URL, "/img/") {
		t.Errorf("URL = %q, want /img/ prefix", payload.URL)
	}

	// DB row is there
	var count int
	if err := a.DB.QueryRow(`SELECT COUNT(*) FROM images`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("images row count = %d, want 1", count)
	}

	// File and thumbnail reachable via /img/*
	resp := authedGET(t, a.Handler(), payload.URL, cookies)
	if resp.Code != 200 {
		t.Errorf("GET %s = %d", payload.URL, resp.Code)
	}
	if payload.ThumbURL != "" {
		resp := authedGET(t, a.Handler(), payload.ThumbURL, cookies)
		if resp.Code != 200 {
			t.Errorf("GET %s = %d", payload.ThumbURL, resp.Code)
		}
	}
}

func TestImageUploadRequiresLogin(t *testing.T) {
	a := newTestApp(t)
	// Only a CSRF cookie, no session. The CSRF middleware still passes
	// (cookie + form token match) so the request reaches the protected
	// group where RequireUser should bounce it to login.
	csrfCookie, token := fetchCSRF(t, a.Handler())
	body, ct := buildMultipart(t, "x.png", pngBytes(t, 8, 8), token)
	req := httptest.NewRequest("POST", "/admin/images", body)
	req.Header.Set("Content-Type", ct)
	req.AddCookie(csrfCookie)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 redirect to login", w.Code)
	}
	if loc := w.Header().Get("Location"); !strings.HasPrefix(loc, "/admin/login") {
		t.Errorf("Location = %q, want /admin/login prefix", loc)
	}
}

func TestImageUploadRequiresCSRF(t *testing.T) {
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	// Deliberately empty token → CSRF middleware should 403.
	body, ct := buildMultipart(t, "x.png", pngBytes(t, 8, 8), "")
	req := httptest.NewRequest("POST", "/admin/images", body)
	req.Header.Set("Content-Type", ct)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("CSRF-less upload status = %d, want 403; body:\n%s", w.Code, w.Body.String())
	}
}

func TestImageUploadRejectsWrongMIME(t *testing.T) {
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	// Plain-text body with a `.png` name. DetectContentType will see
	// "text/plain; charset=utf-8" and the allowlist will refuse.
	w := postUpload(t, a.Handler(), cookies, "nope.png", []byte("hello world"), true)
	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415; body:\n%s", w.Code, w.Body.String())
	}
}

func TestImageUploadEnforcesMaxSize(t *testing.T) {
	a := newTestApp(t)
	// Shrink the cap for this test so we don't need to generate a 10MB body.
	for _, c := range []string{} {
		_ = c
	}
	// Can't easily mutate handler cap after app.New; use Content-Length path
	// by sending a big body. Generate a 2MB PNG and set the cap to 1MB
	// via a fresh app configured for this case.
	cookies := login(t, a.Handler(), "admin", "changeme")
	// 2000x2000 PNG ≈ well over a few MB. Confirm the default 10MB cap
	// works by fabricating a Content-Length larger than the server allows.
	body, ct := buildMultipart(t, "big.png", pngBytes(t, 100, 100), csrfTokenFromJar(cookies))
	req := httptest.NewRequest("POST", "/admin/images", body)
	req.Header.Set("Content-Type", ct)
	// Lie about Content-Length to trip the pre-flight check.
	req.ContentLength = (10 << 20) + 1
	req.Header.Set("X-CSRF-Token", csrfTokenFromJar(cookies))
	req.Header.Set("Accept", "application/json")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("over-limit status = %d, want 413; body:\n%s", w.Code, w.Body.String())
	}
}

func TestImageDeleteRemovesRowAndFiles(t *testing.T) {
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	w := postUpload(t, a.Handler(), cookies, "bye.png", pngBytes(t, 300, 200), true)
	if w.Code != http.StatusCreated {
		t.Fatalf("upload status = %d", w.Code)
	}
	var payload struct {
		ID  int64  `json:"id"`
		URL string `json:"url"`
	}
	_ = json.NewDecoder(w.Body).Decode(&payload)

	// Resolve to the on-disk path so we can assert it's unlinked.
	var storedPath string
	if err := a.DB.QueryRow(`SELECT stored_path FROM images WHERE id = ?`, payload.ID).Scan(&storedPath); err != nil {
		t.Fatal(err)
	}
	absOnDisk := filepath.Join(a.Config.ImageDir, filepath.FromSlash(storedPath))
	if _, err := os.Stat(absOnDisk); err != nil {
		t.Fatalf("stored file missing before delete: %v", err)
	}

	del := authedPOSTForm(t, a.Handler(), "/admin/images/"+itoa64(payload.ID)+"/delete", url.Values{}, cookies)
	if del.Code != http.StatusFound {
		t.Fatalf("delete status = %d, want 302", del.Code)
	}
	var count int
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM images WHERE id = ?`, payload.ID).Scan(&count)
	if count != 0 {
		t.Errorf("images row still present after delete")
	}
	if _, err := os.Stat(absOnDisk); !os.IsNotExist(err) {
		t.Errorf("stored file still present after delete")
	}
}

func TestImagesListJSONMatchesUploaded(t *testing.T) {
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	_ = postUpload(t, a.Handler(), cookies, "one.png", pngBytes(t, 100, 100), true)
	_ = postUpload(t, a.Handler(), cookies, "two.png", pngBytes(t, 200, 150), true)

	req := httptest.NewRequest("GET", "/admin/images?format=json", nil)
	req.Header.Set("Accept", "application/json")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("json list status = %d", w.Code)
	}
	var payload struct {
		Images []struct {
			ID       int64  `json:"id"`
			URL      string `json:"url"`
			ThumbURL string `json:"thumb_url"`
			Filename string `json:"filename"`
		} `json:"images"`
	}
	if err := json.NewDecoder(w.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Images) != 2 {
		t.Fatalf("images count = %d, want 2", len(payload.Images))
	}
}

func TestImagesPagePaginatesAndTogglesView(t *testing.T) {
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	// Upload more than one page worth of images. Keep the count modest
	// so the test is quick but crosses the page boundary (48 per page).
	for i := 0; i < 50; i++ {
		_ = postUpload(t, a.Handler(), cookies,
			"bulk-"+itoa64(int64(i))+".png", pngBytes(t, 16, 16), true)
	}

	// Default view: grid, page 1, shows page size's worth of tiles.
	w := authedGET(t, a.Handler(), "/admin/images", cookies)
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `data-image-filter`) {
		t.Errorf("gallery missing filter input")
	}
	if !strings.Contains(body, `class="view-btn active" aria-label="サムネイル表示"`) {
		t.Errorf("grid view-toggle should start active")
	}
	if !strings.Contains(body, "1 / 2") {
		t.Errorf("pager should show 1/2 for 50 images")
	}
	// Tile count on page 1 = 48 — a single reasonably-unique probe
	// is enough to assert we didn't revert to the unlimited list.
	tiles := strings.Count(body, "<li class=\"image-tile\"")
	if tiles != 48 {
		t.Errorf("tile count on page 1 = %d, want 48", tiles)
	}

	// List view swap.
	w2 := authedGET(t, a.Handler(), "/admin/images?view=list", cookies)
	if !strings.Contains(w2.Body.String(), `image-row-icon`) {
		t.Errorf("list view should render row icons")
	}

	// Page 2 wraps the remaining items.
	w3 := authedGET(t, a.Handler(), "/admin/images?page=2", cookies)
	page2Tiles := strings.Count(w3.Body.String(), "<li class=\"image-tile\"")
	if page2Tiles != 2 {
		t.Errorf("tile count on page 2 = %d, want 2 (50-48)", page2Tiles)
	}
}

func TestImagesPageLinksInSidebar(t *testing.T) {
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	w := authedGET(t, a.Handler(), "/admin/images", cookies)
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	// The wording of the drop zone / library heading moved to the
	// i18n catalogue — assert on the data-attr markers that don't
	// move instead of the copy itself.
	for _, want := range []string{
		`data-drop-zone`,
		`data-upload`,
		`data-image-filter`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("images page missing %q", want)
		}
	}
}

// itoa64 is a tiny helper used by this file so we don't need strconv.
func itoa64(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := false
	if v < 0 {
		neg = true
		v = -v
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func csrfTokenFromJar(cookies []*http.Cookie) string {
	for _, c := range cookies {
		if c.Name == csrf.CookieName {
			return c.Value
		}
	}
	return ""
}
