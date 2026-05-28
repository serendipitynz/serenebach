package content

import (
	"strings"
	"testing"
	"time"

	"github.com/serendipitynz/serenebach/internal/domain"
)

func searchTestTemplate() *domain.Template {
	return &domain.Template{
		Name: "t",
		MainBody: `<!doctype html>
<html lang="{site_lang}">
<head><title>{site_title}</title></head>
<body>
<form action="{search_url}"><input name="q" value="{search_query}"></form>
<!-- BEGIN search_form -->
<form action="{search_url}" method="get" role="search"><input name="q" value="{search_query}"></form>
<!-- END search_form -->
<!-- BEGIN search_results -->
<p>Total: {search_total}</p>
<!-- END search_results -->
<!-- BEGIN search_empty -->
<p>No hits for "{search_query}".</p>
<!-- END search_empty -->
<!-- BEGIN entry -->
<article><a href="{entry_permalink}">{entry_title}</a></article>
<!-- END entry -->
</body>
</html>
`,
	}
}

func TestSearchView_RendersResults(t *testing.T) {
	t.Parallel()
	posted := time.Date(2026, 4, 19, 10, 0, 0, 0, time.UTC)
	site := NewSite(domain.Weblog{
		ID: 1, Title: "Example", BaseURL: "https://example.com", Lang: "ja",
	}).WithSearchForm(true)

	v := SearchView{
		Site:     site,
		Template: searchTestTemplate(),
		Query:    "hello",
		Results: []domain.Entry{
			{ID: 100, Title: "hello world", PostedAt: posted, Status: domain.EntryPublished},
		},
		TotalCount: 1,
		HasQuery:   true,
	}
	out, err := v.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, want := range []string{
		`action="https://example.com/search"`,
		`value="hello"`,
		`Total: 1`,
		`<a href="https://example.com/entry/100/">hello world</a>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output\n%s", want, out)
		}
	}
	if strings.Contains(out, "No hits for") {
		t.Errorf("search_empty should be stripped when results > 0\n%s", out)
	}
}

func TestSearchView_RendersEmptyState(t *testing.T) {
	t.Parallel()
	site := NewSite(domain.Weblog{
		ID: 1, Title: "Example", BaseURL: "https://example.com", Lang: "ja",
	}).WithSearchForm(true)

	v := SearchView{
		Site:       site,
		Template:   searchTestTemplate(),
		Query:      "nothing",
		Results:    nil,
		TotalCount: 0,
		HasQuery:   true,
	}
	out, err := v.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(out, `No hits for "nothing"`) {
		t.Errorf("search_empty should be present when 0 results\n%s", out)
	}
	if strings.Contains(out, "Total: 0") {
		t.Errorf("search_results should be stripped when results == 0\n%s", out)
	}
}

func TestSearchView_GuidanceModeNoQuery(t *testing.T) {
	t.Parallel()
	site := NewSite(domain.Weblog{
		ID: 1, Title: "Example", BaseURL: "https://example.com", Lang: "ja",
	}).WithSearchForm(true)

	v := SearchView{
		Site:       site,
		Template:   searchTestTemplate(),
		Query:      "",
		TotalCount: 0,
		HasQuery:   false,
	}
	out, err := v.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	// Neither block should render when HasQuery is false: the user
	// hasn't entered a query yet, so we show the bare form only.
	for _, unwanted := range []string{"Total:", "No hits for"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("guidance mode should not emit %q\n%s", unwanted, out)
		}
	}
	// The search_form block stays visible (count=1) when the toggle is on.
	if !strings.Contains(out, `method="get"`) {
		t.Errorf("search_form block should render when SearchFormEnabled\n%s", out)
	}
}

func TestSearchView_SearchFormDisabledStripsBlock(t *testing.T) {
	t.Parallel()
	site := NewSite(domain.Weblog{
		ID: 1, Title: "Example", BaseURL: "https://example.com", Lang: "ja",
	}).WithSearchForm(false)

	v := SearchView{
		Site:     site,
		Template: searchTestTemplate(),
		Query:    "hello",
		Results: []domain.Entry{
			{ID: 100, Title: "hello world", PostedAt: time.Now(), Status: domain.EntryPublished},
		},
		TotalCount: 1,
		HasQuery:   true,
	}
	out, err := v.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	// The guarded search_form block must 0-strip; the bare {search_url}
	// outside any block expands to "" so the unguarded form has no action.
	if strings.Contains(out, `method="get"`) {
		t.Errorf("search_form block should 0-strip when SearchFormEnabled=false\n%s", out)
	}
	if strings.Contains(out, `action="https://example.com/search"`) {
		t.Errorf("{search_url} should be empty when SearchFormEnabled=false\n%s", out)
	}
}

func TestTruncateSearchQuery(t *testing.T) {
	if got := TruncateSearchQuery("hello"); got != "hello" {
		t.Errorf("TruncateSearchQuery short input changed: %q", got)
	}
	long := strings.Repeat("a", SearchQueryMaxLen+10)
	got := TruncateSearchQuery(long)
	if len([]rune(got)) > SearchQueryMaxLen {
		t.Errorf("TruncateSearchQuery did not cap; got len %d", len([]rune(got)))
	}
	// Multibyte input: 250 Japanese runes, each 3 bytes, exceeds 200-rune
	// cap and should be truncated at a rune boundary.
	jp := strings.Repeat("あ", 250)
	got = TruncateSearchQuery(jp)
	if rc := len([]rune(got)); rc > SearchQueryMaxLen {
		t.Errorf("multibyte truncate: rune count %d > %d", rc, SearchQueryMaxLen)
	}
}
