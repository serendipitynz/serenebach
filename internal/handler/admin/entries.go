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

const adminEntryListLimit = 200

// mountEntries registers the /admin/entries/* routes. Called from
// MountProtected so the RequireUser middleware already wraps this group.
func (h *Handler) mountEntries(r chi.Router) {
	r.Get("/entries", h.entryList)
	r.Get("/entries/new", h.entryNewForm)
	r.Post("/entries/new", h.entryCreate)
	r.Get("/entries/{id}/edit", h.entryEditForm)
	r.Post("/entries/{id}/edit", h.entryUpdate)
	r.Post("/entries/{id}/delete", h.entryDelete)
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
	Entries []entryRow
	Flash   string
}

func (h *Handler) entryList(w http.ResponseWriter, r *http.Request) {
	entries, err := h.Store.ListEntriesForAdmin(r.Context(), h.wid(), adminEntryListLimit)
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

	renderMain(w, r, pageEntriesList, entriesListPageData{
		pageBase: pageBase{
			Title:      tr(r, "entries.list.title"),
			ActiveMenu: "entries",
			CSRFToken:  csrf.Token(r.Context()),
			User:       session.UserFrom(r.Context()),
		},
		Entries: rows,
		Flash:   r.URL.Query().Get("ok"),
	})
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
		WID:        h.wid(),
		AuthorID:   u.ID,
		CategoryID: domain.Uncategorized,
		Status:     domain.EntryDraft,
		PostedAt:   now,
	}
	h.renderEntryForm(w, r, "/admin/entries/new", entry, "", "", tr(r, "entries.form.titleNew"), "entry-new")
}

func (h *Handler) entryEditForm(w http.ResponseWriter, r *http.Request) {
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
	e, err := h.Store.EntryByID(r.Context(), h.wid(), id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		log.Printf("admin.entryEditForm: %v", err)
		http.Error(w, "failed to load entry", http.StatusInternalServerError)
		return
	}
	if !u.CanEditEntry(e.AuthorID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	// Pre-fill the tag input with the entry's current tags. Failure to
	// load tags is logged but not fatal — better to render the form
	// with an empty tag field than to 500 the edit page.
	tags, err := h.Store.TagsByEntry(r.Context(), e.ID)
	if err != nil {
		log.Printf("admin.entryEditForm: tags: %v", err)
	}
	h.renderEntryForm(w, r, fmt.Sprintf("/admin/entries/%d/edit", id), *e, tagsToCSV(tags), "", tr(r, "entries.form.titleEditPlain"), "entries")
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

func (h *Handler) renderEntryForm(w http.ResponseWriter, r *http.Request, action string, entry domain.Entry, tagsCSV, errMsg, title, activeMenu string) {
	cats, err := h.Store.AllCategories(r.Context(), h.wid())
	if err != nil {
		log.Printf("admin.renderEntryForm: %v", err)
	}
	renderMain(w, r, pageEntryForm, entryFormPageData{
		pageBase: pageBase{
			Title:      title,
			ActiveMenu: activeMenu,
			CSRFToken:  csrf.Token(r.Context()),
			User:       session.UserFrom(r.Context()),
		},
		Action:        action,
		Entry:         entry,
		StatusInt:     int(entry.Status),
		PostedAtLocal: entry.PostedAt.Format("2006-01-02T15:04"),
		Categories:    cats,
		Formats:       buildFormatOptions(),
		CurrentFormat: string(format.Normalize(entry.Format)),
		TagsCSV:       tagsCSV,
		Error:         errMsg,
		Flash:         r.URL.Query().Get("ok"),
	})
}

// ---- create / update ---------------------------------------------------

func parseEntryForm(r *http.Request, base domain.Entry) (domain.Entry, string) {
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

	// Normalise keywords: trim each comma-separated item, drop empties,
	// re-join with ", " so template output is consistent regardless of
	// how the author spaced the input.
	if raw := strings.TrimSpace(r.PostFormValue("keywords")); raw == "" {
		base.Keywords = ""
	} else {
		parts := strings.Split(raw, ",")
		cleaned := parts[:0]
		for _, p := range parts {
			if t := strings.TrimSpace(p); t != "" {
				cleaned = append(cleaned, t)
			}
		}
		base.Keywords = strings.Join(cleaned, ", ")
	}

	if fmtRaw := strings.TrimSpace(r.PostFormValue("format")); fmtRaw != "" {
		base.Format = string(format.Normalize(fmtRaw))
	}

	// Per-entry OG background override. Same stored_path convention as
	// the weblog-level field; empty = inherit the site default. No
	// validation — unresolvable paths fall back at render.
	base.OGBGImagePath = strings.TrimSpace(r.PostFormValue("og_bg_image_path"))

	catRaw := r.PostFormValue("category_id")
	if catRaw != "" {
		if v, err := strconv.ParseInt(catRaw, 10, 64); err == nil {
			base.CategoryID = v
		} else {
			return base, tr(r, "entries.form.error.categoryInvalid")
		}
	}

	statusRaw := r.PostFormValue("status")
	switch statusRaw {
	case "0":
		base.Status = domain.EntryDraft
	case "1":
		base.Status = domain.EntryPublished
	case "-1":
		base.Status = domain.EntryClosed
	default:
		return base, tr(r, "entries.form.error.statusInvalid")
	}

	postedRaw := r.PostFormValue("posted_at")
	if postedRaw != "" {
		if t, err := time.ParseInLocation("2006-01-02T15:04", postedRaw, time.Local); err == nil {
			base.PostedAt = t
		} else {
			return base, tr(r, "entries.form.error.postedAtInvalid")
		}
	}

	return base, ""
}

func (h *Handler) entryCreate(w http.ResponseWriter, r *http.Request) {
	u := session.UserFrom(r.Context())
	base := domain.Entry{
		WID:        h.wid(),
		AuthorID:   u.ID,
		CategoryID: domain.Uncategorized,
		Status:     domain.EntryDraft,
		PostedAt:   time.Now(),
	}
	entry, errMsg := parseEntryForm(r, base)
	tagNames, tagsCSV := parseTagNames(r.PostFormValue("tags"))
	if errMsg != "" {
		h.renderEntryForm(w, r, "/admin/entries/new", entry, tagsCSV, errMsg, tr(r, "entries.form.titleNew"), "entry-new")
		return
	}
	id, err := h.Store.CreateEntry(r.Context(), entry)
	if err != nil {
		if errors.Is(err, repo.ErrSlugInUse) {
			h.renderEntryForm(w, r, "/admin/entries/new", entry, tagsCSV, tr(r, "entries.form.error.slugInUse"), tr(r, "entries.form.titleNew"), "entry-new")
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
	existing, err := h.Store.EntryByID(r.Context(), h.wid(), id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		log.Printf("admin.entryUpdate: load: %v", err)
		http.Error(w, "failed to load entry", http.StatusInternalServerError)
		return
	}
	if !u.CanEditEntry(existing.AuthorID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	entry, errMsg := parseEntryForm(r, *existing)
	tagNames, tagsCSV := parseTagNames(r.PostFormValue("tags"))
	if errMsg != "" {
		h.renderEntryForm(w, r, fmt.Sprintf("/admin/entries/%d/edit", id), entry, tagsCSV, errMsg, tr(r, "entries.form.titleEditPlain"), "entries")
		return
	}
	if err := h.Store.UpdateEntry(r.Context(), entry); err != nil {
		if errors.Is(err, repo.ErrSlugInUse) {
			h.renderEntryForm(w, r, fmt.Sprintf("/admin/entries/%d/edit", id), entry, tagsCSV, tr(r, "entries.form.error.slugInUse"), tr(r, "entries.form.titleEditPlain"), "entries")
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
	http.Redirect(w, r, root(r)+fmt.Sprintf("/admin/entries/%d/edit?ok=saved", id), http.StatusFound)
}

func (h *Handler) entryDelete(w http.ResponseWriter, r *http.Request) {
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
	// Regular-tier authors may only remove their own work; power +
	// admin can delete any entry. Loading the row first lets us
	// compare authorship before we commit to the destructive SQL.
	existing, err := h.Store.EntryByID(r.Context(), h.wid(), id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		log.Printf("admin.entryDelete: load: %v", err)
		http.Error(w, "failed to load entry", http.StatusInternalServerError)
		return
	}
	if !u.CanDeleteEntry(existing.AuthorID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
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
	if h.OG == nil || h.ImageDir == "" {
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

// wid pins admin pages to the app's default weblog while multi-blog UX is
// not wired up.
func (h *Handler) wid() int64 { return h.WID }
