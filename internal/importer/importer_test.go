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
		`CREATE TABLE sb_entry (entry_id INTEGER PRIMARY KEY, entry_wid INTEGER, entry_subj TEXT, entry_cat INTEGER, entry_date INTEGER, entry_auth INTEGER, entry_stat INTEGER, entry_body TEXT, entry_more TEXT, entry_form TEXT, entry_mod INTEGER, entry_file TEXT, entry_key TEXT, entry_sum TEXT)`,
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
		// entry 100's summary is [entitized] (SB stores sum that way) so
		// the import exercises the html.UnescapeString round-trip.
		`INSERT INTO sb_entry VALUES (100, 0, 'Published One', 1, 1700000000, 1, 1, '<p>one</p>', '', '', 1700000100, 'pub-one', 'go,sb', 'Tom &amp; Jerry')`,
		`INSERT INTO sb_entry VALUES (101, 0, 'Draft Skipped', 2, 1700001000, 1, 0, '<p>draft</p>', '', '', 0, '', '', '')`,
		`INSERT INTO sb_entry VALUES (102, 0, 'Published Two', 2, 1700002000, 1, 1, '<p>two</p>', '<p>more</p>', 'html', 1700002100, '', '', '')`,
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

	t.Run("report counts", func(t *testing.T) { assertReportCounts(t, report) })
	t.Run("lint warnings include trackback and amazon", func(t *testing.T) { assertLintWarnings(t, report) })
	t.Run("weblog title overwritten", func(t *testing.T) { assertWeblogTitleOverwritten(t, a.DB) })
	t.Run("active template not stolen by import", func(t *testing.T) { assertActiveTemplateUnchanged(t, a.DB) })
	t.Run("entries owned by author and category remapped", func(t *testing.T) { assertEntryOwnershipAndCategoryRemap(t, a.DB) })
	t.Run("child category parent remap", func(t *testing.T) { assertChildCategoryParentRemap(t, a.DB) })
	t.Run("entries carry legacy_* fields", func(t *testing.T) { assertEntriesLegacyFields(t, a.DB) })
	t.Run("categories carry legacy_* fields", func(t *testing.T) { assertCategoriesLegacyFields(t, a.DB) })
	t.Run("weblog legacy config defaults", func(t *testing.T) { assertWeblogLegacyDefaults(t, a.DB) })
	t.Run("entry summary restored and unescaped", func(t *testing.T) { assertEntrySummaryImported(t, a.DB) })
}

// assertEntrySummaryImported checks that SB3 entry_sum is restored into
// the summary column with its [entitized] HTML entities decoded back to
// raw text (so the render layer's c.Tag escape runs exactly once), and
// that an empty source summary stays empty.
func assertEntrySummaryImported(t *testing.T, db *sql.DB) {
	t.Helper()
	cases := []struct {
		title string
		want  string
	}{
		{"Published One", "Tom & Jerry"}, // 'Tom &amp; Jerry' → unescaped
		{"Published Two", ""},            // no source summary
	}
	for _, c := range cases {
		var got string
		if err := db.QueryRow(`SELECT summary FROM entries WHERE title = ?`, c.title).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != c.want {
			t.Errorf("%s summary = %q, want %q", c.title, got, c.want)
		}
	}
}

func assertReportCounts(t *testing.T, report *importer.Report) {
	t.Helper()
	if !report.WeblogUpdated {
		t.Errorf("expected weblog to be updated")
	}
	if report.Templates != 2 {
		t.Errorf("templates = %d, want 2", report.Templates)
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
}

// assertLintWarnings checks that the legacy fixture template surfaces lint
// warnings for both trackback and amazon — they are flagged as unsupported
// by the lint pass but must survive import as advisory.
func assertLintWarnings(t *testing.T, report *importer.Report) {
	t.Helper()
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
		t.Errorf("missing lint warnings: trackback=%v amazon=%v in %v",
			sawTrackback, sawAmazon, report.Warnings)
	}
}

func assertWeblogTitleOverwritten(t *testing.T, db *sql.DB) {
	t.Helper()
	var title string
	if err := db.QueryRow(`SELECT title FROM weblogs WHERE id = 1`).Scan(&title); err != nil {
		t.Fatal(err)
	}
	if title != "Imported Title" {
		t.Errorf("title = %q, want Imported Title", title)
	}
}

// assertActiveTemplateUnchanged confirms that imported templates do NOT
// steal the active slot — the seed default must still be active.
func assertActiveTemplateUnchanged(t *testing.T, db *sql.DB) {
	t.Helper()
	var activeName string
	if err := db.QueryRow(`SELECT name FROM templates WHERE wid = 1 AND is_active = 1`).Scan(&activeName); err != nil {
		t.Fatal(err)
	}
	if activeName != "default" {
		t.Errorf("active template after import = %q, want default", activeName)
	}
}

// assertEntryOwnershipAndCategoryRemap verifies entries are owned by the
// target author and that their category ids point at the remapped rows.
// Also asserts the draft was skipped and bodies survived round-trip.
func assertEntryOwnershipAndCategoryRemap(t *testing.T, db *sql.DB) {
	t.Helper()
	titles := loadImportedEntries(t, db)
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
}

// assertChildCategoryParentRemap checks the child category's parent_id
// points at the new id of the parent (i.e. the remap layer fired).
func assertChildCategoryParentRemap(t *testing.T, db *sql.DB) {
	t.Helper()
	var parentRemapped, childRemapped int64
	if err := db.QueryRow(`SELECT id FROM categories WHERE name = 'parent'`).Scan(&parentRemapped); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT parent_id FROM categories WHERE name = 'child'`).Scan(&childRemapped); err != nil {
		t.Fatal(err)
	}
	if childRemapped != parentRemapped {
		t.Errorf("child.parent_id = %d, want %d (new id of 'parent')", childRemapped, parentRemapped)
	}
}

// assertEntriesLegacyFields verifies the legacy_* round-trip on imported
// entries; the redirect layer relies on this to resolve Perl URLs by SB3
// identity.
func assertEntriesLegacyFields(t *testing.T, db *sql.DB) {
	t.Helper()
	assertEntryLegacy(t, db, "Published One", 100, "pub-one", "go,sb")
	assertEntryLegacyIDFile(t, db, "Published Two", 102, "")
}

func assertCategoriesLegacyFields(t *testing.T, db *sql.DB) {
	t.Helper()
	assertCategoryLegacy(t, db, "parent", 1, "log/")
	assertCategoryLegacyDir(t, db, "child", "log/sub/")
}

// assertWeblogLegacyDefaults confirms that without sb_config rows the
// weblog inherits config.pl defaults.
func assertWeblogLegacyDefaults(t *testing.T, db *sql.DB) {
	t.Helper()
	var arch, logPath, basePath, cgi, prefix, suffix string
	if err := db.QueryRow(
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

// importedEntry captures the four columns each sub-assertion in
// TestImportCopiesFilteredContent needs to reason about an imported entry.
type importedEntry struct {
	body     string
	author   int64
	category int64
}

// loadImportedEntries reads every imported entry into a title-keyed map so
// individual sub-tests can assert against specific titles without re-running
// the query.
func loadImportedEntries(t *testing.T, db *sql.DB) map[string]importedEntry {
	t.Helper()
	rows, err := db.Query(`SELECT title, body, author_id, category_id FROM entries ORDER BY posted_at`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]importedEntry{}
	for rows.Next() {
		var title, body string
		var author, cat int64
		if err := rows.Scan(&title, &body, &author, &cat); err != nil {
			t.Fatal(err)
		}
		out[title] = importedEntry{body: body, author: author, category: cat}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// assertEntryLegacy verifies an entry's full legacy round-trip (id, file,
// keywords) in one call.
func assertEntryLegacy(t *testing.T, db *sql.DB, title string, wantID int64, wantFile, wantKeywords string) {
	t.Helper()
	var legacyID sql.NullInt64
	var legacyFile, keywords string
	if err := db.QueryRow(
		`SELECT legacy_id, legacy_file, keywords FROM entries WHERE title = ?`,
		title,
	).Scan(&legacyID, &legacyFile, &keywords); err != nil {
		t.Fatal(err)
	}
	if !legacyID.Valid || legacyID.Int64 != wantID {
		t.Errorf("%s legacy_id = %v, want %d", title, legacyID, wantID)
	}
	if legacyFile != wantFile {
		t.Errorf("%s legacy_file = %q, want %q", title, legacyFile, wantFile)
	}
	if keywords != wantKeywords {
		t.Errorf("%s keywords = %q, want %q", title, keywords, wantKeywords)
	}
}

// assertEntryLegacyIDFile checks only legacy_id and legacy_file — used for
// fixtures whose keywords column is irrelevant to the assertion.
func assertEntryLegacyIDFile(t *testing.T, db *sql.DB, title string, wantID int64, wantFile string) {
	t.Helper()
	var legacyID sql.NullInt64
	var legacyFile string
	if err := db.QueryRow(
		`SELECT legacy_id, legacy_file FROM entries WHERE title = ?`,
		title,
	).Scan(&legacyID, &legacyFile); err != nil {
		t.Fatal(err)
	}
	if !legacyID.Valid || legacyID.Int64 != wantID {
		t.Errorf("%s legacy_id = %v, want %d", title, legacyID, wantID)
	}
	if legacyFile != wantFile {
		t.Errorf("%s legacy_file = %q, want %q", title, legacyFile, wantFile)
	}
}

// assertCategoryLegacy verifies legacy_id and legacy_dir for a category.
func assertCategoryLegacy(t *testing.T, db *sql.DB, name string, wantID int64, wantDir string) {
	t.Helper()
	var legacyID sql.NullInt64
	var legacyDir string
	if err := db.QueryRow(
		`SELECT legacy_id, legacy_dir FROM categories WHERE name = ?`,
		name,
	).Scan(&legacyID, &legacyDir); err != nil {
		t.Fatal(err)
	}
	if !legacyID.Valid || legacyID.Int64 != wantID {
		t.Errorf("%s legacy_id = %v, want %d", name, legacyID, wantID)
	}
	if legacyDir != wantDir {
		t.Errorf("%s legacy_dir = %q, want %q", name, legacyDir, wantDir)
	}
}

// assertCategoryLegacyDir checks only legacy_dir, for categories whose
// legacy_id is asserted elsewhere or doesn't matter.
func assertCategoryLegacyDir(t *testing.T, db *sql.DB, name, wantDir string) {
	t.Helper()
	var legacyDir string
	if err := db.QueryRow(
		`SELECT legacy_dir FROM categories WHERE name = ?`,
		name,
	).Scan(&legacyDir); err != nil {
		t.Fatal(err)
	}
	if legacyDir != wantDir {
		t.Errorf("%s legacy_dir = %q, want %q", name, legacyDir, wantDir)
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

// writeFile writes content to t.TempDir-relative path. Used by the
// configure.cgi / init.cgi flat-file tests.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writeFile %s: %v", path, err)
	}
}

func TestImportReadsConfigureCgi(t *testing.T) {
	// Most live SB3 instances keep all URL-shaping config in
	// data/configure.cgi, not in the sb_config table. The importer must
	// pick those values up automatically when configure.cgi sits next to
	// the SQLite source.
	src := buildSB3Fixture(t)
	dir := filepath.Dir(src)
	writeFile(t, filepath.Join(dir, "configure.cgi"),
		"conf_srv_base\thttp://example.com/sb/\n"+
			"conf_dir_log\tarticles/\n"+
			"conf_entry_archive\tIndividual\n"+
			"basic_preid\tarticle\n"+
			"basic_suffix\thtm\n",
	)

	a := destApp(t)
	if _, err := importer.Import(context.Background(), a.DB, src, importer.Options{
		TargetWID: 1, AuthorID: 1, OnlyPublished: true,
	}); err != nil {
		t.Fatalf("Import: %v", err)
	}

	var arch, logPath, basePath, prefix, suffix string
	if err := a.DB.QueryRow(
		`SELECT legacy_archive_type, legacy_log_path, legacy_base_path,
		        legacy_id_prefix, legacy_suffix
		 FROM weblogs WHERE id = 1`,
	).Scan(&arch, &logPath, &basePath, &prefix, &suffix); err != nil {
		t.Fatal(err)
	}
	if arch != "Individual" {
		t.Errorf("archive_type = %q, want Individual", arch)
	}
	if basePath != "/sb/" {
		t.Errorf("base_path = %q, want /sb/ (host stripped)", basePath)
	}
	if logPath != "articles/" {
		t.Errorf("log_path = %q, want articles/", logPath)
	}
	if prefix != "article" {
		t.Errorf("id_prefix = %q, want article", prefix)
	}
	if suffix != "htm" {
		t.Errorf("suffix = %q, want htm", suffix)
	}
}

func TestImportConfigureCgiOverridesSBConfig(t *testing.T) {
	// Layer priority: configure.cgi (highest) > init.cgi > sb_config >
	// config.pl defaults. Seed both an sb_config row and a configure.cgi
	// entry for the same key — configure.cgi must win.
	src := buildSB3Fixture(t)
	db, err := sql.Open("sqlite", "file:"+src)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE sb_config (config_id INTEGER PRIMARY KEY, config_wid INTEGER, config_name TEXT, config_type TEXT, config_data TEXT, config_text TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO sb_config VALUES (1, 0, 'conf_srv_base', 'str', 'http://from-table.example/', '')`); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Dir(src)
	writeFile(t, filepath.Join(dir, "configure.cgi"),
		"conf_srv_base\thttp://from-configure.example/blog/\n",
	)

	a := destApp(t)
	if _, err := importer.Import(context.Background(), a.DB, src, importer.Options{
		TargetWID: 1, AuthorID: 1, OnlyPublished: true,
	}); err != nil {
		t.Fatalf("Import: %v", err)
	}

	var basePath string
	if err := a.DB.QueryRow(
		`SELECT legacy_base_path FROM weblogs WHERE id = 1`,
	).Scan(&basePath); err != nil {
		t.Fatal(err)
	}
	if basePath != "/blog/" {
		t.Errorf("base_path = %q, want /blog/ (configure.cgi must override sb_config)", basePath)
	}
}

func TestImportConfigureCgiOverridesInitCgi(t *testing.T) {
	// configure.cgi wins over init.cgi within the flat-file pair.
	src := buildSB3Fixture(t)
	dir := filepath.Dir(src)
	writeFile(t, filepath.Join(dir, "init.cgi"),
		"conf_srv_base\thttp://from-init.example/init/\n"+
			"basic_preid\tinit-prefix\n",
	)
	writeFile(t, filepath.Join(dir, "configure.cgi"),
		"conf_srv_base\thttp://from-configure.example/conf/\n",
	)

	a := destApp(t)
	if _, err := importer.Import(context.Background(), a.DB, src, importer.Options{
		TargetWID: 1, AuthorID: 1, OnlyPublished: true,
	}); err != nil {
		t.Fatalf("Import: %v", err)
	}

	var basePath, prefix string
	if err := a.DB.QueryRow(
		`SELECT legacy_base_path, legacy_id_prefix FROM weblogs WHERE id = 1`,
	).Scan(&basePath, &prefix); err != nil {
		t.Fatal(err)
	}
	if basePath != "/conf/" {
		t.Errorf("base_path = %q, want /conf/ (configure.cgi wins)", basePath)
	}
	// init.cgi-only key still applies — only conflicts get overwritten.
	if prefix != "init-prefix" {
		t.Errorf("id_prefix = %q, want init-prefix (init.cgi-only key preserved)", prefix)
	}
}

func TestImportDataDirHonouredWithZeroValueOptions(t *testing.T) {
	// Regression: when the caller passes Options{DataDir: dir} alone (no
	// explicit TargetWID / AuthorID), the importer used to wholesale-
	// replace the struct with defaults and lose the explicit DataDir.
	// Now field-level zero-value resolution preserves it.
	src := buildSB3Fixture(t)

	// Put configure.cgi in a *different* directory than the one
	// auto-detect would pick (the SQLite parent), so the only way these
	// values reach legacy_* is through the explicit DataDir option.
	explicit := t.TempDir()
	writeFile(t, filepath.Join(explicit, "configure.cgi"),
		"conf_srv_base\thttp://explicit.example/path/\n",
	)

	a := destApp(t)
	if _, err := importer.Import(context.Background(), a.DB, src, importer.Options{
		DataDir: explicit,
	}); err != nil {
		t.Fatalf("Import: %v", err)
	}

	var basePath string
	if err := a.DB.QueryRow(
		`SELECT legacy_base_path FROM weblogs WHERE id = 1`,
	).Scan(&basePath); err != nil {
		t.Fatal(err)
	}
	if basePath != "/path/" {
		t.Errorf("base_path = %q, want /path/ (explicit DataDir must reach loadLegacyConfig)", basePath)
	}
}

func TestImportConfigureCgiSrvCgiFallback(t *testing.T) {
	// SB2-style configs only set conf_srv_cgi (cgi script URL), not
	// conf_srv_base. sb::Config::verify_values copies the former into
	// the latter when conf_srv_base is empty; the importer mirrors that.
	src := buildSB3Fixture(t)
	dir := filepath.Dir(src)
	writeFile(t, filepath.Join(dir, "configure.cgi"),
		"conf_srv_cgi\thttp://example.com/cgi-blog/\n",
	)

	a := destApp(t)
	if _, err := importer.Import(context.Background(), a.DB, src, importer.Options{
		TargetWID: 1, AuthorID: 1, OnlyPublished: true,
	}); err != nil {
		t.Fatalf("Import: %v", err)
	}

	var basePath string
	if err := a.DB.QueryRow(
		`SELECT legacy_base_path FROM weblogs WHERE id = 1`,
	).Scan(&basePath); err != nil {
		t.Fatal(err)
	}
	if basePath != "/cgi-blog/" {
		t.Errorf("base_path = %q, want /cgi-blog/ (conf_srv_cgi fallback)", basePath)
	}
}

func TestImportSandboxSB3RoundTrip(t *testing.T) {
	// End-to-end against the real SB3 sandbox: data.db carries the
	// content rows, configure.cgi (a sibling file) carries the URL
	// settings. Asserts BasePath comes out as /sb/ — the bug that
	// motivated this layer of the importer.
	sandbox := sandboxSB3DataDB(t)
	a := destApp(t)
	if _, err := importer.Import(context.Background(), a.DB, sandbox, importer.Options{
		TargetWID: 1, AuthorID: 1, OnlyPublished: true,
	}); err != nil {
		t.Fatalf("Import: %v", err)
	}

	var arch, logPath, basePath, prefix, suffix string
	if err := a.DB.QueryRow(
		`SELECT legacy_archive_type, legacy_log_path, legacy_base_path,
		        legacy_id_prefix, legacy_suffix
		 FROM weblogs WHERE id = 1`,
	).Scan(&arch, &logPath, &basePath, &prefix, &suffix); err != nil {
		t.Fatal(err)
	}
	if basePath != "/sb/" {
		t.Errorf("base_path = %q, want /sb/", basePath)
	}
	if logPath != "log/" {
		t.Errorf("log_path = %q, want log/", logPath)
	}
	if arch != "Individual" {
		t.Errorf("archive_type = %q, want Individual", arch)
	}
	// configure.cgi for this sandbox doesn't set basic_preid/basic_suffix,
	// so we should be on config.pl defaults.
	if prefix != "eid" {
		t.Errorf("id_prefix = %q, want eid (default)", prefix)
	}
	if suffix != ".html" {
		t.Errorf("suffix = %q, want .html (default)", suffix)
	}
}

// sandboxSB3DataDB locates _sandbox/data-sb3/data.db relative to the
// repo. Skips the test if the sandbox isn't present (e.g. lean clones
// that don't ship the fixture).
func sandboxSB3DataDB(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// importer_test runs from internal/importer; walk up to repo root.
	root := filepath.Join(wd, "..", "..")
	path := filepath.Join(root, "_sandbox", "data-sb3", "data.db")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("sandbox not present: %v", err)
	}
	return path
}
