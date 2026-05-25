package backup

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/serendipitynz/serenebach/internal/storage/sqlite"
	"github.com/serendipitynz/serenebach/internal/version"
)

// manifest is the in-memory representation of manifest.json.
type manifest struct {
	FormatVersion     int            `json:"format_version"`
	SerenebachVersion string         `json:"serenebach_version"`
	CreatedAt         string         `json:"created_at"`
	Host              string         `json:"host"`
	SourcePaths       sourcePaths    `json:"source_paths"`
	Options           backupOptions  `json:"options"`
	Tables            map[string]int `json:"tables"`
	Files             []fileEntry    `json:"files"`
}

type sourcePaths struct {
	DB          string `json:"db"`
	AnalyticsDB string `json:"analytics_db"`
	MCPAuditDB  string `json:"mcp_audit_db"`
	ImageDir    string `json:"image_dir"`
	TemplateDir string `json:"template_dir"`
	RebuildOut  string `json:"rebuild_out"`
}

type backupOptions struct {
	IncludeAnalytics bool     `json:"include_analytics"`
	IncludePublic    bool     `json:"include_public"`
	Excluded         []string `json:"excluded"`
}

type fileEntry struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

func buildManifest(ctx context.Context, opts *Options) (*manifest, error) {
	mf := &manifest{
		FormatVersion:     1,
		SerenebachVersion: version.Public,
		CreatedAt:         opts.Now.UTC().Format(time.RFC3339),
		Host:              opts.Hostname,
		SourcePaths: sourcePaths{
			DB:          opts.DBPath,
			AnalyticsDB: opts.AnalyticsDBPath,
			MCPAuditDB:  opts.MCPAuditDBPath,
			ImageDir:    opts.ImageDir,
			TemplateDir: opts.TemplateDir,
			RebuildOut:  opts.RebuildOutDir,
		},
		Options: backupOptions{
			IncludeAnalytics: opts.IncludeAnalytics,
			IncludePublic:    opts.IncludePublic,
			Excluded:         opts.Excluded,
		},
		Tables: make(map[string]int),
		Files:  []fileEntry{},
	}

	if mf.Host == "" {
		mf.Host, _ = os.Hostname()
	}
	if opts.Now.IsZero() {
		mf.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}

	db, err := sqlite.Open(opts.DBPath)
	if err != nil {
		return nil, fmt.Errorf("manifest: open db: %w", err)
	}
	defer db.Close()

	tables := []string{
		"weblogs", "entries", "messages", "templates",
		"users", "tags", "categories", "pages", "webhooks",
	}
	for _, t := range tables {
		var n int
		if err := db.QueryRowContext(ctx, fmt.Sprintf("SELECT count(*) FROM %s", t)).Scan(&n); err != nil {
			return nil, fmt.Errorf("manifest: count %s: %w", t, err)
		}
		mf.Tables[t] = n
	}

	// redirects may not exist until migration, so treat errors as 0.
	var redirects int
	_ = db.QueryRowContext(ctx, "SELECT count(*) FROM redirects").Scan(&redirects)
	mf.Tables["redirects"] = redirects

	return mf, nil
}

func (m *manifest) addFile(path string, size int64) {
	m.Files = append(m.Files, fileEntry{Path: path, Size: size})
}

func (m *manifest) setSHA256(path string, sum []byte) {
	for i := range m.Files {
		if m.Files[i].Path == path {
			m.Files[i].SHA256 = hex.EncodeToString(sum)
			return
		}
	}
}

func (m *manifest) toJSON() ([]byte, error) {
	return json.MarshalIndent(m, "", "  ")
}
