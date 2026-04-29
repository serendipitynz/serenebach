package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/serendipitynz/serenebach/internal/csrf"
	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/session"
	"github.com/serendipitynz/serenebach/internal/storage/repo"
)

// mountCategories registers /admin/categories/* routes. Called from
// MountProtected so the RequireUser middleware already wraps this
// group; requireDesign further blocks regular-tier users.
func (h *Handler) mountCategories(r chi.Router) {
	r.Group(func(gr chi.Router) {
		gr.Use(h.requireDesign)
		gr.Get("/categories", h.categoryList)
		gr.Get("/categories/new", h.categoryNewForm)
		gr.Post("/categories/new", h.categoryCreate)
		gr.Get("/categories/{id}/edit", h.categoryEditForm)
		gr.Post("/categories/{id}/edit", h.categoryUpdate)
		gr.Post("/categories/{id}/delete", h.categoryDelete)
		gr.Post("/categories/reorder", h.categoryReorder)
	})
}

// ---- reorder -----------------------------------------------------------

// categoryReorder accepts a JSON body `{"ids": [..]}` with the full list
// of category ids in their new display order. The drag-and-drop UI in
// admin.js is the only caller; CSRF comes in via the X-CSRF-Token header
// so the middleware doesn't need to parse the JSON body.
func (h *Handler) categoryReorder(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "bad JSON", http.StatusBadRequest)
		return
	}
	if len(payload.IDs) == 0 {
		http.Error(w, "empty ids", http.StatusBadRequest)
		return
	}
	if err := h.Store.ReorderCategories(r.Context(), h.wid(), payload.IDs); err != nil {
		log.Printf("admin.categoryReorder: %v", err)
		http.Error(w, "failed to reorder", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

// ---- list --------------------------------------------------------------

// categoryRow decorates domain.Category with the entry count that the
// list page shows in its "記事" column. Populated once per page load.
type categoryRow struct {
	domain.Category
	EntryCount int64
	ParentName string
}

type categoriesListPageData struct {
	pageBase
	Categories []categoryRow
	Flash      string
}

func (h *Handler) categoryList(w http.ResponseWriter, r *http.Request) {
	cats, err := h.Store.AllCategories(r.Context(), h.wid())
	if err != nil {
		log.Printf("admin.categoryList: %v", err)
		http.Error(w, "failed to list categories", http.StatusInternalServerError)
		return
	}
	// Build a (id → name) map so the "parent" column can render the
	// human-readable parent name without another query per row.
	nameByID := make(map[int64]string, len(cats))
	for _, c := range cats {
		nameByID[c.ID] = c.Name
	}

	rows := make([]categoryRow, 0, len(cats))
	for _, c := range cats {
		count, err := h.Store.CountEntriesByCategory(r.Context(), h.wid(), c.ID)
		if err != nil {
			log.Printf("admin.categoryList: count: %v", err)
		}
		rows = append(rows, categoryRow{
			Category:   c,
			EntryCount: count,
			ParentName: nameByID[c.ParentID],
		})
	}

	renderMain(w, r, pageCategoriesList, categoriesListPageData{
		pageBase: pageBase{
			Title:      tr(r, "categories.title"),
			ActiveMenu: "categories",
			CSRFToken:  csrf.Token(r.Context()),
			User:       session.UserFrom(r.Context()),
		},
		Categories: rows,
		Flash:      r.URL.Query().Get("ok"),
	})
}

// ---- new / edit shared form -------------------------------------------

type categoryFormPageData struct {
	pageBase
	Action     string
	Category   domain.Category
	Parents    []domain.Category // candidate parents (excludes self + descendants)
	Templates  []domain.Template // candidate per-category templates (all weblog templates)
	EntryCount int64
	Error      string
}

func (h *Handler) categoryNewForm(w http.ResponseWriter, r *http.Request) {
	h.renderCategoryForm(w, r, "/admin/categories/new",
		domain.Category{WID: h.wid(), ParentID: 0}, 0, "", tr(r, "categories.form.titleNewPlain"), "category-new")
}

func (h *Handler) categoryEditForm(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	c, err := h.Store.CategoryByID(r.Context(), h.wid(), id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		log.Printf("admin.categoryEditForm: %v", err)
		http.Error(w, "failed to load category", http.StatusInternalServerError)
		return
	}
	count, err := h.Store.CountEntriesByCategory(r.Context(), h.wid(), c.ID)
	if err != nil {
		log.Printf("admin.categoryEditForm: count: %v", err)
	}
	h.renderCategoryForm(w, r, fmt.Sprintf("/admin/categories/%d/edit", id),
		*c, count, "", tr(r, "categories.form.titleEditPlain"), "categories")
}

func (h *Handler) renderCategoryForm(
	w http.ResponseWriter, r *http.Request,
	action string, cat domain.Category, entryCount int64,
	errMsg, title, activeMenu string,
) {
	all, err := h.Store.AllCategories(r.Context(), h.wid())
	if err != nil {
		log.Printf("admin.renderCategoryForm: parents: %v", err)
	}
	// Candidate parents = everyone except self and self's descendants so a
	// user can't carve a cycle (A → B → A). Top-level (id=0) stays implicit
	// in the template — the <option value="0"> is hard-coded.
	parents := filterParents(all, cat.ID)
	templates, err := h.Store.ListTemplatesForAdmin(r.Context(), h.wid())
	if err != nil {
		log.Printf("admin.renderCategoryForm: templates: %v", err)
	}
	renderMain(w, r, pageCategoryForm, categoryFormPageData{
		pageBase: pageBase{
			Title:      title,
			ActiveMenu: activeMenu,
			CSRFToken:  csrf.Token(r.Context()),
			User:       session.UserFrom(r.Context()),
		},
		Action:     root(r) + action,
		Category:   cat,
		Parents:    parents,
		Templates:  templates,
		EntryCount: entryCount,
		Error:      errMsg,
	})
}

// filterParents returns the categories that are valid parent candidates
// for the row being edited. "Valid" means: not the row itself, and not any
// descendant of the row (because that would create a cycle). newCatID = 0
// when creating a fresh category (everything is a candidate).
func filterParents(all []domain.Category, selfID int64) []domain.Category {
	if selfID == 0 {
		return all
	}
	// Collect all descendants of selfID.
	desc := map[int64]struct{}{selfID: {}}
	changed := true
	for changed {
		changed = false
		for _, c := range all {
			if _, parentBlocked := desc[c.ParentID]; parentBlocked {
				if _, already := desc[c.ID]; !already {
					desc[c.ID] = struct{}{}
					changed = true
				}
			}
		}
	}
	out := make([]domain.Category, 0, len(all))
	for _, c := range all {
		if _, blocked := desc[c.ID]; blocked {
			continue
		}
		out = append(out, c)
	}
	return out
}

// ---- create / update --------------------------------------------------

// parseCategoryForm pulls the name / slug / parent / sort-order off a
// submitted form, validating along the way. Returns ("") for err on
// success, or a human-facing Japanese string for the form template.
func parseCategoryForm(r *http.Request, base domain.Category) (domain.Category, string) {
	if err := r.ParseForm(); err != nil {
		return base, tr(r, "flash.formParseError")
	}

	base.Name = strings.TrimSpace(r.PostFormValue("name"))
	if base.Name == "" {
		return base, tr(r, "categories.form.error.nameRequired")
	}
	base.Slug = strings.TrimSpace(r.PostFormValue("slug"))

	if raw := strings.TrimSpace(r.PostFormValue("parent_id")); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return base, tr(r, "categories.form.error.parentInvalid")
		}
		if v == base.ID && v != 0 {
			return base, tr(r, "categories.form.error.parentSelf")
		}
		base.ParentID = v
	}

	if raw := strings.TrimSpace(r.PostFormValue("sort_order")); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil {
			return base, tr(r, "categories.form.error.sortOrderInvalid")
		}
		base.SortOrder = v
	}

	base.Description = strings.TrimRight(r.PostFormValue("description"), " \t\r\n")
	base.DescriptionFormat = normaliseDescriptionFormat(r.PostFormValue("description_format"))
	if raw := strings.TrimSpace(r.PostFormValue("template_id")); raw != "" && raw != "0" {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || v < 0 {
			return base, tr(r, "categories.form.error.templateInvalid")
		}
		base.TemplateID = v
	} else {
		base.TemplateID = 0
	}
	return base, ""
}

func (h *Handler) categoryCreate(w http.ResponseWriter, r *http.Request) {
	base := domain.Category{WID: h.wid()}
	cat, errMsg := parseCategoryForm(r, base)
	if errMsg != "" {
		h.renderCategoryForm(w, r, "/admin/categories/new", cat, 0, errMsg, tr(r, "categories.form.titleNewPlain"), "category-new")
		return
	}
	if _, err := h.Store.CreateCategory(r.Context(), cat, cat.SortOrder); err != nil {
		log.Printf("admin.categoryCreate: %v", err)
		http.Error(w, "failed to create category", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, root(r)+"/admin/categories?ok=saved", http.StatusFound)
}

func (h *Handler) categoryUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	existing, err := h.Store.CategoryByID(r.Context(), h.wid(), id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		log.Printf("admin.categoryUpdate: load: %v", err)
		http.Error(w, "failed to load category", http.StatusInternalServerError)
		return
	}
	cat, errMsg := parseCategoryForm(r, *existing)
	if errMsg != "" {
		count, _ := h.Store.CountEntriesByCategory(r.Context(), h.wid(), id)
		h.renderCategoryForm(w, r, fmt.Sprintf("/admin/categories/%d/edit", id), cat, count, errMsg, tr(r, "categories.form.titleEditPlain"), "categories")
		return
	}
	if err := h.Store.UpdateCategory(r.Context(), cat, cat.SortOrder); err != nil {
		log.Printf("admin.categoryUpdate: save: %v", err)
		http.Error(w, "failed to save category", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, root(r)+"/admin/categories?ok=saved", http.StatusFound)
}

func (h *Handler) categoryDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	if err := h.Store.DeleteCategory(r.Context(), h.wid(), id); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		log.Printf("admin.categoryDelete: %v", err)
		http.Error(w, "failed to delete category", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, root(r)+"/admin/categories?ok=deleted", http.StatusFound)
}
