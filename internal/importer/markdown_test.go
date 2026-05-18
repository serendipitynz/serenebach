package importer_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/serendipitynz/serenebach/internal/importer"
)

// writeMD is a tiny helper that drops a markdown file under dir with
// the given content and asserts the write succeeded. Returns the
// full path so the test can refer back to it in assertions.
func writeMD(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return p
}

func TestMarkdownImportInsertsEntry(t *testing.T) {
	a := destApp(t)
	dir := t.TempDir()
	writeMD(t, dir, "hello-world.md", `---
title: "Hello, World"
status: published
posted_at: 2025-01-01T12:00:00+09:00
keywords: "intro,hello"
---

# Body

This is the first entry.
`)

	report, err := importer.Import(context.Background(), a.DB, dir, importer.Options{
		Source:    "md",
		TargetWID: 1,
		AuthorID:  1,
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if report.EntriesInserted != 1 || report.EntriesUpdated != 0 {
		t.Errorf("counts: inserted=%d updated=%d (want 1/0)", report.EntriesInserted, report.EntriesUpdated)
	}
	if len(report.Warnings) != 0 {
		t.Errorf("unexpected warnings: %v", report.Warnings)
	}

	var (
		slug, title, body, format, keywords string
		status                              int
		pinned                              int
	)
	row := a.DB.QueryRow(`SELECT slug, title, body, format, keywords, status, pinned FROM entries WHERE slug = ?`, "hello-world")
	if err := row.Scan(&slug, &title, &body, &format, &keywords, &status, &pinned); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if slug != "hello-world" || title != "Hello, World" {
		t.Errorf("slug/title = %q/%q", slug, title)
	}
	if !strings.Contains(body, "first entry.") {
		t.Errorf("body missing expected text: %q", body)
	}
	if format != "markdown" {
		t.Errorf("format = %q, want markdown", format)
	}
	if keywords != "intro,hello" {
		t.Errorf("keywords = %q", keywords)
	}
	if status != 1 {
		t.Errorf("status = %d, want 1", status)
	}
	if pinned != 0 {
		t.Errorf("pinned = %d, want 0", pinned)
	}
}

func TestMarkdownImportSlugFromFilename(t *testing.T) {
	a := destApp(t)
	dir := t.TempDir()
	writeMD(t, dir, "v4-0-0-beta-11-en.md", `---
title: "Release v4.0.0-beta.11"
---

Body.
`)

	if _, err := importer.Import(context.Background(), a.DB, dir, importer.Options{
		Source: "md", TargetWID: 1, AuthorID: 1,
	}); err != nil {
		t.Fatalf("Import: %v", err)
	}
	var slug string
	if err := a.DB.QueryRow(`SELECT slug FROM entries WHERE title = ?`, "Release v4.0.0-beta.11").Scan(&slug); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if slug != "v4-0-0-beta-11-en" {
		t.Errorf("slug = %q, want v4-0-0-beta-11-en", slug)
	}
}

func TestMarkdownImportSlugFrontMatterOverridesFilename(t *testing.T) {
	a := destApp(t)
	dir := t.TempDir()
	writeMD(t, dir, "filename-slug.md", `---
slug: explicit-slug
title: "Override Test"
---

Body.
`)

	if _, err := importer.Import(context.Background(), a.DB, dir, importer.Options{
		Source: "md", TargetWID: 1, AuthorID: 1,
	}); err != nil {
		t.Fatalf("Import: %v", err)
	}
	var slug string
	if err := a.DB.QueryRow(`SELECT slug FROM entries WHERE title = ?`, "Override Test").Scan(&slug); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if slug != "explicit-slug" {
		t.Errorf("slug = %q, want explicit-slug (front-matter wins)", slug)
	}
}

func TestMarkdownImportInvalidFilenameRequiresSlug(t *testing.T) {
	a := destApp(t)
	dir := t.TempDir()
	// Underscore is not valid in IsValidSlug (only - is allowed as separator).
	writeMD(t, dir, "release_v4.md", `---
title: "No slug, bad filename"
---

Body.
`)

	report, err := importer.Import(context.Background(), a.DB, dir, importer.Options{
		Source: "md", TargetWID: 1, AuthorID: 1,
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if report.EntriesInserted != 0 {
		t.Errorf("expected 0 inserts, got %d", report.EntriesInserted)
	}
	if !anyWarningContains(report.Warnings, "specify `slug:`") {
		t.Errorf("missing helpful slug hint in warnings: %v", report.Warnings)
	}
}

func TestMarkdownImportInvalidFilenameWithFrontMatterSlug(t *testing.T) {
	a := destApp(t)
	dir := t.TempDir()
	writeMD(t, dir, "release_v4.md", `---
slug: release-v4
title: "Imported via front-matter slug"
---

Body.
`)

	report, err := importer.Import(context.Background(), a.DB, dir, importer.Options{
		Source: "md", TargetWID: 1, AuthorID: 1,
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if report.EntriesInserted != 1 {
		t.Errorf("expected 1 insert, got %d (warnings=%v)", report.EntriesInserted, report.Warnings)
	}
	var slug string
	if err := a.DB.QueryRow(`SELECT slug FROM entries WHERE title = ?`, "Imported via front-matter slug").Scan(&slug); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if slug != "release-v4" {
		t.Errorf("slug = %q", slug)
	}
}

func TestMarkdownImportUpsertPreservesPostedAt(t *testing.T) {
	a := destApp(t)
	dir := t.TempDir()

	// First pass: INSERT with a specific posted_at.
	writeMD(t, dir, "same-slug.md", `---
title: "Original Title"
posted_at: 2025-01-01T00:00:00+09:00
---

First body.
`)
	if _, err := importer.Import(context.Background(), a.DB, dir, importer.Options{
		Source: "md", TargetWID: 1, AuthorID: 1,
	}); err != nil {
		t.Fatalf("first import: %v", err)
	}

	var initialPosted, initialID int64
	if err := a.DB.QueryRow(`SELECT id, posted_at FROM entries WHERE slug = ?`, "same-slug").Scan(&initialID, &initialPosted); err != nil {
		t.Fatalf("first read: %v", err)
	}

	// Second pass: update with a *different* posted_at; the importer
	// must keep the original to avoid silently rewriting history.
	writeMD(t, dir, "same-slug.md", `---
title: "Updated Title"
posted_at: 2026-12-31T00:00:00+09:00
---

Second body.
`)
	report, err := importer.Import(context.Background(), a.DB, dir, importer.Options{
		Source: "md", TargetWID: 1, AuthorID: 1,
	})
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if report.EntriesInserted != 0 || report.EntriesUpdated != 1 {
		t.Errorf("second counts: inserted=%d updated=%d (want 0/1)", report.EntriesInserted, report.EntriesUpdated)
	}

	var (
		newPosted, newID  int64
		newTitle, newBody string
	)
	if err := a.DB.QueryRow(`SELECT id, posted_at, title, body FROM entries WHERE slug = ?`, "same-slug").Scan(&newID, &newPosted, &newTitle, &newBody); err != nil {
		t.Fatalf("second read: %v", err)
	}
	if newID != initialID {
		t.Errorf("id changed on update: was %d now %d", initialID, newID)
	}
	if newPosted != initialPosted {
		t.Errorf("posted_at changed on update: was %d now %d", initialPosted, newPosted)
	}
	if newTitle != "Updated Title" || !strings.Contains(newBody, "Second body.") {
		t.Errorf("title/body not refreshed: title=%q body=%q", newTitle, newBody)
	}

	var count int
	if err := a.DB.QueryRow(`SELECT COUNT(*) FROM entries WHERE slug = ?`, "same-slug").Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("entries with slug=same-slug = %d, want 1", count)
	}
}

func TestMarkdownImportSkipsFilesWithoutFrontMatter(t *testing.T) {
	a := destApp(t)
	dir := t.TempDir()
	writeMD(t, dir, "no-frontmatter.md", `# Just a heading, no YAML envelope.`)
	writeMD(t, dir, "valid.md", `---
title: "Valid"
---

Body.
`)

	report, err := importer.Import(context.Background(), a.DB, dir, importer.Options{
		Source: "md", TargetWID: 1, AuthorID: 1,
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if report.EntriesInserted != 1 {
		t.Errorf("inserted = %d, want 1", report.EntriesInserted)
	}
	if !anyWarningContains(report.Warnings, "no YAML front-matter") {
		t.Errorf("missing front-matter warning: %v", report.Warnings)
	}
}

func TestMarkdownImportSkipsMissingTitle(t *testing.T) {
	a := destApp(t)
	dir := t.TempDir()
	writeMD(t, dir, "valid.md", `---
status: published
---

Body without title.
`)

	report, err := importer.Import(context.Background(), a.DB, dir, importer.Options{
		Source: "md", TargetWID: 1, AuthorID: 1,
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if report.EntriesInserted != 0 {
		t.Errorf("inserted = %d, want 0", report.EntriesInserted)
	}
	if !anyWarningContains(report.Warnings, "`title` missing") {
		t.Errorf("missing title warning: %v", report.Warnings)
	}
}

func TestMarkdownImportSlugCollisionSkipsSecond(t *testing.T) {
	a := destApp(t)
	dir := t.TempDir()
	// Both files resolve to slug "shared" (one via filename, the
	// other via front-matter). The second one in sort order should
	// lose, leaving only one row.
	writeMD(t, dir, "a-shared.md", `---
slug: shared
title: "First"
---

A.
`)
	writeMD(t, dir, "shared.md", `---
title: "Second"
---

B.
`)

	report, err := importer.Import(context.Background(), a.DB, dir, importer.Options{
		Source: "md", TargetWID: 1, AuthorID: 1,
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if report.EntriesInserted != 1 {
		t.Errorf("inserted = %d, want 1", report.EntriesInserted)
	}
	if !anyWarningContains(report.Warnings, "duplicate slug") {
		t.Errorf("missing duplicate-slug warning: %v", report.Warnings)
	}

	var title string
	if err := a.DB.QueryRow(`SELECT title FROM entries WHERE slug = ?`, "shared").Scan(&title); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if title != "First" {
		t.Errorf("title = %q, want First (sort order winner)", title)
	}
}

func TestMarkdownImportSkipsSubdirectories(t *testing.T) {
	a := destApp(t)
	dir := t.TempDir()
	sub := filepath.Join(dir, "nested")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeMD(t, sub, "buried.md", `---
title: "Should not be imported"
---

Body.
`)
	writeMD(t, dir, "top.md", `---
title: "Top-level only"
---

Body.
`)

	report, err := importer.Import(context.Background(), a.DB, dir, importer.Options{
		Source: "md", TargetWID: 1, AuthorID: 1,
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if report.EntriesInserted != 1 {
		t.Errorf("inserted = %d, want 1 (subdir should be ignored)", report.EntriesInserted)
	}
	var count int
	if err := a.DB.QueryRow(`SELECT COUNT(*) FROM entries WHERE title = ?`, "Should not be imported").Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("buried entry was imported: count=%d", count)
	}
}

func TestMarkdownImportCategoryResolution(t *testing.T) {
	a := destApp(t)
	if _, err := a.DB.Exec(`INSERT INTO categories (wid, name, slug, created_at, updated_at) VALUES (1, 'English', 'en', 0, 0)`); err != nil {
		t.Fatalf("seed category: %v", err)
	}
	var enID int64
	if err := a.DB.QueryRow(`SELECT id FROM categories WHERE slug = 'en'`).Scan(&enID); err != nil {
		t.Fatalf("read category id: %v", err)
	}

	dir := t.TempDir()
	writeMD(t, dir, "known.md", `---
title: "Known"
category: en
---

Body.
`)
	writeMD(t, dir, "unknown.md", `---
title: "Unknown"
category: nope
---

Body.
`)

	report, err := importer.Import(context.Background(), a.DB, dir, importer.Options{
		Source: "md", TargetWID: 1, AuthorID: 1,
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if report.EntriesInserted != 2 {
		t.Errorf("inserted = %d, want 2", report.EntriesInserted)
	}
	if !anyWarningContains(report.Warnings, `category "nope" not found`) {
		t.Errorf("missing unknown-category warning: %v", report.Warnings)
	}

	var knownCat int64
	if err := a.DB.QueryRow(`SELECT category_id FROM entries WHERE slug = 'known'`).Scan(&knownCat); err != nil {
		t.Fatalf("read known: %v", err)
	}
	if knownCat != enID {
		t.Errorf("known.category_id = %d, want %d", knownCat, enID)
	}
	var unknownCat int64
	if err := a.DB.QueryRow(`SELECT category_id FROM entries WHERE slug = 'unknown'`).Scan(&unknownCat); err != nil {
		t.Fatalf("read unknown: %v", err)
	}
	if unknownCat != -1 {
		t.Errorf("unknown.category_id = %d, want -1", unknownCat)
	}
}

func TestMarkdownImportPreservesExistingUnrelatedEntries(t *testing.T) {
	a := destApp(t)
	// Plant an entry that no markdown file references; it must
	// survive the import unchanged.
	now := time.Now().Unix()
	if _, err := a.DB.Exec(`
		INSERT INTO entries (wid, author_id, category_id, title, body, status, posted_at, created_at, updated_at)
		VALUES (1, 1, -1, 'Untouched', 'body', 1, ?, ?, ?)`, now, now, now); err != nil {
		t.Fatalf("seed entry: %v", err)
	}

	dir := t.TempDir()
	writeMD(t, dir, "new-entry.md", `---
title: "New"
---

Body.
`)
	if _, err := importer.Import(context.Background(), a.DB, dir, importer.Options{
		Source: "md", TargetWID: 1, AuthorID: 1,
	}); err != nil {
		t.Fatalf("Import: %v", err)
	}

	var count int
	if err := a.DB.QueryRow(`SELECT COUNT(*) FROM entries WHERE title = 'Untouched'`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("untouched entry count = %d, want 1 (importer deleted it?)", count)
	}
}

func TestMarkdownImportGeneratesOGCardWhenImageDirSet(t *testing.T) {
	a := destApp(t)
	imgDir := t.TempDir()
	dir := t.TempDir()
	writeMD(t, dir, "with-og.md", `---
title: "OG Test"
---

Body.
`)

	report, err := importer.Import(context.Background(), a.DB, dir, importer.Options{
		Source: "md", TargetWID: 1, AuthorID: 1, ImageDir: imgDir,
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	var entryID int64
	if err := a.DB.QueryRow(`SELECT id FROM entries WHERE slug = 'with-og'`).Scan(&entryID); err != nil {
		t.Fatalf("read entry id: %v", err)
	}
	cardPath := filepath.Join(imgDir, "og", filenameForOGEntryID(entryID))
	info, err := os.Stat(cardPath)
	if err != nil {
		t.Fatalf("OG card not created at %s: %v (warnings=%v)", cardPath, err, report.Warnings)
	}
	if info.Size() == 0 {
		t.Errorf("OG card at %s is empty", cardPath)
	}
}

func TestMarkdownImportSkipsOGWhenImageDirUnset(t *testing.T) {
	a := destApp(t)
	dir := t.TempDir()
	writeMD(t, dir, "no-og.md", `---
title: "Skip OG"
---

Body.
`)

	if _, err := importer.Import(context.Background(), a.DB, dir, importer.Options{
		Source: "md", TargetWID: 1, AuthorID: 1,
		// ImageDir intentionally empty.
	}); err != nil {
		t.Fatalf("Import: %v", err)
	}
	// No file system to assert on, but the import should succeed
	// and the row should be present. The negative "no panic, no
	// stray files" assertion is implicit in test success.
	var count int
	if err := a.DB.QueryRow(`SELECT COUNT(*) FROM entries WHERE slug = 'no-og'`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("entry count = %d, want 1", count)
	}
}

func TestMarkdownImportStatusValues(t *testing.T) {
	a := destApp(t)
	dir := t.TempDir()
	writeMD(t, dir, "pub.md", "---\ntitle: pub\nstatus: published\n---\n")
	writeMD(t, dir, "draft.md", "---\ntitle: draft\nstatus: draft\n---\n")
	writeMD(t, dir, "closed.md", "---\ntitle: closed\nstatus: closed\n---\n")
	writeMD(t, dir, "weird.md", "---\ntitle: weird\nstatus: garbage\n---\n")

	report, err := importer.Import(context.Background(), a.DB, dir, importer.Options{
		Source: "md", TargetWID: 1, AuthorID: 1,
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if report.EntriesInserted != 4 {
		t.Errorf("inserted = %d, want 4", report.EntriesInserted)
	}
	wantStatus := map[string]int{
		"pub":    1,
		"draft":  0,
		"closed": -1,
		"weird":  1, // unknown collapses to published with a warning
	}
	for slug, want := range wantStatus {
		var got int
		if err := a.DB.QueryRow(`SELECT status FROM entries WHERE slug = ?`, slug).Scan(&got); err != nil {
			t.Fatalf("read %s: %v", slug, err)
		}
		if got != want {
			t.Errorf("status[%s] = %d, want %d", slug, got, want)
		}
	}
	if !anyWarningContains(report.Warnings, `unknown status "garbage"`) {
		t.Errorf("missing unknown-status warning: %v", report.Warnings)
	}
}

func TestMarkdownImportToleratesBOM(t *testing.T) {
	a := destApp(t)
	dir := t.TempDir()
	body := append([]byte{0xEF, 0xBB, 0xBF}, []byte(`---
title: "BOM survivor"
---

Body.
`)...)
	if err := os.WriteFile(filepath.Join(dir, "bom.md"), body, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	report, err := importer.Import(context.Background(), a.DB, dir, importer.Options{
		Source: "md", TargetWID: 1, AuthorID: 1,
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if report.EntriesInserted != 1 {
		t.Errorf("inserted = %d, want 1", report.EntriesInserted)
	}
}

func TestMarkdownImportRejectsNonDirectory(t *testing.T) {
	a := destApp(t)
	file := filepath.Join(t.TempDir(), "not-a-dir.md")
	if err := os.WriteFile(file, []byte("# noop"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := importer.Import(context.Background(), a.DB, file, importer.Options{
		Source: "md", TargetWID: 1, AuthorID: 1,
	})
	if err == nil {
		t.Fatal("expected error for non-directory source")
	}
	if !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("err = %v, want 'not a directory'", err)
	}
}

func anyWarningContains(warns []string, sub string) bool {
	for _, w := range warns {
		if strings.Contains(w, sub) {
			return true
		}
	}
	return false
}

// filenameForOGEntryID matches the on-disk naming used by
// generateMarkdownOGCards (`og/<id>.png`).
func filenameForOGEntryID(id int64) string {
	return strconv.FormatInt(id, 10) + ".png"
}
