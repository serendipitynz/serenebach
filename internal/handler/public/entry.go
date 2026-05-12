package public

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/serendipitynz/serenebach/internal/content"
	"github.com/serendipitynz/serenebach/internal/csrf"
	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/storage/repo"
)

func (h *Handler) entry(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	key := chi.URLParam(r, "key")
	entry, viaID, err := h.resolveEntryKey(ctx, key)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		log.Printf("public.entry: load entry: %v", err)
		http.Error(w, "failed to load entry", http.StatusInternalServerError)
		return
	}

	preview := previewFromRequest(r)

	if redirectIDFormToSlug(w, r, entry, viaID) {
		return
	}
	if !entryStatusVisible(w, r, entry.Status, preview) {
		return
	}

	weblog, err := h.Store.WeblogByID(ctx, h.WID)
	if err != nil {
		log.Printf("public.entry: load weblog: %v", err)
		http.Error(w, "site not configured", http.StatusInternalServerError)
		return
	}
	tmpl, ok := h.resolveEntryTemplate(ctx, preview)
	if !ok {
		http.Error(w, "no active template", http.StatusInternalServerError)
		return
	}
	if preview.Active() {
		markPreviewResponse(w)
	}

	data := h.loadEntryViewData(ctx, entry, weblog)

	view := content.EntryView{
		Site:          h.buildSite(ctx, *weblog).WithBasePath(root(r)),
		Template:      tmpl,
		Entry:         *entry,
		Category:      data.category,
		Author:        data.author,
		Prev:          data.prev,
		Next:          data.next,
		Messages:      data.messages,
		CommentMode:   weblog.CommentMode,
		StampCounts:   data.stampCounts,
		Tags:          data.tags,
		ProfileUsers:  data.profileUsers,
		Sidebar:       data.sidebar,
		FormError:     r.URL.Query().Get("err"),
		FormTS:        time.Now().Unix(),
		CookieName:    readCookieEscaped(r, commenterCookieName),
		CookieEmail:   readCookieEscaped(r, commenterCookieEmail),
		CookieURL:     readCookieEscaped(r, commenterCookieURL),
		TurnstileHTML: turnstileWidget(h.Turnstile),
		CSRFToken:     csrf.Token(r.Context()),
	}
	body, err := view.Render()
	if err != nil {
		log.Printf("public.entry: render: %v", err)
		http.Error(w, "render error", http.StatusInternalServerError)
		return
	}
	writeHTML(w, body, "public.entry")
}

// entryViewData bundles every read-side fetch the entry template needs
// so the request handler can pass it as a single struct rather than
// threading ten parameters through.
type entryViewData struct {
	category     *domain.Category
	author       *domain.User
	prev         *domain.Entry
	next         *domain.Entry
	messages     []domain.Message
	stampCounts  map[domain.StampKind]int64
	tags         []domain.Tag
	profileUsers []domain.User
	sidebar      content.SidebarData
}

// redirectIDFormToSlug issues a 301 from /entry/<id>/ to /entry/<slug>/
// when the entry carries a slug. Readers, crawlers, and cached
// bookmarks converge on one canonical spelling; query strings (e.g.
// preview params) survive the hop. Returns true when the redirect was
// written so the caller must stop.
func redirectIDFormToSlug(w http.ResponseWriter, r *http.Request, entry *domain.Entry, viaID bool) bool {
	if !viaID || entry.Slug == "" {
		return false
	}
	canonical := root(r) + "/entry/" + entry.Slug + "/"
	if raw := r.URL.RawQuery; raw != "" {
		canonical += "?" + raw
	}
	http.Redirect(w, r, canonical, http.StatusMovedPermanently)
	return true
}

// entryStatusVisible enforces the 404 / 410 contract for the entry
// page. Preview-authorised admins see drafts and closed entries as
// they'd render once published; anonymous requests keep the strict
// contract. previewFromRequest already collapses the flag to false
// for unauthenticated callers.
func entryStatusVisible(w http.ResponseWriter, r *http.Request, status domain.EntryStatus, preview previewOverride) bool {
	switch status {
	case domain.EntryPublished:
		return true
	case domain.EntryClosed:
		if preview.AllowDraft {
			return true
		}
		http.Error(w, "gone", http.StatusGone)
		return false
	default:
		if preview.AllowDraft {
			return true
		}
		http.NotFound(w, r)
		return false
	}
}

// resolveEntryTemplate honours the preview template selector when set
// (and reachable), falling back to the weblog's active template.
// Returns ok=false only when neither is loadable — the caller surfaces
// that as a 500.
func (h *Handler) resolveEntryTemplate(ctx context.Context, preview previewOverride) (*domain.Template, bool) {
	if preview.TemplateID > 0 {
		t, err := h.Store.TemplateByID(ctx, h.WID, preview.TemplateID)
		if err == nil {
			return t, true
		}
		log.Printf("public.entry: preview template %d missing, falling back: %v", preview.TemplateID, err)
	}
	t, err := h.Store.ActiveTemplate(ctx, h.WID)
	if err != nil {
		log.Printf("public.entry: load template: %v", err)
		return nil, false
	}
	return t, true
}

// loadEntryViewData fetches every dataset the entry template consumes.
// Each lookup is best-effort: a repo failure logs and leaves the
// corresponding field at its zero value, which the template then
// renders as an empty section rather than failing the whole page.
func (h *Handler) loadEntryViewData(ctx context.Context, entry *domain.Entry, weblog *domain.Weblog) entryViewData {
	var data entryViewData

	cats, _ := h.Store.CategoriesByIDs(ctx, []int64{entry.CategoryID})
	if c, ok := cats[entry.CategoryID]; ok {
		data.category = &c
	}
	users, _ := h.Store.UsersByIDs(ctx, []int64{entry.AuthorID})
	if u, ok := users[entry.AuthorID]; ok {
		data.author = &u
	}

	if prev, err := h.Store.PrevPublishedEntry(ctx, h.WID, *entry); err == nil {
		data.prev = prev
	} else if !errors.Is(err, repo.ErrNotFound) {
		log.Printf("public.entry: prev: %v", err)
	}
	if next, err := h.Store.NextPublishedEntry(ctx, h.WID, *entry); err == nil {
		data.next = next
	} else if !errors.Is(err, repo.ErrNotFound) {
		log.Printf("public.entry: next: %v", err)
	}

	messages, err := h.Store.ApprovedMessagesByEntry(ctx, h.WID, entry.ID)
	if err != nil {
		log.Printf("public.entry: messages: %v", err)
	}
	// Repo returns comments oldest-first (the SB3 default); if the
	// weblog has been flipped to newest-first, reverse here.
	if weblog.CommentSortOrder == "desc" {
		for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
			messages[i], messages[j] = messages[j], messages[i]
		}
	}
	data.messages = messages

	if stampCounts, err := h.Store.StampCountsByEntry(ctx, entry.ID); err == nil {
		data.stampCounts = stampCounts
	} else {
		log.Printf("public.entry: stamp counts: %v", err)
	}
	if tags, err := h.Store.TagsByEntry(ctx, entry.ID); err == nil {
		data.tags = tags
	} else {
		log.Printf("public.entry: tags: %v", err)
	}
	if profileUsers, err := h.Store.VisibleProfileUsers(ctx, h.WID); err == nil {
		data.profileUsers = profileUsers
	} else {
		log.Printf("public.entry: profile users: %v", err)
	}
	data.sidebar = h.loadSidebarData(ctx, "public.entry")
	return data
}
