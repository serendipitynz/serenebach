package importer_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"

	"github.com/serendipitynz/serenebach/internal/importer"
)

// buildSB2Fixture writes a synthetic SB2 data directory to t.TempDir
// and returns the path. The fixture is intentionally small but
// exercises every code path the importer has: published / draft /
// uncategorised entries, a comment whose entry will be filtered when
// OnlyPublished=true (so it must be silently dropped), a category
// hierarchy, a template, weblog metadata, and a configure.cgi for the
// legacy_* settings.
//
// The records use the SB2 driver's TAB-separated layout from
// _sandbox/sb2/lib/sb/Driver/Text.pm — one row per file (or one row
// per record for list-only tables), trailing tab before the newline,
// `\t` / `\n` / `\\` per-field escapes.
func buildSB2Fixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// configure.cgi — InitParser format. The SB2 sandbox demonstrates
	// that conf_srv_cgi alone (no conf_srv_base) must fall through to
	// legacy_base_path via the verify_values fallback.
	mustWrite(t, filepath.Join(dir, "configure.cgi"),
		"conf_dbtype\tText\n"+
			"conf_srv_cgi\thttp://example.com/myblog/\n"+
			"conf_dir_log\tlog/\n"+
			"conf_entry_archive\tIndividual\n",
	)

	// weblog.cgi — Data/Weblog.pm elements: id, title, text, ...
	// Only the first three matter for the import.
	mustWrite(t, filepath.Join(dir, "weblog.cgi"),
		sb2Row("0", "Synthetic Blog", "A synthetic SB2 fixture for tests"),
	)

	// category.cgi — Data/Category.pm elements: id, wid, name, text,
	// url, main, order, temp, dir, disp, sub, num, idx
	// Tech is top-level: SB2 marks that with an EMPTY main, not "0"
	// ("0" would mean "child of category 0"). Sub-tech's main="1" makes
	// it Tech's child.
	mustWrite(t, filepath.Join(dir, "category.cgi"),
		sb2Row("1", "0", "Tech", "tech posts", "", "", "0", "0", "tech/", "0", "", "0", ""),
		sb2Row("2", "0", "Sub-tech", "child of tech", "", "1", "0", "0", "tech/sub/", "0", "", "0", ""),
	)

	// entry/100.cgi — published, in category 1. Carries an [entitized]
	// summary so the import exercises the sum → summary unescape path.
	mustMkdir(t, filepath.Join(dir, "entry"))
	mustWrite(t, filepath.Join(dir, "entry", "100.cgi"),
		sb2EntryWithSum("100", "Published Post", "1", "1700000000", "1", "Hello world body.", "", "", "Tom &amp; Jerry"),
	)
	// entry/101.cgi — draft (stat=0), should be skipped under
	// OnlyPublished and counted in SkippedEntries.
	mustWrite(t, filepath.Join(dir, "entry", "101.cgi"),
		sb2Entry("101", "Draft Post", "1", "1700001000", "0", "Draft body.", "", ""),
	)
	// entry/102.cgi — published but uncategorised. SB2 marks that with an
	// EMPTY cat (not "0", which is the real category 0). Must land with
	// category_id = -1, NOT a NULL constraint failure.
	mustWrite(t, filepath.Join(dir, "entry", "102.cgi"),
		sb2Entry("102", "Uncategorised Post", "", "1700002000", "1", "No category here.", "", ""),
	)
	// entry/103.cgi — published but references a non-existent category.
	mustWrite(t, filepath.Join(dir, "entry", "103.cgi"),
		sb2Entry("103", "Bad Category Post", "999", "1700003000", "1", "Body.", "", ""),
	)

	// message/200.cgi — comment on entry 100 (published). Should land.
	mustMkdir(t, filepath.Join(dir, "message"))
	mustWrite(t, filepath.Join(dir, "message", "200.cgi"),
		sb2Message("200", "100", "1", "1700000500", "Reader", "192.0.2.1", "comment on published"),
	)
	// message/201.cgi — comment on entry 101 (draft). When the entry
	// is filtered out by OnlyPublished the comment must be dropped
	// silently (no orphan inserts).
	mustWrite(t, filepath.Join(dir, "message", "201.cgi"),
		sb2Message("201", "101", "1", "1700001500", "Reader", "192.0.2.2", "comment on draft"),
	)

	// template/10.cgi — Data/Template.pm elements: id, wid, use, name,
	// gen, mod, info, main, css, entry, files
	mustMkdir(t, filepath.Join(dir, "template"))
	mustWrite(t, filepath.Join(dir, "template", "10.cgi"),
		sb2Row("10", "0", "0", "default", "0", "0", "info", "<html>{site_title}</html>", "body{}", "", ""),
	)

	return dir
}

// sb2Row encodes one SB2 record as TAB-separated values with the
// per-field `\t` / `\n` / `\\` escapes plus a trailing tab + newline,
// matching Driver::Text._encode.
func sb2Row(fields ...string) string {
	encoded := make([]string, len(fields))
	for i, f := range fields {
		s := strings.ReplaceAll(f, `\`, `\\`)
		s = strings.ReplaceAll(s, "\t", `\t`)
		s = strings.ReplaceAll(s, "\n", `\n`)
		encoded[i] = s
	}
	return strings.Join(encoded, "\t") + "\t\n"
}

// sb2Entry produces a 23-column SB2 entry row in the order defined by
// Data/Entry.pm. Fields the importer does not look at are zero-padded
// so the row keeps the right shape.
func sb2Entry(id, subj, cat, date, stat, body, more, file string) string {
	return sb2EntryWithSum(id, subj, cat, date, stat, body, more, file, "")
}

// sb2EntryWithSum is sb2Entry plus the `sum` (summary) field at column
// 19 — SB stores it [entitized]. Used to exercise the summary import +
// unescape path.
func sb2EntryWithSum(id, subj, cat, date, stat, body, more, file, sum string) string {
	return sb2Row(
		id,      // 0  id
		"0",     // 1  wid
		subj,    // 2  subj
		cat,     // 3  cat
		date,    // 4  date
		"1",     // 5  auth
		stat,    // 6  stat
		"0",     // 7  com
		"0",     // 8  tb
		file,    // 9  file
		"+0900", // 10 tz
		"",      // 11 add
		"",      // 12 edit
		"1",     // 13 acm
		"1",     // 14 atb
		"",      // 15 form
		"",      // 16 ping
		body,    // 17 body
		more,    // 18 more
		sum,     // 19 sum
		"",      // 20 key
		"",      // 21 ext
		"",      // 22 tmp
	)
}

// sb2Message produces a 16-column SB2 message row matching
// Data/Message.pm. Status is passed through directly (SB2 0/1/-1
// already aligns with the destination schema).
func sb2Message(id, eid, stat, date, auth, host, body string) string {
	return sb2Row(
		id,                         // 0  id
		"0",                        // 1  wid
		eid,                        // 2  eid
		stat,                       // 3  stat
		date,                       // 4  date
		auth,                       // 5  auth
		host,                       // 6  host
		"+0900",                    // 7 tz
		"",                         // 8  mail
		"",                         // 9  url
		"Mozilla/5.0 (test agent)", // 10 agnt
		body,                       // 11 body
		"",                         // 12 icon
		"",                         // 13 ext
		"",                         // 14 admn
		"0",                        // 15 out
	)
}

func mustWrite(t *testing.T, path string, parts ...string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.Join(parts, "")), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", path, err)
	}
}

func TestImportSB2BasicRoundTrip(t *testing.T) {
	dir := buildSB2Fixture(t)
	a := destApp(t)
	report, err := importer.Import(context.Background(), a.DB, dir, importer.Options{
		TargetWID:     1,
		AuthorID:      1,
		OnlyPublished: true,
		SBVersion:     2,
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	sb2BasicAssertReportCounts(t, report)
	sb2BasicAssertWeblogMeta(t, a.DB)
	sb2BasicAssertBasePathFallback(t, a.DB)
	sb2BasicAssertCategoryParentRemap(t, a.DB)
	sb2BasicAssertCommentsOnPublishedOnly(t, a.DB)
	sb2BasicAssertSummaryUnescaped(t, a.DB)
}

// sb2BasicAssertSummaryUnescaped verifies SB2 `sum` (column 19) lands in
// the summary column with HTML entities decoded — entry 100's source
// summary is 'Tom &amp; Jerry', which must persist as raw 'Tom & Jerry'.
func sb2BasicAssertSummaryUnescaped(t *testing.T, db *sql.DB) {
	t.Helper()
	var summary string
	if err := db.QueryRow(`SELECT summary FROM entries WHERE wid = 1 AND legacy_id = 100`).Scan(&summary); err != nil {
		t.Fatal(err)
	}
	if summary != "Tom & Jerry" {
		t.Errorf("entry 100 summary = %q, want %q", summary, "Tom & Jerry")
	}
}

func sb2BasicAssertReportCounts(t *testing.T, report *importer.Report) {
	t.Helper()
	if !report.WeblogUpdated {
		t.Errorf("weblog not updated")
	}
	if report.Categories != 2 {
		t.Errorf("categories = %d, want 2", report.Categories)
	}
	if report.Entries != 3 {
		t.Errorf("entries = %d, want 3 (1 draft skipped)", report.Entries)
	}
	if report.Templates != 1 {
		t.Errorf("templates = %d, want 1", report.Templates)
	}
}

func sb2BasicAssertWeblogMeta(t *testing.T, db *sql.DB) {
	t.Helper()
	var title, desc string
	if err := db.QueryRow(`SELECT title, description FROM weblogs WHERE id = 1`).Scan(&title, &desc); err != nil {
		t.Fatal(err)
	}
	if title != "Synthetic Blog" {
		t.Errorf("weblog title = %q, want Synthetic Blog", title)
	}
	if !strings.Contains(desc, "synthetic SB2 fixture") {
		t.Errorf("weblog description = %q", desc)
	}
}

// sb2BasicAssertBasePathFallback exercises the SB2-only path: the
// fixture's configure.cgi has only conf_srv_cgi, so the importer must
// fall back to it when filling in legacy_base_path.
func sb2BasicAssertBasePathFallback(t *testing.T, db *sql.DB) {
	t.Helper()
	var basePath string
	if err := db.QueryRow(`SELECT legacy_base_path FROM weblogs WHERE id = 1`).Scan(&basePath); err != nil {
		t.Fatal(err)
	}
	if basePath != "/myblog/" {
		t.Errorf("legacy_base_path = %q, want /myblog/ (conf_srv_cgi fallback)", basePath)
	}
}

func sb2BasicAssertCategoryParentRemap(t *testing.T, db *sql.DB) {
	t.Helper()
	var parentRemapped, childParent int64
	if err := db.QueryRow(`SELECT id FROM categories WHERE name = 'Tech'`).Scan(&parentRemapped); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT parent_id FROM categories WHERE name = 'Sub-tech'`).Scan(&childParent); err != nil {
		t.Fatal(err)
	}
	if childParent != parentRemapped {
		t.Errorf("child parent_id = %d, want %d", childParent, parentRemapped)
	}
}

// sb2BasicAssertCommentsOnPublishedOnly checks that comments attached
// to the skipped draft were dropped silently; only the comment on the
// published entry should land in messages.
func sb2BasicAssertCommentsOnPublishedOnly(t *testing.T, db *sql.DB) {
	t.Helper()
	var msgCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM messages WHERE wid = 1`).Scan(&msgCount); err != nil {
		t.Fatal(err)
	}
	if msgCount != 1 {
		t.Errorf("messages = %d, want 1", msgCount)
	}
}

// TestImportSB2CategoryParentZero is a regression for the SB2 quirk that
// category ids are 0-based: an EMPTY string means "none", while "0" is the
// real category 0. Collapsing both to the integer 0 (the old bug) detached
// every child of category 0 into a top-level row on the category-parent
// side, and dropped every entry assigned to category 0 to uncategorised on
// the entry side. Both sides are covered here.
func TestImportSB2CategoryParentZero(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "configure.cgi"), "conf_dbtype\tText\n")
	mustWrite(t, filepath.Join(dir, "weblog.cgi"),
		sb2Row("0", "Zero-parent Blog", "category 0 is a real parent"))
	// cat 0: top-level (empty main). cat 1: child of cat 0 (main="0").
	// cat 2: top-level (empty main) — proves empty main is NOT remapped.
	mustWrite(t, filepath.Join(dir, "category.cgi"),
		sb2Row("0", "0", "Root", "", "", "", "0", "0", "root/", "0", "", "0", ""),
		sb2Row("1", "0", "ChildOfZero", "", "", "0", "0", "0", "child/", "0", "", "0", ""),
		sb2Row("2", "0", "OtherTop", "", "", "", "0", "0", "other/", "0", "", "0", ""),
	)
	// entry 100: cat="0" (real category 0) must map to category 0, NOT -1.
	// entry 101: cat="" (uncategorised) must land at -1.
	mustMkdir(t, filepath.Join(dir, "entry"))
	mustWrite(t, filepath.Join(dir, "entry", "100.cgi"),
		sb2Entry("100", "In Category Zero", "0", "1700000000", "1", "body", "", ""))
	mustWrite(t, filepath.Join(dir, "entry", "101.cgi"),
		sb2Entry("101", "Uncategorised", "", "1700000100", "1", "body", "", ""))

	a := destApp(t)
	report, err := importer.Import(context.Background(), a.DB, dir, importer.Options{
		TargetWID: 1, AuthorID: 1, OnlyPublished: true, SBVersion: 2,
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if report.Categories != 3 {
		t.Errorf("categories = %d, want 3", report.Categories)
	}

	byLegacy := func(id int64) (destID, parentID int64) {
		t.Helper()
		if err := a.DB.QueryRow(
			`SELECT id, parent_id FROM categories WHERE wid = 1 AND legacy_id = ?`, id,
		).Scan(&destID, &parentID); err != nil {
			t.Fatalf("category legacy_id=%d: %v", id, err)
		}
		return destID, parentID
	}

	rootDest, rootParent := byLegacy(0)
	if rootParent != 0 {
		t.Errorf("Root (legacy 0) parent_id = %d, want 0 (top-level)", rootParent)
	}
	_, childParent := byLegacy(1)
	if childParent != rootDest {
		t.Errorf("ChildOfZero parent_id = %d, want %d (dest of category 0)", childParent, rootDest)
	}
	_, otherParent := byLegacy(2)
	if otherParent != 0 {
		t.Errorf("OtherTop (empty main) parent_id = %d, want 0 (top-level)", otherParent)
	}

	// Entry side: cat="0" must map to category 0's dest id, cat="" to -1.
	entryCat := func(title string) int64 {
		t.Helper()
		var cat int64
		if err := a.DB.QueryRow(
			`SELECT category_id FROM entries WHERE wid = 1 AND title = ?`, title,
		).Scan(&cat); err != nil {
			t.Fatalf("entry %q: %v", title, err)
		}
		return cat
	}
	if got := entryCat("In Category Zero"); got != rootDest {
		t.Errorf("entry in category 0 category_id = %d, want %d (dest of category 0)", got, rootDest)
	}
	if got := entryCat("Uncategorised"); got != -1 {
		t.Errorf("uncategorised entry category_id = %d, want -1", got)
	}

	// The parent (category 0) exists, so no "parent not found" warning
	// should be emitted for this fixture.
	for _, w := range report.Warnings {
		if strings.Contains(w, "parent legacy id") {
			t.Errorf("unexpected parent-fixup warning: %q", w)
		}
	}
}

func TestImportSB2UncategorisedFallback(t *testing.T) {
	// Regression: entries.category_id is NOT NULL. SB2 entries with
	// cat=0 (uncategorised) or cat referencing a missing row must
	// land at -1 instead of triggering a NULL constraint rollback.
	dir := buildSB2Fixture(t)
	a := destApp(t)
	report, err := importer.Import(context.Background(), a.DB, dir, importer.Options{
		TargetWID: 1, AuthorID: 1, OnlyPublished: true, SBVersion: 2,
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	rows, err := a.DB.Query(`SELECT title, category_id FROM entries WHERE wid = 1 ORDER BY title`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]int64{}
	for rows.Next() {
		var title string
		var cat int64
		if err := rows.Scan(&title, &cat); err != nil {
			t.Fatal(err)
		}
		got[title] = cat
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if got["Uncategorised Post"] != -1 {
		t.Errorf("Uncategorised Post category_id = %d, want -1", got["Uncategorised Post"])
	}
	if got["Bad Category Post"] != -1 {
		t.Errorf("Bad Category Post category_id = %d, want -1", got["Bad Category Post"])
	}

	// And the unknown-category warning must surface.
	var saw bool
	for _, w := range report.Warnings {
		if strings.Contains(w, "unknown SB2 category 999") {
			saw = true
			break
		}
	}
	if !saw {
		t.Errorf("expected warning for unknown category 999; got %v", report.Warnings)
	}
}

func TestImportSB2SkippedCountReported(t *testing.T) {
	// Regression: OnlyPublished=true must populate Report.SkippedEntries
	// so the CLI's "skipped=N" line matches the SB3 importer's contract.
	dir := buildSB2Fixture(t)
	a := destApp(t)
	report, err := importer.Import(context.Background(), a.DB, dir, importer.Options{
		TargetWID: 1, AuthorID: 1, OnlyPublished: true, SBVersion: 2,
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if report.SkippedEntries != 1 {
		t.Errorf("SkippedEntries = %d, want 1 (one draft in fixture)", report.SkippedEntries)
	}
	if report.Entries != 3 {
		t.Errorf("Entries = %d, want 3", report.Entries)
	}
}

func TestImportSB2IncludesDrafts(t *testing.T) {
	dir := buildSB2Fixture(t)
	a := destApp(t)
	report, err := importer.Import(context.Background(), a.DB, dir, importer.Options{
		TargetWID: 1, AuthorID: 1, OnlyPublished: false, SBVersion: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Entries != 4 {
		t.Errorf("Entries = %d, want 4 (incl. draft)", report.Entries)
	}
	if report.SkippedEntries != 0 {
		t.Errorf("SkippedEntries = %d, want 0", report.SkippedEntries)
	}
	// The draft's comment now has a real entry to attach to.
	var msgCount int
	if err := a.DB.QueryRow(`SELECT COUNT(*) FROM messages WHERE wid = 1`).Scan(&msgCount); err != nil {
		t.Fatal(err)
	}
	if msgCount != 2 {
		t.Errorf("messages = %d, want 2", msgCount)
	}
}

func TestImportSB2EUCJPDecode(t *testing.T) {
	// Build a tiny EUC-JP fixture inline (one weblog row + one entry
	// with hiragana in title and body). Confirms jacharset auto-detect
	// fires inside readSB2Records — SB2's "standard" encoding.
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "configure.cgi"), "conf_dbtype\tText\n")
	encEUC := func(s string) string {
		out, _, err := transform.String(japanese.EUCJP.NewEncoder(), s)
		if err != nil {
			t.Fatalf("EUC-JP encode: %v", err)
		}
		return out
	}
	mustWrite(t, filepath.Join(dir, "weblog.cgi"), encEUC("0\tこんにちは\tテスト用\t\n"))
	mustWrite(t, filepath.Join(dir, "category.cgi"), encEUC("1\t0\tカテゴリ\t\t\t\t0\t0\t\t0\t\t0\t\t\n"))
	mustMkdir(t, filepath.Join(dir, "entry"))
	mustWrite(t, filepath.Join(dir, "entry", "1.cgi"),
		encEUC("1\t0\t日本語タイトル\t1\t1700000000\t1\t1\t0\t0\t\t+0900\t\t\t1\t1\t\t\tこれは本文です。\t\t\t\t\t\n"),
	)

	a := destApp(t)
	if _, err := importer.Import(context.Background(), a.DB, dir, importer.Options{
		TargetWID: 1, AuthorID: 1, OnlyPublished: true, SBVersion: 2,
	}); err != nil {
		t.Fatalf("Import: %v", err)
	}

	// Filter by legacy_id so the seeded sample entry doesn't shadow
	// the imported row.
	var title, body string
	if err := a.DB.QueryRow(`SELECT title, body FROM entries WHERE wid = 1 AND legacy_id = 1`).Scan(&title, &body); err != nil {
		t.Fatal(err)
	}
	if title != "日本語タイトル" {
		t.Errorf("title = %q, want 日本語タイトル (EUC-JP decode failed)", title)
	}
	if body != "これは本文です。" {
		t.Errorf("body = %q, want これは本文です。", body)
	}
}

func TestImportSB2NonEUCEncodings(t *testing.T) {
	// SB2 "usually" ships EUC-JP, but real installs vary by server. The
	// importer must NOT hardcode the encoding by version — it routes flat
	// records through jacharset, which auto-detects per content. This
	// proves a UTF-8 and a Shift_JIS SB2 source decode just as cleanly as
	// the EUC-JP case above. Companion to TestImportSB2EUCJPDecode.
	const wantTitle = "日本語タイトル"
	const wantBody = "これは本文です。"

	cases := []struct {
		name string
		enc  func(t *testing.T, s string) string
	}{
		{
			name: "utf-8 (no transform)",
			enc:  func(_ *testing.T, s string) string { return s },
		},
		{
			name: "shift_jis",
			enc: func(t *testing.T, s string) string {
				out, _, err := transform.String(japanese.ShiftJIS.NewEncoder(), s)
				if err != nil {
					t.Fatalf("Shift_JIS encode: %v", err)
				}
				return out
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			enc := func(s string) string { return tc.enc(t, s) }

			mustWrite(t, filepath.Join(dir, "configure.cgi"), "conf_dbtype\tText\n")
			mustWrite(t, filepath.Join(dir, "weblog.cgi"), enc("0\tこんにちは\tテスト用\t\n"))
			mustWrite(t, filepath.Join(dir, "category.cgi"), enc("1\t0\tカテゴリ\t\t\t\t0\t0\t\t0\t\t0\t\t\n"))
			mustMkdir(t, filepath.Join(dir, "entry"))
			mustWrite(t, filepath.Join(dir, "entry", "1.cgi"),
				enc("1\t0\t"+wantTitle+"\t1\t1700000000\t1\t1\t0\t0\t\t+0900\t\t\t1\t1\t\t\t"+wantBody+"\t\t\t\t\t\n"),
			)

			a := destApp(t)
			if _, err := importer.Import(context.Background(), a.DB, dir, importer.Options{
				TargetWID: 1, AuthorID: 1, OnlyPublished: true, SBVersion: 2,
			}); err != nil {
				t.Fatalf("Import: %v", err)
			}

			var title, body string
			if err := a.DB.QueryRow(`SELECT title, body FROM entries WHERE wid = 1 AND legacy_id = 1`).Scan(&title, &body); err != nil {
				t.Fatal(err)
			}
			if title != wantTitle {
				t.Errorf("title = %q, want %q (%s decode failed — version hardcoding?)", title, wantTitle, tc.name)
			}
			if body != wantBody {
				t.Errorf("body = %q, want %q", body, wantBody)
			}
		})
	}
}

func TestImportSB2RejectsBadVersion(t *testing.T) {
	a := destApp(t)
	_, err := importer.Import(context.Background(), a.DB, "/dev/null", importer.Options{
		TargetWID: 1, AuthorID: 1, SBVersion: 9,
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported SB version") {
		t.Errorf("expected unsupported-version error, got %v", err)
	}
}
