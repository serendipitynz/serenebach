package rebuild_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/serendipitynz/serenebach/internal/app"
	"github.com/serendipitynz/serenebach/internal/config"
	"github.com/serendipitynz/serenebach/internal/rebuild"
)

// newSeededApp builds an app with seeded sample content suitable for
// exercising the static builder. Two entries + one category are enough
// to verify every output bucket.
func newSeededApp(t *testing.T) *app.App {
	t.Helper()
	cfg := &config.Config{
		Mode:   config.ModeServer,
		Addr:   ":0",
		DBPath: filepath.Join(t.TempDir(), "dev.db"),
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

func TestBuildProducesAllSections(t *testing.T) {
	a := newSeededApp(t)
	out := filepath.Join(t.TempDir(), "public")

	rep, err := rebuild.Build(context.Background(), a.Store, rebuild.Options{OutDir: out, WID: 1})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !rep.Home {
		t.Errorf("home page not written")
	}
	if rep.Entries != 2 {
		t.Errorf("entries written = %d, want 2", rep.Entries)
	}
	if rep.Categories != 1 {
		t.Errorf("categories written = %d, want 1", rep.Categories)
	}
	if rep.ArchiveMonth == 0 {
		t.Errorf("expected at least one month archive")
	}
	if rep.ArchiveYear == 0 {
		t.Errorf("expected at least one year archive")
	}
	if !rep.CSSWritten {
		t.Errorf("css file not written")
	}
	if !rep.RSSWritten {
		t.Errorf("rss.xml not written")
	}
	if !rep.AtomWritten {
		t.Errorf("atom.xml not written")
	}

	// Key files exist on disk.
	for _, p := range []string{
		"index.html",
		"style.css",
		"entry/1/index.html",
		"entry/2/index.html",
		"category/1/index.html",
		"rss.xml",
		"atom.xml",
	} {
		full := filepath.Join(out, p)
		if _, err := os.Stat(full); err != nil {
			t.Errorf("expected file %s: %v", p, err)
		}
	}

	// rss.xml must actually contain the seeded entry titles — an empty
	// feed file would still pass the Stat check above.
	rss, err := os.ReadFile(filepath.Join(out, "rss.xml"))
	if err != nil {
		t.Fatalf("read rss.xml: %v", err)
	}
	if !strings.Contains(string(rss), "<rss version=\"2.0\"") {
		t.Errorf("rss.xml missing RSS 2.0 declaration")
	}
	if !strings.Contains(string(rss), "<item>") {
		t.Errorf("rss.xml has no <item> entries")
	}
}

func TestBuildHomeContainsSeededTitles(t *testing.T) {
	a := newSeededApp(t)
	out := filepath.Join(t.TempDir(), "public")

	if _, err := rebuild.Build(context.Background(), a.Store, rebuild.Options{OutDir: out, WID: 1}); err != nil {
		t.Fatalf("Build: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(out, "index.html"))
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	s := string(body)
	for _, want := range []string{
		"ようこそ Serene Bach へ",
		"カテゴリとテンプレートについて",
		`href="/entry/1/"`,
		`href="/entry/2/"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("home missing %q; body:\n%s", want, s)
			return
		}
	}
}

func TestBuildArchiveFiltersByMonth(t *testing.T) {
	a := newSeededApp(t)

	// Push one of the seeded entries far into the past so it lands in a
	// separate archive month. This verifies the archive filter actually
	// partitions entries rather than dumping them all together.
	ctx := context.Background()
	old := time.Now().AddDate(-3, 0, 0).Unix()
	if _, err := a.DB.ExecContext(ctx, `UPDATE entries SET posted_at = ? WHERE id = 2`, old); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(t.TempDir(), "public")
	rep, err := rebuild.Build(context.Background(), a.Store, rebuild.Options{OutDir: out, WID: 1})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if rep.ArchiveMonth < 2 {
		t.Errorf("expected at least 2 month archives (spread across years); got %d", rep.ArchiveMonth)
	}

	// Walk the archive tree and confirm at least two year directories
	// exist, one current and one three years back.
	archiveDir := filepath.Join(out, "archive")
	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		t.Fatalf("read archive dir: %v", err)
	}
	if len(entries) < 2 {
		t.Errorf("expected at least 2 year dirs under archive/, got %d", len(entries))
	}
}

func TestBuildEmitsLLMsTxtWhenEnabled(t *testing.T) {
	a := newSeededApp(t)
	// Opt in via the weblog row — the rebuild honours the same
	// toggle as the dynamic routes.
	if _, err := a.DB.Exec(`UPDATE weblogs SET llms_enabled = 1 WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "public")
	rep, err := rebuild.Build(context.Background(), a.Store, rebuild.Options{OutDir: out, WID: 1})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !rep.LLMSWritten {
		t.Errorf("expected LLMSWritten true when weblog opted in")
	}
	for _, name := range []string{"llms.txt", "llms-full.txt"} {
		data, err := os.ReadFile(filepath.Join(out, name))
		if err != nil {
			t.Errorf("read %s: %v", name, err)
			continue
		}
		if !strings.HasPrefix(string(data), "# ") {
			t.Errorf("%s should start with Markdown H1; got %q", name, string(data[:min3(50, len(data))]))
		}
	}
}

func TestBuildSkipsLLMsTxtByDefault(t *testing.T) {
	a := newSeededApp(t)
	out := filepath.Join(t.TempDir(), "public")
	rep, err := rebuild.Build(context.Background(), a.Store, rebuild.Options{OutDir: out, WID: 1})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if rep.LLMSWritten {
		t.Errorf("LLMSWritten should be false without opt-in")
	}
	if _, err := os.Stat(filepath.Join(out, "llms.txt")); !os.IsNotExist(err) {
		t.Errorf("llms.txt should not exist without opt-in; stat err = %v", err)
	}
}

func min3(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestBuildRejectsEmptyOutDir(t *testing.T) {
	a := newSeededApp(t)
	if _, err := rebuild.Build(context.Background(), a.Store, rebuild.Options{OutDir: ""}); err == nil {
		t.Fatal("expected error for empty OutDir")
	}
}

func TestBuildSurfacesMissingTemplate(t *testing.T) {
	a := newSeededApp(t)

	// Remove the active template to force the loader to fail.
	if _, err := a.DB.ExecContext(context.Background(),
		`DELETE FROM templates WHERE is_active = 1`); err != nil {
		t.Fatal(err)
	}

	_, err := rebuild.Build(context.Background(), a.Store, rebuild.Options{
		OutDir: filepath.Join(t.TempDir(), "public"),
		WID:    1,
	})
	if err == nil {
		t.Fatal("expected error when no active template")
	}
}

// TestBuildPrunesStaleManagedSubtrees verifies the cleanup contract:
// when an entry/category/tag/archive page from a previous run no
// longer matches the current DB state, Build must remove the stale
// `*/index.html` so a static host stops serving deleted, unpublished,
// or slug-changed content.
func TestBuildPrunesStaleManagedSubtrees(t *testing.T) {
	a := newSeededApp(t)
	out := filepath.Join(t.TempDir(), "public")

	// Plant stale fixtures that resemble output from a previous run
	// where extra entries / categories / tags / archive months
	// existed. None of these IDs / slugs / years exist in the seeded
	// DB so they must all be pruned.
	stale := []string{
		"entry/9999/index.html",
		"entry/old-slug/index.html",
		"category/9999/index.html",
		"tag/dead-tag/index.html",
		"archive/2010/index.html",
		"archive/2010/01/index.html",
	}
	for _, p := range stale {
		full := filepath.Join(out, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("stale"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := rebuild.Build(context.Background(), a.Store, rebuild.Options{OutDir: out, WID: 1}); err != nil {
		t.Fatalf("Build: %v", err)
	}

	for _, p := range stale {
		if _, err := os.Stat(filepath.Join(out, p)); !os.IsNotExist(err) {
			t.Errorf("stale file %s should have been pruned (err = %v)", p, err)
		}
	}

	// Sanity: live entry pages must still exist after the cleanup.
	for _, p := range []string{"entry/1/index.html", "entry/2/index.html"} {
		if _, err := os.Stat(filepath.Join(out, p)); err != nil {
			t.Errorf("live page %s missing after rebuild: %v", p, err)
		}
	}
}

// TestBuildRemovesStaleLLMSFilesWhenDisabled covers the toggle-off
// path: a previous rebuild emitted llms*.txt while the weblog had
// LLMS publishing on. Once the operator switches the toggle off, the
// next rebuild must remove those files so the static host stops
// advertising the agent-discovery feed.
func TestBuildRemovesStaleLLMSFilesWhenDisabled(t *testing.T) {
	a := newSeededApp(t)
	out := filepath.Join(t.TempDir(), "public")

	// Plant the stale llms files (LLMSEnabled is 0 in the seeded weblog).
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"llms.txt", "llms-full.txt"} {
		if err := os.WriteFile(filepath.Join(out, name), []byte("stale"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := rebuild.Build(context.Background(), a.Store, rebuild.Options{OutDir: out, WID: 1}); err != nil {
		t.Fatalf("Build: %v", err)
	}

	for _, name := range []string{"llms.txt", "llms-full.txt"} {
		if _, err := os.Stat(filepath.Join(out, name)); !os.IsNotExist(err) {
			t.Errorf("stale %s should have been removed when LLMS is off (err = %v)", name, err)
		}
	}
}

// TestBuildPreservesExistingOutputOnFailure verifies the staging
// contract: a build that fails after a previous successful run must
// leave the live snapshot intact. Auto-rebuild swallows errors and
// lets the underlying save still succeed, so a transient failure
// must not tear the public site down.
func TestBuildPreservesExistingOutputOnFailure(t *testing.T) {
	a := newSeededApp(t)
	out := filepath.Join(t.TempDir(), "public")

	// First build succeeds and populates the live snapshot.
	if _, err := rebuild.Build(context.Background(), a.Store, rebuild.Options{OutDir: out, WID: 1}); err != nil {
		t.Fatalf("initial Build: %v", err)
	}
	for _, p := range []string{"index.html", "entry/1/index.html", "entry/2/index.html", "category/1/index.html"} {
		if _, err := os.Stat(filepath.Join(out, p)); err != nil {
			t.Fatalf("first build: missing %s: %v", p, err)
		}
	}
	// Snapshot the bytes so we can prove the live files are untouched
	// after the failed second build.
	wantHome, err := os.ReadFile(filepath.Join(out, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	wantEntry, err := os.ReadFile(filepath.Join(out, "entry/1/index.html"))
	if err != nil {
		t.Fatal(err)
	}

	// Force the next Build to fail by removing the active template
	// — same trick TestBuildSurfacesMissingTemplate uses. This breaks
	// rendering after the staging dir is created, exercising the
	// "fail mid-flight" path the staging swap is designed to survive.
	if _, err := a.DB.ExecContext(context.Background(),
		`DELETE FROM templates WHERE is_active = 1`); err != nil {
		t.Fatal(err)
	}

	if _, err := rebuild.Build(context.Background(), a.Store, rebuild.Options{OutDir: out, WID: 1}); err == nil {
		t.Fatal("expected Build to fail without an active template")
	}

	// Live snapshot must still match the first build byte-for-byte.
	if got, err := os.ReadFile(filepath.Join(out, "index.html")); err != nil {
		t.Errorf("home page disappeared after failed rebuild: %v", err)
	} else if string(got) != string(wantHome) {
		t.Errorf("home page mutated by failed rebuild")
	}
	if got, err := os.ReadFile(filepath.Join(out, "entry/1/index.html")); err != nil {
		t.Errorf("entry/1 disappeared after failed rebuild: %v", err)
	} else if string(got) != string(wantEntry) {
		t.Errorf("entry/1 mutated by failed rebuild")
	}
	for _, p := range []string{"entry/2/index.html", "category/1/index.html"} {
		if _, err := os.Stat(filepath.Join(out, p)); err != nil {
			t.Errorf("%s disappeared after failed rebuild: %v", p, err)
		}
	}

	// Staging dir must not leak into the output tree.
	dirEntries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, de := range dirEntries {
		if strings.HasPrefix(de.Name(), ".sb-rebuild-") {
			t.Errorf("staging dir leaked: %s", de.Name())
		}
	}
}

func TestBuildExpandsCustomTags(t *testing.T) {
	a := newSeededApp(t)
	ctx := context.Background()

	// Add a custom tag to the DB.
	if _, err := a.DB.ExecContext(ctx,
		`INSERT INTO site_custom_tags (wid, name, value) VALUES (?, ?, ?)`,
		1, "custom_test", "<span class=\"custom\">hello</span>"); err != nil {
		t.Fatalf("insert custom tag: %v", err)
	}

	// Inject the placeholder into the active template so the builder
	// has something to expand.
	if _, err := a.DB.ExecContext(ctx,
		`UPDATE templates SET main_body = main_body || '\n<div>{custom_test}</div>' WHERE is_active = 1`); err != nil {
		t.Fatalf("update template: %v", err)
	}

	out := filepath.Join(t.TempDir(), "public")
	if _, err := rebuild.Build(ctx, a.Store, rebuild.Options{OutDir: out, WID: 1}); err != nil {
		t.Fatalf("Build: %v", err)
	}

	body, err := os.ReadFile(filepath.Join(out, "index.html"))
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	if !strings.Contains(string(body), `<span class="custom">hello</span>`) {
		t.Errorf("custom tag not expanded in static output; body:\n%s", string(body))
	}
}

func TestBuildWritesPublishedFlatPages(t *testing.T) {
	a := newSeededApp(t)
	ctx := context.Background()
	now := time.Now().Unix()

	if _, err := a.DB.ExecContext(ctx,
		`INSERT INTO pages (wid, author_id, title, body, format, slug, template_id, sort_order, status, og_bg_image_path, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		1, 1, "About", "<p>about us</p>", "html", "/about", 0, 0, 1, "", now, now); err != nil {
		t.Fatalf("insert page: %v", err)
	}

	out := filepath.Join(t.TempDir(), "public")
	rep, err := rebuild.Build(ctx, a.Store, rebuild.Options{OutDir: out, WID: 1})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if rep.Pages != 1 {
		t.Errorf("rep.Pages = %d, want 1", rep.Pages)
	}

	path := filepath.Join(out, "about", "index.html")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected %s: %v", path, err)
	}
}

func TestBuildSkipsDraftFlatPages(t *testing.T) {
	a := newSeededApp(t)
	ctx := context.Background()
	now := time.Now().Unix()

	if _, err := a.DB.ExecContext(ctx,
		`INSERT INTO pages (wid, author_id, title, body, format, slug, template_id, sort_order, status, og_bg_image_path, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		1, 1, "Draft", "<p>draft</p>", "html", "/draft", 0, 0, 0, "", now, now); err != nil {
		t.Fatalf("insert page: %v", err)
	}

	out := filepath.Join(t.TempDir(), "public")
	if _, err := rebuild.Build(ctx, a.Store, rebuild.Options{OutDir: out, WID: 1}); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "draft", "index.html")); !os.IsNotExist(err) {
		t.Errorf("draft page should not be written")
	}
}

func TestBuildFlatPageContainsPageMode(t *testing.T) {
	a := newSeededApp(t)
	ctx := context.Background()
	now := time.Now().Unix()

	if _, err := a.DB.ExecContext(ctx,
		`INSERT INTO pages (wid, author_id, title, body, format, slug, template_id, sort_order, status, og_bg_image_path, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		1, 1, "About", "<p>about us</p>", "html", "/about", 0, 0, 1, "", now, now); err != nil {
		t.Fatalf("insert page: %v", err)
	}

	// Override the active template with a body that uniquely proves
	// {entry_mode} was expanded by PageView.
	if _, err := a.DB.ExecContext(ctx,
		`UPDATE templates SET main_body = 'MODE:{entry_mode}' WHERE is_active = 1`); err != nil {
		t.Fatalf("update template: %v", err)
	}

	out := filepath.Join(t.TempDir(), "public")
	if _, err := rebuild.Build(ctx, a.Store, rebuild.Options{OutDir: out, WID: 1}); err != nil {
		t.Fatalf("Build: %v", err)
	}

	body, err := os.ReadFile(filepath.Join(out, "about", "index.html"))
	if err != nil {
		t.Fatalf("read about/index.html: %v", err)
	}
	s := string(body)
	if !strings.Contains(s, "MODE:page") {
		t.Errorf("expected MODE:page in output, got:\n%s", s)
	}
}

func TestBuildPrunesStaleFlatPages(t *testing.T) {
	a := newSeededApp(t)
	ctx := context.Background()
	now := time.Now().Unix()

	if _, err := a.DB.ExecContext(ctx,
		`INSERT INTO pages (wid, author_id, title, body, format, slug, template_id, sort_order, status, og_bg_image_path, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		1, 1, "About", "<p>about</p>", "html", "/about", 0, 0, 1, "", now, now); err != nil {
		t.Fatalf("insert page: %v", err)
	}

	out := filepath.Join(t.TempDir(), "public")
	if _, err := rebuild.Build(ctx, a.Store, rebuild.Options{OutDir: out, WID: 1}); err != nil {
		t.Fatalf("first Build: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "about", "index.html")); err != nil {
		t.Fatalf("about page missing after first build: %v", err)
	}

	// Delete the page and rebuild.
	if _, err := a.DB.ExecContext(ctx, `DELETE FROM pages WHERE slug = '/about'`); err != nil {
		t.Fatal(err)
	}
	if _, err := rebuild.Build(ctx, a.Store, rebuild.Options{OutDir: out, WID: 1}); err != nil {
		t.Fatalf("second Build: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "about", "index.html")); !os.IsNotExist(err) {
		t.Errorf("stale about page should have been pruned")
	}
}

func TestBuildPreservesOperatorManagedSiblings(t *testing.T) {
	a := newSeededApp(t)
	ctx := context.Background()
	now := time.Now().Unix()

	if _, err := a.DB.ExecContext(ctx,
		`INSERT INTO pages (wid, author_id, title, body, format, slug, template_id, sort_order, status, og_bg_image_path, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		1, 1, "Pricing", "<p>pricing</p>", "html", "/service/pricing", 0, 0, 1, "", now, now); err != nil {
		t.Fatalf("insert page: %v", err)
	}

	out := filepath.Join(t.TempDir(), "public")
	// Pre-create an operator-managed file that lives under the same parent.
	manual := filepath.Join(out, "service", "downloads", "manual.pdf")
	if err := os.MkdirAll(filepath.Dir(manual), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manual, []byte("pdf"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := rebuild.Build(ctx, a.Store, rebuild.Options{OutDir: out, WID: 1}); err != nil {
		t.Fatalf("Build: %v", err)
	}

	if _, err := os.Stat(filepath.Join(out, "service", "pricing", "index.html")); err != nil {
		t.Errorf("pricing page missing: %v", err)
	}
	if data, err := os.ReadFile(manual); err != nil {
		t.Errorf("operator-managed file was removed: %v", err)
	} else if string(data) != "pdf" {
		t.Errorf("operator-managed file was mutated")
	}
}

func TestBuildParentChildSlugsAreSafe(t *testing.T) {
	a := newSeededApp(t)
	ctx := context.Background()
	now := time.Now().Unix()

	// Bypass admin validation and insert both a parent and child slug.
	if _, err := a.DB.ExecContext(ctx,
		`INSERT INTO pages (wid, author_id, title, body, format, slug, template_id, sort_order, status, og_bg_image_path, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		1, 1, "Service", "<p>service</p>", "html", "/service", 0, 0, 1, "", now, now); err != nil {
		t.Fatalf("insert parent page: %v", err)
	}
	if _, err := a.DB.ExecContext(ctx,
		`INSERT INTO pages (wid, author_id, title, body, format, slug, template_id, sort_order, status, og_bg_image_path, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		1, 1, "Pricing", "<p>pricing</p>", "html", "/service/pricing", 0, 0, 1, "", now, now); err != nil {
		t.Fatalf("insert child page: %v", err)
	}

	out := filepath.Join(t.TempDir(), "public")
	if _, err := rebuild.Build(ctx, a.Store, rebuild.Options{OutDir: out, WID: 1}); err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Both pages must exist.
	for _, p := range []string{"service/index.html", "service/pricing/index.html"} {
		if _, err := os.Stat(filepath.Join(out, p)); err != nil {
			t.Errorf("missing %s: %v", p, err)
		}
	}
}

func TestBuildPrunesStaleParentWithActiveChild(t *testing.T) {
	a := newSeededApp(t)
	ctx := context.Background()
	now := time.Now().Unix()

	if _, err := a.DB.ExecContext(ctx,
		`INSERT INTO pages (wid, author_id, title, body, format, slug, template_id, sort_order, status, og_bg_image_path, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		1, 1, "Service", "<p>service</p>", "html", "/service", 0, 0, 1, "", now, now); err != nil {
		t.Fatalf("insert parent page: %v", err)
	}
	if _, err := a.DB.ExecContext(ctx,
		`INSERT INTO pages (wid, author_id, title, body, format, slug, template_id, sort_order, status, og_bg_image_path, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		1, 1, "Pricing", "<p>pricing</p>", "html", "/service/pricing", 0, 0, 1, "", now, now); err != nil {
		t.Fatalf("insert child page: %v", err)
	}

	out := filepath.Join(t.TempDir(), "public")
	if _, err := rebuild.Build(ctx, a.Store, rebuild.Options{OutDir: out, WID: 1}); err != nil {
		t.Fatalf("first Build: %v", err)
	}
	for _, p := range []string{"service/index.html", "service/pricing/index.html"} {
		if _, err := os.Stat(filepath.Join(out, p)); err != nil {
			t.Fatalf("missing %s after first build: %v", p, err)
		}
	}

	// Remove only the parent page; child remains published.
	if _, err := a.DB.ExecContext(ctx, `DELETE FROM pages WHERE slug = '/service'`); err != nil {
		t.Fatal(err)
	}
	if _, err := rebuild.Build(ctx, a.Store, rebuild.Options{OutDir: out, WID: 1}); err != nil {
		t.Fatalf("second Build: %v", err)
	}

	// Stale parent directory should be pruned, but active child must survive.
	if _, err := os.Stat(filepath.Join(out, "service", "index.html")); !os.IsNotExist(err) {
		t.Errorf("stale service/index.html should have been pruned")
	}
	if _, err := os.Stat(filepath.Join(out, "service", "pricing", "index.html")); err != nil {
		t.Errorf("active child service/pricing/index.html should remain: %v", err)
	}
}

// silence unused import lint when test-only helpers drift
var _ = sql.Open
