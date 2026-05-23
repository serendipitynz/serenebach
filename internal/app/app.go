// Package app wires storage, handlers, and middleware into a single *App
// that cmd/serenebach mounts and serves. The constructor takes a *sql.DB
// plus configuration and returns an http.Handler ready for HTTP or CGI
// hosting; no business logic lives here.
package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/serendipitynz/serenebach/internal/analytics"
	"github.com/serendipitynz/serenebach/internal/basepath"
	"github.com/serendipitynz/serenebach/internal/config"
	"github.com/serendipitynz/serenebach/internal/csrf"
	"github.com/serendipitynz/serenebach/internal/handler/admin"
	"github.com/serendipitynz/serenebach/internal/handler/public"
	"github.com/serendipitynz/serenebach/internal/images"
	"github.com/serendipitynz/serenebach/internal/mcp"
	"github.com/serendipitynz/serenebach/internal/mcpaudit"
	"github.com/serendipitynz/serenebach/internal/og"
	"github.com/serendipitynz/serenebach/internal/session"
	"github.com/serendipitynz/serenebach/internal/storage"
	"github.com/serendipitynz/serenebach/internal/storage/repo"
	"github.com/serendipitynz/serenebach/internal/storage/sqlite"
	"github.com/serendipitynz/serenebach/internal/turnstile"
	"github.com/serendipitynz/serenebach/internal/webhook"
	admintpl "github.com/serendipitynz/serenebach/web/templates/admin"
)

// DefaultWID is the weblog id every handler binds to while multi-blog UX is
// de-prioritized. Schema supports arbitrary wid; only the routing layer
// pretends it's single-tenant.
const DefaultWID int64 = 1

const adminLoginPath = "/admin/login"

type App struct {
	Config    *config.Config
	DB        *sql.DB
	Store     *repo.Store
	Sessions  *session.Manager
	Analytics *analytics.Store
	Audit     *mcpaudit.Store
	// Public is the public-side handler. Exposed so tests (and future
	// features that want to swap out dependencies at runtime) can reach in.
	Public *public.Handler
	// Webhooks is the outbound-webhook dispatcher shared by both
	// handler surfaces. Exposed so tests can toggle AllowLoopback
	// without spinning up an alternate App.
	Webhooks *webhook.Service
	handler  http.Handler
}

// New opens the database, applies migrations, and builds the HTTP handler
// shared by both server and CGI modes.
func New(cfg *config.Config) (*App, error) {
	db, err := openAndMigrate(cfg)
	if err != nil {
		return nil, err
	}

	store := repo.New(db)
	sessions := session.NewManager(store)

	analyticsStore, err := openAnalyticsStore(cfg, db)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	if analyticsStore != nil {
		analyticsStore.WithEntryResolver(makeEntryResolver(store, DefaultWID))
	}
	auditStore, err := openAuditStore(cfg, db)
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	applyDevMode(cfg.DevMode)
	// Publish the configured CSRF multipart cap before any router uses
	// the middleware. Defaults to 1 MiB; raise it only when the no-JS
	// fallback genuinely needs larger bodies.
	if cfg.CSRFMultipartMaxBytes > 0 {
		csrf.MultipartMaxBytes = cfg.CSRFMultipartMaxBytes
	}
	webhookSvc := webhook.New(store, cfg.Mode == config.ModeCGI, cfg.WebhooksDisabled)
	adminH := buildAdminHandler(cfg, store, sessions, analyticsStore, auditStore)
	adminH.Webhooks = webhookSvc
	publicH := buildPublicHandler(cfg, store)
	publicH.Webhooks = webhookSvc
	publicMutationGuard := buildPublicMutationGuard(cfg, store)
	mcpSrv := buildMCPServer(cfg, store, analyticsStore, auditStore)

	// Construct the App shell up front so the first-run install
	// callback can close over it. The handler field is filled in once
	// the router below is fully assembled.
	a := &App{
		Config:    cfg,
		DB:        db,
		Store:     store,
		Sessions:  sessions,
		Analytics: analyticsStore,
		Audit:     auditStore,
		Public:    publicH,
		Webhooks:  webhookSvc,
	}
	adminH.Setup = a.makeSetupCallback(store, adminH)

	imgFS, tmplFS, err := buildAssetFS(cfg)
	if err != nil {
		return nil, err
	}
	a.handler = buildRouter(cfg, store, sessions, analyticsStore, publicMutationGuard, mcpSrv, adminH, publicH, imgFS, tmplFS)
	return a, nil
}

// openAndMigrate opens the configured SQLite file and brings the
// schema up to date. A migration failure closes the connection so
// caller doesn't have to.
func openAndMigrate(cfg *config.Config) (*sql.DB, error) {
	db, err := sqlite.Open(cfg.DBPath)
	if err != nil {
		return nil, err
	}
	if err := storage.Up(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("app: migrate: %w", err)
	}
	return db, nil
}

// applyDevMode toggles the package-level dev knobs that the admin
// templates and the admin handler read.
func applyDevMode(devMode bool) {
	if devMode {
		admintpl.DevRoot = "web/templates/admin"
		admin.DevMode = true
		return
	}
	admintpl.DevRoot = ""
	admin.DevMode = false
}

func buildAdminHandler(cfg *config.Config, store *repo.Store, sessions *session.Manager, analyticsStore *analytics.Store, auditStore *mcpaudit.Store) *admin.Handler {
	rebuilder := admin.NewRebuilderWithImages(cfg.RebuildOutDir, cfg.ImageDir, cfg.TemplateDir, cfg.BasePath)
	rebuilder.TZ = cfg.TZ
	// Open Graph renderer pre-parses its fonts + default background at
	// startup. A failure here (missing embedded asset, bad font) logs
	// and skips the feature rather than refusing to boot — the card
	// generation is nice-to-have, not load-bearing.
	// Renderer is built in every mode (cost is small: font parse + one
	// PNG decode, ~1 ms). Auto-regeneration on save is gated separately
	// by Handler.AutoOG so CGI mode avoids the OOM risk while still
	// allowing explicit manual generation via POST /admin/entries/{id}/og.
	ogRenderer, err := og.New()
	if err != nil {
		log.Printf("app: OG renderer disabled: %v", err)
	}
	return &admin.Handler{
		Store:               store,
		Sessions:            sessions,
		Analytics:           analyticsStore,
		Audit:               auditStore,
		Rebuilder:           rebuilder,
		TrustedProxies:      cfg.TrustedProxies,
		WID:                 DefaultWID,
		ImageDir:            cfg.ImageDir,
		TemplateDir:         cfg.TemplateDir,
		UploadMaxBytes:      cfg.UploadMaxBytes,
		TurnstileConfigured: cfg.TurnstileSiteKey != "" && cfg.TurnstileSecret != "",
		AnalyticsDBPath:     cfg.AnalyticsDBPath,
		MCPAuditDBPath:      cfg.MCPAuditDBPath,
		OG:                  ogRenderer,
		AutoOG:              cfg.Mode != config.ModeCGI,
		TZ:                  cfg.TZ,
	}
}

func buildPublicHandler(cfg *config.Config, store *repo.Store) *public.Handler {
	cfVerifier := turnstile.New(cfg.TurnstileSiteKey, cfg.TurnstileSecret)
	publicH := &public.Handler{Store: store, WID: DefaultWID, Turnstile: cfVerifier, TrustedProxies: cfg.TrustedProxies, TZ: cfg.TZ}
	// Load SB3 legacy URL inputs once at startup. A weblog never
	// touched by the importer leaves all fields empty, which the
	// redirect middleware reads as "off". Errors are non-fatal: a
	// missing weblog row means the seed hasn't run yet, and the
	// public surface still wants to come up.
	if l, err := store.WeblogLegacyURLByID(context.Background(), DefaultWID); err == nil {
		publicH.LegacyURL = l
	}
	return publicH
}

// buildPublicMutationGuard assembles the same-origin allow-list for
// reader POSTs (comment / like / stamp). Combines
// SB_PUBLIC_ALLOWED_ORIGINS (split-origin deployments) with the
// weblog's own BaseURL so the typical single-host deployment works
// without env config. BaseURL is read once here; admins who change it
// on /admin/settings need to restart for the guard to pick up the new
// origin.
func buildPublicMutationGuard(cfg *config.Config, store *repo.Store) public.SameOriginGuard {
	allowedOrigins := append([]string{}, cfg.PublicAllowedOrigins...)
	if w, err := store.WeblogByID(context.Background(), DefaultWID); err == nil && w != nil {
		if w.BaseURL != "" {
			allowedOrigins = append(allowedOrigins, w.BaseURL)
		}
	}
	return public.NewSameOriginGuard(allowedOrigins)
}

func buildMCPServer(cfg *config.Config, store *repo.Store, analyticsStore *analytics.Store, auditStore *mcpaudit.Store) *mcp.Server {
	var mcpImageStore *images.Store
	if cfg.ImageDir != "" {
		mcpImageStore = images.NewStore(cfg.ImageDir)
	}
	return &mcp.Server{
		Store:      store,
		Analytics:  analyticsStore,
		ImageStore: mcpImageStore,
		Audit:      auditStore,
		WID:        DefaultWID,
	}
}

// makeSetupCallback wires the first-run install callback. The mutex
// serialises concurrent POSTs to /setup so a second request can't slip
// past the HasAdminUser check while the first is still mid-Seed —
// without it, two POSTs with different admin names would each pass the
// check and each insert a row, leaving the install with two
// administrators. setupMu is process-local; safe because /setup is a
// one-shot endpoint the gate disables for the rest of the lifetime
// once an admin exists.
func (a *App) makeSetupCallback(store *repo.Store, _ *admin.Handler) func(context.Context, admin.SetupRequest) error {
	var setupMu sync.Mutex
	return func(ctx context.Context, req admin.SetupRequest) error {
		setupMu.Lock()
		defer setupMu.Unlock()
		if done, err := store.HasAdminUser(ctx); err != nil {
			return err
		} else if done {
			return admin.ErrSetupAlreadyDone
		}
		err := a.Seed(ctx, SeedSpec{
			AdminName:     req.AdminName,
			AdminPassword: req.AdminPassword,
			AdminEmail:    req.AdminEmail,
			WeblogTitle:   req.WeblogTitle,
			WeblogDesc:    "",
			WeblogBaseURL: "",
			WeblogLang:    "ja",
			TemplateName:  "default",
			SampleEntries: req.SampleEntries,
		})
		// A different process beat us to the admin INSERT (CGI mode
		// runs each request in its own process, so the mutex above
		// can't span the race). Surface that to the handler so it
		// renders 404 instead of redirecting to a login the user's
		// freshly-typed credentials won't work against.
		if errors.Is(err, ErrAdminAlreadyExists) {
			return admin.ErrSetupAlreadyDone
		}
		return err
	}
}

// buildRouter assembles the chi router with all middleware groups and
// mount points. Kept separate so app.New only needs to orchestrate the
// pieces rather than wire each route inline.
func buildRouter(
	cfg *config.Config,
	store *repo.Store,
	sessions *session.Manager,
	analyticsStore *analytics.Store,
	publicMutationGuard public.SameOriginGuard,
	mcpSrv *mcp.Server,
	adminH *admin.Handler,
	publicH *public.Handler,
	imgFS http.Handler,
	tmplFS http.Handler,
) http.Handler {
	r := chi.NewRouter()
	// Inject the deployment base path into every request context so
	// handlers and templates can generate correct URLs without knowing
	// where the app is mounted.
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(basepath.NewContext(r.Context(), cfg.BasePath)))
		})
	})
	r.Use(middleware.Recoverer)
	// SB3 static-archive redirect must run before StripSlashes — its
	// category dir match depends on the trailing slash, which
	// StripSlashes would chop. Off-pattern requests pass through to
	// the next middleware.
	r.Use(publicH.LegacyStaticMiddleware)
	// StripSlashes lets both `/entry/123` and `/entry/123/` hit the same
	// chi route. The trailing-slash form is what Site.*Permalink now emits
	// (so static builds can map each URL to `<path>/index.html`), but we
	// want old dynamic links without the slash to keep working too.
	r.Use(middleware.StripSlashes)

	// MCP HTTP transport. Sits on the bare router — no CSRF
	// (Bearer-token auth replaces it) and no session loading (IDEs /
	// remote clients don't carry admin cookies). Registered before the
	// wider Group below so chi's "middleware must come before routes"
	// rule doesn't block the admin path middlewares.
	r.Post("/mcp", mcpHTTPHandler(store, mcpSrv))

	// /sb.cgi — SB3 compatibility shim. Mounted outside the CSRF
	// middleware because imported SB3 templates POST comments via
	// this URL without a modern csrf_token. The shim only redirects;
	// destination handlers run their own validation.
	publicH.MountLegacy(r)

	// Reader-facing POSTs (comment / like / stamp). Mounted outside
	// the CSRF middleware so static-rebuilt HTML, which has no way
	// to embed a per-session token, can post to the dynamic backend.
	// SameOriginGuard takes CSRF's place; abuse defence still relies
	// on Turnstile + IP blocklist + spam words downstream.
	r.Group(func(r chi.Router) {
		r.Use(publicMutationGuard.Middleware)
		r.Use(sessions.LoadUser)
		if analyticsStore != nil {
			r.Use(analyticsStore.Middleware)
		}
		publicH.MountMutations(r)
	})

	// Everything else (admin UI, public pages, static assets) wraps in
	// a Group so CSRF + sessions + analytics middleware apply. /mcp is
	// deliberately excluded from all three.
	r.Group(func(r chi.Router) {
		// CSRF sits before session loading so the token cookie is
		// available on every response, including the login page.
		r.Use(csrf.Middleware)
		r.Use(sessions.LoadUser)
		if analyticsStore != nil {
			r.Use(analyticsStore.Middleware)
		}
		// First-run setup gate: until an admin user exists, every
		// request that isn't already heading to /setup or the admin
		// asset bundle is bounced to /setup so a fresh deploy lands
		// on the install screen automatically. Once an admin is
		// created the gate flips to a no-op for the rest of the
		// process lifetime.
		r.Use(setupGate(store))

		// /setup is mounted on the root router (not under /admin) so
		// the URL stays short and so it is reachable before any admin
		// session exists. MountSetup is a no-op when adminH.Setup is
		// nil — i.e. when the caller deliberately disables the
		// install flow.
		adminH.MountSetup(r)

		r.Route("/admin", func(r chi.Router) {
			adminH.MountPublic(r)
			r.Group(func(r chi.Router) {
				r.Use(session.RequireUser(adminLoginPath))
				adminH.MountProtected(r)
			})
		})
		r.Group(func(r chi.Router) {
			publicH.Mount(r)
		})

		// /img/* serves uploaded media straight from disk. Backed by
		// os.Root so traversal via "../" or symlinks pointing outside
		// the configured root is rejected at the syscall layer rather
		// than relying on URL cleaning alone. Read-only by construction.
		if imgFS != nil {
			r.Get("/img/*", imgFS.ServeHTTP)
			r.Head("/img/*", imgFS.ServeHTTP)
		}
		// /template/<id>/<file> — logos, backgrounds, any asset
		// referenced via the {site_parts} tag.
		if tmplFS != nil {
			r.Get("/template/*", tmplFS.ServeHTTP)
			r.Head("/template/*", tmplFS.ServeHTTP)
		}
	})
	return r
}

func (a *App) Handler() http.Handler { return a.handler }

// openAnalyticsStore opens the per-deployment analytics store. The main
// DB is the default so a fresh install needs no extra config. Operators
// with a retention policy that doesn't fit alongside the page rows can
// point SB_ANALYTICS_DB at a dedicated SQLite file. Returns (nil, nil)
// when analytics is disabled — the caller's middleware checks for nil
// before installing the request-recording layer.
func openAnalyticsStore(cfg *config.Config, db *sql.DB) (*analytics.Store, error) {
	if cfg.AnalyticsDisabled {
		return nil, nil //nolint:nilnil // a nil store is the documented "disabled" sentinel.
	}
	if cfg.AnalyticsDBPath != "" && cfg.AnalyticsDBPath != cfg.DBPath {
		s, err := analytics.Open(cfg.AnalyticsDBPath, cfg.AnalyticsRetentionDays)
		if err != nil {
			return nil, fmt.Errorf("app: analytics: %w", err)
		}
		return s, nil
	}
	return analytics.WrapMain(db, cfg.AnalyticsRetentionDays), nil
}

// openAuditStore opens the MCP audit-log store. The mcp_audit_log table
// shipped in migration 0030 lives in the main DB; SB_MCP_AUDIT_DB lets
// operators redirect it to a dedicated SQLite file, in which case the
// mcpaudit package creates the schema on first open so no second
// migration run is required.
func openAuditStore(cfg *config.Config, db *sql.DB) (*mcpaudit.Store, error) {
	if cfg.MCPAuditDBPath != "" && cfg.MCPAuditDBPath != cfg.DBPath {
		s, err := mcpaudit.Open(cfg.MCPAuditDBPath)
		if err != nil {
			return nil, fmt.Errorf("app: mcp audit: %w", err)
		}
		return s, nil
	}
	return mcpaudit.WrapMain(db), nil
}

// buildAssetFS constructs the os.Root-backed static asset handlers used
// for /img and /template. Building them up front means any FS error
// (missing parent directory we cannot create, EACCES, etc.) fails the
// App constructor instead of being smuggled into a request handler
// closure that has no error channel. Either return slot may be nil
// when the corresponding directory is not configured.
func buildAssetFS(cfg *config.Config) (http.Handler, http.Handler, error) {
	var imgFS, tmplFS http.Handler
	if cfg.ImageDir != "" {
		h, err := rootedFileServer(cfg.ImageDir, "/img/")
		if err != nil {
			return nil, nil, fmt.Errorf("img file server: %w", err)
		}
		imgFS = h
	}
	if cfg.TemplateDir != "" {
		h, err := rootedFileServer(cfg.TemplateDir, "/template/")
		if err != nil {
			return nil, nil, fmt.Errorf("template file server: %w", err)
		}
		tmplFS = h
	}
	return imgFS, tmplFS, nil
}

// setupGate redirects every request to /setup until an admin user has
// been created. Once an admin exists the gate caches that fact in an
// atomic bool and short-circuits without touching the DB on subsequent
// requests. The allow-list intentionally stays tiny: only /setup
// itself and the admin static bundle (so the install screen can
// style itself) bypass the redirect. /admin/login is *not* allowlisted
// — it would just fail with bad-credentials anyway when there are no
// users, so sending the operator to /setup instead is friendlier.
func setupGate(store *repo.Store) func(http.Handler) http.Handler {
	var done atomic.Bool
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if done.Load() {
				next.ServeHTTP(w, r)
				return
			}
			p := r.URL.Path
			if p == "/setup" || strings.HasPrefix(p, "/admin/static/") {
				next.ServeHTTP(w, r)
				return
			}
			ok, err := store.HasAdminUser(r.Context())
			if err != nil {
				// A DB error here would block every request.
				// Log and fall through — the downstream handler
				// will surface a clearer error.
				log.Printf("app: setup gate: %v", err)
				next.ServeHTTP(w, r)
				return
			}
			if !ok {
				http.Redirect(w, r, basepath.FromContext(r.Context())+"/setup", http.StatusFound)
				return
			}
			done.Store(true)
			next.ServeHTTP(w, r)
		})
	}
}

// mcpHTTPHandler enforces bearer-token auth on /mcp before forwarding
// into the shared MCP dispatch. We deliberately keep the token check
// inline (rather than a chi middleware) so no other route can
// accidentally pick up this authz surface — MCP tokens are *not*
// admin session equivalents, they only unlock /mcp.
func mcpHTTPHandler(store *repo.Store, srv *mcp.Server) http.HandlerFunc {
	const prefix = "Bearer "
	return func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, prefix) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="serenebach mcp"`)
			http.Error(w, "authorization required", http.StatusUnauthorized)
			return
		}
		raw := strings.TrimSpace(auth[len(prefix):])
		if raw == "" {
			http.Error(w, "empty bearer token", http.StatusUnauthorized)
			return
		}
		tok, err := store.MCPTokenByHash(r.Context(), repo.HashMCPToken(raw))
		if err != nil {
			// repo.ErrNotFound and any real error both collapse to 401
			// — surfacing DB errors to an unauthenticated caller would
			// leak state. Real errors stay in the server log.
			if !errors.Is(err, repo.ErrNotFound) {
				log.Printf("mcp: token lookup: %v", err)
			}
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}
		// Best-effort: refresh last_used_at. Doesn't block or fail the
		// request — this is observational, not an authz gate.
		if err := store.TouchMCPToken(r.Context(), tok.ID); err != nil {
			log.Printf("mcp: touch token %d: %v", tok.ID, err)
		}
		// Inject the token's scope so tools/list filters + tools/call
		// gating see per-request authority. Read-scoped tokens never
		// see the write tool catalogue at all, and a direct write
		// attempt returns a tool error.
		r = r.WithContext(mcp.WithAuth(r.Context(), tok.Scope, tok.ID, tok.AuthorID))
		srv.HandleHTTP(w, r)
	}
}

// makeEntryResolver builds an analytics.EntryResolver that maps slug
// entry paths to numeric ids via the main repo. Numeric paths fall back
// to EntryIDFromPath; errors are logged and return 0 so a failing
// resolver never breaks a public request.
func makeEntryResolver(store *repo.Store, wid int64) analytics.EntryResolver {
	return func(ctx context.Context, path string) int64 {
		const prefix = "/entry/"
		if !strings.HasPrefix(path, prefix) {
			return 0
		}
		rest := strings.TrimPrefix(path, prefix)
		key := rest
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			key = rest[:i]
		}
		if key == "" {
			return 0
		}
		// Numeric keys are handled by EntryIDFromPath; skip the DB hit.
		if id, err := strconv.ParseInt(key, 10, 64); err == nil && id > 0 {
			return id
		}
		// Slug key — look it up. Errors are best-effort logged; never
		// propagate to the HTTP response.
		e, err := store.EntryBySlug(ctx, wid, key)
		if err != nil {
			if !errors.Is(err, repo.ErrNotFound) {
				log.Printf("app: analytics resolver: EntryBySlug %q: %v", key, err)
			}
			return 0
		}
		return e.ID
	}
}

func (a *App) Close() error {
	if a.Analytics != nil {
		_ = a.Analytics.Close()
	}
	if a.Audit != nil {
		_ = a.Audit.Close()
	}
	return a.DB.Close()
}
