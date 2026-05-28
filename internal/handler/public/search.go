package public

import (
	"log"
	"net/http"
	"net/url"

	"github.com/serendipitynz/serenebach/internal/content"
	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/storage/repo"
)

// search handles GET /search?q=<query>&page=<n>. Search is a dynamic
// feature — the static rebuild skips this route entirely (the form is
// only emitted when an operator opts in via static_search_form_enabled,
// and even then the dynamic backend must be reachable from the same
// origin to handle the GET).
func (h *Handler) search(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	weblog, err := h.Store.WeblogByID(ctx, h.WID)
	if err != nil {
		log.Printf("public.search: load weblog: %v", err)
		http.Error(w, "site not configured", http.StatusInternalServerError)
		return
	}

	rawQuery := r.URL.Query().Get("q")
	q := content.TruncateSearchQuery(repo.NormalizeSearch(rawQuery))

	page, ok := parsePageParam(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	pageSize := weblog.EntriesPerPage
	if pageSize <= 0 {
		pageSize = defaultEntryListSize
	}

	hasQuery := repo.HasSearchTerms(q)
	var result *repo.SearchResult
	if hasQuery {
		result, err = h.Store.SearchEntriesPublic(ctx, repo.SearchPublicOptions{
			WID:      h.WID,
			Query:    q,
			Page:     page,
			PageSize: pageSize,
		})
		if err != nil {
			log.Printf("public.search: search: %v", err)
			http.Error(w, "failed to search", http.StatusInternalServerError)
			return
		}
		// Out-of-range page: behave like the list handlers and 404 so a
		// stale URL stops returning an empty result under 200.
		if page > 1 && (result.Total == 0 || (page-1)*pageSize >= result.Total) {
			http.NotFound(w, r)
			return
		}
	}

	tmpl, err := h.Store.ActiveTemplate(ctx, h.WID)
	if err != nil {
		log.Printf("public.search: load template: %v", err)
		http.Error(w, "no active template", http.StatusInternalServerError)
		return
	}

	site := h.buildSite(ctx, *weblog).WithBasePath(root(r))

	var (
		entries []domain.Entry
		total   int
	)
	if result != nil {
		entries = result.Entries
		total = result.Total
	}

	cats, users := h.lookupRefs(ctx, entries, "public.search")
	entryIDs := make([]int64, 0, len(entries))
	for _, e := range entries {
		entryIDs = append(entryIDs, e.ID)
	}
	tagMap, err := h.Store.TagsByEntries(ctx, entryIDs)
	if err != nil {
		log.Printf("public.search: load tags: %v", err)
	}
	profileUsers, err := h.Store.VisibleProfileUsers(ctx, h.WID)
	if err != nil {
		log.Printf("public.search: load profile users: %v", err)
	}
	sidebar := h.loadSidebarData(ctx, "public.search")

	basePath := root(r) + "/search?q=" + url.QueryEscape(q)
	pg := content.Pagination{
		CurrentPage:  page,
		PageSize:     pageSize,
		TotalEntries: int64(total),
		BasePath:     basePath,
	}

	view := content.SearchView{
		Site:         site,
		Template:     tmpl,
		Query:        q,
		Results:      entries,
		Page:         page,
		PageSize:     pageSize,
		TotalCount:   total,
		Categories:   cats,
		Users:        users,
		Tags:         tagMap,
		ProfileUsers: profileUsers,
		Sidebar:      sidebar,
		Pagination:   pg,
		HasQuery:     hasQuery,
	}
	body, err := view.Render()
	if err != nil {
		log.Printf("public.search: render: %v", err)
		http.Error(w, "render error", http.StatusInternalServerError)
		return
	}
	writeHTML(w, body, "public.search")
}

