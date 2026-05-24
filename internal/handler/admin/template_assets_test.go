package admin

import (
	"bytes"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDetectTemplateAssetMIME_NewTypes(t *testing.T) {
	// Font magic bytes that http.DetectContentType recognises.
	woffMagic := []byte("wOFF\x00\x00\x00\x00")
	woff2Magic := []byte("wOF2\x00\x00\x00\x00")
	// Go 1.26+ detects these via signature; pad to at least 4 bytes.
	ttfMagic := []byte("\x00\x01\x00\x00")
	otfMagic := []byte("OTTO\x00\x00\x00\x00")

	cases := []struct {
		filename string
		body     []byte
		want     string
	}{
		// .js sniffs as text/plain; the ambiguous-sniff override maps it to text/javascript.
		{"app.js", []byte("console.log('hi');"), "text/javascript"},
		// Font types — on Go 1.26+ DetectContentType returns font/* directly.
		{"font.woff", woffMagic, "font/woff"},
		{"font.woff2", woff2Magic, "font/woff2"},
		{"font.ttf", ttfMagic, "font/ttf"},
		{"font.otf", otfMagic, "font/otf"},
		// Existing types should keep working.
		{"logo.png", []byte("\x89PNG\r\n\x1a\n"), "image/png"},
		{"style.css", []byte("body { color: red; }"), "text/css"},
		{"readme.txt", []byte("hello world"), "text/plain"},
	}

	for _, tc := range cases {
		t.Run(tc.filename, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			rec := httptest.NewRecorder()

			var buf bytes.Buffer
			writer := multipart.NewWriter(&buf)
			part, _ := writer.CreateFormFile("file", tc.filename)
			_, _ = part.Write(tc.body)
			_ = writer.Close()

			// Reconstruct a minimal FileHeader for the helper.
			_, params, _ := mime.ParseMediaType("multipart/form-data; boundary=" + writer.Boundary())
			reader := multipart.NewReader(&buf, params["boundary"])
			form, _ := reader.ReadForm(int64(buf.Len() + 1024))
			fileHeaders := form.File["file"]
			if len(fileHeaders) == 0 {
				t.Fatalf("failed to build multipart file header for %s", tc.filename)
			}
			header := fileHeaders[0]

			f, _ := header.Open()
			defer f.Close()

			got, ok := detectTemplateAssetMIME(rec, req, f, header)
			if !ok {
				t.Fatalf("detectTemplateAssetMIME rejected %s (body=%q)", tc.filename, tc.body)
			}
			if got != tc.want {
				t.Errorf("%s: got %q, want %q", tc.filename, got, tc.want)
			}
		})
	}
}

func TestDetectTemplateAssetMIME_RejectHTML(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	rec := httptest.NewRecorder()

	body := []byte("<html></html>")
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, _ := writer.CreateFormFile("file", "evil.html")
	_, _ = part.Write(body)
	_ = writer.Close()

	_, params, _ := mime.ParseMediaType("multipart/form-data; boundary=" + writer.Boundary())
	reader := multipart.NewReader(&buf, params["boundary"])
	form, _ := reader.ReadForm(int64(buf.Len() + 1024))
	header := form.File["file"][0]

	f, _ := header.Open()
	defer f.Close()

	_, ok := detectTemplateAssetMIME(rec, req, f, header)
	if ok {
		t.Error("expected .html upload to be rejected, but it was accepted")
	}
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("expected status %d, got %d", http.StatusUnsupportedMediaType, rec.Code)
	}
	wantBody := "対応していない"
	if !strings.Contains(rec.Body.String(), wantBody) {
		t.Errorf("expected body to contain %q, got %q", wantBody, rec.Body.String())
	}
}
