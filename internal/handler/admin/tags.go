package admin

import (
	"errors"
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

// mountTags registers /admin/tags/* routes. Tags are created implicitly
// from the entry form, so there is no /admin/tags/new form — only list,
// rename, delete.
func (h *Handler) mountTags(r chi.Router) {
	r.Get("/tags", h.tagList)
	r.Post("/tags/{id}/update", h.tagUpdate)
	r.Post("/tags/{id}/delete", h.tagDelete)
}

// tagRow decorates domain.Tag with the entry count that drives the list
// table's 記事数 column.
type tagRow struct {
	domain.Tag
	EntryCount int64
}

type tagsListPageData struct {
	pageBase
	Tags  []tagRow
	Flash string
}

func (h *Handler) tagList(w http.ResponseWriter, r *http.Request) {
	tags, err := h.Store.AllTags(r.Context(), h.wid())
	if err != nil {
		log.Printf("admin.tagList: %v", err)
		http.Error(w, "failed to list tags", http.StatusInternalServerError)
		return
	}
	rows := make([]tagRow, 0, len(tags))
	for _, t := range tags {
		// Small N expected in practice; once the tag list gets large,
		// swap this for a single GROUP BY query.
		count, err := h.Store.TagEntryCount(r.Context(), t.ID)
		if err != nil {
			log.Printf("admin.tagList: count: %v", err)
		}
		rows = append(rows, tagRow{Tag: t, EntryCount: count})
	}

	renderMain(w, r, pageTagsList, tagsListPageData{
		pageBase: pageBase{
			Title:      tr(r, "tags.title"),
			ActiveMenu: "tags",
			CSRFToken:  csrf.Token(r.Context()),
			User:       session.UserFrom(r.Context()),
		},
		Tags:  rows,
		Flash: r.URL.Query().Get("ok"),
	})
}

func (h *Handler) tagUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.PostFormValue("name"))
	slug := strings.TrimSpace(r.PostFormValue("slug"))
	if name == "" || slug == "" {
		http.Error(w, "name and slug required", http.StatusBadRequest)
		return
	}
	if !repo.IsValidTagSlug(slug) {
		http.Error(w, "invalid slug format", http.StatusBadRequest)
		return
	}
	err = h.Store.UpdateTag(r.Context(), domain.Tag{
		ID: id, WID: h.wid(), Name: name, Slug: slug,
	})
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if errors.Is(err, repo.ErrSlugInUse) {
			http.Error(w, "name or slug already in use by another tag", http.StatusConflict)
			return
		}
		log.Printf("admin.tagUpdate: %v", err)
		http.Error(w, "failed to update tag", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/tags?ok=saved", http.StatusFound)
}

func (h *Handler) tagDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	if err := h.Store.DeleteTag(r.Context(), h.wid(), id); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		log.Printf("admin.tagDelete: %v", err)
		http.Error(w, "failed to delete tag", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/tags?ok=deleted", http.StatusFound)
}
