// Package images wraps the on-disk layout for uploaded media and the
// pure-Go thumbnail pipeline. Everything here is stdlib plus
// golang.org/x/image/draw for high-quality resize — no cgo, no vips —
// so the single-binary deploy story stays intact.
package images

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// ThumbMaxEdge is the longest-edge cap for generated thumbnails. 240px fits
// comfortably in the admin gallery grid without blocking a mobile column.
const ThumbMaxEdge = 240

// AllowedMIMEs is the whitelist consulted by the upload handler. Anything
// else is rejected before we even touch disk.
var AllowedMIMEs = map[string]bool{
	// image
	"image/jpeg": true,
	"image/png":  true,
	"image/gif":  true,
	"image/webp": true,
	// audio
	"audio/mpeg": true,
	"audio/ogg":  true,
	"audio/mp4":  true,
	// document
	"application/pdf": true,
	"text/plain":      true,
	"text/markdown":   true,
	// movie
	"video/mp4":  true,
	"video/webm": true,
}

// KindFor maps a whitelisted MIME type to its upload kind.
func KindFor(mime string) string {
	switch {
	case strings.HasPrefix(mime, "image/"):
		return "image"
	case strings.HasPrefix(mime, "audio/"):
		return "audio"
	case strings.HasPrefix(mime, "video/"):
		return "movie"
	case mime == "application/pdf",
		mime == "text/plain",
		mime == "text/markdown":
		return "document"
	}
	return ""
}

// ExtensionFor maps a mime type to a canonical file extension. The upload
// handler uses this instead of trusting whatever the browser uploaded so
// `.jpeg` and `.JPG` normalise to `.jpg`.
func ExtensionFor(mime string) string {
	switch mime {
	case "image/jpeg":
		return "jpg"
	case "image/png":
		return "png"
	case "image/gif":
		return "gif"
	case "image/webp":
		return "webp"
	case "audio/mpeg":
		return "mp3"
	case "audio/ogg":
		return "ogg"
	case "audio/mp4":
		return "m4a"
	case "application/pdf":
		return "pdf"
	case "text/plain":
		return "txt"
	case "text/markdown":
		return "md"
	case "video/mp4":
		return "mp4"
	case "video/webm":
		return "webm"
	}
	return ""
}

// Store manages the on-disk image tree. Root is the absolute-or-relative
// directory that holds YYYY/MM/<slug>-<shortid>.<ext> files. Public serving
// mounts a FileServer rooted here; the static rebuild copies this tree
// alongside pre-rendered HTML.
type Store struct {
	Root string
}

func NewStore(root string) *Store { return &Store{Root: root} }

// Stored describes what SaveUpload wrote to disk. Callers persist these
// fields in the images table so `/img/<StoredPath>` and the thumbnail URL
// round-trip back to the same bytes.
type Stored struct {
	Filename   string // sanitised original name, kept for humans / alt text
	StoredPath string // relative to Root, forward slashes (URL-safe)
	ThumbPath  string // "" when thumbnail generation failed / skipped
	SizeBytes  int64
	Kind       string // image / audio / document / movie
	Width      int
	Height     int
}

// SaveUpload streams `src` to a new file under Root, honouring mime to pick
// the extension. When the content decodes as a supported image it also
// writes a JPEG thumbnail sibling (`<path>.thumb.jpg`). The caller supplies
// `now` so tests don't depend on wall clock.
func (s *Store) SaveUpload(src io.Reader, originalName, mime string, now time.Time) (*Stored, error) {
	ext := ExtensionFor(mime)
	if ext == "" {
		return nil, fmt.Errorf("images: unsupported mime %q", mime)
	}

	kind := KindFor(mime)

	yearMonth := filepath.Join(fmt.Sprintf("%04d", now.Year()), fmt.Sprintf("%02d", int(now.Month())))
	absDir := filepath.Join(s.Root, yearMonth)
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return nil, fmt.Errorf("images: mkdir: %w", err)
	}

	slug := sanitiseSlug(strings.TrimSuffix(originalName, filepath.Ext(originalName)))
	if slug == "" {
		slug = "upload"
	}
	short, err := shortID()
	if err != nil {
		return nil, err
	}
	baseName := fmt.Sprintf("%s-%s.%s", slug, short, ext)
	absPath := filepath.Join(absDir, baseName)

	// Buffer the upload so we can both persist it and decode for thumbnails
	// without re-reading from the request body. Upload sizes are capped by
	// the handler well below memory pressure thresholds.
	var buf bytes.Buffer
	size, err := io.Copy(&buf, src)
	if err != nil {
		return nil, fmt.Errorf("images: read body: %w", err)
	}

	if err := os.WriteFile(absPath, buf.Bytes(), 0o644); err != nil {
		return nil, fmt.Errorf("images: write file: %w", err)
	}

	relPath := filepath.ToSlash(filepath.Join(yearMonth, baseName))
	stored := &Stored{
		Filename:   filepath.Base(sanitiseFilename(originalName)),
		StoredPath: relPath,
		SizeBytes:  size,
		Kind:       kind,
	}

	// Only images get decoded and thumbnailed; other kinds skip this step
	// so Width/Height stay at zero (the DB will store NULL via the handler).
	if kind != "image" {
		return stored, nil
	}

	img, _, decodeErr := image.Decode(bytes.NewReader(buf.Bytes()))
	if decodeErr != nil {
		// Keep the file — it passed the MIME check — but skip thumbnailing.
		// The admin UI falls back to the original for its preview.
		return stored, nil //nolint:nilerr // decode failure is a non-fatal fallback; the stored original is still usable.
	}
	stored.Width = img.Bounds().Dx()
	stored.Height = img.Bounds().Dy()

	thumbName := strings.TrimSuffix(baseName, "."+ext) + ".thumb.jpg"
	thumbAbs := filepath.Join(absDir, thumbName)
	if err := writeThumbnail(img, thumbAbs); err != nil {
		return stored, nil //nolint:nilerr // thumbnail failure is non-fatal; admin UI falls back to the original.
	}
	stored.ThumbPath = filepath.ToSlash(filepath.Join(yearMonth, thumbName))
	return stored, nil
}

// DeleteFiles best-effort removes the stored file and its thumbnail. Errors
// are swallowed — the DB row is the source of truth, so re-deleting a
// missing file is fine.
func (s *Store) DeleteFiles(storedPath, thumbPath string) {
	if storedPath != "" {
		_ = os.Remove(filepath.Join(s.Root, filepath.FromSlash(storedPath)))
	}
	if thumbPath != "" {
		_ = os.Remove(filepath.Join(s.Root, filepath.FromSlash(thumbPath)))
	}
}

// writeThumbnail resizes src so its longest edge is ThumbMaxEdge and writes
// the result as a reasonable-quality JPEG. draw.CatmullRom is a good default
// — slower than ApproxBiLinear but visibly crisper on photos.
func writeThumbnail(src image.Image, dstPath string) error {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	if w <= ThumbMaxEdge && h <= ThumbMaxEdge {
		// Already thumbnail-sized: keep aspect without upscaling.
		return encodeJPEG(src, dstPath)
	}
	tw, th := scaleToMax(w, h, ThumbMaxEdge)
	dst := image.NewRGBA(image.Rect(0, 0, tw, th))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
	return encodeJPEG(dst, dstPath)
}

func encodeJPEG(img image.Image, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return jpeg.Encode(f, img, &jpeg.Options{Quality: 82})
}

// scaleToMax returns the target dimensions when fitting (w, h) into a
// max-edge square while preserving aspect ratio.
func scaleToMax(w, h, maxEdge int) (int, int) {
	if w == 0 || h == 0 {
		return maxEdge, maxEdge
	}
	if w >= h {
		return maxEdge, max1(int(float64(h) * float64(maxEdge) / float64(w)))
	}
	return max1(int(float64(w) * float64(maxEdge) / float64(h))), maxEdge
}

func max1(v int) int {
	if v < 1 {
		return 1
	}
	return v
}

// slugSafe keeps ASCII letters/digits/hyphens. Japanese filenames are turned
// into random blanks which is fine — the `filename` column keeps the
// original for display, and stored_path is meant to be URL-safe.
var slugSafe = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func sanitiseSlug(s string) string {
	s = strings.ToLower(s)
	s = slugSafe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-._")
	if len(s) > 48 {
		s = s[:48]
	}
	return s
}

// sanitiseFilename strips any path components and control characters but
// otherwise keeps the original name (including non-ASCII) so the admin UI
// can display the filename the human uploaded.
func sanitiseFilename(name string) string {
	name = filepath.Base(name)
	name = strings.ReplaceAll(name, "\x00", "")
	if name == "" || name == "." || name == ".." {
		return "upload"
	}
	return name
}

// shortID returns a 6-byte hex-encoded random id (12 chars). Cheap
// de-duplication keeps two users who upload "cat.jpg" on the same day
// from colliding without standing up a counter or a hashing scheme.
func shortID() (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("images: shortID: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// ---- decoding helpers (used by tests and uploads) ----------------------

// DecodeHeader reads just enough of src to determine image dimensions and
// verify it decodes. The returned image is the fully-decoded frame —
// callers that only want metadata can discard it. Returns ErrUnsupported
// when the mime type isn't in AllowedMIMEs.
func DecodeHeader(mime string, src io.Reader) (image.Image, error) {
	if !AllowedMIMEs[mime] {
		return nil, ErrUnsupported
	}
	switch mime {
	case "image/jpeg":
		return jpeg.Decode(src)
	case "image/png":
		return png.Decode(src)
	case "image/gif":
		return gif.Decode(src)
	case "image/webp":
		img, _, err := image.Decode(src)
		return img, err
	}
	return nil, ErrUnsupported
}

// ErrUnsupported is returned by DecodeHeader when the mime type isn't in
// the allowlist.
var ErrUnsupported = errors.New("images: unsupported mime type")
