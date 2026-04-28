package admin

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/serendipitynz/serenebach/internal/csrf"
	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/session"
	"github.com/serendipitynz/serenebach/internal/storage/repo"
	"github.com/serendipitynz/serenebach/internal/templatepack"
)

// mountTemplatePack wires the SB3-compatible template bundle flows:
// export (download a .txt) and import (upload one). The bundle replaces
// the directory-of-loose-files workflow that earlier SB versions used.
func (h *Handler) mountTemplatePack(r chi.Router) {
	r.Group(func(gr chi.Router) {
		gr.Use(h.requireDesign)
		gr.Get("/templates/{id}/export", h.templateExport)
		gr.Get("/templates/import", h.templateImportForm)
		gr.Post("/templates/import", h.templateImportSubmit)
	})
}

// ---- export ------------------------------------------------------------

// templateExport bundles the template row + its assets into one
// multipart/mixed template.txt and streams it back as a download. Clones
// SB3's shape so the artifact is consumable by both this admin and the
// legacy template-import tool.
func (h *Handler) templateExport(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	tmpl, err := h.Store.TemplateByID(r.Context(), h.wid(), id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		log.Printf("admin.templateExport: load: %v", err)
		http.Error(w, "failed to load template", http.StatusInternalServerError)
		return
	}

	// Per-export overrides: the admin may tweak the exported template
	// name + info memo before download without mutating the stored row.
	// Empty query params keep whatever's in the DB.
	q := r.URL.Query()
	exportName := tmpl.Name
	exportInfo := tmpl.Info
	if v := q.Get("name"); v != "" {
		exportName = v
	}
	if v := q.Get("memo"); v != "" {
		exportInfo = v
	}

	pack := &templatepack.Pack{
		Name:      exportName,
		Info:      exportInfo,
		MainBody:  tmpl.MainBody,
		CSS:       tmpl.CSS,
		EntryBody: tmpl.EntryBody,
	}
	assets, err := h.Store.ListTemplateAssets(r.Context(), id)
	if err != nil {
		log.Printf("admin.templateExport: list assets: %v", err)
	}
	for _, a := range assets {
		abs := filepath.Join(h.TemplateDir, strconv.FormatInt(id, 10), a.Filename)
		data, err := os.ReadFile(abs)
		if err != nil {
			// Missing file on disk but present in DB — log and skip so
			// the rest of the bundle still ships. The admin can re-upload
			// the missing asset after download if needed.
			log.Printf("admin.templateExport: missing asset %s: %v", abs, err)
			continue
		}
		pack.Assets = append(pack.Assets, templatepack.Asset{
			Filename: a.Filename,
			MimeType: a.MimeType,
			Data:     data,
		})
	}

	w.Header().Set("Content-Type", "application/x-sb-template")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="%s.txt"`, safeFilenameFallback(exportName, "template")))
	if err := templatepack.Write(w, pack, time.Now()); err != nil {
		log.Printf("admin.templateExport: write: %v", err)
		// headers already sent — nothing to surface to the client
	}
}

// ---- import ------------------------------------------------------------

type templateImportPageData struct {
	pageBase
	Error string
	OK    string
}

func (h *Handler) templateImportForm(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	renderMain(w, r, pageTemplateImport, templateImportPageData{
		pageBase: pageBase{
			Title:      tr(r, "templates.import.title"),
			ActiveMenu: "templates",
			CSRFToken:  csrf.Token(r.Context()),
			User:       session.UserFrom(r.Context()),
		},
		Error: q.Get("err"),
		OK:    q.Get("ok"),
	})
}

func (h *Handler) templateImportSubmit(w http.ResponseWriter, r *http.Request) {
	if r.ContentLength > 0 && r.ContentLength > h.uploadMaxBytes() {
		http.Error(w, tr(r, "common.error.fileTooLarge"), http.StatusRequestEntityTooLarge)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, h.uploadMaxBytes())

	if r.MultipartForm == nil {
		if err := r.ParseMultipartForm(h.uploadMaxBytes()); err != nil {
			http.Redirect(w, r, root(r)+"/admin/templates/import?err="+urlEscape(tr(r, "templates.import.error.uploadParse")), http.StatusFound)
			return
		}
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		http.Redirect(w, r, root(r)+"/admin/templates/import?err="+urlEscape(tr(r, "templates.import.error.fileMissing")), http.StatusFound)
		return
	}
	defer file.Close()

	pack, err := templatepack.Parse(file)
	if err != nil {
		log.Printf("admin.templateImport: parse: %v", err)
		http.Redirect(w, r, root(r)+"/admin/templates/import?err="+urlEscape(tr(r, "templates.import.error.parseFailed")), http.StatusFound)
		return
	}
	if pack.MainBody == "" {
		http.Redirect(w, r, root(r)+"/admin/templates/import?err="+urlEscape(tr(r, "templates.import.error.baseRequired")), http.StatusFound)
		return
	}

	name := pack.Name
	if name == "" {
		name = fmt.Sprintf("imported-%d", time.Now().Unix())
	}
	newID, err := h.Store.CreateTemplate(r.Context(), domain.Template{
		WID:       h.wid(),
		Name:      name,
		MainBody:  pack.MainBody,
		EntryBody: pack.EntryBody,
		CSS:       pack.CSS,
		Info:      pack.Info,
	})
	if err != nil {
		log.Printf("admin.templateImport: create: %v", err)
		http.Redirect(w, r, root(r)+"/admin/templates/import?err="+urlEscape(tr(r, "templates.import.error.saveFailed")), http.StatusFound)
		return
	}

	// Write every asset to disk under the new template id, and record
	// its metadata row. Failures here aren't fatal — the template itself
	// is already saved; we log and continue so the admin can re-upload
	// missing assets manually if something goes wrong.
	absDir := filepath.Join(h.TemplateDir, strconv.FormatInt(newID, 10))
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		log.Printf("admin.templateImport: mkdir: %v", err)
	} else {
		for _, a := range pack.Assets {
			safe := filepath.Base(a.Filename)
			if safe == "" || safe == "." || safe == ".." {
				continue
			}
			if err := os.WriteFile(filepath.Join(absDir, safe), a.Data, 0o644); err != nil {
				log.Printf("admin.templateImport: write asset %s: %v", safe, err)
				continue
			}
			if _, err := h.Store.CreateOrReplaceTemplateAsset(r.Context(), domain.TemplateAsset{
				TemplateID: newID,
				Filename:   safe,
				MimeType:   a.MimeType,
				SizeBytes:  int64(len(a.Data)),
			}); err != nil {
				log.Printf("admin.templateImport: register asset %s: %v", safe, err)
			}
		}
	}

	http.Redirect(w, r, root(r)+fmt.Sprintf("/admin/templates/%d/edit?ok=imported", newID), http.StatusFound)
}

// safeFilenameFallback returns name when it's printable-ASCII-and-safe,
// otherwise fallback. Keeps non-Latin template names from ending up in
// Content-Disposition where a lot of clients will escape them in
// surprising ways.
func safeFilenameFallback(name, fallback string) string {
	if name == "" {
		return fallback
	}
	for _, r := range name {
		if r < 0x20 || r == '/' || r == '\\' || r == '"' || r > 0x7E {
			return fallback
		}
	}
	return name
}
