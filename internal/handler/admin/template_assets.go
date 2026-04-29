package admin

import (
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
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
	tplID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || tplID <= 0 {
		http.NotFound(w, r)
		return
	}
	// Confirm the template exists + belongs to this weblog.
	if _, err := h.Store.TemplateByID(r.Context(), h.wid(), tplID); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "failed to load template", http.StatusInternalServerError)
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

	// Sniff the content — don't trust the client Content-Type.
	head := make([]byte, 512)
	n, _ := file.Read(head)
	if _, err := file.Seek(0, 0); err != nil {
		http.Error(w, tr(r, "common.error.fileReadFailed"), http.StatusInternalServerError)
		return
	}
	mt := http.DetectContentType(head[:n])
	if idx := strings.IndexByte(mt, ';'); idx >= 0 {
		mt = strings.TrimSpace(mt[:idx])
	}
	// Fallback: DetectContentType is conservative (e.g. returns
	// "text/plain" for a .css file). Try the filename extension when
	// sniffing lands outside the allowlist.
	if !allowedTemplateAssetMIME[mt] {
		if byExt := mime.TypeByExtension(strings.ToLower(filepath.Ext(header.Filename))); byExt != "" {
			if idx := strings.IndexByte(byExt, ';'); idx >= 0 {
				byExt = strings.TrimSpace(byExt[:idx])
			}
			if allowedTemplateAssetMIME[byExt] {
				mt = byExt
			}
		}
	}
	if !allowedTemplateAssetMIME[mt] {
		http.Error(w, trf(r, "common.error.unsupportedMime", mt), http.StatusUnsupportedMediaType)
		return
	}

	// Sanitise the filename — strip path components, drop control bytes.
	filename := filepath.Base(header.Filename)
	filename = strings.ReplaceAll(filename, "\x00", "")
	if filename == "" || filename == "." || filename == ".." {
		http.Error(w, tr(r, "common.error.invalidFilename"), http.StatusBadRequest)
		return
	}

	absDir := filepath.Join(h.TemplateDir, strconv.FormatInt(tplID, 10))
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		log.Printf("admin.templateAssetUpload: mkdir: %v", err)
		http.Error(w, tr(r, "common.error.mkdirFailed"), http.StatusInternalServerError)
		return
	}
	absPath := filepath.Join(absDir, filename)
	out, err := os.Create(absPath)
	if err != nil {
		log.Printf("admin.templateAssetUpload: create: %v", err)
		http.Error(w, tr(r, "common.error.fileSaveFailed"), http.StatusInternalServerError)
		return
	}
	written, err := io.Copy(out, file)
	_ = out.Close()
	if err != nil {
		_ = os.Remove(absPath)
		log.Printf("admin.templateAssetUpload: copy: %v", err)
		http.Error(w, tr(r, "common.error.fileSaveFailed"), http.StatusInternalServerError)
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

func (h *Handler) templateAssetDelete(w http.ResponseWriter, r *http.Request) {
	tplID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || tplID <= 0 {
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
