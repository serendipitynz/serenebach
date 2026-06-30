package config

import (
	"testing"
	"time"

	"github.com/serendipitynz/serenebach/internal/csrf"
)

// These tests live in package config (not config_test) so they can reach
// the unexported env/flag parsers directly. They assert the real
// contract — what each parser returns for valid / empty / invalid /
// boundary input — rather than chasing line coverage.

func TestEnvOr(t *testing.T) {
	const key = "SB_TEST_ENVOR"

	t.Run("set value wins", func(t *testing.T) {
		t.Setenv(key, "fromenv")
		if got := envOr(key, "fallback"); got != "fromenv" {
			t.Errorf("envOr = %q, want %q", got, "fromenv")
		}
	})

	t.Run("empty falls back", func(t *testing.T) {
		t.Setenv(key, "")
		if got := envOr(key, "fallback"); got != "fallback" {
			t.Errorf("envOr = %q, want fallback", got)
		}
	})
}

func TestParseTZEnv(t *testing.T) {
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	cases := []struct {
		name string
		raw  string
		want *time.Location
	}{
		{"valid IANA name", "Asia/Tokyo", tokyo},
		{"UTC", "UTC", time.UTC},
		{"empty falls back to Local", "", time.Local},
		{"whitespace trimmed then empty", "   ", time.Local},
		{"unknown name falls back to Local", "Mars/Olympus", time.Local},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseTZEnv(tc.raw)
			if got == nil {
				t.Fatal("parseTZEnv must never return nil")
			}
			if got.String() != tc.want.String() {
				t.Errorf("parseTZEnv(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestParseDurationEnv(t *testing.T) {
	const fallback = 7 * time.Second
	cases := []struct {
		name string
		raw  string
		want time.Duration
	}{
		{"valid seconds", "30s", 30 * time.Second},
		{"valid minutes", "2m", 2 * time.Minute},
		{"zero is accepted", "0s", 0},
		{"empty falls back", "", fallback},
		{"garbage falls back", "soon", fallback},
		{"negative falls back", "-5s", fallback},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseDurationEnv(tc.raw, fallback); got != tc.want {
				t.Errorf("parseDurationEnv(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestParseMaxHeaderBytesEnv(t *testing.T) {
	const fallback = 1 << 20
	cases := []struct {
		name string
		raw  string
		want int
	}{
		{"valid positive", "65536", 65536},
		{"empty falls back", "", fallback},
		{"zero falls back (non-positive)", "0", fallback},
		{"negative falls back", "-1", fallback},
		{"garbage falls back", "1MB", fallback},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseMaxHeaderBytesEnv(tc.raw, fallback); got != tc.want {
				t.Errorf("parseMaxHeaderBytesEnv(%q) = %d, want %d", tc.raw, got, tc.want)
			}
		})
	}
}

func TestParseUploadMaxBytes(t *testing.T) {
	defaultBytes := int64(DefaultUploadMaxMB) << 20
	cases := []struct {
		name string
		raw  string
		want int64
	}{
		{"valid MB converted to bytes", "5", 5 << 20},
		{"empty falls back to default MB", "", defaultBytes},
		{"zero falls back", "0", defaultBytes},
		{"negative falls back", "-3", defaultBytes},
		{"garbage falls back", "ten", defaultBytes},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseUploadMaxBytes(tc.raw); got != tc.want {
				t.Errorf("parseUploadMaxBytes(%q) = %d, want %d", tc.raw, got, tc.want)
			}
		})
	}
}

func TestParseCSRFMultipartMaxBytes(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want int64
	}{
		{"valid raw bytes", "2048", 2048},
		{"large value preserved", "33554432", 33554432}, // 32 MiB, no overflow
		{"empty falls back to csrf default", "", csrf.DefaultMultipartMaxBytes},
		{"zero falls back", "0", csrf.DefaultMultipartMaxBytes},
		{"negative falls back", "-1", csrf.DefaultMultipartMaxBytes},
		{"garbage falls back", "1MiB", csrf.DefaultMultipartMaxBytes},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseCSRFMultipartMaxBytes(tc.raw); got != tc.want {
				t.Errorf("parseCSRFMultipartMaxBytes(%q) = %d, want %d", tc.raw, got, tc.want)
			}
		})
	}
}

func TestParseAnalyticsRetention(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want int
	}{
		{"valid days", "90", 90},
		{"zero means keep forever", "0", 0},
		{"empty falls back to default", "", DefaultAnalyticsRetentionDays},
		{"negative falls back", "-7", DefaultAnalyticsRetentionDays},
		{"garbage falls back", "forever", DefaultAnalyticsRetentionDays},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseAnalyticsRetention(tc.raw); got != tc.want {
				t.Errorf("parseAnalyticsRetention(%q) = %d, want %d", tc.raw, got, tc.want)
			}
		})
	}
}

func TestNormalizeBasePath(t *testing.T) {
	// Drives reverse-proxy sub-path serving, so the realistic operator
	// inputs are weighted heavily here.
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty stays empty", "", ""},
		{"root collapses to empty", "/", ""},
		{"missing leading slash gets one", "foo", "/foo"},
		{"trailing slash stripped", "/foo/", "/foo"},
		{"nested path preserved", "/foo/bar", "/foo/bar"},
		{"nested trailing slash stripped", "/foo/bar/", "/foo/bar"},
		{"double leading slash preserved (only trailing trimmed)", "//foo", "//foo"},
		{"multiple trailing slashes stripped", "/foo///", "/foo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeBasePath(tc.in); got != tc.want {
				t.Errorf("normalizeBasePath(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseCSV(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want []string
	}{
		{"empty yields nil (fail-closed source)", "", nil},
		{"whitespace-only yields nil", "   ", nil},
		{"single element", "a", []string{"a"}},
		{"three elements", "a,b,c", []string{"a", "b", "c"}},
		{"surrounding whitespace trimmed", " a , b , c ", []string{"a", "b", "c"}},
		{"empty elements dropped", "a,,b,", []string{"a", "b"}},
		{"only commas yields nil", ",,,", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseCSV(tc.raw)
			if !equalStrings(got, tc.want) {
				t.Errorf("parseCSV(%q) = %#v, want %#v", tc.raw, got, tc.want)
			}
		})
	}
}

// TestParseCSVEmptyIsFailClosedSource locks the security-relevant
// contract: SB_PUBLIC_ALLOWED_ORIGINS feeds parseCSV, and an empty
// result must stay empty (nil) so NewSameOriginGuard treats the
// allow-list as fail-closed rather than silently widening it.
func TestParseCSVEmptyIsFailClosedSource(t *testing.T) {
	if got := parseCSV(""); len(got) != 0 {
		t.Errorf("parseCSV(\"\") = %#v, want empty so the origin allow-list stays fail-closed", got)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestLoadVersionShortCircuits(t *testing.T) {
	// -version must work even when SB_* env is malformed, so it returns
	// before any env reads. Set a poison value to prove it's ignored.
	t.Setenv("SB_TRUSTED_PROXIES", "not-an-ip")
	cfg, sub, args, err := Load([]string{"-version"})
	if err != nil {
		t.Fatalf("Load(-version) error = %v, want nil", err)
	}
	if !cfg.ShowVersion {
		t.Error("ShowVersion = false, want true")
	}
	if sub != "" || len(args) != 0 {
		t.Errorf("subcommand/args = %q/%v, want empty", sub, args)
	}
}

func TestLoadDefaultsAndFlags(t *testing.T) {
	// Clear the env knobs this test asserts on so an operator's local
	// shell can't shift the expected defaults.
	for _, k := range []string{
		"SB_DB", "SB_DEV", "SB_TZ", "SB_TRUSTED_PROXIES",
		"SB_PUBLIC_ALLOWED_ORIGINS", "SB_BASE_PATH", "GATEWAY_INTERFACE",
		"SB_MAX_HEADER_BYTES", "SB_ANALYTICS_RETENTION_DAYS",
	} {
		t.Setenv(k, "")
	}

	cfg, sub, args, err := Load([]string{"-addr=:9999", "-db=/tmp/x.db"})
	if err != nil {
		t.Fatalf("Load error = %v", err)
	}
	if cfg.Addr != ":9999" {
		t.Errorf("Addr = %q, want :9999", cfg.Addr)
	}
	if cfg.DBPath != "/tmp/x.db" {
		t.Errorf("DBPath = %q, want /tmp/x.db", cfg.DBPath)
	}
	if cfg.Mode != ModeServer {
		t.Errorf("Mode = %q, want server (no GATEWAY_INTERFACE)", cfg.Mode)
	}
	if cfg.TZ == nil {
		t.Error("TZ must be non-nil after Load")
	}
	if cfg.MaxHeaderBytes != DefaultMaxHeaderBytes {
		t.Errorf("MaxHeaderBytes = %d, want default %d", cfg.MaxHeaderBytes, DefaultMaxHeaderBytes)
	}
	if cfg.AnalyticsRetentionDays != DefaultAnalyticsRetentionDays {
		t.Errorf("AnalyticsRetentionDays = %d, want default %d", cfg.AnalyticsRetentionDays, DefaultAnalyticsRetentionDays)
	}
	if sub != "" || len(args) != 0 {
		t.Errorf("subcommand/args = %q/%v, want empty when none given", sub, args)
	}
}

func TestLoadSubcommandAndArgs(t *testing.T) {
	t.Setenv("SB_TRUSTED_PROXIES", "")
	_, sub, args, err := Load([]string{"import", "/path/to/sb3.db", "--flag"})
	if err != nil {
		t.Fatalf("Load error = %v", err)
	}
	if sub != "import" {
		t.Errorf("subcommand = %q, want import", sub)
	}
	if len(args) != 2 || args[0] != "/path/to/sb3.db" || args[1] != "--flag" {
		t.Errorf("subArgs = %v, want [/path/to/sb3.db --flag]", args)
	}
}

func TestLoadCGIModeDetectionAndBasePath(t *testing.T) {
	// CGI is auto-detected from GATEWAY_INTERFACE, and BasePath falls
	// back to the directory of SCRIPT_NAME when SB_BASE_PATH is unset.
	t.Setenv("GATEWAY_INTERFACE", "CGI/1.1")
	t.Setenv("SB_BASE_PATH", "")
	t.Setenv("SCRIPT_NAME", "/sb4/serenebach.cgi")
	t.Setenv("SB_TRUSTED_PROXIES", "")

	cfg, _, _, err := Load(nil)
	if err != nil {
		t.Fatalf("Load error = %v", err)
	}
	if cfg.Mode != ModeCGI {
		t.Errorf("Mode = %q, want cgi", cfg.Mode)
	}
	if cfg.BasePath != "/sb4" {
		t.Errorf("BasePath = %q, want /sb4 (from SCRIPT_NAME dir)", cfg.BasePath)
	}
}

func TestLoadExplicitBasePathWinsOverScriptName(t *testing.T) {
	t.Setenv("GATEWAY_INTERFACE", "CGI/1.1")
	t.Setenv("SB_BASE_PATH", "/explicit/")
	t.Setenv("SCRIPT_NAME", "/sb4/serenebach.cgi")
	t.Setenv("SB_TRUSTED_PROXIES", "")

	cfg, _, _, err := Load(nil)
	if err != nil {
		t.Fatalf("Load error = %v", err)
	}
	if cfg.BasePath != "/explicit" {
		t.Errorf("BasePath = %q, want /explicit (env wins, trailing slash normalized)", cfg.BasePath)
	}
}

func TestLoadRejectsBadTrustedProxies(t *testing.T) {
	// A malformed SB_TRUSTED_PROXIES must fail loudly at startup rather
	// than silently trusting nobody / everybody.
	t.Setenv("SB_TRUSTED_PROXIES", "10.0.0.0/99")
	_, _, _, err := Load(nil)
	if err == nil {
		t.Fatal("Load error = nil, want error for invalid SB_TRUSTED_PROXIES")
	}
}

func TestLoadPublicAllowedOriginsParsed(t *testing.T) {
	t.Setenv("SB_TRUSTED_PROXIES", "")
	t.Setenv("SB_PUBLIC_ALLOWED_ORIGINS", " https://a.example , https://b.example ")
	cfg, _, _, err := Load(nil)
	if err != nil {
		t.Fatalf("Load error = %v", err)
	}
	want := []string{"https://a.example", "https://b.example"}
	if !equalStrings(cfg.PublicAllowedOrigins, want) {
		t.Errorf("PublicAllowedOrigins = %#v, want %#v", cfg.PublicAllowedOrigins, want)
	}
}
