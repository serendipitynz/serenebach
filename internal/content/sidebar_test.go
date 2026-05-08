package content

import (
	"strings"
	"testing"
	"time"

	"github.com/serendipitynz/serenebach/internal/domain"
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
