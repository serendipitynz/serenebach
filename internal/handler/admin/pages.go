package admin

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/serendipitynz/serenebach/internal/csrf"
	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/format"
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
}

func (h *Handler) pageNewForm(w http.ResponseWriter, r *http.Request) {
	page := domain.Page{
		WID:    h.wid(),
		Status: domain.PageDraft,
		Slug:   "/",
	}
	h.renderPageForm(w, r, "/admin/pages/new", page, "", tr(r, "pages.form.titleNew"), "page-new")
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
	h.renderPageForm(w, r, fmt.Sprintf("/admin/pages/%d/edit", id), *p, "", tr(r, "pages.form.titleEditPlain"), "pages")
}

func (h *Handler) renderPageForm(w http.ResponseWriter, r *http.Request, action string, page domain.Page, errMsg, title, activeMenu string) {
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
	})
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

	slug := strings.TrimSpace(r.PostFormValue("slug"))
	if slug == "" {
		return base, tr(r, "pages.form.error.slugRequired")
	}
	// Ensure leading slash.
	if slug[0] != '/' {
		slug = "/" + slug
	}
	// Normalize: no trailing slash except root (which we already reject).
	slug = strings.TrimRight(slug, "/")
	if slug == "" {
		slug = "/"
	}
	base.Slug = slug

	// Validate slug characters: only lowercase alphanumerics, hyphens, and slashes.
	// Each segment (between slashes) must be non-empty and valid.
	segments := strings.Split(slug[1:], "/")
	for _, seg := range segments {
		if seg == "" {
			return base, tr(r, "pages.form.error.slugInvalid")
		}
		for _, c := range seg {
			if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
				continue
			}
			return base, tr(r, "pages.form.error.slugInvalid")
		}
	}

	// Reserved paths collision check.
	firstSegment := segments[0]
	for _, reserved := range []string{"entry", "admin", "img", "category", "archive", "tag", "profile", "feed", "rss.xml", "atom.xml", "llms.txt", "llms-full.txt", "rsd.xml", "style.css", "template", "sb.cgi", "mcp"} {
		if firstSegment == reserved {
			return base, tr(r, "pages.form.error.slugReserved")
		}
	}

	if fmtRaw := strings.TrimSpace(r.PostFormValue("format")); fmtRaw != "" {
		base.Format = string(format.Normalize(fmtRaw))
	}

	if tmplRaw := strings.TrimSpace(r.PostFormValue("template_id")); tmplRaw != "" {
		if v, err := strconv.ParseInt(tmplRaw, 10, 64); err == nil {
			base.TemplateID = v
		}
	}

	statusRaw := r.PostFormValue("status")
	switch statusRaw {
	case "0":
		base.Status = domain.PageDraft
	case "1":
		base.Status = domain.PagePublished
	default:
		return base, tr(r, "pages.form.error.statusInvalid")
	}

	return base, ""
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
		h.renderPageForm(w, r, "/admin/pages/new", page, errMsg, tr(r, "pages.form.titleNew"), "page-new")
		return
	}
	id, err := h.Store.CreatePage(r.Context(), page)
	if err != nil {
		if errors.Is(err, repo.ErrSlugInUse) {
			h.renderPageForm(w, r, "/admin/pages/new", page, tr(r, "pages.form.error.slugInUse"), tr(r, "pages.form.titleNew"), "page-new")
			return
		}
		log.Printf("admin.pageCreate: %v", err)
		http.Error(w, "failed to create page", http.StatusInternalServerError)
		return
	}
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
		h.renderPageForm(w, r, fmt.Sprintf("/admin/pages/%d/edit", id), page, errMsg, tr(r, "pages.form.titleEditPlain"), "pages")
		return
	}
	if err := h.Store.UpdatePage(r.Context(), page); err != nil {
		if errors.Is(err, repo.ErrSlugInUse) {
			h.renderPageForm(w, r, fmt.Sprintf("/admin/pages/%d/edit", id), page, tr(r, "pages.form.error.slugInUse"), tr(r, "pages.form.titleEditPlain"), "pages")
			return
		}
		log.Printf("admin.pageUpdate: save: %v", err)
		http.Error(w, "failed to save page", http.StatusInternalServerError)
		return
	}
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
	h.maybeAutoRebuild(r.Context())
	http.Redirect(w, r, root(r)+"/admin/pages?ok=deleted", http.StatusFound)
}
