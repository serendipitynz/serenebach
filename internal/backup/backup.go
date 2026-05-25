// Package backup creates a consistent ZIP archive of a Serene Bach instance.
// It is consumed by the CLI backup subcommand and can later be reused by an
// admin UI trigger.
package backup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Options tells Run what to archive.
type Options struct {
	// DBPath is the main SQLite database file (required).
	DBPath string

	// Optional separate DB files. Empty means "not configured" — no
	// fallback path is guessed.
	AnalyticsDBPath string
	MCPAuditDBPath  string

	// Asset directories. Empty means "not configured".
	ImageDir      string
	TemplateDir   string
	RebuildOutDir string

	// CLI-derived toggles.
	OutPath          string // "" → auto-name / "-" → stdout
	IncludeAnalytics bool
	IncludePublic    bool
	Excluded         []string // subset of {"images", "templates"}
	Quiet            bool

	// Overridable for tests.
	Now      time.Time
	Hostname string
}

// Report is returned on successful completion.
type Report struct {
	OutPath string
	Size    int64
}

// Run validates options, snapshots the database(s), collects files, and
// writes a ZIP archive.
func Run(ctx context.Context, opts Options) (*Report, error) {
	if err := validate(&opts); err != nil {
		return nil, err
	}

	tmpDir, err := makeTempDir()
	if err != nil {
		return nil, err
	}
	defer removeTempDir(tmpDir)

	manifest := buildManifest(&opts)

	if err := snapshotDBs(ctx, &opts, tmpDir, manifest); err != nil {
		return nil, err
	}

	snapshotDBPath := filepath.Join(tmpDir, "serenebach.db")
	if err := fillTableCounts(ctx, snapshotDBPath, manifest); err != nil {
		return nil, err
	}

	report, err := writeArchive(&opts, tmpDir, manifest)
	if err != nil {
		return nil, err
	}

	return report, nil
}

func validate(opts *Options) error {
	if opts.DBPath == "" {
		return fmt.Errorf("db path is required")
	}
	if _, err := os.Stat(opts.DBPath); err != nil {
		return fmt.Errorf("db path %s: %w", opts.DBPath, err)
	}

	if opts.OutPath != "" && opts.OutPath != "-" {
		dir := filepath.Dir(opts.OutPath)
		if _, err := os.Stat(dir); err != nil {
			return fmt.Errorf("output directory %s: %w", dir, err)
		}
	}

	for _, ex := range opts.Excluded {
		switch ex {
		case "images", "templates":
		default:
			return fmt.Errorf("unknown exclude: %q", ex)
		}
	}

	return nil
}

func makeTempDir() (string, error) {
	return os.MkdirTemp("", "serenebach-backup-*")
}

func removeTempDir(dir string) {
	_ = os.RemoveAll(dir)
}

func logStep(opts *Options, format string, args ...any) {
	if !opts.Quiet {
		fmt.Fprintf(os.Stderr, format, args...)
	}
}

func humanSize(n int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)
	switch {
	case n >= GB:
		return fmt.Sprintf("%.1f GB", float64(n)/GB)
	case n >= MB:
		return fmt.Sprintf("%.1f MB", float64(n)/MB)
	case n >= KB:
		return fmt.Sprintf("%.1f KB", float64(n)/KB)
	default:
		return fmt.Sprintf("%d B", n)
	}
}
