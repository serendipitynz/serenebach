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

// tagSortColumns lists the sortable columns of the admin tag list
// with the direction used on first click. The default landing order
// (name ASC) is the zero value of TagSortKey and not in this list —
// users restore it by visiting /admin/tags without query params.
var tagSortColumns = []struct {
	Key        string
	DefaultDir string
}{
	{"id", "desc"},
	{"name", "asc"},
	{"slug", "asc"},
	{"count", "desc"},
}

type tagsListPageData struct {
	pageBase
	Tags      []repo.TagWithCount
	Flash     string
	SortLinks map[string]sortLink
}

func (h *Handler) tagList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	sortRaw := q.Get("sort")
	dirRaw := q.Get("dir")
	sortKey := repo.ParseTagSortKey(sortRaw)
	sortDir := repo.ParseSortDir(dirRaw)

	tags, err := h.Store.ListTagsForAdmin(r.Context(), h.wid(), repo.ListTagsQuery{
		SortBy:  sortKey,
		SortDir: sortDir,
	})
	if err != nil {
		log.Printf("admin.tagList: %v", err)
		http.Error(w, "failed to list tags", http.StatusInternalServerError)
		return
	}

	// Don't echo the synthetic default-name sort back into URLs — the
	// canonical landing page has no ?sort= at all.
	echoSortKey := ""
	if sortRaw != "" {
		echoSortKey = sortKey.String()
	}
	echoSortDir := ""
	if dirRaw != "" {
		echoSortDir = sortDirString(sortDir)
	}
	state := listURLState{
		BasePath: root(r) + "/admin/tags",
		SortKey:  echoSortKey,
		SortDir:  echoSortDir,
	}
	sortLinks := make(map[string]sortLink, len(tagSortColumns))
	for _, col := range tagSortColumns {
		sortLinks[col.Key] = sortLink{
			Href:  state.hrefSort(col.Key, col.DefaultDir),
			Class: state.classFor(col.Key),
		}
	}

	renderMain(w, r, pageTagsList, tagsListPageData{
		pageBase: pageBase{
			Title:      tr(r, "tags.title"),
			ActiveMenu: "tags",
			CSRFToken:  csrf.Token(r.Context()),
			User:       session.UserFrom(r.Context()),
		},
		Tags:      tags,
		Flash:     r.URL.Query().Get("ok"),
		SortLinks: sortLinks,
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
	http.Redirect(w, r, root(r)+"/admin/tags?ok=saved", http.StatusFound)
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
	http.Redirect(w, r, root(r)+"/admin/tags?ok=deleted", http.StatusFound)
}
