// Package public holds the reader-facing HTTP handlers: home, archive,
// category/tag pages, individual entries, feeds (RSS/Atom/RSD), CSS, and
// the comment / like / stamp endpoints. Mutating routes here run under
// SameOriginGuard rather than the CSRF middleware (see sameorigin.go and
// AGENTS.md "Public POST endpoints sit outside the CSRF middleware").
package public

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/serendipitynz/serenebach/internal/basepath"
	"github.com/serendipitynz/serenebach/internal/clientip"
	"github.com/serendipitynz/serenebach/internal/content"
	"github.com/serendipitynz/serenebach/internal/domain"
	publici18n "github.com/serendipitynz/serenebach/internal/handler/public/i18n"
	"github.com/serendipitynz/serenebach/internal/i18n"
	"github.com/serendipitynz/serenebach/internal/storage/repo"
	"github.com/serendipitynz/serenebach/internal/turnstile"
	"github.com/serendipitynz/serenebach/internal/webhook"
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

// commentNumLabel resolves the localised "comments" word for the
// {comment_num} anchor from the public i18n bundle.
func commentNumLabel(lang string) string {
	return publicBundle.T(lang, "comment.numLabel")
}

// siteWithLabel builds a content.Site with the localised comment label
// pre-resolved from the public i18n bundle. The handler layer owns
// the i18n resolution; the content package stays bundle-free.
func siteWithLabel(w domain.Weblog, lang string) content.Site {
	s := content.NewSite(w)
	s.CommentNumLabel = commentNumLabel(lang)
	s.ReadMoreLabel = publicBundle.T(lang, "entry.readMore")
	return s
}

// buildSite is the request-scoped variant of siteWithLabel: it also
// fetches user-defined custom tags from the store so {custom_*}
// placeholders resolve on the rendered page. Errors are logged and
// swallowed so a broken custom-tag query doesn't 500 the public site.
func (h *Handler) buildSite(ctx context.Context, w domain.Weblog) content.Site {
	s := siteWithLabel(w, w.Lang).WithTZ(h.tz())
	tags, err := h.Store.ListCustomTags(ctx, h.WID)
	if err != nil {
		log.Printf("public.buildSite: load custom tags: %v", err)
	}
	s.CustomTags = tags
	return s
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
	// TZ is the timezone used when bucketing entries into year and
	// month archive ranges. Nil falls back to time.Local for
	// backwards compatibility; app.New always sets this from
	// config.Config.TZ so the deployed behaviour is host-independent.
	TZ *time.Location
	// Webhooks dispatches outbound webhook events fired from the
	// public path (comment.received, comment.approved when auto-
	// approved). Nil disables dispatch.
	Webhooks *webhook.Service
}

// tz returns the handler's configured timezone, falling back to
// time.Local when the field has not been wired up (test callers,
// older callsites). Centralised so every archive boundary uses the
// same fallback.
func (h *Handler) tz() *time.Location {
	if h.TZ != nil {
		return h.TZ
	}
	return time.Local
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
	// {key} is either a numeric category id or a custom slug —
	// resolveCategoryKey disambiguates. Symmetric with the entry route.
	r.Get("/category/{key}", h.category)
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
	// sitemap.xml / robots.txt. 404 when the corresponding toggle is off.
	r.Get("/sitemap.xml", h.sitemap)
	r.Get("/robots.txt", h.robotsTxt)
	r.Get("/style.css", h.styleCSS)
	// Per-template CSS endpoint. {site_css} in a page rendered through
	// template N emits this path so category / archive / profile pages
	// pick up their own template's stylesheet, not the active one.
	// chi's trie routes more-specific patterns ahead of `/template/*`
	// (registered at the app level for on-disk assets), so this wins
	// when the path shape matches.
	r.Get("/template/{id}/style.css", h.templateStyleCSS)
	// Catch-all for flat pages. Placed after every specific route so
	// that /entry/, /category/, etc. win first.
	r.Get("/*", h.servePage)
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

// servePage is the catch-all handler for flat pages (/about, /privacy, …).
// It looks up the request path in the pages table and renders via PageView.
// If no page matches, it emits a 404.
func (h *Handler) servePage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	slug := r.URL.Path
	if slug == "" || slug == "/" {
		http.NotFound(w, r)
		return
	}
	// Ensure leading slash.
	if slug[0] != '/' {
		slug = "/" + slug
	}

	page, err := h.Store.PageBySlug(ctx, h.WID, slug)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		log.Printf("public.servePage: lookup %q: %v", slug, err)
		http.Error(w, "failed to load page", http.StatusInternalServerError)
		return
	}
	if page.Status != domain.PagePublished {
		http.NotFound(w, r)
		return
	}

	weblog, err := h.Store.WeblogByID(ctx, h.WID)
	if err != nil {
		log.Printf("public.servePage: load weblog: %v", err)
		http.Error(w, "site not configured", http.StatusInternalServerError)
		return
	}

	var tmpl *domain.Template
	if page.TemplateID != 0 {
		if t, err := h.Store.TemplateByID(ctx, h.WID, page.TemplateID); err == nil {
			tmpl = t
		} else {
			log.Printf("public.servePage: page template %d missing, falling back: %v", page.TemplateID, err)
		}
	}
	if tmpl == nil {
		tmpl, err = h.Store.ActiveTemplate(ctx, h.WID)
		if err != nil {
			log.Printf("public.servePage: load template: %v", err)
			http.Error(w, "no active template", http.StatusInternalServerError)
			return
		}
	}

	profileUsers, err := h.Store.VisibleProfileUsers(ctx, h.WID)
	if err != nil {
		log.Printf("public.servePage: profile users: %v", err)
	}
	sidebar := h.loadSidebarData(ctx, "public.servePage")

	view := content.PageView{
		Site:         h.buildSite(ctx, *weblog).WithBasePath(root(r)),
		Template:     tmpl,
		Page:         *page,
		ProfileUsers: profileUsers,
		Sidebar:      sidebar,
	}
	body, err := view.Render()
	if err != nil {
		log.Printf("public.servePage: render: %v", err)
		http.Error(w, "render error", http.StatusInternalServerError)
		return
	}
	writeHTML(w, body, "public.servePage")
}
