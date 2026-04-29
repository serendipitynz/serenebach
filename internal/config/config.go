package config

import (
	"flag"
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"

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
}

// Load parses top-level flags and returns the resulting Config, the name of
// the subcommand (fs.Arg(0), or "" to run the server), and any positional
// arguments passed after the subcommand name so each subcommand can parse
// its own flags/args.
func Load(args []string) (*Config, string, []string, error) {
	fs := flag.NewFlagSet("serenebach", flag.ContinueOnError)

	var (
		mode   = fs.String("mode", "", "run mode: server|cgi (auto-detected if empty)")
		addr   = fs.String("addr", ":8080", "HTTP listen address (server mode only)")
		dbPath = fs.String("db", envOr("SB_DB", "./data/dev.db"), "path to SQLite database file")
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
