package content

import (
	"strings"
	"testing"
	"time"

	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/storage/repo"
	"github.com/serendipitynz/serenebach/internal/template/sbtemplate"
)

// TestArchiveLabelHonoursSiteTZ guards against the regression where
// the synthetic "first of the month" was built in UTC while the
// formatter projected it into s.TZ — for negative-offset zones that
// rolled the rendered label back to the previous month even though
// the URL still said the requested month.
func TestArchiveLabelHonoursSiteTZ(t *testing.T) {
	t.Parallel()

	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("LoadLocation: %v", err)
	}
	site := NewSite(domain.Weblog{Lang: "ja"}).WithTZ(ny)
	if got, want := archiveLabelFor(site, 2026, 1), "2026-01"; got != want {
		t.Errorf("archiveLabelFor = %q, want %q", got, want)
	}
}

func TestApplyCategorySidebarBlockEmitsNestedSubcategoryList(t *testing.T) {
	t.Parallel()

	tmpl, err := sbtemplate.Parse(
		"<!-- BEGIN category -->\n<div class=\"top\">{category_list}</div>\n<div class=\"deep\">{subcategory_list}</div>\n<!-- END category -->\n",
		sbtemplate.NoCallback,
	)
	if err != nil {
		t.Fatal(err)
	}
	c := tmpl.New()
	site := NewSite(domain.Weblog{Lang: "ja", BaseURL: "https://example.com"})
	cats := []SidebarCategory{
		{Category: domain.Category{ID: 1, Name: "news", ParentID: 0}, Count: 3},
		{Category: domain.Category{ID: 2, Name: "updates", ParentID: 1}, Count: 2},
		{Category: domain.Category{ID: 3, Name: "tech", ParentID: 0}, Count: 5},
	}
	applyCategorySidebarBlock(site, c, tmpl, cats)
	out := c.Render()
	if !strings.Contains(out, `<div class="top"><ul><li><a href=`) {
		t.Errorf("category_list should be single-level <ul>, got: %s", out)
	}
	if strings.Contains(out, `<div class="top"><ul><li><a href="https://example.com/category/1/">news</a> (3)<ul>`) {
		t.Errorf("category_list must NOT nest subcategories (that's subcategory_list's job): %s", out)
	}
	if !strings.Contains(out, `<div class="deep"><ul><li><a href="https://example.com/category/1/">news</a> (3)<ul><li><a href="https://example.com/category/2/">updates</a> (2)</li></ul></li>`) {
		t.Errorf("subcategory_list should nest updates under news: %s", out)
	}
}

func TestApplyCategorySidebarBlockSurvivesParentCycle(t *testing.T) {
	t.Parallel()

	tmpl, err := sbtemplate.Parse("<!-- BEGIN category -->{subcategory_list}<!-- END category -->\n", sbtemplate.NoCallback)
	if err != nil {
		t.Fatal(err)
	}
	c := tmpl.New()
	site := NewSite(domain.Weblog{Lang: "ja"})
	// Self-referential parent — the depth cap prevents stack overflow.
	cats := []SidebarCategory{
		{Category: domain.Category{ID: 1, Name: "loop", ParentID: 1}, Count: 1},
	}
	applyCategorySidebarBlock(site, c, tmpl, cats)
	_ = c.Render() // just needs to terminate
}

// TestApplyLatestEntryBlockSB3Shape pins the SB3 _latest output shape:
// `<li><a href="...">Title</a><Date></li>` where <Date> follows the
// DateFormatList pattern (default carries the leading space + parens).
// Authoring SB3 templates depend on this exact concatenation so the
// inline date doesn't drift across renderers.
func TestApplyLatestEntryBlockSB3Shape(t *testing.T) {
	t.Parallel()

	tmpl, err := sbtemplate.Parse(
		"<!-- BEGIN latest_entry -->\n<section>{latest_entry_list}</section>\n<!-- END latest_entry -->\n",
		sbtemplate.NoCallback,
	)
	if err != nil {
		t.Fatal(err)
	}
	c := tmpl.New()
	site := NewSite(domain.Weblog{ID: 1, BaseURL: "https://example.com/", Lang: "ja"}).WithTZ(time.UTC)
	entries := []domain.Entry{
		{ID: 42, Title: "Hello", PostedAt: time.Date(2026, 4, 19, 10, 0, 0, 0, time.UTC), Status: domain.EntryPublished},
	}
	applyLatestEntryBlock(site, c, tmpl, entries)
	out := c.Render()
	// Default DateFormatList is " (%Mon%/%Day%)" — SB3 parity.
	want := `<section><ul><li><a href="https://example.com/entry/42/">Hello</a> (04/19)</li></ul></section>`
	if !strings.Contains(out, want) {
		t.Errorf("latest_entry_list shape mismatch\nwant substring: %s\ngot: %s", want, out)
	}
}

// TestApplyLatestEntryBlockHonoursDateFormatList covers the operator
// override path: a custom DateFormatList pattern must reach the
// {latest_entry_list} output, not just the package default.
func TestApplyLatestEntryBlockHonoursDateFormatList(t *testing.T) {
	t.Parallel()

	tmpl, err := sbtemplate.Parse(
		"<!-- BEGIN latest_entry -->\n{latest_entry_list}\n<!-- END latest_entry -->\n",
		sbtemplate.NoCallback,
	)
	if err != nil {
		t.Fatal(err)
	}
	c := tmpl.New()
	site := NewSite(domain.Weblog{
		ID: 1, BaseURL: "https://example.com/", Lang: "ja",
		DateFormatList: " [%Year%/%Mon%/%Day%]",
	}).WithTZ(time.UTC)
	entries := []domain.Entry{
		{ID: 7, Title: "x", PostedAt: time.Date(2026, 4, 19, 10, 0, 0, 0, time.UTC), Status: domain.EntryPublished},
	}
	applyLatestEntryBlock(site, c, tmpl, entries)
	out := c.Render()
	if !strings.Contains(out, ` [2026/04/19]</li>`) {
		t.Errorf("custom DateFormatList not applied to latest_entry_list, got: %s", out)
	}
}

// TestApplyRecentCommentBlockSB3Shape pins the SB3 _comment shape:
// `<li>EntryTitle<br />=&gt; <a href="...">AuthorDate</a></li>`.
// The literal "=&gt;" matches SB3's fallback when no lang resource
// registers the arrow key (stock ja/en don't).
func TestApplyRecentCommentBlockSB3Shape(t *testing.T) {
	t.Parallel()

	tmpl, err := sbtemplate.Parse(
		"<!-- BEGIN recent_comment -->\n<section>{recent_comment_list}</section>\n<!-- END recent_comment -->\n",
		sbtemplate.NoCallback,
	)
	if err != nil {
		t.Fatal(err)
	}
	c := tmpl.New()
	site := NewSite(domain.Weblog{ID: 1, BaseURL: "https://example.com/", Lang: "ja"}).WithTZ(time.UTC)
	msgs := []repo.RecentApprovedMessage{
		{EntryID: 9, EntryTitle: "Hello", EntrySlug: "hello", AuthorName: "Visitor", PostedAt: time.Date(2026, 4, 19, 10, 0, 0, 0, time.UTC)},
	}
	applyRecentCommentBlock(site, c, tmpl, msgs)
	out := c.Render()
	want := `<section><ul><li>Hello<br />=&gt; <a href="https://example.com/entry/hello/">Visitor (04/19)</a></li></ul></section>`
	if !strings.Contains(out, want) {
		t.Errorf("recent_comment_list shape mismatch\nwant substring: %s\ngot: %s", want, out)
	}
}

// TestApplyRecentCommentBlockAnonymousFallsBack covers the empty
// AuthorName branch — SB3 happily ships the raw empty string in that
// case, but the Go port substitutes a localised "no name" label so
// the link is never blank.
func TestApplyRecentCommentBlockAnonymousFallsBack(t *testing.T) {
	t.Parallel()

	tmpl, err := sbtemplate.Parse(
		"<!-- BEGIN recent_comment -->\n{recent_comment_list}\n<!-- END recent_comment -->\n",
		sbtemplate.NoCallback,
	)
	if err != nil {
		t.Fatal(err)
	}
	c := tmpl.New()
	site := NewSite(domain.Weblog{ID: 1, BaseURL: "https://example.com/", Lang: "ja"}).WithTZ(time.UTC)
	msgs := []repo.RecentApprovedMessage{
		{EntryID: 9, EntryTitle: "Hello", EntrySlug: "hello", AuthorName: "", PostedAt: time.Date(2026, 4, 19, 10, 0, 0, 0, time.UTC)},
	}
	applyRecentCommentBlock(site, c, tmpl, msgs)
	out := c.Render()
	if !strings.Contains(out, `(名前なし) (04/19)</a>`) {
		t.Errorf("anonymous author should resolve to (名前なし), got: %s", out)
	}
}
