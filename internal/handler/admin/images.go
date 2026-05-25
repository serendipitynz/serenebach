package admin

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/serendipitynz/serenebach/internal/csrf"
	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/images"
	"github.com/serendipitynz/serenebach/internal/session"
	"github.com/serendipitynz/serenebach/internal/storage/repo"
)

const (
	// adminImagePageSize caps how many images a single admin page shows.
	// Small enough that even large libraries render without blocking the
	// UI, big enough that the common case (a few dozen images) is one
	// page. Callers paginate via ?page=N.
	adminImagePageSize = 48
	// adminImageListLimit is the overall ceiling for legacy / JSON
	// callers that don't paginate — kept high so the entry-form image
	// picker sees a meaningful slice without plumbing paging through
	// the JS.
	adminImageListLimit = 200
	// imagesPathPrefix is the public URL base for serving uploads. Must line
	// up with the route mount in internal/app/app.go and the {entry body}
	// HTML that references /img/<stored_path>.
	imagesPathPrefix = "/img/"
)

// mountImages registers the /admin/images/* routes. Called from
// MountProtected so the RequireUser middleware already wraps the group.
func (h *Handler) mountImages(r chi.Router) {
	r.Get("/images", h.imagesList)
	r.Post("/images", h.imagesUpload)
	r.Post("/images/{id}/delete", h.imagesDelete)
	// AI alt-text generation for one stored image. JS calls this after
	// an upload when the user's AIAutoAlt flag is on; also usable
	// manually for older uploads that predate the auto-alt feature.
	r.Post("/images/{id}/alt", h.imagesGenerateAlt)
	// Rename updates the human-facing filename label without touching
	// the on-disk stored_path (so past entry links stay valid).
	r.Post("/images/{id}/rename", h.imagesRename)
}

// ---- gallery page ------------------------------------------------------

type imagesListPageData struct {
	pageBase
	Images       []domain.Image
	UploadMaxMB  int64
	ImagesPrefix string
	FlashSuccess string
	FlashError   string
	// Pagination
	Page       int
	TotalPages int
	TotalCount int64
	PrevPage   int // 0 when no prev
	NextPage   int // 0 when no next
	// View mode — "grid" (default) or "list".
	ViewMode string
}

func (h *Handler) imagesList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	if q.Get("format") == "json" || acceptsJSON(r) {
		h.imagesListJSON(w, r)
		return
	}

	// Paginated page view. Default page = 1; out-of-range values
	// silently clamp so a stale bookmark doesn't 500.
	total, err := h.Store.CountImages(r.Context(), h.wid())
	if err != nil {
		log.Printf("admin.imagesList: count: %v", err)
		http.Error(w, "failed to count images", http.StatusInternalServerError)
		return
	}
	page, totalPages, offset := listPagination(q.Get("page"), total, adminImagePageSize)

	items, err := h.Store.ListImagesForAdmin(r.Context(), h.wid(), "", adminImagePageSize, offset)
	if err != nil {
		log.Printf("admin.imagesList: %v", err)
		http.Error(w, "failed to list images", http.StatusInternalServerError)
		return
	}

	view := q.Get("view")
	if view != "list" {
		view = "grid"
	}

	prev, next := pagerNeighbours(page, totalPages)

	renderMain(w, r, pageImages, imagesListPageData{
		pageBase: pageBase{
			Title:      tr(r, "images.title"),
			ActiveMenu: "images",
			CSRFToken:  csrf.Token(r.Context()),
			User:       session.UserFrom(r.Context()),
		},
		Images:       items,
		UploadMaxMB:  h.uploadMaxBytes() >> 20,
		ImagesPrefix: root(r) + imagesPathPrefix,
		FlashSuccess: q.Get("ok"),
		FlashError:   q.Get("err"),
		Page:         page,
		TotalPages:   totalPages,
		TotalCount:   total,
		PrevPage:     prev,
		NextPage:     next,
		ViewMode:     view,
	})
}

// imagesListJSON powers the entry-form image picker. Single window of
// adminImageListLimit rows — the picker does its own client-side
// filtering and doesn't page.
func (h *Handler) imagesListJSON(w http.ResponseWriter, r *http.Request) {
	items, err := h.Store.ListImagesForAdmin(r.Context(), h.wid(), "", adminImageListLimit, 0)
	if err != nil {
		log.Printf("admin.imagesList: %v", err)
		http.Error(w, "failed to list images", http.StatusInternalServerError)
		return
	}
	imgPrefix := root(r) + imagesPathPrefix
	payload := make([]map[string]any, 0, len(items))
	for _, img := range items {
		entry := map[string]any{
			"id":          img.ID,
			"filename":    img.Filename,
			"stored_path": img.StoredPath,
			"url":         imgPrefix + img.StoredPath,
			"kind":        img.Kind,
			"alt":         img.AltText,
		}
		if img.Width.Valid {
			entry["width"] = img.Width.Int64
		}
		if img.Height.Valid {
			entry["height"] = img.Height.Int64
		}
		if img.ThumbPath != "" {
			entry["thumb_url"] = imgPrefix + img.ThumbPath
		} else {
			entry["thumb_url"] = imgPrefix + img.StoredPath
		}
		payload = append(payload, entry)
	}
	writeJSON(w, http.StatusOK, map[string]any{"images": payload})
}

// ---- upload ------------------------------------------------------------

// imagesUpload handles a single-file multipart POST. Returns JSON when the
// client sets Accept: application/json (the drop-zone fetch does), and
// otherwise redirects back to the gallery so a progressive-enhancement
// `<form>` without JS still round-trips cleanly.
//
//nolint:gocyclo
func (h *Handler) imagesUpload(w http.ResponseWriter, r *http.Request) {
	wantsJSON := acceptsJSON(r)

	if r.ContentLength > 0 && r.ContentLength > h.uploadMaxBytes() {
		respondUpload(w, wantsJSON, http.StatusRequestEntityTooLarge,
			trf(r, "common.error.fileTooLargeWithLimit", h.uploadMaxBytes()>>20), root(r))
		return
	}
	// Hard cap on the body — MaxBytesReader will return an error from
	// subsequent reads if the client lies about Content-Length.
	r.Body = http.MaxBytesReader(w, r.Body, h.uploadMaxBytes())

	// CSRF middleware already ParseMultipartForm'd the body for us (for
	// no-JS flows). JS uploads use X-CSRF-Token, which means the body
	// hasn't been parsed yet — do it now.
	if r.MultipartForm == nil {
		if err := r.ParseMultipartForm(h.uploadMaxBytes()); err != nil {
			respondUpload(w, wantsJSON, http.StatusBadRequest, tr(r, "common.error.uploadParse"), root(r))
			return
		}
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		respondUpload(w, wantsJSON, http.StatusBadRequest, tr(r, "common.error.fileMissing"), root(r))
		return
	}
	defer file.Close()

	mime, err := detectUploadMIME(file, header)
	if err != nil {
		respondUpload(w, wantsJSON, http.StatusUnsupportedMediaType,
			tr(r, "common.error.unsupportedImage"), root(r))
		return
	}

	kind := images.KindFor(mime)

	store := h.imageStore()
	stored, err := store.SaveUpload(file, header.Filename, mime, time.Now())
	if err != nil {
		log.Printf("admin.imagesUpload: save: %v", err)
		respondUpload(w, wantsJSON, http.StatusInternalServerError, tr(r, "common.error.fileSaveFailed"), root(r))
		return
	}

	u := session.UserFrom(r.Context())
	img := domain.Image{
		WID:        h.wid(),
		UploadedBy: u.ID,
		Kind:       kind,
		Filename:   stored.Filename,
		StoredPath: stored.StoredPath,
		ThumbPath:  stored.ThumbPath,
		MimeType:   mime,
		SizeBytes:  stored.SizeBytes,
	}
	if stored.Width > 0 {
		img.Width = sql.NullInt64{Int64: int64(stored.Width), Valid: true}
	}
	if stored.Height > 0 {
		img.Height = sql.NullInt64{Int64: int64(stored.Height), Valid: true}
	}
	id, err := h.Store.CreateImage(r.Context(), img)
	if err != nil {
		log.Printf("admin.imagesUpload: db insert: %v", err)
		// Orphan file — best effort clean up so the disk doesn't grow on a
		// transient DB hiccup.
		store.DeleteFiles(stored.StoredPath, stored.ThumbPath)
		respondUpload(w, wantsJSON, http.StatusInternalServerError, tr(r, "common.error.imageDBSaveFailed"), root(r))
		return
	}

	uploadedImage := domain.Image{
		ID:         id,
		WID:        h.wid(),
		UploadedBy: u.ID,
		Kind:       kind,
		Filename:   stored.Filename,
		StoredPath: stored.StoredPath,
		ThumbPath:  stored.ThumbPath,
		MimeType:   mime,
		SizeBytes:  stored.SizeBytes,
	}
	if stored.Width > 0 {
		uploadedImage.Width = sql.NullInt64{Int64: int64(stored.Width), Valid: true}
	}
	if stored.Height > 0 {
		uploadedImage.Height = sql.NullInt64{Int64: int64(stored.Height), Valid: true}
	}
	if kind == domain.KindImage {
		h.dispatchImageUploaded(r.Context(), uploadedImage, imagesPathPrefix+stored.StoredPath)
	}

	if wantsJSON {
		// When the uploader opted into auto-alt and has a usable AI
		// provider wired up, flag it so the client can immediately
		// POST /admin/images/{id}/alt and show the "alt 生成中…"
		// badge. Upload stays fast; the second round-trip handles the
		// slow part visibly.
		autoAlt := false
		if u != nil && u.AIAutoAlt && u.AIKind != "" && mimeSupportsVision(mime) {
			autoAlt = true
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"id":                 id,
			"filename":           stored.Filename,
			"url":                root(r) + imagesPathPrefix + stored.StoredPath,
			"thumb_url":          thumbURL(stored, root(r)),
			"kind":               kind,
			"width":              stored.Width,
			"height":             stored.Height,
			"size":               stored.SizeBytes,
			"auto_alt_requested": autoAlt,
		})
		return
	}
	http.Redirect(w, r, root(r)+"/admin/images?ok=1", http.StatusFound)
}

// mimeSupportsVision whitelists the MIME types every provider we
// ship can actually decode. Keeps the auto-alt trigger from firing
// for formats like BMP that our storage layer already rejects.
func mimeSupportsVision(mime string) bool {
	switch mime {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		return true
	}
	return false
}

// ---- delete ------------------------------------------------------------

func (h *Handler) imagesDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := parsePositiveID(r, "id")
	if !ok {
		http.NotFound(w, r)
		return
	}
	img, err := h.Store.ImageByID(r.Context(), h.wid(), id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		log.Printf("admin.imagesDelete: load: %v", err)
		http.Error(w, "failed to load image", http.StatusInternalServerError)
		return
	}
	// Regular users can only scrap images they uploaded themselves.
	// Power + Admin pass through unconditionally.
	u := session.UserFrom(r.Context())
	if u == nil || !u.CanDeleteImage(img.UploadedBy) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if err := h.Store.DeleteImage(r.Context(), h.wid(), id); err != nil {
		log.Printf("admin.imagesDelete: delete: %v", err)
		http.Error(w, "failed to delete image", http.StatusInternalServerError)
		return
	}
	h.imageStore().DeleteFiles(img.StoredPath, img.ThumbPath)
	http.Redirect(w, r, root(r)+"/admin/images?ok=deleted", http.StatusFound)
}

// ---- helpers -----------------------------------------------------------

// imageStore builds a disk-layer helper bound to the configured image root.
// Kept short-lived because Store is stateless and cheap to construct.
func (h *Handler) imageStore() *images.Store {
	return images.NewStore(h.ImageDir)
}

func (h *Handler) uploadMaxBytes() int64 {
	if h.UploadMaxBytes <= 0 {
		return 10 << 20
	}
	return h.UploadMaxBytes
}

// detectUploadMIME rewinds the uploaded file after peeking at the first
// 512 bytes through http.DetectContentType. We trust the detector over the
// client-supplied Content-Type because browsers can be tricked / wrong.
// Extension-based normalisation is applied for edge cases the stdlib
// detector gets wrong (text/markdown, audio/mp4, audio/ogg).
func detectUploadMIME(f multipart.File, h *multipart.FileHeader) (string, error) {
	head := make([]byte, 512)
	n, _ := f.Read(head)
	if _, err := f.Seek(0, 0); err != nil {
		return "", fmt.Errorf("rewind: %w", err)
	}
	ct := http.DetectContentType(head[:n])
	// DetectContentType returns "image/jpeg; charset=utf-8" etc — trim the
	// parameters so it matches the allowlist keys exactly.
	if idx := strings.IndexByte(ct, ';'); idx >= 0 {
		ct = strings.TrimSpace(ct[:idx])
	}

	// Extension-based normalisation for formats the stdlib misdetects.
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(h.Filename), "."))
	switch {
	case ct == "text/plain" && ext == "md":
		ct = "text/markdown"
	case ct == "video/mp4" && ext == "m4a":
		ct = "audio/mp4"
	case ct == "application/ogg" && ext == "ogg":
		ct = "audio/ogg"
	}

	if !images.AllowedMIMEs[ct] {
		return "", fmt.Errorf("mime %q not allowed", ct)
	}
	return ct, nil
}

func acceptsJSON(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "application/json")
}

func encodeJSON(w io.Writer, payload any) error {
	return json.NewEncoder(w).Encode(payload)
}

func urlEscape(s string) string { return url.QueryEscape(s) }

// respondUpload writes either a JSON error or a redirect with ?err=...
// depending on what the client asked for, so drop-zone and plain-form
// clients both get a sensible response shape.
func respondUpload(w http.ResponseWriter, asJSON bool, status int, msg, basePath string) {
	if asJSON {
		writeJSON(w, status, map[string]string{"error": msg})
		return
	}
	w.Header().Set("Location", basePath+"/admin/images?err="+urlEscape(msg))
	w.WriteHeader(http.StatusFound)
}

// imagesRename updates the human-facing filename label for an upload.
// Only the DB filename column is touched; stored_path stays the same so
// past entry links don't break.
func (h *Handler) imagesRename(w http.ResponseWriter, r *http.Request) {
	id, ok := parsePositiveID(r, "id")
	if !ok {
		http.NotFound(w, r)
		return
	}
	img, err := h.Store.ImageByID(r.Context(), h.wid(), id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		log.Printf("admin.imagesRename: load: %v", err)
		http.Error(w, "failed to load image", http.StatusInternalServerError)
		return
	}
	u := session.UserFrom(r.Context())
	if u == nil || !u.CanDeleteImage(img.UploadedBy) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	newName := strings.TrimSpace(r.FormValue("filename"))
	if newName == "" || strings.ContainsAny(newName, "\x00\x01\x02\x03\x04\x05\x06\x07\x08\x09\x0a\x0b\x0c\x0d\x0e\x0f") {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid filename"})
		return
	}
	if len(newName) > 255 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "filename too long"})
		return
	}

	if err := h.Store.UpdateImageFilename(r.Context(), h.wid(), id, newName); err != nil {
		log.Printf("admin.imagesRename: update: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "save failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "filename": newName})
}

// humanSize formats a byte count as B / KB / MB.
// Registered as a template func so the linter cannot see the call site.
//
//nolint:unused
func humanSize(b int64) string {
	switch {
	case b < 1024:
		return fmt.Sprintf("%d B", b)
	case b < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(b)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(b)/(1024*1024))
	}
}

// thumbURL returns the URL for the thumbnail, or the full image when no
// thumbnail was generated.
func thumbURL(s *images.Stored, basePath string) string {
	if s.ThumbPath != "" {
		return basePath + imagesPathPrefix + s.ThumbPath
	}
	return basePath + imagesPathPrefix + s.StoredPath
}
