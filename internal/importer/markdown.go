package importer

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/og"
)

// mdFrontMatter is the YAML envelope at the top of each markdown file.
// All fields are optional in YAML decoding; required-ness is enforced
// in the validation step so we can surface targeted warnings.
type mdFrontMatter struct {
	Slug     string `yaml:"slug"`
	Title    string `yaml:"title"`
	Status   string `yaml:"status"`    // "published" | "draft" | "closed" (default published)
	PostedAt string `yaml:"posted_at"` // RFC3339; falls back to file mtime
	Category string `yaml:"category"`  // categories.slug; missing → uncategorised
	Keywords string `yaml:"keywords"`  // SEO meta keywords, stored verbatim
	Pinned   bool   `yaml:"pinned"`
	More     string `yaml:"more"` // optional sequel body
}

// mdRecord is the validated, resolved view of one markdown file ready
// for upsert. categoryID is filled after the category slug is looked
// up against the destination DB; entryID is filled by upsert.
type mdRecord struct {
	path       string
	slug       string
	slugSource string // "frontmatter" | "filename" — diagnostics only
	title      string
	body       string
	more       string
	keywords   string
	pinned     bool
	status     int
	postedAt   int64
	category   string // raw front-matter value; "" means uncategorised
	categoryID int64
	entryID    int64 // populated after upsert; used by OG generation
}

// importMarkdown reads sourceDir (non-recursively) for *.md files,
// validates each one, and upserts the resulting entries into dest.
// OG cards are generated post-commit so a render failure can't roll
// back a successful write. The DB write itself is one transaction
// to match the SB2/SB3 importer contract: all-or-nothing on commit.
func importMarkdown(ctx context.Context, dest *sql.DB, sourceDir string, opts Options) (*Report, error) {
	absDir, err := filepath.Abs(sourceDir)
	if err != nil {
		return nil, fmt.Errorf("importer: abs path: %w", err)
	}
	info, err := os.Stat(absDir)
	if err != nil {
		return nil, fmt.Errorf("importer: stat source dir: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("importer: source path %q is not a directory", absDir)
	}

	report := &Report{}

	paths, err := listMarkdownFiles(absDir)
	if err != nil {
		return nil, err
	}
	records := collectMarkdownRecords(paths, report)

	tx, err := dest.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("importer: begin tx: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	if err := verifyDestinationAuthor(ctx, tx, opts.AuthorID); err != nil {
		return nil, err
	}

	catMap, err := loadCategorySlugMap(ctx, tx, opts.TargetWID)
	if err != nil {
		return nil, err
	}
	for i := range records {
		records[i].categoryID = resolveMDCategory(&records[i], catMap, report)
	}

	if err := upsertMarkdownRecords(ctx, tx, opts, records, report); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("importer: commit: %w", err)
	}
	tx = nil

	// OG cards are nice-to-have for static rebuilds: a render failure
	// here logs + warns but does not invalidate the import.
	if opts.ImageDir != "" {
		generateMarkdownOGCards(ctx, dest, opts, records, report)
	}

	return report, nil
}

// listMarkdownFiles returns the *.md files directly under dir,
// sorted by basename so the import order is stable across runs.
// Subdirectories are explicitly ignored — the spec calls for a
// flat layout to keep slug→file mapping predictable.
func listMarkdownFiles(dir string) ([]string, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("importer: read source dir: %w", err)
	}
	var out []string
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".md") {
			continue
		}
		out = append(out, filepath.Join(dir, name))
	}
	sort.Strings(out)
	return out, nil
}

// collectMarkdownRecords parses each file, validates required fields,
// resolves the slug (frontmatter > filename), and dedupes slugs within
// this run. Errors that affect a single file produce a Warnings entry
// and a skip; only the records that survive are returned for upsert.
func collectMarkdownRecords(paths []string, report *Report) []mdRecord {
	records := make([]mdRecord, 0, len(paths))
	slugSeen := map[string]string{} // slug → first path that claimed it

	for _, p := range paths {
		rec, warns, err := parseMarkdownFile(p)
		report.Warnings = append(report.Warnings, warns...)
		if err != nil {
			report.Warnings = append(report.Warnings,
				fmt.Sprintf("%s: %v", filepath.Base(p), err))
			continue
		}
		if rec == nil {
			continue
		}
		if prev, dup := slugSeen[rec.slug]; dup {
			report.Warnings = append(report.Warnings,
				fmt.Sprintf("%s: duplicate slug %q (already claimed by %s); skipping",
					filepath.Base(p), rec.slug, filepath.Base(prev)))
			continue
		}
		slugSeen[rec.slug] = p
		records = append(records, *rec)
	}
	return records
}

// parseMarkdownFile reads one file, splits front-matter from body, and
// returns a populated mdRecord plus any warnings. A nil record + nil
// error means the file was malformed in a recoverable way (warnings
// describe why) and should be skipped. A non-nil error indicates an
// I/O-level failure the caller should also treat as a skip but log
// distinctly.
func parseMarkdownFile(path string) (*mdRecord, []string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	// Strip UTF-8 BOM so YAML doesn't choke on the leading bytes.
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})

	fm, body, ok := splitFrontMatter(raw)
	if !ok {
		return nil, []string{
			fmt.Sprintf("%s: no YAML front-matter found (expected `---` block at start)", filepath.Base(path)),
		}, nil
	}

	var meta mdFrontMatter
	if err := yaml.Unmarshal(fm, &meta); err != nil {
		return nil, []string{
			fmt.Sprintf("%s: front-matter YAML parse: %v", filepath.Base(path), err),
		}, nil
	}

	var warns []string

	if strings.TrimSpace(meta.Title) == "" {
		warns = append(warns, fmt.Sprintf("%s: front-matter `title` missing or empty; skipping", filepath.Base(path)))
		return nil, warns, nil
	}

	slug, source, slugOK := resolveMarkdownSlug(meta.Slug, path, &warns)
	if !slugOK {
		return nil, warns, nil
	}

	posted, ok := parseMarkdownPostedAt(meta.PostedAt, path, &warns)
	if !ok {
		// parse failure already warned (when applicable); fall back
		// to mtime so the record can still be imported.
		if info, err := os.Stat(path); err == nil {
			posted = info.ModTime().Unix()
		} else {
			posted = time.Now().Unix()
		}
	}

	rec := &mdRecord{
		path:       path,
		slug:       slug,
		slugSource: source,
		title:      strings.TrimSpace(meta.Title),
		body:       string(body),
		more:       meta.More,
		keywords:   meta.Keywords,
		pinned:     meta.Pinned,
		status:     markdownStatusValue(meta.Status, path, &warns),
		postedAt:   posted,
		category:   strings.TrimSpace(meta.Category),
	}
	return rec, warns, nil
}

// splitFrontMatter returns the YAML bytes (without the surrounding
// `---` markers), the body bytes (markdown), and ok=false when the
// document has no front-matter envelope at all. Both leading and
// closing fences must sit on their own lines, matching the convention
// every popular SSG (Hugo / Jekyll / Astro) enforces.
func splitFrontMatter(raw []byte) (frontMatter, body []byte, ok bool) {
	// Normalise CRLF early so the line-anchored split below works
	// on Windows-authored files.
	raw = bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))
	if !bytes.HasPrefix(raw, []byte("---\n")) {
		return nil, nil, false
	}
	rest := raw[len("---\n"):]
	end := bytes.Index(rest, []byte("\n---\n"))
	if end < 0 {
		// Allow the file to end exactly at `---` with no trailing
		// newline (uncommon but harmless to support).
		if bytes.HasSuffix(rest, []byte("\n---")) {
			end = len(rest) - len("\n---")
			return rest[:end], nil, true
		}
		return nil, nil, false
	}
	fm := rest[:end]
	bodyStart := end + len("\n---\n")
	return fm, rest[bodyStart:], true
}

// resolveMarkdownSlug applies the spec's slug resolution order:
//  1. front-matter `slug` if present + IsValidSlug
//  2. filename basename if IsValidSlug
//  3. skip with a helpful warning when both fail
//
// Returns (slug, source, ok). source identifies which path produced
// the slug; collision warnings use this so the operator knows whether
// to fix a filename or a YAML field.
func resolveMarkdownSlug(fmSlug, path string, warns *[]string) (string, string, bool) {
	if s := strings.TrimSpace(fmSlug); s != "" {
		if domain.IsValidSlug(s) {
			return s, "frontmatter", true
		}
		*warns = append(*warns, fmt.Sprintf(
			"%s: front-matter `slug` %q is not a valid slug (expected [a-z0-9-], 1-100 chars); skipping",
			filepath.Base(path), s))
		return "", "", false
	}
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if domain.IsValidSlug(base) {
		return base, "filename", true
	}
	*warns = append(*warns, fmt.Sprintf(
		"%s: filename is not a valid slug and front-matter `slug` is not set; specify `slug:` in front-matter or rename the file to match [a-z0-9-]",
		filepath.Base(path)))
	return "", "", false
}

// parseMarkdownPostedAt accepts RFC3339. An empty string returns
// (0, false) so the caller falls through to the file mtime cleanly.
// A malformed non-empty value adds a warning and also returns ok=false
// so the same mtime fallback kicks in (importing-with-warning beats
// silently dropping the entry).
func parseMarkdownPostedAt(raw, path string, warns *[]string) (int64, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		*warns = append(*warns,
			fmt.Sprintf("%s: front-matter `posted_at` %q is not RFC3339: %v", filepath.Base(path), raw, err))
		return 0, false
	}
	return t.Unix(), true
}

// markdownStatusValue maps the textual status to the integer the
// entries.status column uses (0=draft, 1=published, -1=closed).
// An unrecognised value defaults to published with a warning so the
// import doesn't silently mark something draft.
func markdownStatusValue(raw, path string, warns *[]string) int {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "published":
		return 1
	case "draft":
		return 0
	case "closed":
		return -1
	default:
		*warns = append(*warns,
			fmt.Sprintf("%s: unknown status %q; treating as published", filepath.Base(path), raw))
		return 1
	}
}

// loadCategorySlugMap reads the destination categories table and
// returns slug → id for the requested weblog. Empty slugs are skipped
// (they're not addressable from front-matter anyway).
func loadCategorySlugMap(ctx context.Context, tx *sql.Tx, wid int64) (map[string]int64, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT id, slug FROM categories WHERE wid = ? AND slug != ''`, wid)
	if err != nil {
		return nil, fmt.Errorf("importer: load categories: %w", err)
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var id int64
		var slug string
		if err := rows.Scan(&id, &slug); err != nil {
			return nil, fmt.Errorf("importer: scan category: %w", err)
		}
		out[slug] = id
	}
	return out, rows.Err()
}

// resolveMDCategory looks the front-matter slug up in catMap; on miss
// it returns -1 (the SB convention for "uncategorised") and records
// a warning. Empty inputs collapse to -1 silently.
func resolveMDCategory(rec *mdRecord, catMap map[string]int64, report *Report) int64 {
	if rec.category == "" {
		return -1
	}
	if id, ok := catMap[rec.category]; ok {
		return id
	}
	report.Warnings = append(report.Warnings,
		fmt.Sprintf("%s: category %q not found in destination; importing as uncategorised",
			filepath.Base(rec.path), rec.category))
	return -1
}

// upsertMarkdownRecords runs INSERT-or-UPDATE for each record. Uses
// the (wid, slug) partial unique index added in 0013 as the key.
// posted_at and created_at are preserved on UPDATE so re-importing a
// file doesn't shuffle archive boundaries.
func upsertMarkdownRecords(ctx context.Context, tx *sql.Tx, opts Options, records []mdRecord, report *Report) error {
	if len(records) == 0 {
		return nil
	}
	selStmt, err := tx.PrepareContext(ctx,
		`SELECT id FROM entries WHERE wid = ? AND slug = ?`)
	if err != nil {
		return fmt.Errorf("importer: prepare select: %w", err)
	}
	defer selStmt.Close()

	insStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO entries
		  (wid, author_id, category_id, title, slug, body, more, format, status,
		   keywords, pinned, posted_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'markdown', ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("importer: prepare insert: %w", err)
	}
	defer insStmt.Close()

	updStmt, err := tx.PrepareContext(ctx, `
		UPDATE entries
		   SET title       = ?,
		       body        = ?,
		       more        = ?,
		       format      = 'markdown',
		       status      = ?,
		       category_id = ?,
		       keywords    = ?,
		       pinned      = ?,
		       updated_at  = ?
		 WHERE id = ?`)
	if err != nil {
		return fmt.Errorf("importer: prepare update: %w", err)
	}
	defer updStmt.Close()

	now := time.Now().Unix()
	for i := range records {
		rec := &records[i]
		var existingID int64
		err := selStmt.QueryRowContext(ctx, opts.TargetWID, rec.slug).Scan(&existingID)
		switch {
		case err == nil:
			rec.entryID = existingID
			if _, err := updStmt.ExecContext(ctx,
				rec.title, rec.body, rec.more, rec.status, rec.categoryID,
				rec.keywords, boolToInt(rec.pinned), now, existingID); err != nil {
				return fmt.Errorf("importer: update entry slug=%s: %w", rec.slug, err)
			}
			report.EntriesUpdated++
		case errors.Is(err, sql.ErrNoRows):
			res, err := insStmt.ExecContext(ctx,
				opts.TargetWID, opts.AuthorID, rec.categoryID,
				rec.title, rec.slug, rec.body, rec.more, rec.status,
				rec.keywords, boolToInt(rec.pinned),
				rec.postedAt, rec.postedAt, now)
			if err != nil {
				return fmt.Errorf("importer: insert entry slug=%s: %w", rec.slug, err)
			}
			newID, err := res.LastInsertId()
			if err != nil {
				return fmt.Errorf("importer: last insert id: %w", err)
			}
			rec.entryID = newID
			report.EntriesInserted++
		default:
			return fmt.Errorf("importer: select entry slug=%s: %w", rec.slug, err)
		}
	}
	return nil
}

// generateMarkdownOGCards writes one PNG per imported record into
// <ImageDir>/og/. Mirrors the admin behaviour: log + warn on failure,
// never block the rest of the run. The renderer is constructed once
// since opentype font parsing is non-trivial — sharing the instance
// across all records makes a 50-entry import noticeably faster.
func generateMarkdownOGCards(ctx context.Context, dest *sql.DB, opts Options, records []mdRecord, report *Report) {
	if opts.ImageDir == "" || len(records) == 0 {
		return
	}
	renderer, err := og.New()
	if err != nil {
		report.Warnings = append(report.Warnings, fmt.Sprintf("OG renderer init failed: %v", err))
		return
	}
	weblog, err := loadWeblogForOG(ctx, dest, opts.TargetWID)
	if err != nil {
		report.Warnings = append(report.Warnings, fmt.Sprintf("OG: load weblog: %v", err))
		return
	}
	ogDir := filepath.Join(opts.ImageDir, "og")
	if err := os.MkdirAll(ogDir, 0o755); err != nil {
		report.Warnings = append(report.Warnings, fmt.Sprintf("OG: mkdir %s: %v", ogDir, err))
		return
	}
	bgPath := resolveOGBGPath(opts.ImageDir, "", weblog.OGBGImagePath)
	for _, rec := range records {
		if rec.entryID == 0 {
			continue
		}
		path := filepath.Join(ogDir, fmt.Sprintf("%d.png", rec.entryID))
		f, err := os.Create(path)
		if err != nil {
			report.Warnings = append(report.Warnings, fmt.Sprintf("OG: create %s: %v", path, err))
			continue
		}
		if err := renderer.RenderCard(f, rec.title, weblog.Title, og.Options{
			BGPath:    bgPath,
			TextColor: weblog.OGTextColor,
		}); err != nil {
			report.Warnings = append(report.Warnings,
				fmt.Sprintf("OG: render entry %d: %v", rec.entryID, err))
			log.Printf("importer: og render entry %d: %v", rec.entryID, err)
		}
		_ = f.Close()
	}
}

// ogWeblog is the small slice of weblogs columns OG rendering needs.
// Defined locally so the importer doesn't have to import the repo
// package (which would create a circular dep through internal/app).
type ogWeblog struct {
	Title         string
	OGBGImagePath string
	OGTextColor   string
}

func loadWeblogForOG(ctx context.Context, dest *sql.DB, wid int64) (*ogWeblog, error) {
	var w ogWeblog
	err := dest.QueryRowContext(ctx,
		`SELECT COALESCE(title, ''), COALESCE(og_bg_image_path, ''), COALESCE(og_text_color, '')
		   FROM weblogs WHERE id = ?`, wid).Scan(&w.Title, &w.OGBGImagePath, &w.OGTextColor)
	if err != nil {
		return nil, err
	}
	return &w, nil
}

// resolveOGBGPath mirrors admin.Handler.resolveOGBG: pick the first
// non-empty stored_path, then join with ImageDir. Empty inputs leave
// the renderer to use its embedded default.
func resolveOGBGPath(imageDir, entryPath, weblogPath string) string {
	chosen := entryPath
	if chosen == "" {
		chosen = weblogPath
	}
	if chosen == "" || imageDir == "" {
		return ""
	}
	return filepath.Join(imageDir, filepath.FromSlash(chosen))
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
