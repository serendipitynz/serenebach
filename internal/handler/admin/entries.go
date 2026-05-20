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
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/serendipitynz/serenebach/internal/csrf"
	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/format"
	"github.com/serendipitynz/serenebach/internal/og"
	"github.com/serendipitynz/serenebach/internal/session"
	"github.com/serendipitynz/serenebach/internal/storage/repo"
)

// adminEntryPageSize is the per-page row count for the admin entry
// list. Aligned with the comments list pager so the UI feels
// consistent; images stay at their grid-friendly 48.
const adminEntryPageSize = 50

// entrySortColumns lists the sortable columns of the admin entry list
// along with the direction used the first time a user clicks an
// otherwise-inactive column header (subsequent clicks toggle). Time
// columns default to descending — newest first — while text columns
// default to ascending.
var entrySortColumns = []struct {
	Key        string
	DefaultDir string
}{
	{"id", "desc"},
	{"title", "asc"},
	{"slug", "asc"},
	{"category", "asc"},
	{"posted", "desc"},
	{"status", "asc"},
}

// mountEntries registers the /admin/entries/* routes. Called from
// MountProtected so the RequireUser middleware already wraps this group.
func (h *Handler) mountEntries(r chi.Router) {
	r.Get("/entries", h.entryList)
	r.Get("/entries/new", h.entryNewForm)
	r.Post("/entries/new", h.entryCreate)
	r.Get("/entries/{id}/edit", h.entryEditForm)
	r.Post("/entries/{id}/edit", h.entryUpdate)
	r.Post("/entries/{id}/delete", h.entryDelete)
	r.Post("/entries/{id}/og", h.entryOGRegenerate)
	r.Post("/entries/{id}/pin", h.entryPin)
	r.Delete("/entries/{id}/pin", h.entryPin)
}

// loadEntryForEdit reads the entry pointed to by the {id} URL param,
// enforces the "can edit this entry" permission, and writes the
// appropriate HTTP error response on failure. Returns (entry, user, true)
// on success; on failure it has already written to w and the caller
// must return immediately.
//
// Permission rule mirrors u.CanEditEntry: power+admin tier can edit any
// entry, regular tier can edit only their own.
func (h *Handler) loadEntryForEdit(w http.ResponseWriter, r *http.Request) (*domain.Entry, *domain.User, bool) {
	return h.loadEntryWith(w, r, func(u *domain.User, authorID int64) bool {
		return u.CanEditEntry(authorID)
	})
}

// loadEntryForDelete is loadEntryForEdit with CanDeleteEntry. Delete
// keeps its own helper because the permission check is the only thing
// that differs and a flag-style API would obscure the intent at the
// call sites.
func (h *Handler) loadEntryForDelete(w http.ResponseWriter, r *http.Request) (*domain.Entry, *domain.User, bool) {
	return h.loadEntryWith(w, r, func(u *domain.User, authorID int64) bool {
		return u.CanDeleteEntry(authorID)
	})
}

// loadEntryWith is the shared body of loadEntryForEdit / loadEntryForDelete.
// Kept private so callers stick to the named edit/delete variants.
func (h *Handler) loadEntryWith(w http.ResponseWriter, r *http.Request, allow func(*domain.User, int64) bool) (*domain.Entry, *domain.User, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return nil, nil, false
	}
	e, err := h.Store.EntryByID(r.Context(), h.wid(), id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return nil, nil, false
		}
		log.Printf("admin.loadEntry: %v", err)
		http.Error(w, "failed to load entry", http.StatusInternalServerError)
		return nil, nil, false
	}
	u := session.UserFrom(r.Context())
	if !allow(u, e.AuthorID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return nil, nil, false
	}
	return e, u, true
}

// ---- list ---------------------------------------------------------------

// entryRow pairs a domain.Entry with its category name so the admin
// listing can surface the category column without an N+1 per render.
type entryRow struct {
	domain.Entry
	CategoryName string
}

type entriesListPageData struct {
	pageBase
	Entries    []entryRow
	Flash      string
	Search     string
	SortLinks  map[string]sortLink
	Pager      pagerView
	TotalCount int64
}

func (h *Handler) entryList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	u := session.UserFrom(r.Context())

	search := repo.NormalizeSearch(q.Get("q"))
	sortKey := repo.ParseEntrySortKey(q.Get("sort"))
	sortDir := repo.ParseSortDir(q.Get("dir"))

	listQ := repo.ListEntriesQuery{
		Search:  search,
		SortBy:  sortKey,
		SortDir: sortDir,
		Limit:   adminEntryPageSize,
	}
	// Regular-tier authors only see their own entries; pushing this
	// into SQL keeps pagination correct (post-filtering would yield
	// short pages and a wrong totalCount).
	if u != nil && u.Role == domain.RoleRegular {
		ownerID := u.ID
		listQ.OwnerID = &ownerID
	}

	total, err := h.Store.CountEntriesForAdmin(r.Context(), h.wid(), listQ)
	if err != nil {
		log.Printf("admin.entryList: count: %v", err)
		http.Error(w, "failed to list entries", http.StatusInternalServerError)
		return
	}
	page, totalPages, offset := listPagination(q.Get("page"), total, adminEntryPageSize)
	listQ.Offset = offset

	entries, err := h.Store.ListEntriesForAdmin(r.Context(), h.wid(), listQ)
	if err != nil {
		log.Printf("admin.entryList: %v", err)
		http.Error(w, "failed to list entries", http.StatusInternalServerError)
		return
	}

	catIDs := make([]int64, 0, len(entries))
	for _, e := range entries {
		catIDs = append(catIDs, e.CategoryID)
	}
	cats, err := h.Store.CategoriesByIDs(r.Context(), catIDs)
	if err != nil {
		log.Printf("admin.entryList: categories: %v", err)
	}
	rows := make([]entryRow, 0, len(entries))
	for _, e := range entries {
		name := ""
		if c, ok := cats[e.CategoryID]; ok {
			name = c.Name
		}
		rows = append(rows, entryRow{Entry: e, CategoryName: name})
	}

	state := listURLState{
		BasePath: root(r) + "/admin/entries",
		Search:   search,
		SortKey:  sortKey.String(),
		SortDir:  sortDirString(sortDir),
		Page:     page,
	}
	sortLinks := make(map[string]sortLink, len(entrySortColumns))
	for _, col := range entrySortColumns {
		sortLinks[col.Key] = sortLink{
			Href:  state.hrefSort(col.Key, col.DefaultDir),
			Class: state.classFor(col.Key),
		}
	}
	prev, next := pagerNeighbours(page, totalPages)

	renderMain(w, r, pageEntriesList, entriesListPageData{
		pageBase: pageBase{
			Title:      tr(r, "entries.list.title"),
			ActiveMenu: "entries",
			CSRFToken:  csrf.Token(r.Context()),
			User:       u,
		},
		Entries:    rows,
		Flash:      q.Get("ok"),
		Search:     search,
		SortLinks:  sortLinks,
		Pager: pagerView{
			Page:       page,
			TotalPages: totalPages,
			PrevHref:   state.hrefPage(prev),
			NextHref:   state.hrefPage(next),
		},
		TotalCount: total,
	})
}

// sortDirString is the inverse of repo.ParseSortDir — returns the
// lowercase token used in URL params and CSS class names.
func sortDirString(d repo.SortDir) string {
	if d == repo.SortAsc {
		return "asc"
	}
	return "desc"
}

// ---- new / edit shared form --------------------------------------------

// formatOption is one row in the body-format dropdown. Built from
// format.Supported so a new formatter only has to register itself there.
type formatOption struct {
	Value string
	Label string
	Hint  string
}

type entryFormPageData struct {
	pageBase
	Action        string
	Entry         domain.Entry
	StatusInt     int
	PostedAtLocal string
	Categories    []domain.Category
	Formats       []formatOption
	CurrentFormat string
	// TagsCSV is the comma-separated form the user sees in the tag
	// input. On edit it's computed from the entry's current tags; on
	// a validation-failed save we echo back whatever the user typed
	// so the rejection isn't also data loss.
	TagsCSV string
	Error   string
	Flash   string
	// OGCardTS is the mtime (unix seconds) of the entry's OG card
	// file, used as a cache-busting query param on the preview img.
	// Zero means the file doesn't exist yet.
	OGCardTS int64
	// CommentsAllowedOverall is true when the weblog-level comment
	// mode is anything other than "closed". The form uses this to
	// decide whether to render the per-entry "accept comments"
	// checkbox; when comments are closed site-wide the per-entry
	// flag is meaningless, so we hide the control.
	CommentsAllowedOverall bool
}

// buildFormatOptions exposes the formatter catalogue to the template with
// one call per render. Cheap to rebuild on every call — the list is tiny.
func buildFormatOptions() []formatOption {
	out := make([]formatOption, 0, len(format.Supported))
	for _, s := range format.Supported {
		out = append(out, formatOption{Value: string(s.Kind), Label: s.Label, Hint: s.Hint})
	}
	return out
}

func (h *Handler) entryNewForm(w http.ResponseWriter, r *http.Request) {
	u := session.UserFrom(r.Context())
	now := time.Now()
	entry := domain.Entry{
		WID:            h.wid(),
		AuthorID:       u.ID,
		CategoryID:     domain.Uncategorized,
		Status:         domain.EntryDraft,
		PostedAt:       now,
		AcceptComments: true,
	}
	h.renderEntryForm(w, r, "/admin/entries/new", entry, "", "", tr(r, "entries.form.titleNew"), "entry-new", 0)
}

func (h *Handler) entryEditForm(w http.ResponseWriter, r *http.Request) {
	e, _, ok := h.loadEntryForEdit(w, r)
	if !ok {
		return
	}
	// Pre-fill the tag input with the entry's current tags. Failure to
	// load tags is logged but not fatal — better to render the form
	// with an empty tag field than to 500 the edit page.
	tags, err := h.Store.TagsByEntry(r.Context(), e.ID)
	if err != nil {
		log.Printf("admin.entryEditForm: tags: %v", err)
	}
	var ogCardTS int64
	if h.ImageDir != "" && e.ID > 0 {
		ogPath := filepath.Join(h.ImageDir, "og", strconv.FormatInt(e.ID, 10)+".png")
		if info, err := os.Stat(ogPath); err == nil {
			ogCardTS = info.ModTime().Unix()
		}
	}
	h.renderEntryForm(w, r, fmt.Sprintf("/admin/entries/%d/edit", e.ID), *e, tagsToCSV(tags), "", tr(r, "entries.form.titleEditPlain"), "entries", ogCardTS)
}

// tagsToCSV renders a tag slice as the comma-separated input value used
// by the admin entry form. Kept package-private so the form template
// only sees a string.
func tagsToCSV(tags []domain.Tag) string {
	if len(tags) == 0 {
		return ""
	}
	names := make([]string, 0, len(tags))
	for _, t := range tags {
		names = append(names, t.Name)
	}
	return strings.Join(names, ", ")
}

// parseTagNames turns the comma-separated "tags" input into a deduped
// slice of trimmed names, plus the exact CSV the user typed so the
// error-rerender echoes it back. Empty items are dropped.
func parseTagNames(raw string) (names []string, csv string) {
	csv = strings.TrimSpace(raw)
	if csv == "" {
		return nil, ""
	}
	seen := map[string]struct{}{}
	for _, p := range strings.Split(csv, ",") {
		n := strings.TrimSpace(p)
		if n == "" {
			continue
		}
		if _, dup := seen[n]; dup {
			continue
		}
		seen[n] = struct{}{}
		names = append(names, n)
	}
	return names, csv
}

func (h *Handler) renderEntryForm(w http.ResponseWriter, r *http.Request, action string, entry domain.Entry, tagsCSV, errMsg, title, activeMenu string, ogCardTS int64) {
	cats, err := h.Store.AllCategories(r.Context(), h.wid())
	if err != nil {
		log.Printf("admin.renderEntryForm: %v", err)
	}
	commentsAllowed := true
	if weblog, err := h.Store.WeblogByID(r.Context(), h.wid()); err == nil {
		commentsAllowed = weblog.CommentMode != domain.CommentClosed
	} else {
		log.Printf("admin.renderEntryForm: weblog: %v", err)
	}
	renderMain(w, r, pageEntryForm, entryFormPageData{
		pageBase: pageBase{
			Title:      title,
			ActiveMenu: activeMenu,
			CSRFToken:  csrf.Token(r.Context()),
			User:       session.UserFrom(r.Context()),
		},
		Action:                 root(r) + action,
		Entry:                  entry,
		StatusInt:              int(entry.Status),
		PostedAtLocal:          entry.PostedAt.In(h.tz()).Format("2006-01-02T15:04"),
		Categories:             cats,
		Formats:                buildFormatOptions(),
		CurrentFormat:          string(format.Normalize(entry.Format)),
		TagsCSV:                tagsCSV,
		Error:                  errMsg,
		Flash:                  r.URL.Query().Get("ok"),
		OGCardTS:               ogCardTS,
		CommentsAllowedOverall: commentsAllowed,
	})
}

// ---- create / update ---------------------------------------------------

func (h *Handler) parseEntryForm(r *http.Request, base domain.Entry) (domain.Entry, string) {
	if err := r.ParseForm(); err != nil {
		return base, tr(r, "flash.formParseError")
	}

	base.Title = strings.TrimSpace(r.PostFormValue("title"))
	base.Body = r.PostFormValue("body")
	base.More = r.PostFormValue("more")
	if base.Title == "" {
		return base, tr(r, "entries.form.error.titleRequired")
	}

	// Slug is optional. Empty means "fall back to /entry/<id>/" — the
	// canonical path in that case. When filled, validate the format here
	// so DB-level uniqueness is the only failure mode left to surface
	// back to the form.
	base.Slug = strings.TrimSpace(r.PostFormValue("slug"))
	if base.Slug != "" && !domain.IsValidSlug(base.Slug) {
		return base, tr(r, "entries.form.error.slugInvalid")
	}

	base.Keywords = normaliseEntryKeywords(r.PostFormValue("keywords"))

	if fmtRaw := strings.TrimSpace(r.PostFormValue("format")); fmtRaw != "" {
		base.Format = string(format.Normalize(fmtRaw))
	}

	// Per-entry OG background override. Same stored_path convention as
	// the weblog-level field; empty = inherit the site default. No
	// validation — unresolvable paths fall back at render.
	base.OGBGImagePath = strings.TrimSpace(r.PostFormValue("og_bg_image_path"))

	// Checkbox: present = pinned, absent = not pinned.
	base.Pinned = r.PostFormValue("pinned") == "1"

	// Per-entry comment acceptance. The hidden `accept_comments_present`
	// marker is only emitted when the form actually rendered the
	// checkbox (i.e. comment_mode is not "closed"); without it we
	// preserve the existing value so a globally-closed save doesn't
	// silently flip the per-entry preference.
	if r.PostFormValue("accept_comments_present") == "1" {
		base.AcceptComments = r.PostFormValue("accept_comments") == "1"
	}

	catID, errMsg := parseEntryCategoryID(r, r.PostFormValue("category_id"), base.CategoryID)
	if errMsg != "" {
		return base, errMsg
	}
	base.CategoryID = catID

	status, errMsg := parseEntryStatus(r, r.PostFormValue("status"))
	if errMsg != "" {
		return base, errMsg
	}
	base.Status = status

	postedAt, errMsg := h.parseEntryPostedAt(r, r.PostFormValue("posted_at"), base.PostedAt)
	if errMsg != "" {
		return base, errMsg
	}
	base.PostedAt = postedAt

	return base, ""
}

// normaliseEntryKeywords trims each comma-separated item, drops empty
// entries, and re-joins with ", " so template output stays consistent
// regardless of how the author spaced the original input.
func normaliseEntryKeywords(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parts := strings.Split(raw, ",")
	cleaned := parts[:0]
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			cleaned = append(cleaned, t)
		}
	}
	return strings.Join(cleaned, ", ")
}

// parseEntryCategoryID accepts the raw category_id form value and
// returns the parsed id (or the fallback when the input is empty).
// Non-numeric input surfaces the localised "categoryInvalid" message
// so the caller can flash it back.
func parseEntryCategoryID(r *http.Request, raw string, fallback int64) (int64, string) {
	if raw == "" {
		return fallback, ""
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return fallback, tr(r, "entries.form.error.categoryInvalid")
	}
	return v, ""
}

func parseEntryStatus(r *http.Request, raw string) (domain.EntryStatus, string) {
	switch raw {
	case "0":
		return domain.EntryDraft, ""
	case "1":
		return domain.EntryPublished, ""
	case "-1":
		return domain.EntryClosed, ""
	}
	return 0, tr(r, "entries.form.error.statusInvalid")
}

// parseEntryPostedAt accepts a "2006-01-02T15:04" datetime-local input
// in the weblog's timezone. Empty input keeps the existing posted_at;
// malformed input surfaces the localised "postedAtInvalid" message.
func (h *Handler) parseEntryPostedAt(r *http.Request, raw string, fallback time.Time) (time.Time, string) {
	if raw == "" {
		return fallback, ""
	}
	t, err := time.ParseInLocation("2006-01-02T15:04", raw, h.tz())
	if err != nil {
		return fallback, tr(r, "entries.form.error.postedAtInvalid")
	}
	return t, ""
}

func (h *Handler) entryCreate(w http.ResponseWriter, r *http.Request) {
	u := session.UserFrom(r.Context())
	base := domain.Entry{
		WID:            h.wid(),
		AuthorID:       u.ID,
		CategoryID:     domain.Uncategorized,
		Status:         domain.EntryDraft,
		PostedAt:       time.Now(),
		AcceptComments: true,
	}
	entry, errMsg := h.parseEntryForm(r, base)
	tagNames, tagsCSV := parseTagNames(r.PostFormValue("tags"))
	if errMsg != "" {
		h.renderEntryForm(w, r, "/admin/entries/new", entry, tagsCSV, errMsg, tr(r, "entries.form.titleNew"), "entry-new", 0)
		return
	}
	id, err := h.Store.CreateEntry(r.Context(), entry)
	if err != nil {
		if errors.Is(err, repo.ErrSlugInUse) {
			h.renderEntryForm(w, r, "/admin/entries/new", entry, tagsCSV, tr(r, "entries.form.error.slugInUse"), tr(r, "entries.form.titleNew"), "entry-new", 0)
			return
		}
		log.Printf("admin.entryCreate: %v", err)
		http.Error(w, "failed to create entry", http.StatusInternalServerError)
		return
	}
	entry.ID = id
	if err := h.syncEntryTags(r.Context(), id, tagNames); err != nil {
		log.Printf("admin.entryCreate: tags: %v", err)
	}
	h.regenerateOGCard(r.Context(), entry)
	h.maybeAutoRebuild(r.Context())
	if entry.Status == domain.EntryPublished {
		h.dispatchEntryEvent(r.Context(), "entry.published", entry)
	}
	http.Redirect(w, r, root(r)+fmt.Sprintf("/admin/entries/%d/edit?ok=saved", id), http.StatusFound)
}

// syncEntryTags resolves a list of tag names (creating missing tags)
// and replaces the entry's tag assignment in one shot. Shared between
// create and update paths so their contract stays identical.
func (h *Handler) syncEntryTags(ctx context.Context, entryID int64, names []string) error {
	tags, err := h.Store.EnsureTagsByName(ctx, h.wid(), names)
	if err != nil {
		return fmt.Errorf("ensure tags: %w", err)
	}
	ids := make([]int64, 0, len(tags))
	for _, t := range tags {
		ids = append(ids, t.ID)
	}
	return h.Store.SetEntryTags(ctx, entryID, ids)
}

func (h *Handler) entryUpdate(w http.ResponseWriter, r *http.Request) {
	existing, _, ok := h.loadEntryForEdit(w, r)
	if !ok {
		return
	}
	id := existing.ID
	entry, errMsg := h.parseEntryForm(r, *existing)
	tagNames, tagsCSV := parseTagNames(r.PostFormValue("tags"))
	if errMsg != "" {
		h.renderEntryForm(w, r, fmt.Sprintf("/admin/entries/%d/edit", id), entry, tagsCSV, errMsg, tr(r, "entries.form.titleEditPlain"), "entries", 0)
		return
	}
	wasPublished := existing.Status == domain.EntryPublished
	if err := h.Store.UpdateEntry(r.Context(), entry); err != nil {
		if errors.Is(err, repo.ErrSlugInUse) {
			h.renderEntryForm(w, r, fmt.Sprintf("/admin/entries/%d/edit", id), entry, tagsCSV, tr(r, "entries.form.error.slugInUse"), tr(r, "entries.form.titleEditPlain"), "entries", 0)
			return
		}
		log.Printf("admin.entryUpdate: save: %v", err)
		http.Error(w, "failed to save entry", http.StatusInternalServerError)
		return
	}
	if err := h.syncEntryTags(r.Context(), id, tagNames); err != nil {
		log.Printf("admin.entryUpdate: tags: %v", err)
	}
	h.regenerateOGCard(r.Context(), entry)
	h.maybeAutoRebuild(r.Context())
	switch {
	case entry.Status == domain.EntryPublished && !wasPublished:
		h.dispatchEntryEvent(r.Context(), "entry.published", entry)
	case entry.Status == domain.EntryPublished && wasPublished:
		h.dispatchEntryEvent(r.Context(), "entry.updated", entry)
	}
	http.Redirect(w, r, root(r)+fmt.Sprintf("/admin/entries/%d/edit?ok=saved", id), http.StatusFound)
}

func (h *Handler) entryDelete(w http.ResponseWriter, r *http.Request) {
	// Regular-tier authors may only remove their own work; power +
	// admin can delete any entry. Loading the row first lets us
	// compare authorship before we commit to the destructive SQL.
	existing, _, ok := h.loadEntryForDelete(w, r)
	if !ok {
		return
	}
	id := existing.ID
	if err := h.Store.DeleteEntry(r.Context(), h.wid(), id); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		log.Printf("admin.entryDelete: %v", err)
		http.Error(w, "failed to delete entry", http.StatusInternalServerError)
		return
	}
	h.removeOGCard(id)
	h.maybeAutoRebuild(r.Context())
	h.dispatchEntryEvent(r.Context(), "entry.deleted", *existing)
	http.Redirect(w, r, root(r)+"/admin/entries?ok=deleted", http.StatusFound)
}

// ---- OG card generation ------------------------------------------------

// ogCardPath returns the on-disk path + public URL for the OG card
// belonging to the given entry id. Lives under <ImageDir>/og/ so the
// existing /img/* route + static rebuild mirror automatically cover
// serving and copying the file.
func (h *Handler) ogCardPath(entryID int64) (absDir, absFile, urlPath string) {
	absDir = filepath.Join(h.ImageDir, "og")
	name := strconv.FormatInt(entryID, 10) + ".png"
	absFile = filepath.Join(absDir, name)
	urlPath = "/img/og/" + name
	return
}

// regenerateOGCard writes the card for one entry. Failures are logged
// but never surface to the HTTP layer — a template-layer PNG glitch
// shouldn't abort the save/publish path. Background selection picks
// the first non-empty of entry.OGBGImagePath → weblog.OGBGImagePath
// → the embedded default; paths resolve against ImageDir to match the
// uploaded-images storage layout.
func (h *Handler) regenerateOGCard(ctx context.Context, entry domain.Entry) {
	if h.OG == nil || h.ImageDir == "" || !h.AutoOG {
		return
	}
	weblog, err := h.Store.WeblogByID(ctx, h.wid())
	if err != nil {
		log.Printf("admin.regenerateOGCard: load weblog: %v", err)
		return
	}
	absDir, absFile, _ := h.ogCardPath(entry.ID)
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		log.Printf("admin.regenerateOGCard: mkdir: %v", err)
		return
	}
	f, err := os.Create(absFile)
	if err != nil {
		log.Printf("admin.regenerateOGCard: create: %v", err)
		return
	}
	defer f.Close()
	bgPath := h.resolveOGBG(entry.OGBGImagePath, weblog.OGBGImagePath)
	if err := h.OG.RenderCard(f, entry.Title, weblog.Title, og.Options{
		BGPath:    bgPath,
		TextColor: weblog.OGTextColor,
	}); err != nil {
		log.Printf("admin.regenerateOGCard: render: %v", err)
	}
}

// resolveOGBG picks the first non-empty stored_path and returns its
// absolute on-disk path under ImageDir. Empty inputs collapse to ""
// so the renderer knows to use its embedded default.
func (h *Handler) resolveOGBG(entryPath, weblogPath string) string {
	chosen := entryPath
	if chosen == "" {
		chosen = weblogPath
	}
	if chosen == "" || h.ImageDir == "" {
		return ""
	}
	return filepath.Join(h.ImageDir, filepath.FromSlash(chosen))
}

// removeOGCard best-effort unlinks the card on entry delete. A missing
// file is fine; the DB row is already gone so nothing references it.
func (h *Handler) removeOGCard(entryID int64) {
	if h.ImageDir == "" {
		return
	}
	_, absFile, _ := h.ogCardPath(entryID)
	_ = os.Remove(absFile)
}

// entryOGRegenerate is the manual "build OG card now" endpoint.
// CGI deployments disable AutoOG to avoid OOM-killing the request
// process during save, so operators trigger generation here when
// they want a card on disk. Server-mode deployments use this as
// a "rebuild this one card" affordance after editing the title or
// background. Response is a tiny JSON payload so cgi.Serve's
// response buffering doesn't compound the RenderCard memory peak.
func (h *Handler) entryOGRegenerate(w http.ResponseWriter, r *http.Request) {
	if h.OG == nil || h.ImageDir == "" {
		http.Error(w, "og disabled", http.StatusServiceUnavailable)
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	entry, err := h.Store.EntryByID(r.Context(), h.wid(), id)
	if err != nil {
		log.Printf("admin.entryOGRegenerate: load: %v", err)
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	u := session.UserFrom(r.Context())
	if u == nil || !u.CanEditEntry(entry.AuthorID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	weblog, err := h.Store.WeblogByID(r.Context(), h.wid())
	if err != nil {
		log.Printf("admin.entryOGRegenerate: load weblog: %v", err)
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	absDir, absFile, urlPath := h.ogCardPath(entry.ID)
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		log.Printf("admin.entryOGRegenerate: mkdir: %v", err)
		http.Error(w, "fs", http.StatusInternalServerError)
		return
	}
	f, err := os.Create(absFile)
	if err != nil {
		log.Printf("admin.entryOGRegenerate: create: %v", err)
		http.Error(w, "fs", http.StatusInternalServerError)
		return
	}
	defer f.Close()
	bgPath := h.resolveOGBG(entry.OGBGImagePath, weblog.OGBGImagePath)
	if err := h.OG.RenderCard(f, entry.Title, weblog.Title, og.Options{
		BGPath:    bgPath,
		TextColor: weblog.OGTextColor,
	}); err != nil {
		log.Printf("admin.entryOGRegenerate: render: %v", err)
		http.Error(w, "render", http.StatusInternalServerError)
		return
	}
	// Cache-bust the preview by appending the file's mtime — the
	// <img src> on the form caches aggressively otherwise.
	info, _ := f.Stat()
	var ts int64
	if info != nil {
		ts = info.ModTime().Unix()
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	fmt.Fprintf(w, `{"ok":true,"url":%q,"ts":%d}`, root(r)+urlPath, ts)
}

// entryPin handles POST (pin) and DELETE (unpin) for /admin/entries/{id}/pin.
// It responds with JSON so the list page can toggle the badge without a full
// reload.
func (h *Handler) entryPin(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	u := session.UserFrom(r.Context())
	existing, err := h.Store.EntryByID(r.Context(), h.wid(), id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		log.Printf("admin.entryPin: load: %v", err)
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	if !u.CanEditEntry(existing.AuthorID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	pinned := r.Method == http.MethodPost
	if err := h.Store.SetEntryPinned(r.Context(), h.wid(), id, pinned); err != nil {
		log.Printf("admin.entryPin: %v", err)
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	h.maybeAutoRebuild(r.Context())
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if pinned {
		fmt.Fprintf(w, `{"ok":true,"pinned":true}`)
	} else {
		fmt.Fprintf(w, `{"ok":true,"pinned":false}`)
	}
}

// wid pins admin pages to the app's default weblog while multi-blog UX is
// not wired up.
func (h *Handler) wid() int64 { return h.WID }
