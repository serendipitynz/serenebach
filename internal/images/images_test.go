package images

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newTestImage produces a plain-colour rectangle of the requested size so
// tests can cover both "smaller than thumbnail max" and "needs resize" paths.
func newTestImage(w, h int, c color.RGBA) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	return img
}

func TestSaveUploadWritesFileAndThumbnail(t *testing.T) {
	root := t.TempDir()
	s := NewStore(root)

	var buf bytes.Buffer
	if err := png.Encode(&buf, newTestImage(400, 300, color.RGBA{10, 20, 30, 255})); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 4, 19, 10, 0, 0, 0, time.UTC)
	stored, err := s.SaveUpload(&buf, "hello world.PNG", "image/png", now)
	if err != nil {
		t.Fatalf("SaveUpload: %v", err)
	}

	if stored.Kind != "image" {
		t.Errorf("Kind = %q, want image", stored.Kind)
	}
	if !strings.HasPrefix(stored.StoredPath, "2026/04/") {
		t.Errorf("StoredPath = %q, want YYYY/MM prefix", stored.StoredPath)
	}
	if !strings.HasSuffix(stored.StoredPath, ".png") {
		t.Errorf("StoredPath = %q, want .png suffix", stored.StoredPath)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(stored.StoredPath))); err != nil {
		t.Errorf("stored file missing: %v", err)
	}
	if stored.ThumbPath == "" {
		t.Fatalf("ThumbPath empty; want sibling .thumb.jpg")
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(stored.ThumbPath))); err != nil {
		t.Errorf("thumb file missing: %v", err)
	}
	if stored.Width != 400 || stored.Height != 300 {
		t.Errorf("dims = %dx%d, want 400x300", stored.Width, stored.Height)
	}
	if stored.SizeBytes == 0 {
		t.Errorf("SizeBytes = 0; want positive")
	}
}

func TestSaveUploadThumbnailDownsizesLongestEdge(t *testing.T) {
	root := t.TempDir()
	s := NewStore(root)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, newTestImage(600, 300, color.RGBA{200, 50, 50, 255}), &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}

	stored, err := s.SaveUpload(&buf, "wide.jpg", "image/jpeg", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	thumb, err := os.Open(filepath.Join(root, filepath.FromSlash(stored.ThumbPath)))
	if err != nil {
		t.Fatal(err)
	}
	defer thumb.Close()
	decoded, err := jpeg.Decode(thumb)
	if err != nil {
		t.Fatal(err)
	}
	got := decoded.Bounds()
	if got.Dx() != ThumbMaxEdge {
		t.Errorf("thumb width = %d, want %d", got.Dx(), ThumbMaxEdge)
	}
	// Aspect ratio preserved → height scales to half the max edge.
	if got.Dy() != ThumbMaxEdge/2 {
		t.Errorf("thumb height = %d, want %d", got.Dy(), ThumbMaxEdge/2)
	}
}

func TestSaveUploadSkipsThumbnailForBadBytes(t *testing.T) {
	root := t.TempDir()
	s := NewStore(root)

	// Bytes that claim to be PNG but fail to decode. The handler already
	// validated MIME via DetectContentType on the first 512 bytes, so
	// arriving here with garbage is an edge we just tolerate (keep the
	// file, skip the thumb).
	stored, err := s.SaveUpload(strings.NewReader("not actually an image"), "x.png", "image/png", time.Now())
	if err != nil {
		t.Fatalf("SaveUpload: %v", err)
	}
	if stored.ThumbPath != "" {
		t.Errorf("ThumbPath = %q; want empty when decode fails", stored.ThumbPath)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(stored.StoredPath))); err != nil {
		t.Errorf("original file should still land on disk: %v", err)
	}
}

func TestSaveUploadAcceptsAudio(t *testing.T) {
	root := t.TempDir()
	s := NewStore(root)
	stored, err := s.SaveUpload(strings.NewReader(strings.Repeat("x", 1024)), "song.mp3", "audio/mpeg", time.Now())
	if err != nil {
		t.Fatalf("SaveUpload: %v", err)
	}
	if stored.Kind != "audio" {
		t.Errorf("Kind = %q, want audio", stored.Kind)
	}
	if stored.ThumbPath != "" {
		t.Errorf("ThumbPath = %q; want empty for non-image", stored.ThumbPath)
	}
	if stored.Width != 0 || stored.Height != 0 {
		t.Errorf("dims = %dx%d; want 0x0 for non-image", stored.Width, stored.Height)
	}
}

func TestSaveUploadAcceptsDocument(t *testing.T) {
	root := t.TempDir()
	s := NewStore(root)
	stored, err := s.SaveUpload(strings.NewReader("# hello"), "notes.md", "text/markdown", time.Now())
	if err != nil {
		t.Fatalf("SaveUpload: %v", err)
	}
	if stored.Kind != "document" {
		t.Errorf("Kind = %q, want document", stored.Kind)
	}
}

func TestSaveUploadRejectsUnsupportedMIME(t *testing.T) {
	root := t.TempDir()
	s := NewStore(root)
	_, err := s.SaveUpload(strings.NewReader("..."), "file.zip", "application/zip", time.Now())
	if err == nil {
		t.Fatalf("want error for unsupported mime upload")
	}
}

func TestDeleteFilesRemovesPaths(t *testing.T) {
	root := t.TempDir()
	s := NewStore(root)
	var buf bytes.Buffer
	_ = png.Encode(&buf, newTestImage(80, 80, color.RGBA{0, 0, 0, 255}))
	stored, err := s.SaveUpload(&buf, "delete-me.png", "image/png", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	s.DeleteFiles(stored.StoredPath, stored.ThumbPath)
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(stored.StoredPath))); !os.IsNotExist(err) {
		t.Errorf("stored file still present after DeleteFiles")
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(stored.ThumbPath))); !os.IsNotExist(err) {
		t.Errorf("thumb file still present after DeleteFiles")
	}
}

func TestSanitiseSlugStripsNonASCII(t *testing.T) {
	cases := map[string]string{
		"Hello World":   "hello-world",
		"日本語タイトル":       "",
		"mixed 日本 name": "mixed-name",
		"already-clean": "already-clean",
		"   ":           "",
		"...dots...":    "dots",
	}
	for in, want := range cases {
		got := sanitiseSlug(in)
		if got != want {
			t.Errorf("sanitiseSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestScaleToMaxPreservesAspect(t *testing.T) {
	tw, th := scaleToMax(1000, 500, 240)
	if tw != 240 || th != 120 {
		t.Errorf("1000x500 → %dx%d, want 240x120", tw, th)
	}
	tw, th = scaleToMax(500, 1000, 240)
	if tw != 120 || th != 240 {
		t.Errorf("500x1000 → %dx%d, want 120x240", tw, th)
	}
}
