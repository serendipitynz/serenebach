package admin

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/serendipitynz/serenebach/internal/csrf"
	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/format"
	"github.com/serendipitynz/serenebach/internal/og"
	"github.com/serendipitynz/serenebach/internal/session"
	"github.com/serendipitynz/serenebach/internal/storage/repo"
)

// mountPages registers the /admin/pages/* routes.
func (h *Handler) mountPages(r chi.Router) {
	r.Get("/pages", h.pageList)
	r.Get("/pages/new", h.pageNewForm)
	r.Post("/pages/new", h.pageCreate)
	r.Get("/pages/{id}/edit", h.pageEditForm)
	r.Post("/pages/{id}/edit", h.pageUpdate)
	r.Post("/pages/{id}/delete", h.pageDelete)
	r.Post("/pages/{id}/og", h.pageOGRegenerate)
}

// ---- list ---------------------------------------------------------------

type pageRow struct {
	domain.Page
	TemplateName string
}

type pagesListPageData struct {
	pageBase
	Pages []pageRow
	Flash string
}

func (h *Handler) pageList(w http.ResponseWriter, r *http.Request) {
	pages, err := h.Store.ListPagesForAdmin(r.Context(), h.wid())
	if err != nil {
		log.Printf("admin.pageList: %v", err)
		http.Error(w, "failed to list pages", http.StatusInternalServerError)
		return
	}
	u := session.UserFrom(r.Context())
	if u != nil {
		filtered := make([]domain.Page, 0, len(pages))
		for _, p := range pages {
			if u.CanEditEntry(p.AuthorID) {
				filtered = append(filtered, p)
			}
		}
		pages = filtered
	}
	templates, err := h.Store.ListTemplatesForAdmin(r.Context(), h.wid())
	if err != nil {
		log.Printf("admin.pageList: templates: %v", err)
	}
	tmplMap := make(map[int64]string, len(templates))
	for _, t := range templates {
		tmplMap[t.ID] = t.Name
	}
	rows := make([]pageRow, 0, len(pages))
	for _, p := range pages {
		name := ""
		if p.TemplateID != 0 {
			name = tmplMap[p.TemplateID]
		}
		rows = append(rows, pageRow{Page: p, TemplateName: name})
	}
	renderMain(w, r, pagePagesList, pagesListPageData{
		pageBase: pageBase{
			Title:      tr(r, "pages.list.title"),
			ActiveMenu: "pages",
			CSRFToken:  csrf.Token(r.Context()),
			User:       session.UserFrom(r.Context()),
		},
		Pages: rows,
		Flash: r.URL.Query().Get("ok"),
	})
}

// ---- new / edit shared form --------------------------------------------

type pageFormPageData struct {
	pageBase
	Action        string
	Page          domain.Page
	StatusInt     int
	Templates     []domain.Template
	Formats       []formatOption
	CurrentFormat string
	Error         string
	Flash         string
	// OGCardTS is the mtime (unix seconds) of the page's OG card
	// file, used as a cache-busting query param on the preview img.
	// Zero means the file doesn't exist yet.
	OGCardTS int64
}

func (h *Handler) pageNewForm(w http.ResponseWriter, r *http.Request) {
	page := domain.Page{
		WID:    h.wid(),
		Status: domain.PageDraft,
		Slug:   "/",
	}
	h.renderPageForm(w, r, "/admin/pages/new", page, "", tr(r, "pages.form.titleNew"), "page-new", 0)
}

func (h *Handler) pageEditForm(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	u := session.UserFrom(r.Context())
	if u == nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	p, err := h.Store.PageByID(r.Context(), h.wid(), id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		log.Printf("admin.pageEditForm: %v", err)
		http.Error(w, "failed to load page", http.StatusInternalServerError)
		return
	}
	if !u.CanEditEntry(p.AuthorID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var ogCardTS int64
	if h.ImageDir != "" && p.ID > 0 {
		_, absFile, _ := h.pageOGCardPath(p.ID)
		if info, err := os.Stat(absFile); err == nil {
			ogCardTS = info.ModTime().Unix()
		}
	}
	h.renderPageForm(w, r, fmt.Sprintf("/admin/pages/%d/edit", id), *p, "", tr(r, "pages.form.titleEditPlain"), "pages", ogCardTS)
}

func (h *Handler) renderPageForm(w http.ResponseWriter, r *http.Request, action string, page domain.Page, errMsg, title, activeMenu string, ogCardTS int64) {
	templates, err := h.Store.ListTemplatesForAdmin(r.Context(), h.wid())
	if err != nil {
		log.Printf("admin.renderPageForm: templates: %v", err)
	}
	renderMain(w, r, pagePageForm, pageFormPageData{
		pageBase: pageBase{
			Title:      title,
			ActiveMenu: activeMenu,
			CSRFToken:  csrf.Token(r.Context()),
			User:       session.UserFrom(r.Context()),
		},
		Action:        root(r) + action,
		Page:          page,
		StatusInt:     int(page.Status),
		Templates:     templates,
		Formats:       buildFormatOptions(),
		CurrentFormat: string(format.Normalize(page.Format)),
		Error:         errMsg,
		Flash:         r.URL.Query().Get("ok"),
		OGCardTS:      ogCardTS,
	})
}

// validatePageSlug checks that the proposed slug does not collide with
// any existing page slug, either exactly or as a prefix/child.  The
// excludeID argument lets updates skip the page being edited.
func validatePageSlug(ctx context.Context, store *repo.Store, wid int64, slug string, excludeID int64) error {
	pages, err := store.ListPagesForAdmin(ctx, wid)
	if err != nil {
		return err
	}
	for _, p := range pages {
		if p.ID == excludeID {
			continue
		}
		existing := p.Slug
		if existing == slug {
			return repo.ErrSlugInUse
		}
		if strings.HasPrefix(existing, slug+"/") || strings.HasPrefix(slug, existing+"/") {
			return repo.ErrSlugPrefixConflict
		}
	}
	return nil
}

// ---- create / update ---------------------------------------------------

func parsePageForm(r *http.Request, base domain.Page) (domain.Page, string) {
	if err := r.ParseForm(); err != nil {
		return base, tr(r, "flash.formParseError")
	}

	base.Title = strings.TrimSpace(r.PostFormValue("title"))
	base.Body = r.PostFormValue("body")
	if base.Title == "" {
		return base, tr(r, "pages.form.error.titleRequired")
	}

	slug, errMsg := normalisePageSlug(r, r.PostFormValue("slug"))
	// Mirror the pre-refactor behaviour: when the input was non-empty
	// but failed segment / reserved-prefix validation, surface the
	// normalised slug back through base so the re-rendered form shows
	// the user what was rejected. Empty input (= slug-required error)
	// leaves base.Slug at the caller-supplied default — "/" for create,
	// the saved value for edit — same as before.
	if slug != "" {
		base.Slug = slug
	}
	if errMsg != "" {
		return base, errMsg
	}

	if fmtRaw := strings.TrimSpace(r.PostFormValue("format")); fmtRaw != "" {
		base.Format = string(format.Normalize(fmtRaw))
	}

	if tmplRaw := strings.TrimSpace(r.PostFormValue("template_id")); tmplRaw != "" {
		if v, err := strconv.ParseInt(tmplRaw, 10, 64); err == nil {
			base.TemplateID = v
		}
	}

	// Per-page OG background override. Same stored_path convention as
	// the weblog-level field; empty = inherit the site default.
	base.OGBGImagePath = strings.TrimSpace(r.PostFormValue("og_bg_image_path"))

	status, errMsg := parsePageStatus(r, r.PostFormValue("status"))
	if errMsg != "" {
		return base, errMsg
	}
	base.Status = status

	return base, ""
}

// reservedPagePathPrefixes are the first-segment slugs an admin can't
// claim for a /pages/ entry because the public router already owns
// them. Kept narrow on purpose — the admin namespace check is the
// last defence, not the first (chi route order would mostly catch
// these too).
var reservedPagePathPrefixes = []string{
	"entry", "admin", "img", "category", "archive", "tag", "profile",
	"feed", "rss.xml", "atom.xml", "llms.txt", "llms-full.txt",
	"rsd.xml", "style.css", "template", "sb.cgi", "mcp",
}

// normalisePageSlug trims, normalises and validates the submitted
// slug. Returns the cleaned slug plus a translated error message on
// validation failure (callers feed that straight back into the form
// flash). Empty slug is treated as a required-field error.
func normalisePageSlug(r *http.Request, raw string) (slug, errMsg string) {
	slug = strings.TrimSpace(raw)
	if slug == "" {
		return "", tr(r, "pages.form.error.slugRequired")
	}
	if slug[0] != '/' {
		slug = "/" + slug
	}
	// Normalize: no trailing slash except root (which we already reject
	// via the segment validation below).
	slug = strings.TrimRight(slug, "/")
	if slug == "" {
		slug = "/"
	}

	segments := strings.Split(slug[1:], "/")
	for _, seg := range segments {
		if !isValidPageSlugSegment(seg) {
			return slug, tr(r, "pages.form.error.slugInvalid")
		}
	}
	if isReservedPagePathPrefix(segments[0]) {
		return slug, tr(r, "pages.form.error.slugReserved")
	}
	return slug, ""
}

// isValidPageSlugSegment enforces lowercase alphanumerics, hyphens
// only — the same character set as entry slugs.
func isValidPageSlugSegment(seg string) bool {
	if seg == "" {
		return false
	}
	for _, c := range seg {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '-':
		default:
			return false
		}
	}
	return true
}

func isReservedPagePathPrefix(seg string) bool {
	for _, reserved := range reservedPagePathPrefixes {
		if seg == reserved {
			return true
		}
	}
	return false
}

func parsePageStatus(r *http.Request, raw string) (domain.PageStatus, string) {
	switch raw {
	case "0":
		return domain.PageDraft, ""
	case "1":
		return domain.PagePublished, ""
	}
	return 0, tr(r, "pages.form.error.statusInvalid")
}

func (h *Handler) pageCreate(w http.ResponseWriter, r *http.Request) {
	u := session.UserFrom(r.Context())
	base := domain.Page{
		WID:      h.wid(),
		AuthorID: u.ID,
		Status:   domain.PageDraft,
		Slug:     "/",
	}
	page, errMsg := parsePageForm(r, base)
	if errMsg != "" {
		h.renderPageForm(w, r, "/admin/pages/new", page, errMsg, tr(r, "pages.form.titleNew"), "page-new", 0)
		return
	}
	if err := validatePageSlug(r.Context(), h.Store, h.wid(), page.Slug, 0); err != nil {
		var msg string
		switch {
		case errors.Is(err, repo.ErrSlugPrefixConflict):
			msg = tr(r, "pages.form.error.slugPrefixConflict")
		case errors.Is(err, repo.ErrSlugInUse):
			msg = tr(r, "pages.form.error.slugInUse")
		default:
			log.Printf("admin.pageCreate: validate: %v", err)
			http.Error(w, "failed to validate page", http.StatusInternalServerError)
			return
		}
		h.renderPageForm(w, r, "/admin/pages/new", page, msg, tr(r, "pages.form.titleNew"), "page-new", 0)
		return
	}
	id, err := h.Store.CreatePage(r.Context(), page)
	if err != nil {
		if errors.Is(err, repo.ErrSlugInUse) {
			h.renderPageForm(w, r, "/admin/pages/new", page, tr(r, "pages.form.error.slugInUse"), tr(r, "pages.form.titleNew"), "page-new", 0)
			return
		}
		log.Printf("admin.pageCreate: %v", err)
		http.Error(w, "failed to create page", http.StatusInternalServerError)
		return
	}
	page.ID = id
	h.regeneratePageOGCard(r.Context(), page)
	h.maybeAutoRebuild(r.Context())
	http.Redirect(w, r, root(r)+fmt.Sprintf("/admin/pages/%d/edit?ok=saved", id), http.StatusFound)
}

func (h *Handler) pageUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	u := session.UserFrom(r.Context())
	if u == nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	existing, err := h.Store.PageByID(r.Context(), h.wid(), id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		log.Printf("admin.pageUpdate: load: %v", err)
		http.Error(w, "failed to load page", http.StatusInternalServerError)
		return
	}
	if !u.CanEditEntry(existing.AuthorID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	page, errMsg := parsePageForm(r, *existing)
	if errMsg != "" {
		h.renderPageForm(w, r, fmt.Sprintf("/admin/pages/%d/edit", id), page, errMsg, tr(r, "pages.form.titleEditPlain"), "pages", 0)
		return
	}
	if err := validatePageSlug(r.Context(), h.Store, h.wid(), page.Slug, id); err != nil {
		var msg string
		switch {
		case errors.Is(err, repo.ErrSlugPrefixConflict):
			msg = tr(r, "pages.form.error.slugPrefixConflict")
		case errors.Is(err, repo.ErrSlugInUse):
			msg = tr(r, "pages.form.error.slugInUse")
		default:
			log.Printf("admin.pageUpdate: validate: %v", err)
			http.Error(w, "failed to validate page", http.StatusInternalServerError)
			return
		}
		h.renderPageForm(w, r, fmt.Sprintf("/admin/pages/%d/edit", id), page, msg, tr(r, "pages.form.titleEditPlain"), "pages", 0)
		return
	}
	if err := h.Store.UpdatePage(r.Context(), page); err != nil {
		if errors.Is(err, repo.ErrSlugInUse) {
			h.renderPageForm(w, r, fmt.Sprintf("/admin/pages/%d/edit", id), page, tr(r, "pages.form.error.slugInUse"), tr(r, "pages.form.titleEditPlain"), "pages", 0)
			return
		}
		log.Printf("admin.pageUpdate: save: %v", err)
		http.Error(w, "failed to save page", http.StatusInternalServerError)
		return
	}
	h.regeneratePageOGCard(r.Context(), page)
	h.maybeAutoRebuild(r.Context())
	http.Redirect(w, r, root(r)+fmt.Sprintf("/admin/pages/%d/edit?ok=saved", id), http.StatusFound)
}

func (h *Handler) pageDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	u := session.UserFrom(r.Context())
	if u == nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	existing, err := h.Store.PageByID(r.Context(), h.wid(), id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		log.Printf("admin.pageDelete: load: %v", err)
		http.Error(w, "failed to load page", http.StatusInternalServerError)
		return
	}
	if !u.CanDeleteEntry(existing.AuthorID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if err := h.Store.DeletePage(r.Context(), h.wid(), id); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		log.Printf("admin.pageDelete: %v", err)
		http.Error(w, "failed to delete page", http.StatusInternalServerError)
		return
	}
	h.removePageOGCard(id)
	h.maybeAutoRebuild(r.Context())
	http.Redirect(w, r, root(r)+"/admin/pages?ok=deleted", http.StatusFound)
}

// ---- OG card generation for pages ---------------------------------------

// pageOGCardPath returns the on-disk path + public URL for the OG card
// belonging to the given page id. Lives under <ImageDir>/og/ so the
// existing /img/* route + static rebuild mirror automatically cover
// serving and copying the file.
func (h *Handler) pageOGCardPath(pageID int64) (absDir, absFile, urlPath string) {
	absDir = filepath.Join(h.ImageDir, "og")
	name := "page_" + strconv.FormatInt(pageID, 10) + ".png"
	absFile = filepath.Join(absDir, name)
	urlPath = "/img/og/" + name
	return
}

// regeneratePageOGCard writes the card for one page. Failures are logged
// but never surface to the HTTP layer.
func (h *Handler) regeneratePageOGCard(ctx context.Context, page domain.Page) {
	if h.OG == nil || h.ImageDir == "" || !h.AutoOG {
		return
	}
	weblog, err := h.Store.WeblogByID(ctx, h.wid())
	if err != nil {
		log.Printf("admin.regeneratePageOGCard: load weblog: %v", err)
		return
	}
	absDir, absFile, _ := h.pageOGCardPath(page.ID)
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		log.Printf("admin.regeneratePageOGCard: mkdir: %v", err)
		return
	}
	f, err := os.Create(absFile)
	if err != nil {
		log.Printf("admin.regeneratePageOGCard: create: %v", err)
		return
	}
	defer f.Close()
	bgPath := h.resolveOGBG(page.OGBGImagePath, weblog.OGBGImagePath)
	if err := h.OG.RenderCard(f, page.Title, weblog.Title, og.Options{
		BGPath:    bgPath,
		TextColor: weblog.OGTextColor,
	}); err != nil {
		log.Printf("admin.regeneratePageOGCard: render: %v", err)
	}
}

// removePageOGCard best-effort unlinks the card on page delete.
func (h *Handler) removePageOGCard(pageID int64) {
	if h.ImageDir == "" {
		return
	}
	_, absFile, _ := h.pageOGCardPath(pageID)
	_ = os.Remove(absFile)
}

// pageOGRegenerate is the manual "build OG card now" endpoint for pages.
func (h *Handler) pageOGRegenerate(w http.ResponseWriter, r *http.Request) {
	if h.OG == nil || h.ImageDir == "" {
		http.Error(w, "og disabled", http.StatusServiceUnavailable)
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	page, err := h.Store.PageByID(r.Context(), h.wid(), id)
	if err != nil {
		log.Printf("admin.pageOGRegenerate: load: %v", err)
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	u := session.UserFrom(r.Context())
	if u == nil || !u.CanEditEntry(page.AuthorID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	weblog, err := h.Store.WeblogByID(r.Context(), h.wid())
	if err != nil {
		log.Printf("admin.pageOGRegenerate: load weblog: %v", err)
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	absDir, absFile, urlPath := h.pageOGCardPath(page.ID)
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		log.Printf("admin.pageOGRegenerate: mkdir: %v", err)
		http.Error(w, "fs", http.StatusInternalServerError)
		return
	}
	f, err := os.Create(absFile)
	if err != nil {
		log.Printf("admin.pageOGRegenerate: create: %v", err)
		http.Error(w, "fs", http.StatusInternalServerError)
		return
	}
	defer f.Close()
	bgPath := h.resolveOGBG(page.OGBGImagePath, weblog.OGBGImagePath)
	if err := h.OG.RenderCard(f, page.Title, weblog.Title, og.Options{
		BGPath:    bgPath,
		TextColor: weblog.OGTextColor,
	}); err != nil {
		log.Printf("admin.pageOGRegenerate: render: %v", err)
		http.Error(w, "render", http.StatusInternalServerError)
		return
	}
	info, _ := f.Stat()
	var ts int64
	if info != nil {
		ts = info.ModTime().Unix()
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	fmt.Fprintf(w, `{"ok":true,"url":%q,"ts":%d}`, root(r)+urlPath, ts)
}
