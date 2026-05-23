package admin

import (
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/storage/repo"
)

// mountTemplateAssets registers the asset-side routes for the template
// editor. Assets live under <TemplateDir>/<template_id>/<filename> on
// disk and are served read-only via /template/<template_id>/<filename>.
func (h *Handler) mountTemplateAssets(r chi.Router) {
	r.Group(func(gr chi.Router) {
		gr.Use(h.requireDesign)
		gr.Post("/templates/{id}/assets", h.templateAssetUpload)
		gr.Post("/templates/{id}/assets/{assetID}/delete", h.templateAssetDelete)
	})
}

// allowed asset mime types — anything the template layer is plausibly
// going to reference from HTML / CSS. Broader than the entry-image
// allowlist on purpose: templates reach for .svg / .js / .woff too.
var allowedTemplateAssetMIME = map[string]bool{
	"image/jpeg":    true,
	"image/png":     true,
	"image/gif":     true,
	"image/webp":    true,
	"image/svg+xml": true,
	"text/css":      true,
	"text/plain":    true,
	// JS + fonts land under the generic app/* + font/* families; extend
	// when a template in the wild actually needs them.
}

// templateAssetUpload handles a multipart POST from the template editor.
// The CSRF header pattern mirrors the image upload flow so the admin JS
// can stay on one FormData + X-CSRF-Token recipe.
func (h *Handler) templateAssetUpload(w http.ResponseWriter, r *http.Request) {
	tplID, ok := h.loadTemplateForAssetUpload(w, r)
	if !ok {
		return
	}

	if r.ContentLength > 0 && r.ContentLength > h.uploadMaxBytes() {
		http.Error(w, tr(r, "common.error.fileTooLarge"), http.StatusRequestEntityTooLarge)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, h.uploadMaxBytes())

	if r.MultipartForm == nil {
		if err := r.ParseMultipartForm(h.uploadMaxBytes()); err != nil {
			http.Error(w, tr(r, "common.error.uploadParse"), http.StatusBadRequest)
			return
		}
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, tr(r, "common.error.fileMissing"), http.StatusBadRequest)
		return
	}
	defer file.Close()

	mt, ok := detectTemplateAssetMIME(w, r, file, header)
	if !ok {
		return
	}
	filename, ok := sanitiseTemplateAssetFilename(w, r, header.Filename)
	if !ok {
		return
	}

	absPath, written, ok := h.persistTemplateAssetFile(w, r, tplID, filename, file)
	if !ok {
		return
	}

	if _, err := h.Store.CreateOrReplaceTemplateAsset(r.Context(), domain.TemplateAsset{
		TemplateID: tplID,
		Filename:   filename,
		MimeType:   mt,
		SizeBytes:  written,
	}); err != nil {
		_ = os.Remove(absPath)
		log.Printf("admin.templateAssetUpload: db insert: %v", err)
		http.Error(w, tr(r, "common.error.metaSaveFailed"), http.StatusInternalServerError)
		return
	}

	if acceptsJSON(r) {
		writeJSON(w, http.StatusCreated, map[string]any{
			"filename": filename,
			"url":      fmt.Sprintf("/template/%d/%s", tplID, filename),
			"mime":     mt,
			"size":     written,
		})
		return
	}
	http.Redirect(w, r, root(r)+fmt.Sprintf("/admin/templates/%d/edit?ok=asset-uploaded", tplID), http.StatusFound)
}

// loadTemplateForAssetUpload parses the {id} route param and confirms
// the template row exists in this weblog. ok=false means the response
// has already been written (404 or 500) and the caller must stop.
func (h *Handler) loadTemplateForAssetUpload(w http.ResponseWriter, r *http.Request) (int64, bool) {
	tplID, ok := parsePositiveID(r, "id")
	if !ok {
		http.NotFound(w, r)
		return 0, false
	}
	if _, err := h.Store.TemplateByID(r.Context(), h.wid(), tplID); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return 0, false
		}
		http.Error(w, "failed to load template", http.StatusInternalServerError)
		return 0, false
	}
	return tplID, true
}

// detectTemplateAssetMIME sniffs the file's content and falls back to
// the filename extension when DetectContentType lands outside the
// allowlist (e.g. .css commonly sniffs as "text/plain"). Writes the
// appropriate 4xx / 5xx response and returns ok=false on failure.
func detectTemplateAssetMIME(w http.ResponseWriter, r *http.Request, file io.ReadSeeker, header *multipart.FileHeader) (string, bool) {
	head := make([]byte, 512)
	n, _ := file.Read(head)
	if _, err := file.Seek(0, 0); err != nil {
		http.Error(w, tr(r, "common.error.fileReadFailed"), http.StatusInternalServerError)
		return "", false
	}
	mt := http.DetectContentType(head[:n])
	if idx := strings.IndexByte(mt, ';'); idx >= 0 {
		mt = strings.TrimSpace(mt[:idx])
	}
	if !allowedTemplateAssetMIME[mt] {
		mt = templateAssetMIMEFromExt(mt, header.Filename)
	}
	if !allowedTemplateAssetMIME[mt] {
		http.Error(w, trf(r, "common.error.unsupportedMime", mt), http.StatusUnsupportedMediaType)
		return "", false
	}
	return mt, true
}

// sanitiseTemplateAssetFilename strips any path components, drops NUL
// bytes, and rejects the special-case basenames ("", ".", "..") that
// would resolve to a directory or its parent rather than a file.
func sanitiseTemplateAssetFilename(w http.ResponseWriter, r *http.Request, raw string) (string, bool) {
	filename := filepath.Base(raw)
	filename = strings.ReplaceAll(filename, "\x00", "")
	if filename == "" || filename == "." || filename == ".." {
		http.Error(w, tr(r, "common.error.invalidFilename"), http.StatusBadRequest)
		return "", false
	}
	return filename, true
}

// persistTemplateAssetFile creates the per-template asset directory
// (idempotent), opens the destination, and copies the uploaded body
// in. Cleans up a partial file on copy failure so a half-written
// asset never appears on disk without a matching DB row.
func (h *Handler) persistTemplateAssetFile(w http.ResponseWriter, r *http.Request, tplID int64, filename string, file io.Reader) (string, int64, bool) {
	absDir := filepath.Join(h.TemplateDir, strconv.FormatInt(tplID, 10))
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		log.Printf("admin.templateAssetUpload: mkdir: %v", err)
		http.Error(w, tr(r, "common.error.mkdirFailed"), http.StatusInternalServerError)
		return "", 0, false
	}
	absPath := filepath.Join(absDir, filename)
	out, err := os.Create(absPath)
	if err != nil {
		log.Printf("admin.templateAssetUpload: create: %v", err)
		http.Error(w, tr(r, "common.error.fileSaveFailed"), http.StatusInternalServerError)
		return "", 0, false
	}
	written, err := io.Copy(out, file)
	_ = out.Close()
	if err != nil {
		_ = os.Remove(absPath)
		log.Printf("admin.templateAssetUpload: copy: %v", err)
		http.Error(w, tr(r, "common.error.fileSaveFailed"), http.StatusInternalServerError)
		return "", 0, false
	}
	return absPath, written, true
}

func (h *Handler) templateAssetDelete(w http.ResponseWriter, r *http.Request) {
	tplID, ok := parsePositiveID(r, "id")
	if !ok {
		http.NotFound(w, r)
		return
	}
	assetID, err := strconv.ParseInt(chi.URLParam(r, "assetID"), 10, 64)
	if err != nil || assetID <= 0 {
		http.NotFound(w, r)
		return
	}
	asset, err := h.Store.TemplateAssetByID(r.Context(), assetID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		log.Printf("admin.templateAssetDelete: load: %v", err)
		http.Error(w, "failed to load asset", http.StatusInternalServerError)
		return
	}
	if asset.TemplateID != tplID {
		http.NotFound(w, r)
		return
	}
	if err := h.Store.DeleteTemplateAsset(r.Context(), assetID); err != nil {
		log.Printf("admin.templateAssetDelete: delete: %v", err)
		http.Error(w, "failed to delete asset", http.StatusInternalServerError)
		return
	}
	// Best-effort unlink. DB is the source of truth; we don't care if the
	// file was already gone.
	_ = os.Remove(filepath.Join(h.TemplateDir, strconv.FormatInt(tplID, 10), asset.Filename))
	http.Redirect(w, r, root(r)+fmt.Sprintf("/admin/templates/%d/edit?ok=asset-deleted", tplID), http.StatusFound)
}

// templateAssetMIMEFromExt resolves the upload MIME by filename extension
// when the sniffed `current` value sits outside the asset allowlist. It
// returns `current` unchanged when the extension is unknown or the
// extension-derived type is also outside the allowlist — the caller's
// next allowlist check then rejects the upload.
func templateAssetMIMEFromExt(current, filename string) string {
	byExt := mime.TypeByExtension(strings.ToLower(filepath.Ext(filename)))
	if byExt == "" {
		return current
	}
	if idx := strings.IndexByte(byExt, ';'); idx >= 0 {
		byExt = strings.TrimSpace(byExt[:idx])
	}
	if allowedTemplateAssetMIME[byExt] {
		return byExt
	}
	return current
}
