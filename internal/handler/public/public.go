package public

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/serendipitynz/serenebach/internal/basepath"
	"github.com/serendipitynz/serenebach/internal/clientip"
	"github.com/serendipitynz/serenebach/internal/content"
	"github.com/serendipitynz/serenebach/internal/csrf"
	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/feed"
	publici18n "github.com/serendipitynz/serenebach/internal/handler/public/i18n"
	"github.com/serendipitynz/serenebach/internal/i18n"
	"github.com/serendipitynz/serenebach/internal/llmstxt"
	"github.com/serendipitynz/serenebach/internal/spam"
	"github.com/serendipitynz/serenebach/internal/storage/repo"
	"github.com/serendipitynz/serenebach/internal/turnstile"
)

// publicBundle carries reader-facing engine strings (comment
// submission errors, sidebar fallback labels). Kept package-level
// because the catalogues are compile-time immutable; a parse failure
// here is a build-time problem and should panic.
var publicBundle = loadPublicBundle()

func loadPublicBundle() *i18n.Bundle {
	raw, err := publici18n.Catalogues()
	if err != nil {
		panic("public: load i18n catalogues: " + err.Error())
	}
	b, err := i18n.LoadBundle("ja", raw)
	if err != nil {
		panic("public: i18n bundle: " + err.Error())
	}
	return b
}

// tr resolves a reader-facing key. Locale preference: the blog's
// configured Lang (weblog.Lang) wins when supported, so visitors
// consistently see errors in the blog's own language; otherwise the
// Accept-Language header drives the pick; otherwise the bundle
// default ("ja"). Unknown keys return the key literal so drift is
// visible rather than blank.
func tr(weblog *domain.Weblog, r *http.Request, key string) string {
	if weblog != nil {
		if _, ok := publicBundle.Locales[weblog.Lang]; ok {
			return publicBundle.T(weblog.Lang, key)
		}
	}
	return publicBundle.T(publicBundle.Resolve(r), key)
}

const defaultEntryListSize = 10

const (
	commenterCookieName  = "sb_name"
	commenterCookieEmail = "sb_email"
	commenterCookieURL   = "sb_url"
	commenterCookieTTL   = 30 * 24 * time.Hour
)

type Handler struct {
	Store     *repo.Store
	WID       int64
	Turnstile turnstile.Verifier
	// LegacyURL caches weblogs.legacy_* once at startup. The redirect
	// layer (legacy_cgi.go, legacy_static.go) consults it to translate
	// SB3-shaped requests into Go canonical URLs. Zero value means
	// "no SB3 import has run; redirect off."
	LegacyURL repo.WeblogLegacyURL
	// TrustedProxies controls when forwarded headers may be honoured by
	// clientIP. Zero value (no proxies trusted) is the safe default for
	// directly-exposed binaries; operators behind a known proxy
	// configure it via SB_TRUSTED_PROXIES.
	TrustedProxies clientip.Resolver
}

// MountLegacy wires the SB3 `/sb.cgi` compatibility shim. Kept on a
// separate entry point so the app can mount it outside the CSRF
// middleware group — imported SB3 templates post comments via
// `/sb.cgi?mode=comment` and don't carry a modern CSRF token. The
// shim itself only redirects; the destination handlers
// (`/entry/<id>/comment` etc.) still enforce their own validations.
func (h *Handler) MountLegacy(r chi.Router) {
	r.Get("/sb.cgi", h.legacyCGI)
	r.Post("/sb.cgi", h.legacyCGI)
}

// root returns the deployment base path for the current request (e.g. "/sb4").
func root(r *http.Request) string {
	return basepath.FromContext(r.Context())
}

// The URL patterns here must track whatever Site.EntryPermalink /
// Site.CategoryPermalink / Site.ArchivePermalink produce. When one of those
// changes, change the matching route together.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/", h.home)
	// {key} is either a numeric entry id or a custom slug — resolveEntryKey
	// disambiguates. Reader-facing POSTs (comment / like / stamp) live in
	// MountMutations so they can run outside the CSRF middleware while a
	// same-origin guard takes its place; static-rendered HTML can then
	// post to the dynamic backend without a per-session token.
	r.Get("/entry/{key}", h.entry)
	r.Get("/category/{id}", h.category)
	r.Get("/tag/{slug}", h.tag)
	r.Get("/archive/{year}", h.archiveYear)
	r.Get("/archive/{year}/{month}", h.archiveMonth)
	r.Get("/profile/{id}", h.profile)
	r.Get("/rss.xml", h.rssFeed)
	r.Get("/atom.xml", h.atomFeed)
	// RSD (Really Simple Discovery) — SB3 emitted this for XML-RPC
	// client discovery (MarsEdit etc.). The Go port doesn't ship
	// XML-RPC, but the XML document itself is cheap to serve so
	// imported templates referencing {site_rsd} resolve to a real
	// URL instead of a dead string.
	r.Get("/rsd.xml", h.rsdFeed)
	// llms.txt / llms-full.txt. Both routes 404 unless the weblog has
	// flipped the opt-in toggle on the 基本設定 tab.
	r.Get("/llms.txt", h.llmsIndex)
	r.Get("/llms-full.txt", h.llmsFull)
	r.Get("/style.css", h.styleCSS)
	// Per-template CSS endpoint. {site_css} in a page rendered through
	// template N emits this path so category / archive / profile pages
	// pick up their own template's stylesheet, not the active one.
	// chi's trie routes more-specific patterns ahead of `/template/*`
	// (registered at the app level for on-disk assets), so this wins
	// when the path shape matches.
	r.Get("/template/{id}/style.css", h.templateStyleCSS)
}

// MountMutations registers the reader-facing POST endpoints (comment
// / like / stamp). Mounted in app.go on a sub-router that swaps the
// CSRF middleware for SameOriginGuard so a static-rebuilt HTML page,
// which has no way to embed a per-session token, can still post to
// the dynamic backend. The same-origin check covers the realistic
// browser-driven attack; spam from non-browser clients remains the
// Turnstile + IP-blocklist + spam-words layer's job.
func (h *Handler) MountMutations(r chi.Router) {
	r.Post("/entry/{key}/comment", h.commentSubmit)
	r.Post("/entry/{key}/like", h.entryLike)
	r.Post("/entry/{key}/stamp", h.entryStamp)
}

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
	writeCSS(w, content.RenderTemplateCSS(*weblog, tmpl))
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
	writeCSS(w, content.RenderTemplateCSS(*weblog, tmpl))
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

// ---- feeds --------------------------------------------------------------

// rssFeed and atomFeed share the entry + reference load; only the encoder
// differs. Entries are capped at feed.DefaultEntryLimit inside the
// builder, so we ask the repo for exactly that many rather than every
// published entry.
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

func (h *Handler) rssFeed(w http.ResponseWriter, r *http.Request) {
	h.serveFeed(w, r, feed.BuildRSS, feed.MIMERSS, "public.rss")
}

// rsdFeed serves an RSD 1.0 discovery document at /rsd.xml. The
// endpoint itself is mostly metadata — the advertised XML-RPC
// interface isn't implemented (the Go port has no blog-edit API
// yet), so editors like MarsEdit will fetch this, see no working
// apiLink, and fall back to their own UI. That's acceptable: the
// tag {site_rsd} now resolves to a real URL so imported SB3
// templates stop silently emitting an empty href.
func (h *Handler) rsdFeed(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	weblog, err := h.Store.WeblogByID(ctx, h.WID)
	if err != nil {
		log.Printf("public.rsd: load weblog: %v", err)
		http.Error(w, "site not configured", http.StatusInternalServerError)
		return
	}
	site := content.NewSite(*weblog)
	body := `<?xml version="1.0"?>
<rsd version="1.0" xmlns="http://archipelago.phrasewise.com/rsd">
 <service>
  <engineName>Serene Bach</engineName>
  <engineLink>https://github.com/serendipitynz/serenebach</engineLink>
  <homePageLink>` + html.EscapeString(site.TopURL()) + `</homePageLink>
  <apis>
   <api name="Atom" blogID="" preferred="true" apiLink="` + html.EscapeString(site.AtomURL()) + `" />
  </apis>
 </service>
</rsd>
`
	w.Header().Set("Content-Type", "application/rsd+xml; charset=utf-8")
	_, _ = w.Write([]byte(body))
}

func (h *Handler) atomFeed(w http.ResponseWriter, r *http.Request) {
	h.serveFeed(w, r, feed.BuildAtom, feed.MIMEAtom, "public.atom")
}

func (h *Handler) serveFeed(w http.ResponseWriter, r *http.Request, build func(feed.Options) ([]byte, error), mime, logTag string) {
	ctx := r.Context()
	weblog, err := h.Store.WeblogByID(ctx, h.WID)
	if err != nil {
		log.Printf("%s: load weblog: %v", logTag, err)
		http.Error(w, "site not configured", http.StatusInternalServerError)
		return
	}
	entries, err := h.Store.RecentPublishedEntries(ctx, h.WID, feed.DefaultEntryLimit)
	if err != nil {
		log.Printf("%s: load entries: %v", logTag, err)
		http.Error(w, "failed to load entries", http.StatusInternalServerError)
		return
	}
	cats, users := h.lookupRefs(ctx, entries, logTag)
	body, err := build(feed.Options{
		Site:       content.NewSite(*weblog),
		Entries:    entries,
		Users:      users,
		Categories: cats,
	})
	if err != nil {
		log.Printf("%s: build: %v", logTag, err)
		http.Error(w, "failed to build feed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", mime)
	if _, err := w.Write(body); err != nil {
		log.Printf("%s: write: %v", logTag, err)
	}
}

// ---- list pages (home, category, archive) -------------------------------

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
	// Per-category template pin beats the archive pin. Stale / missing
	// id silently falls through to the normal resolver rather than
	// breaking the page — operators see the fallback in admin anyway.
	var tmpl *domain.Template
	// Admin preview overrides every other pin (category / archive / use).
	// Checked first so the operator's explicit request always wins.
	if preview.TemplateID > 0 {
		if t, err := h.Store.TemplateByID(ctx, h.WID, preview.TemplateID); err == nil {
			tmpl = t
		} else {
			log.Printf("%s: preview template %d missing, falling back: %v", logTag, preview.TemplateID, err)
		}
	}
	if tmpl == nil && cat != nil && cat.TemplateID != 0 {
		if t, err := h.Store.TemplateByID(ctx, h.WID, cat.TemplateID); err == nil {
			tmpl = t
		} else {
			log.Printf("%s: category template pin %d missing, falling back: %v", logTag, cat.TemplateID, err)
		}
	}
	if tmpl == nil {
		tmpl, err = h.pickTemplate(ctx, weblog, useArchiveTemplate)
		if err != nil {
			log.Printf("%s: load template: %v", logTag, err)
			http.Error(w, "no active template", http.StatusInternalServerError)
			return
		}
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
		Site:         content.NewSite(*weblog),
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

// pickTemplate resolves the template to render with. Archive routes
// honour weblog.ArchiveTemplateID when set; everything else falls
// through to the currently-active template. Tolerant of a stale pin:
// if the referenced row is gone we log and fall back to active rather
// than erroring out the page.
func (h *Handler) pickTemplate(ctx context.Context, weblog *domain.Weblog, useArchive bool) (*domain.Template, error) {
	if useArchive && weblog.ArchiveTemplateID != 0 {
		if t, err := h.Store.TemplateByID(ctx, h.WID, weblog.ArchiveTemplateID); err == nil {
			return t, nil
		} else {
			log.Printf("public.pickTemplate: archive pin %d missing, falling back: %v", weblog.ArchiveTemplateID, err)
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
	if periods, err := h.Store.ArchivePeriodsWithCounts(ctx, h.WID); err == nil {
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
	from := time.Date(year, time.January, 1, 0, 0, 0, 0, time.Local)
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
	from := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.Local)
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

// ---- single entry -------------------------------------------------------

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
		Site:          content.NewSite(*weblog),
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

// ---- comments -----------------------------------------------------------

// minFormLifetime is the shortest time a legitimate visitor could plausibly
// take between loading the form and submitting it. Faster submissions are
// almost certainly bots and we reject them before touching the database.
const minFormLifetime = 3 * time.Second

// commentRateWindow is the sliding window for the IP-based rate limit.
const commentRateWindow = 60 * time.Second

// commentRateLimit is the max comments one IP can post within the window.
const commentRateLimit = 3

func (h *Handler) commentSubmit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	entry, _, err := h.resolveEntryKey(ctx, chi.URLParam(r, "key"))
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		log.Printf("public.commentSubmit: load entry: %v", err)
		http.Error(w, "failed to load entry", http.StatusInternalServerError)
		return
	}
	entryID := entry.ID

	weblog, err := h.Store.WeblogByID(ctx, h.WID)
	if err != nil {
		log.Printf("public.commentSubmit: load weblog: %v", err)
		http.Error(w, "site not configured", http.StatusInternalServerError)
		return
	}
	if weblog.CommentMode == domain.CommentClosed {
		http.Error(w, "comments are closed", http.StatusForbidden)
		return
	}
	if entry.Status != domain.EntryPublished {
		http.NotFound(w, r)
		return
	}

	// Preserve the URL key the user submitted from so the redirect lands
	// on the same canonical surface (slug when present, id otherwise).
	siteBack := root(r) + "/entry/" + entryKeyFor(entry) + "/"
	redirectBack := func(reason string) {
		target := siteBack
		if reason != "" {
			target += "?err=" + url.QueryEscape(reason) + "#comment-form"
		}
		http.Redirect(w, r, target, http.StatusSeeOther)
	}

	if err := r.ParseForm(); err != nil {
		redirectBack(tr(weblog, r, "comment.error.parseForm"))
		return
	}

	// Honeypot — legitimate users can't see or fill this field.
	if strings.TrimSpace(r.PostFormValue("website")) != "" {
		log.Printf("public.commentSubmit: honeypot tripped from %s", h.clientIP(r))
		redirectBack("")
		return
	}

	// IP blacklist — silent drop of any client matching a configured
	// block range. Runs before any other anti-spam layer so blocked
	// ranges never touch the DB or the Turnstile API. Empty /
	// misconfigured lists are no-ops.
	if blocklist := spam.ParseIPBlocklist(weblog.IPBlacklist); len(blocklist) > 0 {
		if ipAddr := h.clientIP(r); ipAddr != "" && blocklist.Contains(ipAddr) {
			log.Printf("public.commentSubmit: ip-blacklist hit from %s", ipAddr)
			redirectBack("")
			return
		}
	}

	// Time check — reject submissions that arrive suspiciously soon after
	// the form was rendered.
	if ts, err := strconv.ParseInt(r.PostFormValue("_ts"), 10, 64); err == nil {
		if time.Since(time.Unix(ts, 0)) < minFormLifetime {
			redirectBack(tr(weblog, r, "comment.error.tooFast"))
			return
		}
	}

	name := strings.TrimSpace(r.PostFormValue("name"))
	email := strings.TrimSpace(r.PostFormValue("email"))
	urlField := strings.TrimSpace(r.PostFormValue("url"))
	body := strings.TrimSpace(r.PostFormValue("description"))
	if name == "" || body == "" {
		redirectBack(tr(weblog, r, "comment.error.required"))
		return
	}
	const maxBodyLen = 5000
	if len(body) > maxBodyLen {
		redirectBack(tr(weblog, r, "comment.error.tooLong"))
		return
	}
	// URL allow-list: http / https / mailto only. Rejects
	// javascript:, data:, vbscript:, etc. so a stored URL can never
	// ride an anchor into a browser-executed script.
	if urlField != "" && !isAllowedCommentURLScheme(urlField) {
		redirectBack(tr(weblog, r, "comment.error.badScheme"))
		return
	}

	// Spam-word check: silent rejection, same UX as honeypot trip.
	if spam.MatchesAny([]string{name, email, urlField, body}, spam.ParseWords(weblog.SpamWords)) {
		log.Printf("public.commentSubmit: spam word match from %s", h.clientIP(r))
		redirectBack("")
		return
	}

	// Turnstile verification. Skipped entirely when not configured.
	if h.Turnstile != nil && h.Turnstile.Enabled() {
		token := r.PostFormValue("cf-turnstile-response")
		ok, err := h.Turnstile.Verify(ctx, token, h.clientIP(r))
		if err != nil {
			log.Printf("public.commentSubmit: turnstile error: %v", err)
			redirectBack(tr(weblog, r, "comment.error.turnstileVerify"))
			return
		}
		if !ok {
			redirectBack(tr(weblog, r, "comment.error.turnstileFail"))
			return
		}
	}

	ip := h.clientIP(r)
	if ip != "" {
		if n, err := h.Store.CountRecentCommentsFromIP(ctx, ip, commentRateWindow); err == nil && n >= commentRateLimit {
			redirectBack(tr(weblog, r, "comment.error.rateLimit"))
			return
		}
	}

	status := resolveMessageStatus(ctx, h, weblog.CommentMode, email)

	msg := domain.Message{
		WID:         h.WID,
		EntryID:     entry.ID,
		Status:      status,
		PostedAt:    time.Now(),
		AuthorName:  name,
		AuthorEmail: email,
		AuthorURL:   urlField,
		Body:        body,
		IPAddress:   ip,
		UserAgent:   r.UserAgent(),
	}
	if _, err := h.Store.CreateMessage(ctx, msg); err != nil {
		log.Printf("public.commentSubmit: create: %v", err)
		http.Error(w, "failed to save comment", http.StatusInternalServerError)
		return
	}

	// Cookie prefill — save on explicit opt-in, clear on opt-out so visitors
	// can change machines / reset. Classic SB3 behaviour.
	if r.PostFormValue("set_cookie") == "1" {
		setPrefillCookie(w, r, commenterCookieName, name)
		setPrefillCookie(w, r, commenterCookieEmail, email)
		setPrefillCookie(w, r, commenterCookieURL, urlField)
	} else {
		clearPrefillCookie(w, commenterCookieName)
		clearPrefillCookie(w, commenterCookieEmail)
		clearPrefillCookie(w, commenterCookieURL)
	}

	// Successful submit: drop back to the entry page. Using SeeOther (303)
	// converts the POST into a GET so refreshes don't resend.
	http.Redirect(w, r, root(r)+fmt.Sprintf("/entry/%d/#comments", entryID), http.StatusSeeOther)
}

// resolveMessageStatus turns the weblog's CommentMode + the submitter's
// email into a concrete starting status. "open" always publishes; "closed"
// is rejected earlier so never reaches here; "moderated" auto-approves when
// the email has been vetted before ("trust memory") and otherwise queues.
func resolveMessageStatus(ctx context.Context, h *Handler, mode domain.CommentMode, email string) domain.MessageStatus {
	if mode == domain.CommentOpen {
		return domain.MessageApproved
	}
	if email == "" {
		return domain.MessageWaiting
	}
	trusted, err := h.Store.HasApprovedCommentFromEmail(ctx, h.WID, email)
	if err != nil {
		log.Printf("public.commentSubmit: trust lookup: %v", err)
		return domain.MessageWaiting
	}
	if trusted {
		return domain.MessageApproved
	}
	return domain.MessageWaiting
}

// ---- likes --------------------------------------------------------------

// likedCookieName returns the short-circuit cookie name scoped to one entry.
// The cookie exists purely for UX — the DB fingerprint row is what actually
// enforces uniqueness.
func likedCookieName(entryID int64) string {
	return "sb_liked_" + strconv.FormatInt(entryID, 10)
}

// fingerprintFor hashes the client IP + User-Agent into a short hex string
// that we can uniquely-index per entry. It's not a security boundary —
// determined attackers rotate IPs — but it catches the obvious "click 10
// times" case even when cookies are cleared.
func (h *Handler) fingerprintFor(r *http.Request) string {
	sum := sha256.Sum256([]byte(h.clientIP(r) + "|" + r.UserAgent()))
	return hex.EncodeToString(sum[:16])
}

func (h *Handler) entryLike(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	entry, _, err := h.resolveEntryKey(ctx, chi.URLParam(r, "key"))
	if err != nil || entry.Status != domain.EntryPublished {
		http.NotFound(w, r)
		return
	}
	entryID := entry.ID

	back := root(r) + "/entry/" + entryKeyFor(entry) + "/"

	// Cookie short-circuit — avoids a needless DB round-trip when a browser
	// has already liked this entry.
	if _, err := r.Cookie(likedCookieName(entryID)); err == nil {
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}

	newLike, err := h.Store.LikeEntry(ctx, entryID, h.fingerprintFor(r))
	if err != nil {
		log.Printf("public.entryLike: %v", err)
		http.Error(w, "failed to record like", http.StatusInternalServerError)
		return
	}

	// Always set the cookie so future attempts short-circuit cheaply, even
	// when the fingerprint rejected the repeat.
	http.SetCookie(w, &http.Cookie{
		Name:     likedCookieName(entryID),
		Value:    "1",
		Path:     "/",
		MaxAge:   365 * 24 * 60 * 60,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
	})
	_ = newLike
	http.Redirect(w, r, back, http.StatusSeeOther)
}

// stampedCookieName is the scoped short-circuit cookie set once per
// (entry, kind) reaction. Matches the per-entry like cookie's naming
// so the browser devtools cookie list stays scannable.
func stampedCookieName(entryID int64, kind domain.StampKind) string {
	return "sb_stamped_" + strconv.FormatInt(entryID, 10) + "_" + string(kind)
}

func (h *Handler) entryStamp(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	entry, _, err := h.resolveEntryKey(ctx, chi.URLParam(r, "key"))
	if err != nil || entry.Status != domain.EntryPublished {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	kind := domain.StampKind(r.PostFormValue("kind"))
	if !kind.Valid() {
		http.Error(w, "invalid stamp kind", http.StatusBadRequest)
		return
	}
	entryID := entry.ID

	back := root(r) + "/entry/" + entryKeyFor(entry) + "/"

	// Cookie short-circuit per (entry, kind) so re-clicking the same
	// reaction button doesn't need a DB round-trip.
	if _, err := r.Cookie(stampedCookieName(entryID, kind)); err == nil {
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}

	if _, err := h.Store.StampEntry(ctx, entryID, kind, h.fingerprintFor(r)); err != nil {
		log.Printf("public.entryStamp: %v", err)
		http.Error(w, "failed to record stamp", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     stampedCookieName(entryID, kind),
		Value:    "1",
		Path:     "/",
		MaxAge:   365 * 24 * 60 * 60,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
	})
	http.Redirect(w, r, back, http.StatusSeeOther)
}

// readCookieEscaped pulls one of the commenter-prefill cookies, URL-decodes
// it, and HTML-escapes the result so it can land in a `value="..."`
// attribute without breaking layout or enabling script injection.
func readCookieEscaped(r *http.Request, name string) string {
	c, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	raw, err := url.QueryUnescape(c.Value)
	if err != nil {
		raw = c.Value
	}
	return html.EscapeString(raw)
}

func setPrefillCookie(w http.ResponseWriter, r *http.Request, name, value string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    url.QueryEscape(value),
		Path:     "/",
		MaxAge:   int(commenterCookieTTL.Seconds()),
		HttpOnly: false, // UX cookie — JS may read it to pre-fill a richer editor later.
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
	})
}

func clearPrefillCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{
		Name:   name,
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
}

// turnstileWidget returns the embed HTML when the injected verifier is
// enabled. Handlers that don't plug in a Turnstile verifier just get "".
func turnstileWidget(v turnstile.Verifier) string {
	if v == nil || !v.Enabled() {
		return ""
	}
	if c, ok := v.(interface{ WidgetHTML() string }); ok {
		return c.WidgetHTML()
	}
	return ""
}

// clientIP pulls the originating IP from common proxy headers before falling
// back to RemoteAddr. Good enough for rate-limit fingerprinting; not a
// security boundary.
// isAllowedCommentURLScheme accepts only http / https / mailto plus
// the schemeless forms (site-relative / protocol-relative URLs).
// Guards against `javascript:` / `data:` / `vbscript:` etc. being
// stored and later clicked by a reader. The render-time helper
// `safeExternalURL` in internal/content is a belt-and-braces second
// line of defence.
func isAllowedCommentURLScheme(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return true
	}
	if strings.HasPrefix(s, "//") || strings.HasPrefix(s, "/") {
		return true
	}
	colon := strings.Index(s, ":")
	if colon < 0 {
		return true // relative — no scheme to worry about
	}
	switch strings.ToLower(s[:colon]) {
	case "http", "https", "mailto":
		return true
	}
	return false
}

// clientIP resolves the originating client address through h.TrustedProxies.
// Forwarded headers are honoured only when the immediate peer is on the
// configured proxy list; everyone else gets RemoteAddr so a directly-
// exposed binary can't be spoofed via X-Forwarded-For.
func (h *Handler) clientIP(r *http.Request) string {
	return h.TrustedProxies.From(r)
}

// ---- URL param helpers --------------------------------------------------

func parseYear(raw string) (int, bool) {
	y, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	if y < 1970 || y > 9999 {
		return 0, false
	}
	return y, true
}

func parseMonth(raw string) (int, bool) {
	m, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	if m < 1 || m > 12 {
		return 0, false
	}
	return m, true
}

func padMonth(m int) string {
	if m < 10 {
		return "0" + strconv.Itoa(m)
	}
	return strconv.Itoa(m)
}

func writeHTML(w http.ResponseWriter, body, tag string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := w.Write([]byte(body)); err != nil {
		log.Printf("%s: write: %v", tag, err)
	}
}
