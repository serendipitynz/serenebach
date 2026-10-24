package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestImagesUploadRejectsOversizedDimensions(t *testing.T) {
	h, db := newAdminTestHandler(t)
	h.ImageDir = t.TempDir()

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("file", "huge.gif")
	if err != nil {
		t.Fatal(err)
	}
	// 13-byte GIF header declaring 30000×30000.
	if _, err := part.Write([]byte("GIF89a\x30\x75\x30\x75\x00\x00\x00")); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/admin/images/upload", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Accept", "application/json")
	req = withAdmin(req)
	rec := httptest.NewRecorder()

	h.imagesUpload(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, rec.Body.String())
	}
	if !strings.Contains(resp["error"], "20000") {
		t.Errorf("error = %q, want the per-side limit in it", resp["error"])
	}

	var n int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM images`).Scan(&n); err != nil {
		t.Fatalf("count images: %v", err)
	}
	if n != 0 {
		t.Errorf("images rows = %d, want 0", n)
	}
	if entries, err := os.ReadDir(h.ImageDir); err != nil || len(entries) != 0 {
		t.Errorf("image dir entries = %v (err %v), want none", entries, err)
	}
}
