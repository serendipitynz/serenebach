package content

import (
	"strings"
	"testing"
	"time"

	"github.com/serendipitynz/serenebach/internal/domain"
)

func TestEntryViewRendersWithSequelBlock(t *testing.T) {
	t.Parallel()

	tmpl := &domain.Template{
		MainBody: `<!doctype html>
<html>
<!-- BEGIN entry -->
<article>
<!-- BEGIN sequel -->
<nav>{prev_entry} | {next_entry}</nav>
<!-- END sequel -->
<h1>{entry_title}</h1>
<div>{entry_description}</div>
<div class="sequel">{entry_sequel}</div>
</article>
<!-- END entry -->
</html>
`,
	}
	posted := time.Date(2026, 4, 19, 10, 0, 0, 0, time.UTC)
	site := NewSite(domain.Weblog{ID: 1, BaseURL: "https://example.com", Lang: "ja"})
	v := EntryView{
		Site:     site,
		Template: tmpl,
		Entry: domain.Entry{
			ID: 100, Title: "Main", Body: "<p>body</p>", More: "<p>追記</p>",
			Status: domain.EntryPublished, PostedAt: posted,
		},
		Prev: &domain.Entry{ID: 99, Title: "Older", PostedAt: posted.Add(-24 * time.Hour)},
		Next: &domain.Entry{ID: 101, Title: "Newer", PostedAt: posted.Add(24 * time.Hour)},
	}
	out, err := v.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, want := range []string{
		`<h1>Main</h1>`,
		`<p>body</p>`,
		`<div class="sequel"><p>追記</p></div>`,
		`<a href="https://example.com/entry/99/">« Older</a>`,
		`<a href="https://example.com/entry/101/">Newer »</a>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q\nfull output:\n%s", want, out)
			return
		}
	}
}

func TestEntryViewOmitsNavAtEdges(t *testing.T) {
	t.Parallel()

	tmpl := &domain.Template{
		MainBody: `<!-- BEGIN entry -->
<!-- BEGIN sequel -->
NAV:{prev_entry}|{next_entry}
<!-- END sequel -->
<!-- END entry -->
`,
	}
	v := EntryView{
		Site:     NewSite(domain.Weblog{Lang: "ja"}),
		Template: tmpl,
		Entry:    domain.Entry{ID: 1, Status: domain.EntryPublished, PostedAt: time.Unix(0, 0)},
		// Prev/Next left nil
	}
	out, err := v.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(out, "NAV:|") {
		t.Errorf("expected empty nav tags at edges; got %q", out)
	}
}

func TestEntryViewEscapesNavTitles(t *testing.T) {
	t.Parallel()

	tmpl := &domain.Template{
		MainBody: `<!-- BEGIN entry -->
<!-- BEGIN sequel -->
{prev_entry}
<!-- END sequel -->
<!-- END entry -->
`,
	}
	v := EntryView{
		Site:     NewSite(domain.Weblog{Lang: "ja"}),
		Template: tmpl,
		Entry:    domain.Entry{ID: 1, Status: domain.EntryPublished, PostedAt: time.Unix(0, 0)},
		Prev:     &domain.Entry{ID: 2, Title: `<script>alert("x")</script>`, PostedAt: time.Unix(0, 0)},
	}
	out, err := v.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(out, "<script>") {
		t.Errorf("prev title was not escaped: %q", out)
	}
	if !strings.Contains(out, `&lt;script&gt;`) {
		t.Errorf("expected escaped <script> in output: %q", out)
	}
}
