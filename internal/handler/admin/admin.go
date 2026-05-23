// Package admin holds the HTTP handlers for the admin UI: entry / category /
// tag CRUD, comments moderation, templates and design settings, users,
// images, AI features, and the rebuild/setup flows. Routes are exposed in
// two groups: MountProtected for session-guarded admin routes (the bulk of
// the surface) and MountPublic for unauthenticated routes such as
// /admin/login and /admin/static/*. CSRF protection wraps both groups.
package admin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/serendipitynz/serenebach/internal/analytics"
	"github.com/serendipitynz/serenebach/internal/auth"
	"github.com/serendipitynz/serenebach/internal/basepath"
	"github.com/serendipitynz/serenebach/internal/clientip"
	"github.com/serendipitynz/serenebach/internal/csrf"
	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/mcpaudit"
	"github.com/serendipitynz/serenebach/internal/og"
	"github.com/serendipitynz/serenebach/internal/session"
	"github.com/serendipitynz/serenebach/internal/storage/repo"
	"github.com/serendipitynz/serenebach/internal/webhook"
	admintpl "github.com/serendipitynz/serenebach/web/templates/admin"
)

type Handler struct {
	Store     *repo.Store
	Sessions  *session.Manager
	Analytics *analytics.Store // may be nil when analytics is disabled
	Rebuilder *Rebuilder       // may be nil when the rebuild trigger is not wired
	// TrustedProxies gates whether forwarded headers can drive the
	// login rate-limiter's IP key. Same Resolver the public handler
	// uses; configured via SB_TRUSTED_PROXIES.
	TrustedProxies clientip.Resolver
	WID            int64 // weblog id all admin operations bind to
	// ImageDir is the on-disk root for uploaded media. The upload handler
	// writes files here; the app-level /img/* route serves them back.
	ImageDir string
	// TemplateDir is the on-disk root for per-template assets. Each
	// template gets its own <TemplateDir>/<id>/ sub-directory, and the
	// public /template/<id>/<file> route serves them read-only.
	TemplateDir string
	// UploadMaxBytes caps a single image upload. 0 falls back to 10 MB.
	UploadMaxBytes int64
	// TurnstileConfigured mirrors whether both Turnstile keys were set at
	// startup. Purely informational — the public handler owns the actual
	// verifier. Shown on /admin/settings so the admin can tell whether
	// bot checks are on without grepping env vars.
	TurnstileConfigured bool
	// AnalyticsDBPath is the external analytics SQLite path when set. ""
	// means "co-located with the main DB". Shown read-only on settings.
	AnalyticsDBPath string
	// MCPAuditDBPath is the external MCP-audit SQLite path when set. ""
	// means "main DB via the mcp_audit_log table". Shown read-only on
	// /admin/settings/ops.
	MCPAuditDBPath string
	// Audit is the read/write surface for the MCP audit log. nil means
	// auditing is disabled (equivalent to a nil analytics store) — the
	// panel on /admin/settings/ops then renders empty.
	Audit *mcpaudit.Store
	// OG owns Open Graph card generation. May be nil if OG is disabled —
	// callers fall back to skipping card updates, not erroring the save.
	OG *og.Renderer
	// AutoOG controls whether save/publish handlers regenerate the OG
	// card automatically. False in CGI mode — under cgi.Serve the
	// PNG encode peak (~10 MB+) on top of buffered response bytes
	// can OOM-kill shared-hosting processes mid-handler. CGI operators
	// trigger generation explicitly via POST /admin/entries/{id}/og.
	AutoOG bool
	// Setup runs the first-run install against the application. nil
	// disables the /setup endpoint entirely (MountSetup becomes a
	// no-op). app.New populates this with a closure around app.Seed
	// to avoid a handler→app import cycle.
	Setup SetupRunner
	// TZ is the timezone admins type publish dates in (the
	// posted_at form input is interpreted in this zone) and that
	// the same value is rendered back into for editing. Nil falls
	// back to time.Local so test callers keep working without
	// extra wiring; app.New always sets this from config.Config.TZ.
	TZ *time.Location
	// Webhooks dispatches outbound webhook events (entry.published,
	// comment.received, ...). Nil disables the feature entirely; the
	// dispatch helpers check for nil before invoking.
	Webhooks *webhook.Service
}

// tz returns the handler's configured timezone, falling back to
// time.Local when the field has not been wired up.
func (h *Handler) tz() *time.Location {
	if h.TZ != nil {
		return h.TZ
	}
	return time.Local
}

// root returns the deployment base path for the current request (e.g. "/sb4").
// Used to prefix all generated redirect URLs so the app works when mounted
// under a sub-directory (shared hosting CGI, reverse-proxy sub-path, etc.).
func root(r *http.Request) string {
	return basepath.FromContext(r.Context())
}

// MountPublic registers routes that do not require authentication (the
// login form + its POST target, plus the embedded admin assets so the
// login page can style itself before a cookie exists).
func (h *Handler) MountPublic(r chi.Router) {
	r.Get("/login", h.loginForm)
	r.Post("/login", h.loginSubmit)
	r.Get("/static/admin.css", serveAsset("admin.css", "text/css; charset=utf-8"))
	r.Get("/static/admin.js", serveAsset("admin.js", "application/javascript; charset=utf-8"))
	// Logos and favicon live under assets/. Keep serving them by
	// explicit route so the embedded FS stays private (no directory
	// listings, no accidental enumeration of templates/css).
	r.Get("/static/sb_logo_dark.svg", serveEmbedded("assets/sb_logo_dark.svg", "image/svg+xml"))
	r.Get("/static/sb_logo_light.svg", serveEmbedded("assets/sb_logo_light.svg", "image/svg+xml"))
	r.Get("/static/sb_logo_gray.svg", serveEmbedded("assets/sb_logo_gray.svg", "image/svg+xml"))
	r.Get("/static/favicon.png", serveEmbedded("assets/favicon.png", "image/png"))

	// Ace editor ships as a small bundle of files (core + modes + themes).
	// Serve them straight off the embedded FS so Ace's internal loader
	// (`basePath` config) can resolve mode-html.js / theme-solarized_*.js
	// by name without each file needing its own chi route.
	aceSub, err := fs.Sub(admintpl.FS(), "assets/ace")
	if err == nil {
		aceFS := http.StripPrefix("/admin/static/ace/", http.FileServer(http.FS(aceSub)))
		r.Get("/static/ace/*", aceFS.ServeHTTP)
		r.Head("/static/ace/*", aceFS.ServeHTTP)
	}
}

// MountProtected registers routes that require an active session. The caller
// wraps this group in session.RequireUser.
func (h *Handler) MountProtected(r chi.Router) {
	r.Get("/", h.home)
	r.Post("/logout", h.logout)
	h.mountEntries(r)
	h.mountPages(r)
	h.mountImages(r)
	h.mountCategories(r)
	h.mountLinks(r)
	h.mountTags(r)
	h.mountUsers(r)
	h.mountProfile(r)
	h.mountComments(r)
	h.mountAnalytics(r)
	h.mountRebuild(r)
	h.mountSettings(r)
	h.mountWebhooks(r)
	h.mountMCPTokens(r)
	h.mountTemplatesDesign(r)
	h.mountTemplateAssets(r)
	h.mountTemplatePack(r)
	h.mountHelp(r)
	// AI writing-assist endpoint shared by the Ace toolbar (rewrite /
	// continue / summarise) and the entry-form title / tag suggestion
	// buttons.
	r.Post("/ai/compose", h.aiCompose)
}

// assetETag computes a stable ETag from the embedded asset bytes.
// The bytes are immutable for the lifetime of the binary, so the
// hash is computed once at first call and reused. Quotes are part
// of the RFC 7232 strong-validator format.
func assetETag(body []byte) string {
	sum := sha256.Sum256(body)
	return `"` + hex.EncodeToString(sum[:8]) + `"` // 16 hex chars is plenty
}

// serveAsset reads one embedded admin asset and writes it with the given
// Content-Type. The file list is fixed at build time so there's no path
// traversal surface to worry about.
func serveAsset(name, contentType string) http.HandlerFunc {
	body, err := admintpl.Raw(name)
	if err != nil {
		return func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "admin asset missing", http.StatusInternalServerError)
		}
	}
	etag := assetETag(body)
	return func(w http.ResponseWriter, r *http.Request) {
		if match := r.Header.Get("If-None-Match"); match != "" && match == etag {
			w.Header().Set("ETag", etag)
			w.Header().Set("Cache-Control", "public, max-age=300")
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "public, max-age=300")
		w.Header().Set("ETag", etag)
		_, _ = w.Write(body)
	}
}

// serveEmbedded is serveAsset's cousin for files under assets/ (logos,
// favicon). The second path component is resolved against the admin
// template FS and read once at startup so misnamed requests return a
// clear error rather than silently 404'ing at request time.
func serveEmbedded(name, contentType string) http.HandlerFunc {
	body, err := admintpl.Raw(name)
	if err != nil {
		return func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "admin asset missing", http.StatusInternalServerError)
		}
	}
	etag := assetETag(body)
	return func(w http.ResponseWriter, r *http.Request) {
		if match := r.Header.Get("If-None-Match"); match != "" && match == etag {
			w.Header().Set("ETag", etag)
			w.Header().Set("Cache-Control", "public, max-age=86400")
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Header().Set("ETag", etag)
		_, _ = w.Write(body)
	}
}

// ---- login / logout ----------------------------------------------------

type loginData struct {
	Error     string
	Name      string
	Next      string
	CSRFToken string
}

func (h *Handler) loginForm(w http.ResponseWriter, r *http.Request) {
	if session.UserFrom(r.Context()) != nil {
		http.Redirect(w, r, root(r)+safeNext(r.URL.Query().Get("next")), http.StatusFound)
		return
	}
	renderLogin(w, r, loginData{
		Next:      safeNextQueryParam(r.URL.Query().Get("next")),
		CSRFToken: csrf.Token(r.Context()),
	})
}

func (h *Handler) loginSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	name := postFormValue(r, "name")
	pw := r.PostFormValue("password")
	nextParam := r.URL.Query().Get("next")
	token := csrf.Token(r.Context())

	if name == "" || pw == "" {
		renderLogin(w, r, loginData{
			Error: tr(r, "login.errorEmpty"), Name: name, Next: safeNextQueryParam(nextParam), CSRFToken: token,
		})
		return
	}

	// Brute-force guard: block the (ip, name) pair after too many
	// recent failures. bcrypt cost 12 alone was not enough — a
	// determined attacker can still run ~4 guesses / sec, which makes
	// dictionary attacks against a weak password feasible over a day.
	ip := h.loginRemoteIP(r)
	if ok, retry := defaultLoginLimiter.allow(ip, name); !ok {
		if retry > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
		}
		renderLogin(w, r, loginData{
			Error: tr(r, "login.errorRateLimit"), Name: name, Next: safeNextQueryParam(nextParam), CSRFToken: token,
		})
		return
	}

	user, hash, err := h.Store.UserByName(r.Context(), name)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			defaultLoginLimiter.recordFailure(ip, name)
			renderLogin(w, r, loginData{
				Error: tr(r, "login.errorBadCredentials"), Name: name, Next: safeNextQueryParam(nextParam), CSRFToken: token,
			})
			return
		}
		log.Printf("admin.login: lookup: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := auth.VerifyPassword(hash, pw); err != nil {
		defaultLoginLimiter.recordFailure(ip, name)
		renderLogin(w, r, loginData{
			Error: tr(r, "login.errorBadCredentials"), Name: name, Next: safeNextQueryParam(nextParam), CSRFToken: token,
		})
		return
	}

	defaultLoginLimiter.recordSuccess(ip, name)
	if _, err := h.Sessions.Create(r.Context(), w, r, user.ID); err != nil {
		log.Printf("admin.login: create session: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, root(r)+safeNext(nextParam), http.StatusFound)
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if err := h.Sessions.Destroy(r.Context(), w, r); err != nil {
		log.Printf("admin.logout: %v", err)
	}
	http.Redirect(w, r, root(r)+"/admin/login", http.StatusFound)
}

// ---- dashboard ---------------------------------------------------------

type dashboardStats struct {
	PublishedEntries int64
	DraftEntries     int64
	WaitingComments  int64
	ApprovedComments int64
	RecentPV         int64
	RecentUniques    int64
	LastBuildLabel   string
}

type homePageData struct {
	pageBase
	DisplayName   string
	Stats         dashboardStats
	RecentEntries []domain.Entry
}

func (h *Handler) home(w http.ResponseWriter, r *http.Request) {
	u := session.UserFrom(r.Context())
	name := u.DisplayName
	if name == "" {
		name = u.Name
	}

	data := homePageData{
		pageBase: pageBase{
			Title:      tr(r, "home.title"),
			ActiveMenu: "home",
			CSRFToken:  csrf.Token(r.Context()),
			User:       u,
		},
		DisplayName: name,
		Stats:       h.collectDashboardStats(r.Context()),
	}

	entries, err := h.Store.ListEntriesForAdmin(r.Context(), h.wid(), repo.ListEntriesQuery{Limit: 5})
	if err != nil {
		log.Printf("admin.home: recent entries: %v", err)
	}
	data.RecentEntries = entries

	renderMain(w, r, pageHome, data)
}

// collectDashboardStats snapshots every counter the home page shows. Each
// query is fast and independent; we issue them sequentially to keep the
// code straightforward and because SQLite's single-writer model makes
// parallel queries a minor win at best.
func (h *Handler) collectDashboardStats(ctx context.Context) dashboardStats {
	var s dashboardStats
	if n, err := h.Store.CountEntriesByStatus(ctx, h.wid(), domain.EntryPublished); err == nil {
		s.PublishedEntries = n
	}
	if n, err := h.Store.CountEntriesByStatus(ctx, h.wid(), domain.EntryDraft); err == nil {
		s.DraftEntries = n
	}
	if n, err := h.Store.CountMessagesByStatus(ctx, h.wid(), domain.MessageWaiting); err == nil {
		s.WaitingComments = n
	}
	if n, err := h.Store.CountMessagesByStatus(ctx, h.wid(), domain.MessageApproved); err == nil {
		s.ApprovedComments = n
	}
	if h.Analytics != nil {
		if sum, err := h.Analytics.Summarise(ctx, time.Now().Add(-7*24*time.Hour)); err == nil {
			s.RecentPV = sum.PageViews
			s.RecentUniques = sum.UniqueVisitors
		}
	}
	if h.Rebuilder != nil {
		if t := lastBuildTime(h.Rebuilder.OutDir); !t.IsZero() {
			s.LastBuildLabel = t.Format("2006-01-02 15:04")
		}
	}
	return s
}

// ---- helpers -----------------------------------------------------------

// safeNext validates a `next` redirect target to prevent open-redirect abuse.
// Only same-origin relative paths under /admin are accepted; anything else
// falls back to the admin home.
func safeNext(raw string) string {
	if raw == "" {
		return "/admin/"
	}
	u, err := url.Parse(raw)
	if err != nil || u.IsAbs() || u.Host != "" {
		return "/admin/"
	}
	p := u.Path
	if !strings.HasPrefix(p, "/admin") {
		return "/admin/"
	}
	return raw
}

// safeNextQueryParam returns the validated next value for embedding back
// into the login form's action attribute, or "" if raw isn't a safe
// target. Returned as a plain (non-encoded) path because html/template
// applies URL-query escaping automatically when the value is
// interpolated into `action="...?next={{.Next}}"`. Encoding here too
// caused a double-escape (%252F…) that broke the post-login redirect.
func safeNextQueryParam(raw string) string {
	if safeNext(raw) == "/admin/" && raw != "/admin/" {
		return ""
	}
	return raw
}
