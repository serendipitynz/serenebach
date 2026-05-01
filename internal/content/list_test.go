package content

import (
	"strings"
	"testing"
	"time"

	"github.com/serendipitynz/serenebach/internal/domain"
)

func TestListViewRendersEntriesAndSiteVars(t *testing.T) {
	t.Parallel()

	tmpl := &domain.Template{
		Name: "t",
		MainBody: `<!doctype html>
<html lang="{site_lang}">
<head><title>{site_title}</title></head>
<body>
<!-- BEGIN title -->
<h1>{blog_name}</h1>
<!-- END title -->
<!-- BEGIN entry -->
<article><h2><a href="{entry_permalink}">{entry_title}</a></h2>
<div>{entry_description}</div>
<p>by {user_disp_name} ({user_name}) in {category_name} on {entry_date}</p>
</article>
<!-- END entry -->
</body>
</html>
`,
	}
	posted := time.Date(2026, 4, 19, 10, 0, 0, 0, time.UTC)
	v := ListView{
		Site: NewSite(domain.Weblog{
			ID: 1, Title: "Example", Description: "", BaseURL: "https://example.com", Lang: "ja",
		}),
		Template: tmpl,
		Entries: []domain.Entry{
			{ID: 100, AuthorID: 1, CategoryID: 10, Title: "Hello", Body: "<p>one</p>", PostedAt: posted, Status: domain.EntryPublished},
			{ID: 101, AuthorID: 1, CategoryID: 10, Title: "World", Body: "<p>two</p>", PostedAt: posted, Status: domain.EntryPublished},
		},
		Categories: map[int64]domain.Category{10: {ID: 10, Name: "news"}},
		Users:      map[int64]domain.User{1: {ID: 1, Name: "admin", DisplayName: "Admin"}},
	}
	out, err := v.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, want := range []string{
		`lang="ja"`,
		`<title>Example</title>`,
		// {blog_name} is the SB3 anchor form — {blog_name_only} is
		// available for templates that want plain text.
		`<h1><a href="https://example.com/">Example</a></h1>`,
		`<a href="https://example.com/entry/100/">Hello</a>`,
		`<a href="https://example.com/entry/101/">World</a>`,
		`<p>one</p>`,
		`<p>two</p>`,
		// Default list-date pattern is "%Mon%/%Day%" (SB3 convention)
		// when no weblog-level override is configured.
		// SB3 semantics: {user_disp_name} = display, {user_name} = login.
		// SB3 semantics: {category_name} is a link to the category page.
		`by Admin (admin) in <a href="https://example.com/category/10/">news</a> on 04/19`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q\nfull output:\n%s", want, out)
			return
		}
	}
}

func TestListViewHidesEntriesWhenEmpty(t *testing.T) {
	t.Parallel()

	tmpl := &domain.Template{MainBody: "<!-- BEGIN entry -->E<!-- END entry -->\n"}
	v := ListView{Template: tmpl, Site: NewSite(domain.Weblog{Lang: "ja"})}
	out, err := v.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(out, "E") {
		t.Errorf("entry block leaked when entries empty: %q", out)
	}
}

func TestListViewErrorsWhenNoTemplate(t *testing.T) {
	t.Parallel()
	v := ListView{}
	if _, err := v.Render(); err == nil {
		t.Fatal("expected error with nil template")
	}
}
