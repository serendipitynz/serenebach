package importer_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/serendipitynz/serenebach/internal/importer"
)

// sandboxSB2DataDir locates _sandbox/sb2/data relative to the repo
// root. The directory is gitignored — tests skip when it isn't checked
// out (CI runners, lean clones).
func sandboxSB2DataDir(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// importer_test runs from internal/importer; walk up to repo root.
	root := filepath.Join(wd, "..", "..")
	path := filepath.Join(root, "_sandbox", "sb2", "data")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("SB2 sandbox not present: %v", err)
	}
	return path
}

func TestImportSB2SandboxRoundTrip(t *testing.T) {
	dir := sandboxSB2DataDir(t)
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

	t.Logf("SB2 import: weblog=%t templates=%d categories=%d entries=%d", report.WeblogUpdated, report.Templates, report.Categories, report.Entries)

	// The sandbox is a real blog — assert non-trivial counts rather
	// than exact numbers (which would couple this test to the fixture's
	// exact contents and break on benign updates).
	if !report.WeblogUpdated {
		t.Errorf("weblog not updated; SB2 weblog.cgi likely missed")
	}
	if report.Categories < 5 {
		t.Errorf("categories = %d, want at least 5", report.Categories)
	}
	if report.Entries < 50 {
		t.Errorf("entries = %d, want at least 50 (sandbox has ~400)", report.Entries)
	}
	if report.Templates < 1 {
		t.Errorf("templates = %d, want at least 1", report.Templates)
	}

	// Comments table should have rows pointing at imported entries.
	var msgCount int
	if err := a.DB.QueryRow(`SELECT COUNT(*) FROM messages WHERE wid = 1`).Scan(&msgCount); err != nil {
		t.Fatal(err)
	}
	if msgCount < 50 {
		t.Errorf("imported comments = %d, want at least 50", msgCount)
	}

	// configure.cgi → legacy_*. The sandbox has conf_srv_cgi set to
	// http://127.0.0.1/sblog/, no conf_srv_base, so the fallback
	// must produce /sblog/.
	var basePath string
	if err := a.DB.QueryRow(`SELECT legacy_base_path FROM weblogs WHERE id = 1`).Scan(&basePath); err != nil {
		t.Fatal(err)
	}
	if basePath != "/sblog/" {
		t.Errorf("legacy_base_path = %q, want /sblog/", basePath)
	}

	// Spot-check entry body decoding: pick one entry, look for any
	// Japanese hiragana — confirms the EUC-JP → UTF-8 conversion fired.
	var body string
	if err := a.DB.QueryRow(
		`SELECT body FROM entries WHERE legacy_id IS NOT NULL AND length(body) > 100 ORDER BY posted_at LIMIT 1`,
	).Scan(&body); err != nil {
		t.Fatal(err)
	}
	if !containsHiragana(body) {
		t.Errorf("first entry body lacks hiragana — EUC-JP decode may have failed; body[:80]=%q", truncate(body, 80))
	}

	// Comments should map to real imported entries (foreign-key style).
	var orphan int
	if err := a.DB.QueryRow(
		`SELECT COUNT(*) FROM messages m WHERE NOT EXISTS (SELECT 1 FROM entries e WHERE e.id = m.entry_id)`,
	).Scan(&orphan); err != nil {
		t.Fatal(err)
	}
	if orphan != 0 {
		t.Errorf("orphan comments after import = %d", orphan)
	}
}

func TestImportSB2SandboxIncludesDrafts(t *testing.T) {
	// OnlyPublished=false should pull a higher entry count than the
	// published-only run (assuming the fixture has at least one draft;
	// SB2's entry stat=0 marks drafts).
	dir := sandboxSB2DataDir(t)

	a1 := destApp(t)
	r1, err := importer.Import(context.Background(), a1.DB, dir, importer.Options{
		TargetWID: 1, AuthorID: 1, OnlyPublished: true, SBVersion: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	a2 := destApp(t)
	r2, err := importer.Import(context.Background(), a2.DB, dir, importer.Options{
		TargetWID: 1, AuthorID: 1, OnlyPublished: false, SBVersion: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if r2.Entries < r1.Entries {
		t.Errorf("OnlyPublished=false produced %d entries; expected >= published-only %d", r2.Entries, r1.Entries)
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

func containsHiragana(s string) bool {
	for _, r := range s {
		if r >= 0x3040 && r <= 0x309F {
			return true
		}
	}
	return false
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
