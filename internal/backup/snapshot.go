package backup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/serendipitynz/serenebach/internal/storage/sqlite"
)

// snapshotDBs runs VACUUM INTO for the main DB and any requested
// auxiliary DBs into the temporary directory.
func snapshotDBs(ctx context.Context, opts *Options, tmpDir string, mf *manifest) error {
	steps := []struct {
		srcPath string
		dstName string
	}{
		{opts.DBPath, "serenebach.db"},
	}

	if opts.IncludeAnalytics && opts.AnalyticsDBPath != "" {
		steps = append(steps, struct{ srcPath, dstName string }{opts.AnalyticsDBPath, "analytics.db"})
	}
	if opts.IncludeAnalytics && opts.MCPAuditDBPath != "" {
		steps = append(steps, struct{ srcPath, dstName string }{opts.MCPAuditDBPath, "mcp_audit.db"})
	}

	for i, step := range steps {
		label := "SQLite database"
		if i > 0 {
			label = fmt.Sprintf("aux DB (%s)", step.dstName)
		}
		logStep(opts, "[%d/4] snapshot %s...", i+1, label)

		db, err := sqlite.Open(step.srcPath)
		if err != nil {
			return fmt.Errorf("open %s: %w", step.srcPath, err)
		}

		dst := filepath.Join(tmpDir, step.dstName)
		if _, err := db.ExecContext(ctx, "VACUUM INTO ?", dst); err != nil {
			_ = db.Close()
			return fmt.Errorf("vacuum into %s: %w", step.dstName, err)
		}
		_ = db.Close()

		info, err := os.Stat(dst)
		if err != nil {
			return fmt.Errorf("stat snapshot %s: %w", step.dstName, err)
		}
		mf.addFile(fmt.Sprintf("db/%s", step.dstName), info.Size())

		logStep(opts, " done (%s)\n", humanSize(info.Size()))
	}

	return nil
}
