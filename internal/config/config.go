// Package config is the single source of truth for SB_* environment
// variables and CLI flags consumed at startup. Loading happens once in
// main; downstream packages receive a populated struct rather than reading
// os.Getenv on their own.
package config

import (
	"flag"
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/serendipitynz/serenebach/internal/clientip"
)

type Mode string

const (
	ModeServer Mode = "server"
	ModeCGI    Mode = "cgi"
)

type Config struct {
	Mode   Mode
	Addr   string
	DBPath string
	// Turnstile is Cloudflare's bot-check widget. When both fields are set
	// the comment form renders the challenge and the POST handler verifies
	// tokens. Leaving either field empty disables the feature entirely.
	TurnstileSiteKey string
	TurnstileSecret  string
	// AnalyticsDisabled turns off pageview recording entirely.
	AnalyticsDisabled bool
	// AnalyticsDBPath, when set, routes analytics reads/writes to a separate
	// SQLite file (created on demand). Empty means "use the main DB", which
	// keeps backups simple but lets the page_views table grow there.
	AnalyticsDBPath string
	// AnalyticsRetentionDays caps how long raw pageview rows are kept.
	// 0 means "keep forever"; any positive value deletes rows older than
	// that threshold via probabilistic in-request cleanup.
	AnalyticsRetentionDays int
	// MCPAuditDBPath, when set, routes MCP write-tool audit rows to a
	// separate SQLite file (created on demand). Empty means "use the
	// main DB" via the mcp_audit_log table shipped in migration 0030.
	// Mirrors SB_ANALYTICS_DB so operators who already split analytics
	// have a familiar knob for the audit trail.
	MCPAuditDBPath string
	// RebuildOutDir is where the static build lands. Admin UI and the CLI
	// `build` subcommand share the same default so one click does the
	// same thing as `task build-site`.
	RebuildOutDir string
	// ImageDir is where uploaded images land on disk. Served read-only at
	// /img/* and picked up by the static rebuild so pre-rendered pages
	// still find their media.
	ImageDir string
	// TemplateDir is where per-template assets (images / js / css referenced
	// by templates) are written. Served read-only at /template/<template_id>/*
	// and mirrored by the static rebuild.
	TemplateDir string
	// UploadMaxBytes caps a single image upload (Content-Length + body
	// read). Configured via SB_UPLOAD_MAX_MB (default 10 MB).
	UploadMaxBytes int64
	// TrustedProxies decides which peers are allowed to set the
	// X-Forwarded-For / X-Real-IP headers the IP blacklist, login
	// rate-limiter, and like/stamp fingerprint consult. Empty (the
	// default) means "trust nobody — always use RemoteAddr". Configured
	// via SB_TRUSTED_PROXIES as a comma-separated CIDR / address list.
	TrustedProxies clientip.Resolver
	// PublicAllowedOrigins extends the same-origin allow-list used by
	// reader-facing POST endpoints (comment / like / stamp). The
	// runtime always includes weblogs.base_url; this list is for
	// split-origin deployments where the static HTML is served from a
	// different host than the dynamic backend. Configured via
	// SB_PUBLIC_ALLOWED_ORIGINS, comma-separated, e.g.
	// "https://static.example.net".
	PublicAllowedOrigins []string
	// BasePath is the URL prefix under which the app is mounted (e.g.
	// "/sb4"). Empty means the app is at the root. Configured via
	// SB_BASE_PATH; in CGI mode it is auto-detected from SCRIPT_NAME
	// when SB_BASE_PATH is not set.
	BasePath string
	// DevMode disables template and i18n caching so edits on disk are
	// reflected on the next request. Intended for local development only.
	DevMode bool
	// HTTP server timeouts (server mode only; CGI mode uses the host's
	// per-process model). Each field maps to the matching http.Server
	// knob and is overridable via the corresponding SB_* env var
	// (time.ParseDuration syntax, e.g. "10s", "1m"). Defaults are tuned
	// to absorb OG-image generation while keeping Slowloris-class
	// attacks from being practical. ReadTimeout defaults to 0 because
	// http.Server.ReadTimeout covers the request body too; capping it
	// would conflict with multi-megabyte image uploads on slow links.
	// Body size is bounded by MaxBytesReader at the handler layer.
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	// MaxHeaderBytes caps the size of request headers (request line +
	// headers); 0 lets net/http fall back to its 1 MiB default.
	MaxHeaderBytes int
	// ShutdownTimeout bounds how long graceful shutdown waits for
	// in-flight requests to drain after SIGINT/SIGTERM before the
	// process exits.
	ShutdownTimeout time.Duration
	// WebhooksDisabled cuts the outbound-webhook dispatcher to a
	// no-op. Configured via SB_WEBHOOKS_DISABLED=1 — operators reach
	// for it when a misbehaving subscriber is hammering an upstream
	// and removing rows from /admin/settings/webhooks isn't fast
	// enough. Per-row enable/disable lives in the admin UI for
	// non-emergency cases.
	WebhooksDisabled bool
	// TZ is the timezone used to render entry dates and to interpret
	// archive month/year boundaries and admin form posted_at input.
	// Defaults to time.Local so a fresh deploy keeps the historical
	// "host TZ" behaviour. Override via SB_TZ (any IANA name, e.g.
	// "Asia/Tokyo" or "UTC") so the same binary renders identical
	// archives regardless of where it runs (Docker / Sakura / VPS).
	// Always non-nil after Load(); callers can dereference safely.
	//
	// SB2/SB3 stored a per-entry TZ with conf_timezone as default.
	// SB_TZ is the single-process equivalent and is the first step
	// toward restoring that granularity (the per-weblog and per-entry
	// columns are not yet implemented).
	TZ *time.Location
	// ShowVersion is set when the operator passed `-version` /
	// `--version` on the command line. main reads it and prints
	// version.Full() to stdout before any DB or server setup so the
	// flag works even on a freshly-unpacked binary without a config.
	ShowVersion bool
}

// Load parses top-level flags and returns the resulting Config, the name of
// the subcommand (fs.Arg(0), or "" to run the server), and any positional
// arguments passed after the subcommand name so each subcommand can parse
// its own flags/args.
func Load(args []string) (*Config, string, []string, error) {
	fs := flag.NewFlagSet("serenebach", flag.ContinueOnError)

	var (
		mode        = fs.String("mode", "", "run mode: server|cgi (auto-detected if empty)")
		addr        = fs.String("addr", ":8080", "HTTP listen address (server mode only)")
		dbPath      = fs.String("db", envOr("SB_DB", "./data/dev.db"), "path to SQLite database file")
		showVersion = fs.Bool("version", false, "print version and exit")
	)

	if err := fs.Parse(args); err != nil {
		return nil, "", nil, err
	}

	cfg := &Config{
		Addr:                   *addr,
		DBPath:                 *dbPath,
		TurnstileSiteKey:       os.Getenv("SB_TURNSTILE_SITEKEY"),
		TurnstileSecret:        os.Getenv("SB_TURNSTILE_SECRET"),
		AnalyticsDisabled:      os.Getenv("SB_ANALYTICS_DISABLED") == "1",
		AnalyticsDBPath:        os.Getenv("SB_ANALYTICS_DB"),
		AnalyticsRetentionDays: parseAnalyticsRetention(os.Getenv("SB_ANALYTICS_RETENTION_DAYS")),
		MCPAuditDBPath:         os.Getenv("SB_MCP_AUDIT_DB"),
		RebuildOutDir:          envOr("SB_REBUILD_OUT", "./data/public"),
		ImageDir:               envOr("SB_IMAGE_DIR", "./data/img"),
		TemplateDir:            envOr("SB_TEMPLATE_DIR", "./data/templates"),
		UploadMaxBytes:         parseUploadMaxBytes(os.Getenv("SB_UPLOAD_MAX_MB")),
		DevMode:                os.Getenv("SB_DEV") == "1",
		ReadHeaderTimeout:      parseDurationEnv(os.Getenv("SB_READ_HEADER_TIMEOUT"), DefaultReadHeaderTimeout),
		ReadTimeout:            parseDurationEnv(os.Getenv("SB_READ_TIMEOUT"), DefaultReadTimeout),
		WriteTimeout:           parseDurationEnv(os.Getenv("SB_WRITE_TIMEOUT"), DefaultWriteTimeout),
		IdleTimeout:            parseDurationEnv(os.Getenv("SB_IDLE_TIMEOUT"), DefaultIdleTimeout),
		MaxHeaderBytes:         parseMaxHeaderBytesEnv(os.Getenv("SB_MAX_HEADER_BYTES"), DefaultMaxHeaderBytes),
		ShutdownTimeout:        parseDurationEnv(os.Getenv("SB_SHUTDOWN_TIMEOUT"), DefaultShutdownTimeout),
		WebhooksDisabled:       os.Getenv("SB_WEBHOOKS_DISABLED") == "1",
		TZ:                     parseTZEnv(os.Getenv("SB_TZ")),
		ShowVersion:            *showVersion,
	}

	resolver, err := clientip.Parse(os.Getenv("SB_TRUSTED_PROXIES"))
	if err != nil {
		return nil, "", nil, fmt.Errorf("config: SB_TRUSTED_PROXIES: %w", err)
	}
	cfg.TrustedProxies = resolver
	cfg.PublicAllowedOrigins = parseCSV(os.Getenv("SB_PUBLIC_ALLOWED_ORIGINS"))

	switch Mode(*mode) {
	case ModeServer, ModeCGI:
		cfg.Mode = Mode(*mode)
	case "":
		if os.Getenv("GATEWAY_INTERFACE") != "" {
			cfg.Mode = ModeCGI
		} else {
			cfg.Mode = ModeServer
		}
	default:
		cfg.Mode = ModeServer
	}

	// BasePath: explicit env var wins; in CGI mode fall back to the
	// directory component of SCRIPT_NAME (/sb4/serenebach.cgi → /sb4).
	cfg.BasePath = os.Getenv("SB_BASE_PATH")
	if cfg.BasePath == "" && cfg.Mode == ModeCGI {
		if sn := os.Getenv("SCRIPT_NAME"); sn != "" {
			dir := path.Dir(sn)
			if dir != "/" && dir != "." {
				cfg.BasePath = dir
			}
		}
	}
	cfg.BasePath = normalizeBasePath(cfg.BasePath)

	subcmd := ""
	var subArgs []string
	if fs.NArg() > 0 {
		subcmd = fs.Arg(0)
		subArgs = fs.Args()[1:]
	}
	return cfg, subcmd, subArgs, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// DefaultAnalyticsRetentionDays is the cutoff the Config uses when the env
// variable is unset. 30 days is generous enough to spot weekly patterns and
// still bounds the table growth for high-traffic blogs.
const DefaultAnalyticsRetentionDays = 30

// DefaultUploadMaxMB is the ceiling on a single image upload when
// SB_UPLOAD_MAX_MB isn't set. Ten megabytes covers phone-camera JPEGs
// without inviting abuse.
const DefaultUploadMaxMB = 10

// HTTP server timeout defaults. WriteTimeout is intentionally generous
// because OG-image generation runs on the request goroutine on first
// access. ReadTimeout defaults to 0 (no whole-request deadline) because
// http.Server.ReadTimeout covers the body too — combining a short
// deadline with the 10 MiB upload ceiling would cut off legitimate
// uploads on slow links. Slowloris defence is carried by
// ReadHeaderTimeout instead, and body size is bounded by
// MaxBytesReader at the handler layer. ShutdownTimeout caps the drain
// window after SIGINT/SIGTERM.
const (
	DefaultReadHeaderTimeout = 10 * time.Second
	DefaultReadTimeout       = 0
	DefaultWriteTimeout      = 60 * time.Second
	DefaultIdleTimeout       = 120 * time.Second
	DefaultMaxHeaderBytes    = 1 << 20 // 1 MiB
	DefaultShutdownTimeout   = 15 * time.Second
)

// parseTZEnv resolves SB_TZ to a *time.Location. Empty input or an
// unrecognised name falls back to time.Local so a typo never bricks
// the boot path; misconfigurations are visible in logs (the first
// archive page that renders) rather than at startup. Always non-nil.
func parseTZEnv(raw string) *time.Location {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Local
	}
	loc, err := time.LoadLocation(raw)
	if err != nil {
		return time.Local
	}
	return loc
}

func parseDurationEnv(raw string, fallback time.Duration) time.Duration {
	if raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < 0 {
		return fallback
	}
	return d
}

func parseMaxHeaderBytesEnv(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func parseUploadMaxBytes(raw string) int64 {
	if raw == "" {
		return int64(DefaultUploadMaxMB) << 20
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return int64(DefaultUploadMaxMB) << 20
	}
	return int64(n) << 20
}

func parseAnalyticsRetention(raw string) int {
	if raw == "" {
		return DefaultAnalyticsRetentionDays
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return DefaultAnalyticsRetentionDays
	}
	return n
}

// parseCSV splits a comma-separated list, trimming whitespace and
// dropping empty entries. Returns nil for empty input.
// normalizeBasePath strips trailing slashes and collapses "/" to "".
// A leading slash is kept so the result is always either "" or "/sub/path".
func normalizeBasePath(p string) string {
	p = strings.TrimRight(p, "/")
	if p == "" {
		return ""
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return p
}

func parseCSV(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}
