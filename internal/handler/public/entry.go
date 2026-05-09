package public

import (
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

	// Request came in via /entry/<id>/ but the entry has a slug — 301 to
	// the canonical URL so readers, crawlers, and cached bookmarks
	// converge on one spelling. Propagate the raw query string so a
	// preview param survives the hop.
	if viaID && entry.Slug != "" {
		canonical := root(r) + "/entry/" + entry.Slug + "/"
		if raw := r.URL.RawQuery; raw != "" {
			canonical += "?" + raw
		}
		http.Redirect(w, r, canonical, http.StatusMovedPermanently)
		return
	}
	// Preview-authorised admins see drafts and closed entries just as
	// they'd appear once published; anonymous requests keep the strict
	// 404 / 410 contract. previewFromRequest already silently collapses
	// the flag to false for unauthenticated callers.
	switch entry.Status {
	case domain.EntryPublished:
		// fall through
	case domain.EntryClosed:
		if !preview.AllowDraft {
			http.Error(w, "gone", http.StatusGone)
			return
		}
	default:
		if !preview.AllowDraft {
			http.NotFound(w, r)
			return
		}
	}

	weblog, err := h.Store.WeblogByID(ctx, h.WID)
	if err != nil {
		log.Printf("public.entry: load weblog: %v", err)
		http.Error(w, "site not configured", http.StatusInternalServerError)
		return
	}
	var tmpl *domain.Template
	if preview.TemplateID > 0 {
		if t, err := h.Store.TemplateByID(ctx, h.WID, preview.TemplateID); err == nil {
			tmpl = t
		} else {
			log.Printf("public.entry: preview template %d missing, falling back: %v", preview.TemplateID, err)
		}
	}
	if tmpl == nil {
		tmpl, err = h.Store.ActiveTemplate(ctx, h.WID)
		if err != nil {
			log.Printf("public.entry: load template: %v", err)
			http.Error(w, "no active template", http.StatusInternalServerError)
			return
		}
	}
	if preview.Active() {
		markPreviewResponse(w)
	}

	cats, _ := h.Store.CategoriesByIDs(ctx, []int64{entry.CategoryID})
	users, _ := h.Store.UsersByIDs(ctx, []int64{entry.AuthorID})

	var catPtr *domain.Category
	if c, ok := cats[entry.CategoryID]; ok {
		catPtr = &c
	}
	var authorPtr *domain.User
	if u, ok := users[entry.AuthorID]; ok {
		authorPtr = &u
	}

	prev, err := h.Store.PrevPublishedEntry(ctx, h.WID, *entry)
	if err != nil && !errors.Is(err, repo.ErrNotFound) {
		log.Printf("public.entry: prev: %v", err)
	}
	next, err := h.Store.NextPublishedEntry(ctx, h.WID, *entry)
	if err != nil && !errors.Is(err, repo.ErrNotFound) {
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

	stampCounts, err := h.Store.StampCountsByEntry(ctx, entry.ID)
	if err != nil {
		log.Printf("public.entry: stamp counts: %v", err)
	}

	tags, err := h.Store.TagsByEntry(ctx, entry.ID)
	if err != nil {
		log.Printf("public.entry: tags: %v", err)
	}
	profileUsers, err := h.Store.VisibleProfileUsers(ctx, h.WID)
	if err != nil {
		log.Printf("public.entry: profile users: %v", err)
	}
	sidebar := h.loadSidebarData(ctx, "public.entry")

	view := content.EntryView{
		Site:          h.buildSite(ctx, *weblog).WithBasePath(root(r)),
		Template:      tmpl,
		Entry:         *entry,
		Category:      catPtr,
		Author:        authorPtr,
		Prev:          prev,
		Next:          next,
		Messages:      messages,
		CommentMode:   weblog.CommentMode,
		StampCounts:   stampCounts,
		Tags:          tags,
		ProfileUsers:  profileUsers,
		Sidebar:       sidebar,
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
