package importer_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/serendipitynz/serenebach/internal/app"
	"github.com/serendipitynz/serenebach/internal/config"
	"github.com/serendipitynz/serenebach/internal/importer"
)

// buildSB3Fixture creates a minimal SB3-shaped SQLite file on disk and
// populates it with a handful of categories, templates, and entries. The
// returned path points at the file; the caller doesn't need to clean it up
// (it lives inside t.TempDir()).
func buildSB3Fixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sb3.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ddl := []string{
		`CREATE TABLE sb_weblog (weblog_id INTEGER PRIMARY KEY, weblog_title TEXT, weblog_text TEXT)`,
		`CREATE TABLE sb_category (category_id INTEGER PRIMARY KEY, category_wid INTEGER, category_name TEXT, category_main INTEGER, category_order INTEGER, category_dir TEXT)`,
		`CREATE TABLE sb_template (template_id INTEGER PRIMARY KEY, template_wid INTEGER, template_name TEXT, template_main TEXT, template_entry TEXT, template_css TEXT, template_info TEXT)`,
		`CREATE TABLE sb_entry (entry_id INTEGER PRIMARY KEY, entry_wid INTEGER, entry_subj TEXT, entry_cat INTEGER, entry_date INTEGER, entry_auth INTEGER, entry_stat INTEGER, entry_body TEXT, entry_more TEXT, entry_form TEXT, entry_mod INTEGER, entry_file TEXT, entry_key TEXT)`,
	}
	for _, s := range ddl {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("ddl: %v", err)
		}
	}

	seed := []string{
		`INSERT INTO sb_weblog VALUES (0, 'Imported Title', 'imported description')`,
		`INSERT INTO sb_category VALUES (1, 0, 'parent', 0, 0, 'log/')`,
		`INSERT INTO sb_category VALUES (2, 0, 'child', 1, 1, 'log/sub/')`,
		`INSERT INTO sb_template VALUES (10, 0, 'summer', '<html>{site_title}</html>', '', 'body{}', 'info')`,
		// "legacy" carries a trackback block + amazon tag so the lint
		// path has something to warn about. Must survive import
		// untouched — the warnings are advisory.
		`INSERT INTO sb_template VALUES (11, 0, 'legacy', '<html>' || X'0a' || '<!-- BEGIN trackback_area -->' || X'0a' || 'x' || X'0a' || '<!-- END trackback_area -->' || X'0a' || '{amazon_link}' || X'0a' || '</html>', '', '', 'info')`,
		// entry 100 has both a custom save name and keywords; entry 102
		// has neither so we cover the empty-string path through legacy_*.
		`INSERT INTO sb_entry VALUES (100, 0, 'Published One', 1, 1700000000, 1, 1, '<p>one</p>', '', '', 1700000100, 'pub-one', 'go,sb')`,
		`INSERT INTO sb_entry VALUES (101, 0, 'Draft Skipped', 2, 1700001000, 1, 0, '<p>draft</p>', '', '', 0, '', '')`,
		`INSERT INTO sb_entry VALUES (102, 0, 'Published Two', 2, 1700002000, 1, 1, '<p>two</p>', '<p>more</p>', 'html', 1700002100, '', '')`,
	}
	for _, s := range seed {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	return path
}

// destApp builds a fresh destination app (empty DB → migrate → seed).
func destApp(t *testing.T) *app.App {
	t.Helper()
	cfg := &config.Config{
		Mode:   config.ModeServer,
		Addr:   ":0",
		DBPath: filepath.Join(t.TempDir(), "dest.db"),
	}
	a, err := app.New(cfg)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	if err := a.Seed(context.Background(), app.DefaultSeed()); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	return a
}

func TestImportCopiesFilteredContent(t *testing.T) {
	src := buildSB3Fixture(t)
	a := destApp(t)

	report, err := importer.Import(context.Background(), a.DB, src, importer.Options{
		TargetWID:     1,
		AuthorID:      1,
		OnlyPublished: true,
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if !report.WeblogUpdated {
		t.Errorf("expected weblog to be updated")
	}
	if report.Templates != 2 {
		t.Errorf("templates = %d, want 2", report.Templates)
	}
	// The legacy template uses trackback + amazon — both flagged as
	// unsupported by the lint pass. At least one warning for each
	// must land in the report.
	var sawTrackback, sawAmazon bool
	for _, w := range report.Warnings {
		if strings.Contains(w, "trackback_area") {
			sawTrackback = true
		}
		if strings.Contains(w, "amazon_link") {
			sawAmazon = true
		}
	}
	if !sawTrackback || !sawAmazon {
		t.Errorf("missing lint warnings: trackback=%v amazon=%v in %v", sawTrackback, sawAmazon, report.Warnings)
	}
	if report.Categories != 2 {
		t.Errorf("categories = %d, want 2", report.Categories)
	}
	if report.Entries != 2 {
		t.Errorf("entries = %d, want 2 (1 draft skipped)", report.Entries)
	}
	if report.SkippedEntries != 1 {
		t.Errorf("skipped = %d, want 1", report.SkippedEntries)
	}

	// Weblog title/description overwritten
	var title string
	if err := a.DB.QueryRow(`SELECT title FROM weblogs WHERE id = 1`).Scan(&title); err != nil {
		t.Fatal(err)
	}
	if title != "Imported Title" {
		t.Errorf("title = %q, want Imported Title", title)
	}

	// Imported templates must NOT steal the active slot (seed default remains active).
	var activeName string
	if err := a.DB.QueryRow(`SELECT name FROM templates WHERE wid = 1 AND is_active = 1`).Scan(&activeName); err != nil {
		t.Fatal(err)
	}
	if activeName != "default" {
		t.Errorf("active template after import = %q, want default", activeName)
	}

	// Entries must all be owned by author 1 and mapped to remapped category ids.
	rows, err := a.DB.Query(`SELECT title, body, author_id, category_id FROM entries ORDER BY posted_at`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	titles := map[string]struct {
		body     string
		author   int64
		category int64
	}{}
	for rows.Next() {
		var title, body string
		var author, cat int64
		if err := rows.Scan(&title, &body, &author, &cat); err != nil {
			t.Fatal(err)
		}
		titles[title] = struct {
			body     string
			author   int64
			category int64
		}{body: body, author: author, category: cat}
	}
	pub1, ok := titles["Published One"]
	if !ok {
		t.Fatalf("Published One not imported; got %v", titles)
	}
	if pub1.author != 1 {
		t.Errorf("Published One author = %d, want 1", pub1.author)
	}
	if pub1.category == -1 {
		t.Errorf("Published One category not remapped; got -1")
	}
	if _, skipped := titles["Draft Skipped"]; skipped {
		t.Errorf("draft entry should not have been imported")
	}
	if pub1.body != "<p>one</p>" || titles["Published Two"].body != "<p>two</p>" {
		t.Errorf("entry bodies wrong: %v", titles)
	}

	// Child category's parent_id should point at the new id of the parent.
	var parentRemapped, childRemapped int64
	if err := a.DB.QueryRow(`SELECT id FROM categories WHERE name = 'parent'`).Scan(&parentRemapped); err != nil {
		t.Fatal(err)
	}
	if err := a.DB.QueryRow(`SELECT parent_id FROM categories WHERE name = 'child'`).Scan(&childRemapped); err != nil {
		t.Fatal(err)
	}
	if childRemapped != parentRemapped {
		t.Errorf("child.parent_id = %d, want %d (new id of 'parent')", childRemapped, parentRemapped)
	}

	// Legacy URL inputs must round-trip into the destination so the
	// redirect layer can find Perl URLs by their SB3 identity.
	var pubOneLegacyID sql.NullInt64
	var pubOneLegacyFile, pubOneKeywords string
	if err := a.DB.QueryRow(
		`SELECT legacy_id, legacy_file, keywords FROM entries WHERE title = 'Published One'`,
	).Scan(&pubOneLegacyID, &pubOneLegacyFile, &pubOneKeywords); err != nil {
		t.Fatal(err)
	}
	if !pubOneLegacyID.Valid || pubOneLegacyID.Int64 != 100 {
		t.Errorf("Published One legacy_id = %v, want 100", pubOneLegacyID)
	}
	if pubOneLegacyFile != "pub-one" {
		t.Errorf("Published One legacy_file = %q, want pub-one", pubOneLegacyFile)
	}
	if pubOneKeywords != "go,sb" {
		t.Errorf("Published One keywords = %q, want go,sb", pubOneKeywords)
	}

	var pubTwoLegacyID sql.NullInt64
	var pubTwoLegacyFile string
	if err := a.DB.QueryRow(
		`SELECT legacy_id, legacy_file FROM entries WHERE title = 'Published Two'`,
	).Scan(&pubTwoLegacyID, &pubTwoLegacyFile); err != nil {
		t.Fatal(err)
	}
	if !pubTwoLegacyID.Valid || pubTwoLegacyID.Int64 != 102 {
		t.Errorf("Published Two legacy_id = %v, want 102", pubTwoLegacyID)
	}
	if pubTwoLegacyFile != "" {
		t.Errorf("Published Two legacy_file = %q, want empty", pubTwoLegacyFile)
	}

	// Categories carry their SB3 id and configured dir.
	var parentLegacyID sql.NullInt64
	var parentLegacyDir string
	if err := a.DB.QueryRow(
		`SELECT legacy_id, legacy_dir FROM categories WHERE name = 'parent'`,
	).Scan(&parentLegacyID, &parentLegacyDir); err != nil {
		t.Fatal(err)
	}
	if !parentLegacyID.Valid || parentLegacyID.Int64 != 1 {
		t.Errorf("parent legacy_id = %v, want 1", parentLegacyID)
	}
	if parentLegacyDir != "log/" {
		t.Errorf("parent legacy_dir = %q, want log/", parentLegacyDir)
	}
	var childLegacyDir string
	if err := a.DB.QueryRow(
		`SELECT legacy_dir FROM categories WHERE name = 'child'`,
	).Scan(&childLegacyDir); err != nil {
		t.Fatal(err)
	}
	if childLegacyDir != "log/sub/" {
		t.Errorf("child legacy_dir = %q, want log/sub/", childLegacyDir)
	}

	// Without sb_config rows the weblog inherits config.pl defaults.
	var arch, logPath, basePath, cgi, prefix, suffix string
	if err := a.DB.QueryRow(
		`SELECT legacy_archive_type, legacy_log_path, legacy_base_path,
		        legacy_cgi_name, legacy_id_prefix, legacy_suffix
		 FROM weblogs WHERE id = 1`,
	).Scan(&arch, &logPath, &basePath, &cgi, &prefix, &suffix); err != nil {
		t.Fatal(err)
	}
	if arch != "Individual" || logPath != "log/" || basePath != "/" ||
		cgi != "sb.cgi" || prefix != "eid" || suffix != ".html" {
		t.Errorf("legacy defaults wrong: arch=%q log=%q base=%q cgi=%q prefix=%q suffix=%q",
			arch, logPath, basePath, cgi, prefix, suffix)
	}
}

func TestImportReadsSBConfigOverrides(t *testing.T) {
	src := buildSB3Fixture(t)

	// Add an sb_config table on top of the standard fixture and seed a
	// mix of global (wid=0) and weblog-scoped rows. The weblog-scoped
	// row must win for archive type; suffix uses a leading-slash log
	// path and a fully-qualified base URL to exercise normalisation.
	db, err := sql.Open("sqlite", "file:"+src)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stmts := []string{
		`CREATE TABLE sb_config (config_id INTEGER PRIMARY KEY, config_wid INTEGER, config_name TEXT, config_type TEXT, config_data TEXT, config_text TEXT)`,
		`INSERT INTO sb_config VALUES (1, 0, 'conf_entry_archive', 'str', 'Monthly', '')`,
		`INSERT INTO sb_config VALUES (2, 0, 'conf_dir_log', 'str', '/archive', '')`,
		`INSERT INTO sb_config VALUES (3, 0, 'conf_srv_base', 'str', 'https://old.example.com/blog', '')`,
		`INSERT INTO sb_config VALUES (4, 0, 'basic_preid', 'str', 'p', '')`,
		// Weblog-scoped override: archive type swings back to Individual.
		`INSERT INTO sb_config VALUES (5, 0, 'conf_entry_archive', 'str', 'Individual', '')`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}

	a := destApp(t)
	if _, err := importer.Import(context.Background(), a.DB, src, importer.Options{
		TargetWID: 1, AuthorID: 1, OnlyPublished: true,
	}); err != nil {
		t.Fatalf("Import: %v", err)
	}

	var arch, logPath, basePath, cgi, prefix, suffix string
	if err := a.DB.QueryRow(
		`SELECT legacy_archive_type, legacy_log_path, legacy_base_path,
		        legacy_cgi_name, legacy_id_prefix, legacy_suffix
		 FROM weblogs WHERE id = 1`,
	).Scan(&arch, &logPath, &basePath, &cgi, &prefix, &suffix); err != nil {
		t.Fatal(err)
	}
	if arch != "Individual" {
		t.Errorf("archive_type = %q, want Individual (later override wins)", arch)
	}
	if logPath != "archive/" {
		t.Errorf("log_path = %q, want archive/ (leading slash stripped, trailing added)", logPath)
	}
	if basePath != "/blog/" {
		t.Errorf("base_path = %q, want /blog/ (host stripped, trailing slash added)", basePath)
	}
	if cgi != "sb.cgi" {
		t.Errorf("cgi_name = %q, want default sb.cgi", cgi)
	}
	if prefix != "p" {
		t.Errorf("id_prefix = %q, want p", prefix)
	}
	if suffix != ".html" {
		t.Errorf("suffix = %q, want default .html", suffix)
	}
}

func TestImportIncludesDraftsWhenAsked(t *testing.T) {
	src := buildSB3Fixture(t)
	a := destApp(t)

	report, err := importer.Import(context.Background(), a.DB, src, importer.Options{
		TargetWID:     1,
		AuthorID:      1,
		OnlyPublished: false,
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if report.Entries != 3 {
		t.Errorf("entries = %d, want 3 (all)", report.Entries)
	}
	if report.SkippedEntries != 0 {
		t.Errorf("skipped = %d, want 0", report.SkippedEntries)
	}
}

func TestImportFailsOnMissingAuthor(t *testing.T) {
	src := buildSB3Fixture(t)
	a := destApp(t)

	_, err := importer.Import(context.Background(), a.DB, src, importer.Options{
		TargetWID: 1,
		AuthorID:  99999,
	})
	if err == nil {
		t.Fatal("expected error for missing author")
	}
}

func TestImportAgainstRealSB3IfAvailable(t *testing.T) {
	// Opportunistic check: if SB_TEST_SB3_DB points at a real SB3
	// SQLite, run the import against it. Skips otherwise so a normal
	// CI run (without the fixture) stays green.
	path := os.Getenv("SB_TEST_SB3_DB")
	if path == "" {
		t.Skip("SB_TEST_SB3_DB not set; no real SB3 fixture available")
	}
	if _, err := os.Stat(path); err != nil {
		t.Skipf("SB_TEST_SB3_DB=%q: %v", path, err)
	}

	a := destApp(t)
	report, err := importer.Import(context.Background(), a.DB, path, importer.Options{
		TargetWID:     1,
		AuthorID:      1,
		OnlyPublished: true,
	})
	if err != nil {
		t.Fatalf("Import real SB3: %v", err)
	}
	t.Logf("real SB3 import: templates=%d categories=%d entries=%d skipped=%d",
		report.Templates, report.Categories, report.Entries, report.SkippedEntries)
	if report.Entries == 0 {
		t.Errorf("expected some entries from real SB3 db")
	}

	// Every imported entry should carry its SB3 id forward; without it the
	// redirect layer cannot resolve /sb.cgi?eid= or /log/eid{id}.html.
	var entriesWithLegacyID int
	if err := a.DB.QueryRow(
		`SELECT COUNT(*) FROM entries WHERE legacy_id IS NOT NULL`,
	).Scan(&entriesWithLegacyID); err != nil {
		t.Fatal(err)
	}
	if entriesWithLegacyID != report.Entries {
		t.Errorf("legacy_id missing: %d of %d entries have it", entriesWithLegacyID, report.Entries)
	}

	// Same invariant for categories; the SB3 sandbox uses 0-based ids
	// (category_id=0 is real data), so this also exercises the
	// NULL-allowed legacy_id design.
	var catsWithLegacyID int
	if err := a.DB.QueryRow(
		`SELECT COUNT(*) FROM categories WHERE legacy_id IS NOT NULL`,
	).Scan(&catsWithLegacyID); err != nil {
		t.Fatal(err)
	}
	if catsWithLegacyID != report.Categories {
		t.Errorf("category legacy_id missing: %d of %d", catsWithLegacyID, report.Categories)
	}
}
