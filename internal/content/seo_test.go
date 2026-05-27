package content

import (
	"strings"
	"testing"
	"time"

	"github.com/serendipitynz/serenebach/internal/domain"
)

func TestExcerptPlain(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		body   string
		format string
		want   string
	}{
		{"empty", "", "html", ""},
		{"strips tags", "<p>Hello <b>world</b></p>", "html", "Hello world"},
		{"unescapes entities once", "<p>Tom &amp; Jerry</p>", "html", "Tom & Jerry"},
		// Adjacent tags carry no whitespace, so stripping concatenates —
		// this matches SB3's _remove_tag_fast + linefeed removal exactly.
		{"adjacent tags concatenate", "<p>a</p><p>b</p>", "html", "ab"},
		// When whitespace IS present between blocks, every run collapses
		// to a single space so the one-line description stays readable.
		{"collapses whitespace runs", "<p>a</p>\n\n  <p>b</p>", "html", "a b"},
		{"markdown rendered then stripped", "# Title\n\nbody text", "markdown", "Title body text"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := excerptPlain(tc.body, tc.format); got != tc.want {
				t.Errorf("excerptPlain(%q, %q) = %q, want %q", tc.body, tc.format, got, tc.want)
			}
		})
	}

	t.Run("clips to 200 runes with ellipsis", func(t *testing.T) {
		body := "<p>" + strings.Repeat("あ", 250) + "</p>"
		got := excerptPlain(body, "html")
		if r := []rune(got); len(r) != summaryLength {
			t.Fatalf("clipped length = %d runes, want %d", len(r), summaryLength)
		}
		if !strings.HasSuffix(got, excerptEllipsis) {
			t.Errorf("expected trailing %q, got %q", excerptEllipsis, got)
		}
	})

	t.Run("short text is not clipped", func(t *testing.T) {
		got := excerptPlain("<p>short</p>", "html")
		if got != "short" {
			t.Errorf("got %q, want %q", got, "short")
		}
	})
}

func TestEntryExcerpt(t *testing.T) {
	t.Parallel()

	t.Run("stored summary wins", func(t *testing.T) {
		e := domain.Entry{Summary: "hand-written", Body: "<p>body</p>", Title: "T"}
		if got := entryExcerpt(e); got != "hand-written" {
			t.Errorf("got %q, want %q", got, "hand-written")
		}
	})
	t.Run("falls back to body clip", func(t *testing.T) {
		e := domain.Entry{Body: "<p>derived body</p>", Title: "T", Format: "html"}
		if got := entryExcerpt(e); got != "derived body" {
			t.Errorf("got %q, want %q", got, "derived body")
		}
	})
	t.Run("falls back to title when body empty", func(t *testing.T) {
		e := domain.Entry{Body: "", Title: "Only Title"}
		if got := entryExcerpt(e); got != "Only Title" {
			t.Errorf("got %q, want %q", got, "Only Title")
		}
	})
}

// seoHeadTemplate mirrors the shipped default head: entry-only canonical
// / noindex blocks plus a sequel-gated description, then the entry body.
const seoHeadTemplate = `<!doctype html>
<html>
<head>
<!-- BEGIN entry_canonical -->
<link rel="canonical" href="{entry_canonical_url}">
<!-- END entry_canonical -->
<!-- BEGIN entry_noindex -->
<meta name="robots" content="noindex,follow">
<!-- END entry_noindex -->
<!-- BEGIN sequel -->
<meta name="description" content="{entry_excerpt}">
<!-- END sequel -->
</head>
<!-- BEGIN entry -->
<article><h1>{entry_title}</h1><p class="ex">{entry_excerpt}</p></article>
<!-- END entry -->
</html>
`

func TestEntryViewSEOMetaPresent(t *testing.T) {
	t.Parallel()

	site := NewSite(domain.Weblog{ID: 1, BaseURL: "https://example.com", Lang: "ja"})
	v := EntryView{
		Site:     site,
		Template: &domain.Template{MainBody: seoHeadTemplate},
		Entry: domain.Entry{
			ID: 100, Title: "Main", Body: "<p>body text</p>",
			Summary:      "explicit summary",
			CanonicalURL: "https://example.com/?a=1&b=2",
			NoIndex:      true,
			Status:       domain.EntryPublished,
			PostedAt:     time.Date(2026, 4, 19, 10, 0, 0, 0, time.UTC),
		},
	}
	out, err := v.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	for _, want := range []string{
		`<meta name="description" content="explicit summary">`,
		`<meta name="robots" content="noindex,follow">`,
		`<link rel="canonical" href="https://example.com/?a=1&amp;b=2">`,
		`<p class="ex">explicit summary</p>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q\nfull output:\n%s", want, out)
		}
	}
	// Regression guard: canonical URL must be escaped exactly once, never
	// double-escaped (the c.Tag-vs-pre-escape trap).
	if strings.Contains(out, "&amp;amp;") {
		t.Errorf("canonical URL was double-escaped:\n%s", out)
	}
}

func TestEntryViewSEOMetaAbsentWhenUnset(t *testing.T) {
	t.Parallel()

	site := NewSite(domain.Weblog{ID: 1, BaseURL: "https://example.com", Lang: "ja"})
	v := EntryView{
		Site:     site,
		Template: &domain.Template{MainBody: seoHeadTemplate},
		Entry: domain.Entry{
			ID: 100, Title: "Main", Body: "<p>body text</p>",
			// no Summary / CanonicalURL, NoIndex false
			Status:   domain.EntryPublished,
			PostedAt: time.Date(2026, 4, 19, 10, 0, 0, 0, time.UTC),
		},
	}
	out, err := v.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	if strings.Contains(out, "rel=\"canonical\"") {
		t.Errorf("canonical link emitted with no CanonicalURL:\n%s", out)
	}
	if strings.Contains(out, "noindex,follow") {
		t.Errorf("robots noindex emitted with NoIndex=false:\n%s", out)
	}
	// description falls back to the body clip, so it is non-empty.
	if !strings.Contains(out, `<meta name="description" content="body text">`) {
		t.Errorf("description should fall back to body clip:\n%s", out)
	}
}

// pageSEOTemplate exercises the entry-only SEO blocks on a flat page:
// canonical / noindex live at head level (outside sequel, so they render
// for pages), and {entry_excerpt} sits in the entry block.
const pageSEOTemplate = `<!doctype html>
<html>
<head>
<!-- BEGIN entry_canonical -->
<link rel="canonical" href="{entry_canonical_url}">
<!-- END entry_canonical -->
<!-- BEGIN entry_noindex -->
<meta name="robots" content="noindex,follow">
<!-- END entry_noindex -->
</head>
<!-- BEGIN entry -->
<article><h1>{entry_title}</h1><p class="ex">{entry_excerpt}</p></article>
<!-- END entry -->
</html>
`

func TestPageViewSEOMetaPresent(t *testing.T) {
	t.Parallel()

	site := NewSite(domain.Weblog{ID: 1, BaseURL: "https://example.com", Lang: "ja"})
	v := PageView{
		Site:     site,
		Template: &domain.Template{MainBody: pageSEOTemplate},
		Page: domain.Page{
			ID: 1, Title: "About", Body: "<p>page body</p>", Slug: "/about",
			Summary:      "page summary",
			CanonicalURL: "https://example.com/?a=1&b=2",
			NoIndex:      true,
			Status:       domain.PagePublished,
		},
	}
	out, err := v.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, want := range []string{
		`<p class="ex">page summary</p>`,
		`<meta name="robots" content="noindex,follow">`,
		`<link rel="canonical" href="https://example.com/?a=1&amp;b=2">`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q\nfull output:\n%s", want, out)
		}
	}
	if strings.Contains(out, "&amp;amp;") {
		t.Errorf("canonical URL was double-escaped:\n%s", out)
	}
}

func TestPageViewSEOMetaAbsentWhenUnset(t *testing.T) {
	t.Parallel()

	site := NewSite(domain.Weblog{ID: 1, BaseURL: "https://example.com", Lang: "ja"})
	v := PageView{
		Site:     site,
		Template: &domain.Template{MainBody: pageSEOTemplate},
		Page: domain.Page{
			ID: 1, Title: "About", Body: "<p>page body</p>", Slug: "/about",
			// no Summary / CanonicalURL, NoIndex false
			Status: domain.PagePublished,
		},
	}
	out, err := v.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(out, `rel="canonical"`) {
		t.Errorf("canonical link emitted with no CanonicalURL:\n%s", out)
	}
	if strings.Contains(out, "noindex,follow") {
		t.Errorf("robots noindex emitted with NoIndex=false:\n%s", out)
	}
	// excerpt falls back to the body clip (pages have no SB3 sum).
	if !strings.Contains(out, `<p class="ex">page body</p>`) {
		t.Errorf("excerpt should fall back to body clip:\n%s", out)
	}
}

func TestListViewEmitsExcerptButNotEntryMeta(t *testing.T) {
	t.Parallel()

	tmpl := &domain.Template{MainBody: seoHeadTemplate}
	posted := time.Date(2026, 4, 19, 10, 0, 0, 0, time.UTC)
	v := ListView{
		Site:     NewSite(domain.Weblog{ID: 1, BaseURL: "https://example.com", Lang: "ja"}),
		Template: tmpl,
		Entries: []domain.Entry{
			{ID: 100, Title: "Hello", Body: "<p>list body</p>", Summary: "list excerpt",
				CanonicalURL: "https://other.example/x", NoIndex: true,
				Status: domain.EntryPublished, PostedAt: posted},
		},
		Categories: map[int64]domain.Category{},
		Users:      map[int64]domain.User{},
	}
	out, err := v.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	// {entry_excerpt} is set on list pages too.
	if !strings.Contains(out, `<p class="ex">list excerpt</p>`) {
		t.Errorf("list page should expose {entry_excerpt}:\n%s", out)
	}
	// canonical / noindex blocks are entry-only; on a list page they must
	// be 0-striped (count 0) — no markers, no rendered meta, even when the
	// entry itself carries those fields.
	for _, leak := range []string{`rel="canonical"`, "noindex,follow", "<!-- BEGIN entry_canonical", "<!-- BEGIN entry_noindex"} {
		if strings.Contains(out, leak) {
			t.Errorf("list page leaked entry-only SEO block %q:\n%s", leak, out)
		}
	}
}
