package public

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/serendipitynz/serenebach/internal/content"
	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/llmstxt"
	"github.com/serendipitynz/serenebach/internal/storage/repo"
)

// styleCSS serves the active template's CSS in dev mode — the static
// rebuild writes the same bytes to `<out>/style.css` but that only
// helps for deployed sites. Without this handler `/style.css` 404s
// whenever the dev server runs, which breaks every template preview.
// Kept as an alias for the active template only; pages rendered
// through a pinned template point at /template/<id>/style.css
// instead (see Site.CSSURL()).
func (h *Handler) styleCSS(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tmpl, err := h.Store.ActiveTemplate(ctx, h.WID)
	if err != nil {
		log.Printf("public.styleCSS: load template: %v", err)
		http.Error(w, "no active template", http.StatusInternalServerError)
		return
	}
	weblog, err := h.Store.WeblogByID(ctx, h.WID)
	if err != nil {
		log.Printf("public.styleCSS: load weblog: %v", err)
		http.Error(w, "site not configured", http.StatusInternalServerError)
		return
	}
	writeCSS(w, content.RenderTemplateCSS(content.NewSite(*weblog).WithBasePath(root(r)), tmpl))
}

// templateStyleCSS serves the CSS column of a specific template row
// (path: /template/{id}/style.css). Category / archive / profile
// pages that pin a non-active template depend on this so the reader
// loads the right stylesheet — otherwise {site_css} would resolve
// to /style.css and the per-category CSS would silently not apply.
func (h *Handler) templateStyleCSS(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	tmpl, err := h.Store.TemplateByID(ctx, h.WID, id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		log.Printf("public.templateStyleCSS: load template %d: %v", id, err)
		http.Error(w, "failed to load template", http.StatusInternalServerError)
		return
	}
	weblog, err := h.Store.WeblogByID(ctx, h.WID)
	if err != nil {
		log.Printf("public.templateStyleCSS: load weblog: %v", err)
		http.Error(w, "site not configured", http.StatusInternalServerError)
		return
	}
	writeCSS(w, content.RenderTemplateCSS(content.NewSite(*weblog).WithBasePath(root(r)), tmpl))
}

func writeCSS(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	// Templates change infrequently; short cache with revalidation
	// balances "edit + reload" workflow against repeat-visit speed.
	w.Header().Set("Cache-Control", "public, max-age=60")
	_, _ = w.Write([]byte(body))
}

// resolveEntryKey loads an entry from either a numeric id or a slug.
// Returns (entry, isNumericKey, error) — the isNumericKey flag lets the
// GET handler emit a 301 redirect when the request came in via id but
// the entry has since acquired a canonical slug.
func (h *Handler) resolveEntryKey(ctx context.Context, key string) (*domain.Entry, bool, error) {
	if id, err := strconv.ParseInt(key, 10, 64); err == nil && id > 0 {
		e, err := h.Store.EntryByID(ctx, h.WID, id)
		return e, true, err
	}
	e, err := h.Store.EntryBySlug(ctx, h.WID, key)
	return e, false, err
}

// entryKeyFor is the URL segment used to refer to this entry — slug when
// set, numeric id otherwise. Mirrors content.Site.EntryPermalink's key
// choice without needing a full Site value.
func entryKeyFor(e *domain.Entry) string {
	if e != nil && e.Slug != "" {
		return e.Slug
	}
	return strconv.FormatInt(e.ID, 10)
}

// llmsIndex + llmsFull implement the /llms.txt + /llms-full.txt
// public routes. Both 404 when the weblog hasn't flipped
// the opt-in toggle on the 基本設定 tab — a blog owner who'd rather
// not feed AI crawlers stays out of discovery without any extra
// config. When enabled, the routes serve text/plain Markdown so
// ordinary curl / wget + any AI agent can ingest them directly.
//
// Entry count for both routes is capped by llmsMaxEntries so a
// decade-old blog doesn't produce a multi-megabyte response. Most
// agents chunk client-side anyway; the index is the discovery
// surface, llms-full is the batch-retrieve shortcut.
const llmsMaxEntries = 500

func (h *Handler) llmsIndex(w http.ResponseWriter, r *http.Request) {
	h.serveLLMs(w, r, llmstxt.Index, "public.llms.index")
}

func (h *Handler) llmsFull(w http.ResponseWriter, r *http.Request) {
	h.serveLLMs(w, r, llmstxt.Full, "public.llms.full")
}

func (h *Handler) serveLLMs(w http.ResponseWriter, r *http.Request, render func(llmstxt.Input) string, logTag string) {
	ctx := r.Context()
	weblog, err := h.Store.WeblogByID(ctx, h.WID)
	if err != nil {
		log.Printf("%s: load weblog: %v", logTag, err)
		http.Error(w, "site not configured", http.StatusInternalServerError)
		return
	}
	if !weblog.LLMSEnabled {
		http.NotFound(w, r)
		return
	}
	entries, err := h.Store.RecentPublishedEntries(ctx, h.WID, llmsMaxEntries)
	if err != nil {
		log.Printf("%s: load entries: %v", logTag, err)
		http.Error(w, "failed to load entries", http.StatusInternalServerError)
		return
	}
	body := render(llmstxt.Input{Weblog: *weblog, Entries: entries})
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write([]byte(body))
}
