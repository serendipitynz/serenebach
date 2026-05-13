// Package importer migrates data from a Serene Bach 3 SQLite database into
// the Go version's schema. Scope is deliberately narrow: weblog metadata,
// categories, templates (as inactive), and published entries. Users,
// comments, images, plugins, and Amazon/trackback tables are skipped —
// SB3 used crypt() hashes incompatible with bcrypt, and the rest either
// belong to dropped features or will land in later phases.
package importer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/serendipitynz/serenebach/internal/importer/initparser"
	"github.com/serendipitynz/serenebach/internal/template/lint"
	"github.com/serendipitynz/serenebach/internal/template/sbtemplate"
)

// Report summarises the work done by an import run.
type Report struct {
	WeblogUpdated  bool
	Templates      int
	Categories     int
	Entries        int
	SkippedEntries int // closed / draft rows the caller asked us to skip
	Warnings       []string
}

// Options controls import behaviour. Defaults are fine for the common
// "import my own blog" case.
type Options struct {
	// TargetWID is the weblog id the imported rows are bound to. Zero
	// resolves to 1 — every test and CLI path has historically used the
	// seeded default weblog.
	TargetWID int64
	// AuthorID is the user id every imported entry is attributed to. It
	// MUST exist in the destination database. Zero resolves to 1 (the
	// seeded admin).
	AuthorID int64
	// OnlyPublished, when true, skips entries whose status is not
	// EntryPublished (== 1). Defaults to false (= include drafts and
	// closed rows). Every existing caller passes this explicitly, so the
	// zero-value semantic is here only as a guardrail.
	OnlyPublished bool
	// DataDir is the SB3 data directory holding configure.cgi / init.cgi.
	// Empty means auto-detect from the parent (or grandparent) of
	// sourcePath; the importer is happy to run without a flat-file dir
	// at all and fall back to sb_config alone.
	DataDir string
	// SBVersion selects the source format. 0 / 3 read from a SB3 SQLite
	// database (sourcePath = data.db). 2 reads from a SB2 flat-file
	// data directory (sourcePath = the data dir itself; DataDir is
	// ignored in that mode). Other values return an error.
	SBVersion int
}

// SB3 config.pl defaults used when sb_config has no override row. Real
// installations often left every default in place, so applying these is
// the difference between recording usable redirect inputs and recording
// nothing for those weblogs.
const (
	legacyDefaultArchiveType = "Individual"
	legacyDefaultLogPath     = "log/"
	legacyDefaultBasePath    = "/"
	legacyDefaultCgiName     = "sb.cgi"
	legacyDefaultIDPrefix    = "eid"
	legacyDefaultSuffix      = ".html"
)

// legacyURLConfig captures the per-weblog SB3 settings needed to
// reconstruct old public URLs at redirect time. The redirect layer reads
// the same values back out of weblogs.legacy_*.
type legacyURLConfig struct {
	ArchiveType string
	LogPath     string
	BasePath    string
	CgiName     string
	IDPrefix    string
	Suffix      string
}

func defaultLegacyURLConfig() legacyURLConfig {
	return legacyURLConfig{
		ArchiveType: legacyDefaultArchiveType,
		LogPath:     legacyDefaultLogPath,
		BasePath:    legacyDefaultBasePath,
		CgiName:     legacyDefaultCgiName,
		IDPrefix:    legacyDefaultIDPrefix,
		Suffix:      legacyDefaultSuffix,
	}
}

// Import opens the source described by opts.SBVersion at sourcePath
// (read-only) and copies data into dest. The entire operation runs in
// a single destination transaction: any error rolls back everything.
//
// SBVersion 0 / 3: sourcePath is a SB3 data.db (SQLite).
// SBVersion 2:     sourcePath is the SB2 data directory (flat files).
func Import(ctx context.Context, dest *sql.DB, sourcePath string, opts Options) (*Report, error) {
	if opts.TargetWID == 0 {
		opts.TargetWID = 1
	}
	if opts.AuthorID == 0 {
		opts.AuthorID = 1
	}

	switch opts.SBVersion {
	case 0, 3:
		return importSB3(ctx, dest, sourcePath, opts)
	case 2:
		return importSB2(ctx, dest, sourcePath, opts)
	default:
		return nil, fmt.Errorf("importer: unsupported SB version %d (expected 2 or 3)", opts.SBVersion)
	}
}

// importSB3 is the original Import body — SB3 SQLite source. Kept as
// its own function so the dispatcher in Import stays readable.
func importSB3(ctx context.Context, dest *sql.DB, sourcePath string, opts Options) (*Report, error) {
	absPath, err := filepath.Abs(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("importer: abs path: %w", err)
	}
	src, err := openSB3Source(ctx, absPath)
	if err != nil {
		return nil, err
	}
	defer src.Close()

	tx, err := dest.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("importer: begin tx: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	report := &Report{}
	if err := verifyDestinationAuthor(ctx, tx, opts.AuthorID); err != nil {
		return nil, err
	}

	sb3WID, err := importWeblog(ctx, src, tx, opts, report)
	if err != nil {
		return nil, err
	}
	cfg, err := loadLegacyConfig(ctx, src, sb3WID, resolveSB3DataDir(opts.DataDir, absPath))
	if err != nil {
		return nil, err
	}
	if err := applyLegacyConfig(ctx, tx, opts.TargetWID, cfg); err != nil {
		return nil, err
	}
	if err := importTemplates(ctx, src, tx, opts, report); err != nil {
		return nil, err
	}
	catMap, err := importCategories(ctx, src, tx, opts, report)
	if err != nil {
		return nil, err
	}
	if err := importEntries(ctx, src, tx, opts, catMap, report); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("importer: commit: %w", err)
	}
	tx = nil
	return report, nil
}

// openSB3Source opens absPath as a read-only SB3 SQLite source and
// pings it. Returned *sql.DB is the caller's responsibility to Close.
func openSB3Source(ctx context.Context, absPath string) (*sql.DB, error) {
	src, err := sql.Open("sqlite", "file:"+absPath+"?mode=ro&_pragma=query_only(true)")
	if err != nil {
		return nil, fmt.Errorf("importer: open source: %w", err)
	}
	if err := src.PingContext(ctx); err != nil {
		_ = src.Close()
		return nil, fmt.Errorf("importer: ping source: %w", err)
	}
	return src, nil
}

// resolveSB3DataDir returns the explicit override when non-empty, else
// auto-detects from absPath. Encapsulates the empty-string branch so
// callers don't have to.
func resolveSB3DataDir(explicit, absPath string) string {
	if explicit != "" {
		return explicit
	}
	return detectDataDir(absPath)
}

// verifyDestinationAuthor fails fast when opts.AuthorID does not refer
// to an existing user row. Shared by both SB2 and SB3 importers.
func verifyDestinationAuthor(ctx context.Context, tx *sql.Tx, authorID int64) error {
	var name string
	if err := tx.QueryRowContext(ctx, `SELECT name FROM users WHERE id = ?`, authorID).Scan(&name); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("importer: AuthorID %d does not exist in destination — seed an admin user first", authorID)
		}
		return fmt.Errorf("importer: check author: %w", err)
	}
	return nil
}

// importWeblog copies the first source weblog's title/description and
// returns the SB3 weblog_id so subsequent passes (notably sb_config
// lookup) can scope their queries.
func importWeblog(ctx context.Context, src *sql.DB, tx *sql.Tx, opts Options, report *Report) (int64, error) {
	var sb3WID int64
	var title, desc string
	err := src.QueryRowContext(ctx,
		`SELECT weblog_id, COALESCE(weblog_title,''), COALESCE(weblog_text,'') FROM sb_weblog ORDER BY weblog_id LIMIT 1`,
	).Scan(&sb3WID, &title, &desc)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			report.Warnings = append(report.Warnings, "source has no sb_weblog row; leaving destination weblog untouched")
			return 0, nil
		}
		return 0, fmt.Errorf("importer: read weblog: %w", err)
	}
	now := time.Now().Unix()
	if _, err := tx.ExecContext(ctx, `
		UPDATE weblogs SET title = ?, description = ?, updated_at = ? WHERE id = ?`,
		title, desc, now, opts.TargetWID); err != nil {
		return 0, fmt.Errorf("importer: update weblog: %w", err)
	}
	report.WeblogUpdated = true
	return sb3WID, nil
}

// loadLegacyConfig builds the legacy URL config by layering, in
// increasing priority order:
//
//  1. config.pl defaults (defaultLegacyURLConfig)
//  2. sb_config rows (rarely populated in real installs, but the schema
//     has supported it since SB3.0)
//  3. <data-dir>/init.cgi   — installation-time overrides
//  4. <data-dir>/configure.cgi — admin-edited settings, the actual
//     source of truth for most live blogs
//
// Each layer overlays only its non-empty values, mirroring sb::Config's
// own load semantics. Missing files / missing sb_config table are fine
// — both flat-file and DB sources are best-effort.
func loadLegacyConfig(ctx context.Context, src *sql.DB, sb3WID int64, dataDir string) (legacyURLConfig, error) {
	merged := map[string]string{}
	if err := overlaySBConfig(ctx, merged, src, sb3WID); err != nil {
		return legacyURLConfig{}, err
	}
	if dataDir != "" {
		init, err := initparser.ParseFile(filepath.Join(dataDir, "init.cgi"))
		if err != nil {
			return legacyURLConfig{}, fmt.Errorf("importer: read init.cgi: %w", err)
		}
		overlay(merged, init)
		conf, err := initparser.ParseFile(filepath.Join(dataDir, "configure.cgi"))
		if err != nil {
			return legacyURLConfig{}, fmt.Errorf("importer: read configure.cgi: %w", err)
		}
		overlay(merged, conf)
	}

	cfg := defaultLegacyURLConfig()
	if v := merged["conf_entry_archive"]; v != "" {
		cfg.ArchiveType = v
	}
	if v := merged["conf_dir_log"]; v != "" {
		cfg.LogPath = v
	}
	// conf_srv_base is the canonical source. sb::Config::verify_values
	// substitutes conf_srv_cgi when conf_srv_base is empty (SB2 deployments
	// often only set the cgi URL), so the importer does the same.
	if v := merged["conf_srv_base"]; v != "" {
		cfg.BasePath = v
	} else if v := merged["conf_srv_cgi"]; v != "" {
		cfg.BasePath = v
	}
	if v := merged["basic_sb"]; v != "" {
		cfg.CgiName = v
	}
	if v := merged["basic_preid"]; v != "" {
		cfg.IDPrefix = v
	}
	if v := merged["basic_suffix"]; v != "" {
		cfg.Suffix = v
	}
	cfg.normalise()
	return cfg, nil
}

// overlaySBConfig reads the SB3 sb_config rows for sb3WID (and the
// global wid=0 fallback) and writes their non-empty values into dst.
// A missing table or query error is silently treated as "no overrides"
// — this matches the prior behaviour and keeps the importer running on
// unusual source DB shapes. A nil src is treated as "no DB to read"
// so the SB2 importer can call this helper without an SQLite source.
func overlaySBConfig(ctx context.Context, dst map[string]string, src *sql.DB, sb3WID int64) error {
	if src == nil {
		return nil
	}
	rows, err := src.QueryContext(ctx, `
		SELECT config_name, COALESCE(config_data,''), COALESCE(config_wid,0)
		FROM sb_config
		WHERE config_wid = 0 OR config_wid = ?
		ORDER BY config_wid ASC`, sb3WID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	for rows.Next() {
		var name, data string
		var wid int64
		if err := rows.Scan(&name, &data, &wid); err != nil {
			return fmt.Errorf("importer: scan sb_config: %w", err)
		}
		if data == "" {
			continue
		}
		dst[name] = data
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("importer: read sb_config: %w", err)
	}
	return nil
}

// overlay writes every non-empty entry of src into dst, replacing any
// prior value. Empty values are dropped so a later layer can never
// blank out an earlier non-empty setting.
func overlay(dst, src map[string]string) {
	for k, v := range src {
		if v == "" {
			continue
		}
		dst[k] = v
	}
}

// detectDataDir locates the SB3 data dir from a SQLite file path. SB3's
// default install puts data.db directly in `data/` next to configure.cgi
// (the sandbox layout), but some operators move the SQLite under
// `data/sqlite/data.db`; we try both. Empty return means no flat-file
// config could be found — the caller proceeds with defaults + sb_config.
func detectDataDir(sourcePath string) string {
	candidates := []string{filepath.Dir(sourcePath), filepath.Dir(filepath.Dir(sourcePath))}
	for _, dir := range candidates {
		for _, name := range []string{"configure.cgi", "init.cgi"} {
			if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
				return dir
			}
		}
	}
	return ""
}

// normalise rewrites loosely-formatted SB3 inputs into the canonical
// shape the redirect layer expects:
//   - LogPath:  trailing slash, no leading slash (e.g. "log/").
//   - BasePath: path portion only (scheme/host stripped if present), with
//     leading and trailing slash (e.g. "/blog/").
func (c *legacyURLConfig) normalise() {
	c.LogPath = strings.TrimPrefix(c.LogPath, "/")
	if c.LogPath != "" && !strings.HasSuffix(c.LogPath, "/") {
		c.LogPath += "/"
	}
	if i := strings.Index(c.BasePath, "://"); i >= 0 {
		rest := c.BasePath[i+3:]
		if j := strings.Index(rest, "/"); j >= 0 {
			c.BasePath = rest[j:]
		} else {
			c.BasePath = "/"
		}
	}
	if c.BasePath == "" {
		c.BasePath = "/"
	}
	if !strings.HasPrefix(c.BasePath, "/") {
		c.BasePath = "/" + c.BasePath
	}
	if !strings.HasSuffix(c.BasePath, "/") {
		c.BasePath += "/"
	}
}

func applyLegacyConfig(ctx context.Context, tx *sql.Tx, targetWID int64, cfg legacyURLConfig) error {
	if _, err := tx.ExecContext(ctx, `
		UPDATE weblogs
		SET legacy_archive_type = ?,
		    legacy_log_path     = ?,
		    legacy_base_path    = ?,
		    legacy_cgi_name     = ?,
		    legacy_id_prefix    = ?,
		    legacy_suffix       = ?
		WHERE id = ?`,
		cfg.ArchiveType, cfg.LogPath, cfg.BasePath, cfg.CgiName, cfg.IDPrefix, cfg.Suffix,
		targetWID); err != nil {
		return fmt.Errorf("importer: apply legacy config: %w", err)
	}
	return nil
}

func importTemplates(ctx context.Context, src *sql.DB, tx *sql.Tx, opts Options, report *Report) error {
	rows, err := src.QueryContext(ctx, `
		SELECT
			COALESCE(template_name,''),
			COALESCE(template_main,''),
			COALESCE(template_entry,''),
			COALESCE(template_css,''),
			COALESCE(template_info,'')
		FROM sb_template`)
	if err != nil {
		return fmt.Errorf("importer: read templates: %w", err)
	}
	defer rows.Close()

	now := time.Now().Unix()
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO templates (wid, name, is_active, main_body, entry_body, css, info, created_at, updated_at)
		VALUES (?, ?, 0, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("importer: prepare template insert: %w", err)
	}
	defer stmt.Close()

	for rows.Next() {
		var name, main, entry, css, info string
		if err := rows.Scan(&name, &main, &entry, &css, &info); err != nil {
			return fmt.Errorf("importer: scan template: %w", err)
		}
		if name == "" {
			name = "imported"
		}
		if _, err := stmt.ExecContext(ctx, opts.TargetWID, name, main, entry, css, info, now, now); err != nil {
			return fmt.Errorf("importer: insert template %q: %w", name, err)
		}
		report.Templates++
		// Surface tags / blocks the Go port can't populate so the
		// operator knows which pieces of the imported template will
		// render empty (dead form actions, missing sidebar, …). Parse
		// failures are skipped silently — the template saved fine, the
		// lint is a bonus not a gate.
		lintTemplateBody(name, main, report)
		lintTemplateBody(name, entry, report)
	}
	return rows.Err()
}

// lintTemplateBody runs the imported body through sbtemplate + lint
// and pushes any unsupported findings into report.Warnings. `behaviour
// differs` findings are dropped here to keep imports quiet; admins
// can revisit from the template editor later.
func lintTemplateBody(name, body string, report *Report) {
	if body == "" {
		return
	}
	tmpl, err := sbtemplate.Parse(body, sbtemplate.NoCallback)
	if err != nil {
		return
	}
	for _, f := range lint.Analyze(tmpl) {
		if f.Severity != lint.SevUnsupported {
			continue
		}
		report.Warnings = append(report.Warnings,
			fmt.Sprintf("template %q: %s {%s} not supported by Go port (%s)", name, f.Kind, f.Name, f.Note))
	}
}

// importCategories inserts each SB3 category into the destination and
// returns the id mapping so entries can redirect `entry_cat` → new id.
func importCategories(ctx context.Context, src *sql.DB, tx *sql.Tx, opts Options, report *Report) (map[int64]int64, error) {
	rows, err := src.QueryContext(ctx, `
		SELECT
			category_id,
			COALESCE(category_name, ''),
			COALESCE(category_main, 0),
			COALESCE(category_order, 0),
			COALESCE(category_dir, '')
		FROM sb_category
		ORDER BY category_main, category_order, category_id`)
	if err != nil {
		return nil, fmt.Errorf("importer: read categories: %w", err)
	}
	defer rows.Close()

	now := time.Now().Unix()
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO categories (wid, parent_id, name, slug, sort_order, legacy_id, legacy_dir, created_at, updated_at)
		VALUES (?, 0, ?, '', ?, ?, ?, ?, ?)`)
	if err != nil {
		return nil, fmt.Errorf("importer: prepare category insert: %w", err)
	}
	defer stmt.Close()

	idMap := make(map[int64]int64)
	for rows.Next() {
		var sb3ID, parent int64
		var name, dir string
		var order int
		if err := rows.Scan(&sb3ID, &name, &parent, &order, &dir); err != nil {
			return nil, fmt.Errorf("importer: scan category: %w", err)
		}
		res, err := stmt.ExecContext(ctx, opts.TargetWID, name, order, sb3ID, dir, now, now)
		if err != nil {
			return nil, fmt.Errorf("importer: insert category %d/%q: %w", sb3ID, name, err)
		}
		newID, err := res.LastInsertId()
		if err != nil {
			return nil, fmt.Errorf("importer: lastid category %d: %w", sb3ID, err)
		}
		idMap[sb3ID] = newID
		report.Categories++
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Second pass: rebuild parent_id using the id map now that every row is in.
	for sb3ID, newID := range idMap {
		var sb3Parent int64
		if err := src.QueryRowContext(ctx, `SELECT COALESCE(category_main,0) FROM sb_category WHERE category_id = ?`, sb3ID).Scan(&sb3Parent); err != nil {
			continue
		}
		if sb3Parent == 0 {
			continue
		}
		if newParent, ok := idMap[sb3Parent]; ok {
			if _, err := tx.ExecContext(ctx, `UPDATE categories SET parent_id = ? WHERE id = ?`, newParent, newID); err != nil {
				return nil, fmt.Errorf("importer: fix parent for category %d: %w", newID, err)
			}
		}
	}
	return idMap, nil
}

func importEntries(ctx context.Context, src *sql.DB, tx *sql.Tx, opts Options, catMap map[int64]int64, report *Report) error {
	// SQLite is dynamically typed and real SB3 data contains the occasional
	// stringly-typed entry_cat (e.g. "life" instead of 0). Scan into
	// NullString and parse leniently so a single stray row doesn't abort the
	// whole migration.
	rows, err := src.QueryContext(ctx, `
		SELECT
			entry_id,
			COALESCE(entry_subj, ''),
			entry_cat,
			COALESCE(entry_date, 0),
			COALESCE(entry_body, ''),
			COALESCE(entry_more, ''),
			COALESCE(entry_form, ''),
			COALESCE(entry_stat, 0),
			COALESCE(entry_mod, 0),
			COALESCE(entry_file, ''),
			COALESCE(entry_key, '')
		FROM sb_entry
		ORDER BY entry_date`)
	if err != nil {
		return fmt.Errorf("importer: read entries: %w", err)
	}
	defer rows.Close()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO entries (wid, author_id, category_id, title, body, more, format, status, posted_at, legacy_id, legacy_file, keywords, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("importer: prepare entry insert: %w", err)
	}
	defer stmt.Close()

	for rows.Next() {
		var sb3ID, postedAt, modAt int64
		var title, body, more, format, file, keywords string
		var status int
		var catRaw sql.NullString
		if err := rows.Scan(&sb3ID, &title, &catRaw, &postedAt, &body, &more, &format, &status, &modAt, &file, &keywords); err != nil {
			return fmt.Errorf("importer: scan entry: %w", err)
		}
		if opts.OnlyPublished && status != 1 {
			report.SkippedEntries++
			continue
		}
		newCat := resolveCategory(catRaw, catMap, report, sb3ID)
		createdAt := postedAt
		if createdAt == 0 {
			createdAt = time.Now().Unix()
		}
		updatedAt := modAt
		if updatedAt == 0 {
			updatedAt = createdAt
		}
		if _, err := stmt.ExecContext(ctx,
			opts.TargetWID, opts.AuthorID, newCat,
			title, body, more, format, status,
			postedAt, sb3ID, file, keywords,
			createdAt, updatedAt); err != nil {
			return fmt.Errorf("importer: insert entry %d: %w", sb3ID, err)
		}
		report.Entries++
	}
	return rows.Err()
}

// resolveCategory converts an SB3 entry_cat cell (either an integer id or, in
// rare corrupted rows, the category *name* as a string) into the new schema's
// category_id. Unknown values fall back to -1 (uncategorised) plus a warning.
func resolveCategory(raw sql.NullString, catMap map[int64]int64, report *Report, sb3EntryID int64) int64 {
	if !raw.Valid || raw.String == "" {
		return -1
	}
	if id, err := strconv.ParseInt(raw.String, 10, 64); err == nil {
		if mapped, ok := catMap[id]; ok {
			return mapped
		}
		report.Warnings = append(report.Warnings,
			fmt.Sprintf("entry %d references unknown category id %d; imported as uncategorised", sb3EntryID, id))
		return -1
	}
	report.Warnings = append(report.Warnings,
		fmt.Sprintf("entry %d has non-numeric entry_cat %q; imported as uncategorised", sb3EntryID, raw.String))
	return -1
}
