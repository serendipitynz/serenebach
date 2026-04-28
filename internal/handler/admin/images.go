package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
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

	// JSON variant powers the entry-form image picker. Keep the legacy
	// high-limit, single-window shape — the picker does its own
	// filtering client-side and doesn't page.
	if q.Get("format") == "json" || acceptsJSON(r) {
		items, err := h.Store.ListImagesForAdmin(r.Context(), h.wid(), adminImageListLimit, 0)
		if err != nil {
			log.Printf("admin.imagesList: %v", err)
			http.Error(w, "failed to list images", http.StatusInternalServerError)
			return
		}
		payload := make([]map[string]any, 0, len(items))
		for _, img := range items {
			entry := map[string]any{
				"id":          img.ID,
				"filename":    img.Filename,
				"stored_path": img.StoredPath,
				"url":         imagesPathPrefix + img.StoredPath,
				"width":       img.Width,
				"height":      img.Height,
				"alt":         img.AltText,
			}
			if img.ThumbPath != "" {
				entry["thumb_url"] = imagesPathPrefix + img.ThumbPath
			} else {
				entry["thumb_url"] = imagesPathPrefix + img.StoredPath
			}
			payload = append(payload, entry)
		}
		writeJSON(w, http.StatusOK, map[string]any{"images": payload})
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
	page := 1
	if raw := q.Get("page"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			page = v
		}
	}
	totalPages := int((total + int64(adminImagePageSize) - 1) / int64(adminImagePageSize))
	if totalPages == 0 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
	}
	offset := (page - 1) * adminImagePageSize

	items, err := h.Store.ListImagesForAdmin(r.Context(), h.wid(), adminImagePageSize, offset)
	if err != nil {
		log.Printf("admin.imagesList: %v", err)
		http.Error(w, "failed to list images", http.StatusInternalServerError)
		return
	}

	view := q.Get("view")
	if view != "list" {
		view = "grid"
	}

	prev := 0
	if page > 1 {
		prev = page - 1
	}
	next := 0
	if page < totalPages {
		next = page + 1
	}

	renderMain(w, r, pageImages, imagesListPageData{
		pageBase: pageBase{
			Title:      tr(r, "images.title"),
			ActiveMenu: "images",
			CSRFToken:  csrf.Token(r.Context()),
			User:       session.UserFrom(r.Context()),
		},
		Images:       items,
		UploadMaxMB:  h.uploadMaxBytes() >> 20,
		ImagesPrefix: imagesPathPrefix,
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

// ---- upload ------------------------------------------------------------

// imagesUpload handles a single-file multipart POST. Returns JSON when the
// client sets Accept: application/json (the drop-zone fetch does), and
// otherwise redirects back to the gallery so a progressive-enhancement
// `<form>` without JS still round-trips cleanly.
func (h *Handler) imagesUpload(w http.ResponseWriter, r *http.Request) {
	wantsJSON := acceptsJSON(r)

	if r.ContentLength > 0 && r.ContentLength > h.uploadMaxBytes() {
		respondUpload(w, wantsJSON, http.StatusRequestEntityTooLarge,
			trf(r, "common.error.fileTooLargeWithLimit", h.uploadMaxBytes()>>20), "")
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
			respondUpload(w, wantsJSON, http.StatusBadRequest, tr(r, "common.error.uploadParse"), "")
			return
		}
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		respondUpload(w, wantsJSON, http.StatusBadRequest, tr(r, "common.error.fileMissing"), "")
		return
	}
	defer file.Close()

	mime, err := detectImageMIME(file, header)
	if err != nil {
		respondUpload(w, wantsJSON, http.StatusUnsupportedMediaType,
			tr(r, "common.error.unsupportedImage"), "")
		return
	}

	store := h.imageStore()
	stored, err := store.SaveUpload(file, header.Filename, mime, time.Now())
	if err != nil {
		log.Printf("admin.imagesUpload: save: %v", err)
		respondUpload(w, wantsJSON, http.StatusInternalServerError, tr(r, "common.error.fileSaveFailed"), "")
		return
	}

	u := session.UserFrom(r.Context())
	id, err := h.Store.CreateImage(r.Context(), domain.Image{
		WID:        h.wid(),
		UploadedBy: u.ID,
		Filename:   stored.Filename,
		StoredPath: stored.StoredPath,
		ThumbPath:  stored.ThumbPath,
		MimeType:   mime,
		SizeBytes:  stored.SizeBytes,
		Width:      stored.Width,
		Height:     stored.Height,
	})
	if err != nil {
		log.Printf("admin.imagesUpload: db insert: %v", err)
		// Orphan file — best effort clean up so the disk doesn't grow on a
		// transient DB hiccup.
		store.DeleteFiles(stored.StoredPath, stored.ThumbPath)
		respondUpload(w, wantsJSON, http.StatusInternalServerError, tr(r, "common.error.imageDBSaveFailed"), "")
		return
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
			"url":                imagesPathPrefix + stored.StoredPath,
			"thumb_url":          thumbURL(stored),
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
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
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

// detectImageMIME rewinds the uploaded file after peeking at the first
// 512 bytes through http.DetectContentType. We trust the detector over the
// client-supplied Content-Type because browsers can be tricked / wrong.
func detectImageMIME(f multipart.File, _ *multipart.FileHeader) (string, error) {
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
	if !images.AllowedMIMEs[ct] {
		return "", fmt.Errorf("mime %q not allowed", ct)
	}
	return ct, nil
}

func acceptsJSON(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "application/json")
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = encodeJSON(w, payload)
}

func encodeJSON(w io.Writer, payload any) error {
	return json.NewEncoder(w).Encode(payload)
}

func urlEscape(s string) string { return url.QueryEscape(s) }

// respondUpload writes either a JSON error or a redirect with ?err=...
// depending on what the client asked for, so drop-zone and plain-form
// clients both get a sensible response shape.
func respondUpload(w http.ResponseWriter, asJSON bool, status int, msg string, _ string) {
	if asJSON {
		writeJSON(w, status, map[string]string{"error": msg})
		return
	}
	w.Header().Set("Location", "/admin/images?err="+urlEscape(msg))
	w.WriteHeader(http.StatusFound)
}

// thumbURL returns the URL for the thumbnail, or the full image when no
// thumbnail was generated.
func thumbURL(s *images.Stored) string {
	if s.ThumbPath != "" {
		return imagesPathPrefix + s.ThumbPath
	}
	return imagesPathPrefix + s.StoredPath
}
