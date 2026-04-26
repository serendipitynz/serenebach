package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/serendipitynz/serenebach/internal/analytics"
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
	Public  *public.Handler
	handler http.Handler
}

// New opens the database, applies migrations, and builds the HTTP handler
// shared by both server and CGI modes.
func New(cfg *config.Config) (*App, error) {
	db, err := sqlite.Open(cfg.DBPath)
	if err != nil {
		return nil, err
	}
	if err := storage.Up(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("app: migrate: %w", err)
	}

	store := repo.New(db)
	sessions := session.NewManager(store)
	cfVerifier := turnstile.New(cfg.TurnstileSiteKey, cfg.TurnstileSecret)

	// Analytics store lives in the main DB by default. Operators with a
	// retention policy that doesn't fit in the main file can point
	// SB_ANALYTICS_DB at a dedicated SQLite file.
	var analyticsStore *analytics.Store
	if !cfg.AnalyticsDisabled {
		if cfg.AnalyticsDBPath != "" && cfg.AnalyticsDBPath != cfg.DBPath {
			var openErr error
			analyticsStore, openErr = analytics.Open(cfg.AnalyticsDBPath, cfg.AnalyticsRetentionDays)
			if openErr != nil {
				_ = db.Close()
				return nil, fmt.Errorf("app: analytics: %w", openErr)
			}
		} else {
			analyticsStore = analytics.WrapMain(db, cfg.AnalyticsRetentionDays)
		}
	}

	// Open Graph renderer pre-parses its fonts + default background at
	// startup. A failure here (missing embedded asset, bad font) logs
	// and skips the feature rather than refusing to boot — the card
	// generation is nice-to-have, not load-bearing.
	ogRenderer, err := og.New()
	if err != nil {
		log.Printf("app: OG renderer disabled: %v", err)
		ogRenderer = nil
	}

	rebuilder := admin.NewRebuilderWithImages(cfg.RebuildOutDir, cfg.ImageDir, cfg.TemplateDir)
	adminH := &admin.Handler{
		Store:               store,
		Sessions:            sessions,
		Analytics:           analyticsStore,
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
	}
	publicH := &public.Handler{Store: store, WID: DefaultWID, Turnstile: cfVerifier, TrustedProxies: cfg.TrustedProxies}
	// Load SB3 legacy URL inputs once at startup. A weblog never
	// touched by the importer leaves all fields empty, which the
	// redirect middleware reads as "off". Errors are non-fatal: a
	// missing weblog row means the seed hasn't run yet, and the
	// public surface still wants to come up.
	if l, err := store.WeblogLegacyURLByID(context.Background(), DefaultWID); err == nil {
		publicH.LegacyURL = l
	}

	// Build the same-origin allow-list for reader POSTs (comment /
	// like / stamp). Combine SB_PUBLIC_ALLOWED_ORIGINS (split-origin
	// deployments) with the weblog's own BaseURL so the typical
	// single-host deployment works without env config. BaseURL is
	// read once here; admins who change it on /admin/settings need to
	// restart for the guard to pick up the new origin.
	allowedOrigins := append([]string{}, cfg.PublicAllowedOrigins...)
	if w, err := store.WeblogByID(context.Background(), DefaultWID); err == nil && w != nil {
		if w.BaseURL != "" {
			allowedOrigins = append(allowedOrigins, w.BaseURL)
		}
	}
	publicMutationGuard := public.NewSameOriginGuard(allowedOrigins)
	var mcpImageStore *images.Store
	if cfg.ImageDir != "" {
		mcpImageStore = images.NewStore(cfg.ImageDir)
	}

	// MCP audit log: default to the main DB (mcp_audit_log table shipped
	// in migration 0030). When SB_MCP_AUDIT_DB is set to a different
	// path, open a dedicated SQLite file — the mcpaudit package creates
	// the schema on first open so operators don't need a second
	// migration run.
	var auditStore *mcpaudit.Store
	if cfg.MCPAuditDBPath != "" && cfg.MCPAuditDBPath != cfg.DBPath {
		var openErr error
		auditStore, openErr = mcpaudit.Open(cfg.MCPAuditDBPath)
		if openErr != nil {
			_ = db.Close()
			return nil, fmt.Errorf("app: mcp audit: %w", openErr)
		}
	} else {
		auditStore = mcpaudit.WrapMain(db)
	}
	adminH.Audit = auditStore

	mcpSrv := &mcp.Server{
		Store:      store,
		Analytics:  analyticsStore,
		ImageStore: mcpImageStore,
		Audit:      auditStore,
		WID:        DefaultWID,
	}

	r := chi.NewRouter()
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

		// /img/* serves uploaded media straight from disk. chi's
		// FileServer pattern requires the wildcard suffix so the Dir
		// handler receives the sub-path. Read-only by construction.
		if cfg.ImageDir != "" {
			fs := http.StripPrefix("/img/", http.FileServer(http.Dir(cfg.ImageDir)))
			r.Get("/img/*", fs.ServeHTTP)
			r.Head("/img/*", fs.ServeHTTP)
		}
		// /template/<id>/<file> — logos, backgrounds, any asset
		// referenced via the {site_parts} tag.
		if cfg.TemplateDir != "" {
			fs := http.StripPrefix("/template/", http.FileServer(http.Dir(cfg.TemplateDir)))
			r.Get("/template/*", fs.ServeHTTP)
			r.Head("/template/*", fs.ServeHTTP)
		}
	})

	return &App{
		Config:    cfg,
		DB:        db,
		Store:     store,
		Sessions:  sessions,
		Analytics: analyticsStore,
		Audit:     auditStore,
		Public:    publicH,
		handler:   r,
	}, nil
}

func (a *App) Handler() http.Handler { return a.handler }

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

func (a *App) Close() error {
	if a.Analytics != nil {
		_ = a.Analytics.Close()
	}
	if a.Audit != nil {
		_ = a.Audit.Close()
	}
	return a.DB.Close()
}
