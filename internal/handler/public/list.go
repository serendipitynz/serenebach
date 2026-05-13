package public

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/serendipitynz/serenebach/internal/content"
	"github.com/serendipitynz/serenebach/internal/csrf"
	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/storage/repo"
)

const defaultEntryListSize = 10

// renderList is the shared "load weblog + template, enrich with category/user
// maps, render ListView" tail used by home/category/archive handlers. The
// caller supplies the already-filtered entry slice and an optional PageTitle.
// `useArchiveTemplate` routes category + archive pages through the pinned
// archive template (when configured via デザイン設定 > 設定); home pages
// leave it false and always use the active template.
func (h *Handler) renderList(w http.ResponseWriter, r *http.Request, entries []domain.Entry, pageTitle, logTag string, useArchiveTemplate bool, cat *domain.Category, mode, modeCtx string, pg content.Pagination) {
	ctx := r.Context()

	weblog, err := h.Store.WeblogByID(ctx, h.WID)
	if err != nil {
		log.Printf("%s: load weblog: %v", logTag, err)
		http.Error(w, "site not configured", http.StatusInternalServerError)
		return
	}
	preview := previewFromRequest(r)
	tmpl, err := h.resolveListTemplate(ctx, logTag, preview, cat, useArchiveTemplate, weblog.ArchiveTemplateID)
	if err != nil {
		log.Printf("%s: load template: %v", logTag, err)
		http.Error(w, "no active template", http.StatusInternalServerError)
		return
	}
	if preview.Active() {
		markPreviewResponse(w)
	}

	cats, users := h.lookupRefs(ctx, entries, logTag)
	entryIDs := make([]int64, 0, len(entries))
	for _, e := range entries {
		entryIDs = append(entryIDs, e.ID)
	}
	tagMap, err := h.Store.TagsByEntries(ctx, entryIDs)
	if err != nil {
		log.Printf("%s: load tags: %v", logTag, err)
	}
	profileUsers, err := h.Store.VisibleProfileUsers(ctx, h.WID)
	if err != nil {
		log.Printf("%s: load profile users: %v", logTag, err)
	}
	sidebar := h.loadSidebarData(ctx, logTag)

	view := content.ListView{
		Site:         h.buildSite(ctx, *weblog).WithBasePath(root(r)),
		Template:     tmpl,
		Entries:      entries,
		Categories:   cats,
		Users:        users,
		Tags:         tagMap,
		Category:     cat,
		ProfileUsers: profileUsers,
		Sidebar:      sidebar,
		Pagination:   pg,
		PageTitle:    pageTitle,
		Mode:         mode,
		ModeContext:  modeCtx,
		CSRFToken:    csrf.Token(r.Context()),
	}
	body, err := view.Render()
	if err != nil {
		log.Printf("%s: render: %v", logTag, err)
		http.Error(w, "render error", http.StatusInternalServerError)
		return
	}
	writeHTML(w, body, logTag)
}

// resolveListTemplate picks the template the list page will render
// with. Admin preview overrides every other pin (checked first so the
// operator's explicit request always wins), then a per-category pin
// (cat.TemplateID), then the archive or active template via
// pickTemplate. Stale / missing pins log and fall through rather than
// erroring out the page — operators see the fallback in admin anyway.
func (h *Handler) resolveListTemplate(ctx context.Context, logTag string, preview previewOverride, cat *domain.Category, useArchiveTemplate bool, archiveTemplateID int64) (*domain.Template, error) {
	if preview.TemplateID > 0 {
		if t, err := h.Store.TemplateByID(ctx, h.WID, preview.TemplateID); err == nil {
			return t, nil
		} else {
			log.Printf("%s: preview template %d missing, falling back: %v", logTag, preview.TemplateID, err)
		}
	}
	if cat != nil && cat.TemplateID != 0 {
		if t, err := h.Store.TemplateByID(ctx, h.WID, cat.TemplateID); err == nil {
			return t, nil
		} else {
			log.Printf("%s: category template pin %d missing, falling back: %v", logTag, cat.TemplateID, err)
		}
	}
	var pinID int64
	if useArchiveTemplate {
		pinID = archiveTemplateID
	}
	return h.pickTemplate(ctx, pinID)
}

// pickTemplate resolves the template to render with. When pinID is non-zero
// it tries that template first; otherwise it falls through to the
// currently-active template. Tolerant of a stale pin: if the referenced row
// is gone we log and fall back to active rather than erroring out the page.
func (h *Handler) pickTemplate(ctx context.Context, pinID int64) (*domain.Template, error) {
	if pinID != 0 {
		if t, err := h.Store.TemplateByID(ctx, h.WID, pinID); err == nil {
			return t, nil
		} else {
			log.Printf("public.pickTemplate: pin %d missing, falling back: %v", pinID, err)
		}
	}
	return h.Store.ActiveTemplate(ctx, h.WID)
}

// defaultSidebarLatestLimit caps how many entries / comments land in
// the SB3 sidebar blocks (`{latest_entry_list}`, `{recent_comment_list}`).
// 5 matches SB3's shipped defaults.
const defaultSidebarLatestLimit = 5

// loadSidebarData pre-fetches every input the SB3 sidebar blocks need
// in one place, so renderList / entry both hand a populated
// content.SidebarData to the view. Failures are logged and collapsed
// to empty slices — a missing sidebar is always better than a 500.
func (h *Handler) loadSidebarData(ctx context.Context, logTag string) content.SidebarData {
	var out content.SidebarData
	if periods, err := h.Store.ArchivePeriodsWithCounts(ctx, h.WID, h.tz()); err == nil {
		out.Archives = periods
	} else {
		log.Printf("%s: archives: %v", logTag, err)
	}
	if cats, err := h.Store.AllCategories(ctx, h.WID); err == nil {
		tree := make([]content.SidebarCategory, 0, len(cats))
		for _, c := range cats {
			count, err := h.Store.CountEntriesByCategory(ctx, h.WID, c.ID)
			if err != nil {
				log.Printf("%s: category count: %v", logTag, err)
			}
			tree = append(tree, content.SidebarCategory{Category: c, Count: count})
		}
		out.CategoryTree = tree
	} else {
		log.Printf("%s: categories: %v", logTag, err)
	}
	if msgs, err := h.Store.RecentApprovedMessages(ctx, h.WID, defaultSidebarLatestLimit); err == nil {
		out.RecentComments = msgs
	} else {
		log.Printf("%s: recent comments: %v", logTag, err)
	}
	if latest, err := h.Store.RecentPublishedEntries(ctx, h.WID, defaultSidebarLatestLimit); err == nil {
		out.LatestEntries = latest
	} else {
		log.Printf("%s: latest entries: %v", logTag, err)
	}
	if links, err := h.Store.VisibleLinks(ctx, h.WID); err == nil {
		out.Links = links
	} else {
		log.Printf("%s: links: %v", logTag, err)
	}
	return out
}

func (h *Handler) lookupRefs(ctx context.Context, entries []domain.Entry, logTag string) (map[int64]domain.Category, map[int64]domain.User) {
	catIDs := make([]int64, 0, len(entries))
	userIDs := make([]int64, 0, len(entries))
	for _, e := range entries {
		catIDs = append(catIDs, e.CategoryID)
		userIDs = append(userIDs, e.AuthorID)
	}
	cats, err := h.Store.CategoriesByIDs(ctx, catIDs)
	if err != nil {
		log.Printf("%s: load categories: %v", logTag, err)
	}
	users, err := h.Store.UsersByIDs(ctx, userIDs)
	if err != nil {
		log.Printf("%s: load users: %v", logTag, err)
	}
	return cats, users
}

func (h *Handler) home(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	page, ok := parsePageParam(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	size, asc := h.listTuning(ctx)
	total, err := h.Store.CountPublishedEntries(ctx, h.WID)
	if err != nil {
		log.Printf("public.home: count: %v", err)
		http.Error(w, "failed to count entries", http.StatusInternalServerError)
		return
	}
	pg, offset, ok := paginationFor(page, size, total, root(r)+"/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	entries, err := h.Store.RecentPublishedEntriesPage(ctx, h.WID, size, offset)
	if err != nil {
		log.Printf("public.home: load entries: %v", err)
		http.Error(w, "failed to load entries", http.StatusInternalServerError)
		return
	}
	if asc {
		reverseEntries(entries)
	}
	h.renderList(w, r, entries, "", "public.home", false, nil, "page", "", pg)
}

// listTuning reads the weblog's display-size + sort preferences. Falls
// back to (defaultEntryListSize, false) on any error so a missing
// weblog row still produces a page — preferring degraded UX over a 500.
func (h *Handler) listTuning(ctx context.Context) (size int, sortAsc bool) {
	weblog, err := h.Store.WeblogByID(ctx, h.WID)
	if err != nil {
		return defaultEntryListSize, false
	}
	size = weblog.EntriesPerPage
	if size <= 0 {
		size = defaultEntryListSize
	}
	return size, weblog.EntrySortOrder == "asc"
}

// reverseEntries flips an entry slice in place so the "日付の古いもの
// を上に" setting ("oldest on top") takes effect. Kept as a separate
// helper so the per-handler call site stays one readable line.
func reverseEntries(es []domain.Entry) {
	for i, j := 0, len(es)-1; i < j; i, j = i+1, j-1 {
		es[i], es[j] = es[j], es[i]
	}
}

// parsePageParam reads `?page=N` off the request URL, defaulting to 1
// when missing. Returns (page, ok) — ok=false signals the caller
// should 404: N parses but is < 1, OR N is non-numeric. Values
// past the last page don't 404 here (the count isn't known yet); the
// handler checks that after computing the total.
func parsePageParam(r *http.Request) (int, bool) {
	raw := r.URL.Query().Get("page")
	if raw == "" {
		return 1, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 0, false
	}
	return n, true
}

// paginationFor builds content.Pagination for a list-page render and
// reports (offset, ok). ok=false signals an out-of-range page —
// handlers respond with 404 so ?page=999 on a 3-page blog doesn't
// show an empty list under a successful status.
func paginationFor(page, size int, total int64, basePath string) (content.Pagination, int, bool) {
	pg := content.Pagination{
		CurrentPage:  page,
		PageSize:     size,
		TotalEntries: total,
		BasePath:     basePath,
	}
	if page > 1 && page > pg.PageCount() {
		return pg, 0, false
	}
	return pg, (page - 1) * size, true
}

func (h *Handler) category(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	cat, err := h.Store.CategoryByID(ctx, h.WID, id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		log.Printf("public.category: load category: %v", err)
		http.Error(w, "failed to load category", http.StatusInternalServerError)
		return
	}
	page, ok := parsePageParam(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	size, asc := h.listTuning(ctx)
	total, err := h.Store.CountPublishedEntriesByCategory(ctx, h.WID, cat.ID)
	if err != nil {
		log.Printf("public.category: count: %v", err)
		http.Error(w, "failed to count entries", http.StatusInternalServerError)
		return
	}
	basePath := root(r) + "/category/" + strconv.FormatInt(cat.ID, 10) + "/"
	pg, offset, ok := paginationFor(page, size, total, basePath)
	if !ok {
		http.NotFound(w, r)
		return
	}
	entries, err := h.Store.PublishedEntriesByCategoryPage(ctx, h.WID, cat.ID, size, offset)
	if err != nil {
		log.Printf("public.category: load entries: %v", err)
		http.Error(w, "failed to load entries", http.StatusInternalServerError)
		return
	}
	if asc {
		reverseEntries(entries)
	}
	pageTitle := "Category: " + cat.Name
	h.renderList(w, r, entries, pageTitle, "public.category", true, cat, "cat", strconv.FormatInt(cat.ID, 10), pg)
}

func (h *Handler) tag(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	slug := chi.URLParam(r, "slug")
	t, err := h.Store.TagBySlug(ctx, h.WID, slug)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		log.Printf("public.tag: load tag: %v", err)
		http.Error(w, "failed to load tag", http.StatusInternalServerError)
		return
	}
	page, ok := parsePageParam(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	size, asc := h.listTuning(ctx)
	total, err := h.Store.CountPublishedEntriesByTag(ctx, h.WID, t.ID)
	if err != nil {
		log.Printf("public.tag: count: %v", err)
		http.Error(w, "failed to count entries", http.StatusInternalServerError)
		return
	}
	basePath := root(r) + "/tag/" + t.Slug + "/"
	pg, offset, ok := paginationFor(page, size, total, basePath)
	if !ok {
		http.NotFound(w, r)
		return
	}
	entries, err := h.Store.PublishedEntriesByTagPage(ctx, h.WID, t.ID, size, offset)
	if err != nil {
		log.Printf("public.tag: load entries: %v", err)
		http.Error(w, "failed to load entries", http.StatusInternalServerError)
		return
	}
	if asc {
		reverseEntries(entries)
	}
	// Tag pages render through the archive template when one is pinned
	// — same convention as categories and date archives, matching reader
	// expectation that "browse by …" pages share one look.
	h.renderList(w, r, entries, "Tag: "+t.Name, "public.tag", true, nil, "tag", t.Slug, pg)
}

func (h *Handler) archiveYear(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	year, ok := parseYear(chi.URLParam(r, "year"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	page, ok := parsePageParam(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	size, asc := h.listTuning(ctx)
	from := time.Date(year, time.January, 1, 0, 0, 0, 0, h.tz())
	to := from.AddDate(1, 0, 0)
	total, err := h.Store.CountPublishedEntriesInRange(ctx, h.WID, from, to)
	if err != nil {
		log.Printf("public.archiveYear: count: %v", err)
		http.Error(w, "failed to count entries", http.StatusInternalServerError)
		return
	}
	basePath := root(r) + "/archive/" + strconv.Itoa(year) + "/"
	pg, offset, ok := paginationFor(page, size, total, basePath)
	if !ok {
		http.NotFound(w, r)
		return
	}
	entries, err := h.Store.PublishedEntriesInRangePage(ctx, h.WID, from, to, size, offset)
	if err != nil {
		log.Printf("public.archiveYear: %v", err)
		http.Error(w, "failed to load entries", http.StatusInternalServerError)
		return
	}
	if asc {
		reverseEntries(entries)
	}
	pageTitle := "Archive: " + strconv.Itoa(year)
	h.renderList(w, r, entries, pageTitle, "public.archiveYear", true, nil, "arc", strconv.Itoa(year), pg)
}

func (h *Handler) archiveMonth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	year, ok1 := parseYear(chi.URLParam(r, "year"))
	month, ok2 := parseMonth(chi.URLParam(r, "month"))
	if !ok1 || !ok2 {
		http.NotFound(w, r)
		return
	}
	page, ok := parsePageParam(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	size, asc := h.listTuning(ctx)
	from := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, h.tz())
	to := from.AddDate(0, 1, 0)
	total, err := h.Store.CountPublishedEntriesInRange(ctx, h.WID, from, to)
	if err != nil {
		log.Printf("public.archiveMonth: count: %v", err)
		http.Error(w, "failed to count entries", http.StatusInternalServerError)
		return
	}
	basePath := root(r) + "/archive/" + strconv.Itoa(year) + "/" + padMonth(month) + "/"
	pg, offset, ok := paginationFor(page, size, total, basePath)
	if !ok {
		http.NotFound(w, r)
		return
	}
	entries, err := h.Store.PublishedEntriesInRangePage(ctx, h.WID, from, to, size, offset)
	if err != nil {
		log.Printf("public.archiveMonth: %v", err)
		http.Error(w, "failed to load entries", http.StatusInternalServerError)
		return
	}
	if asc {
		reverseEntries(entries)
	}
	pageTitle := "Archive: " + strconv.Itoa(year) + "/" + padMonth(month)
	h.renderList(w, r, entries, pageTitle, "public.archiveMonth", true, nil, "arc", fmt.Sprintf("%04d%s", year, padMonth(month)), pg)
}
