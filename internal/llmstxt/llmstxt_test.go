package llmstxt

import (
	"strings"
	"testing"
	"time"

	"github.com/serendipitynz/serenebach/internal/domain"
)

func sampleInput() Input {
	return Input{
		Weblog: domain.Weblog{
			Title:       "Example",
			Description: "A test blog.\nSecond line.",
			BaseURL:     "https://example.com/",
		},
		Entries: []domain.Entry{
			{ID: 1, Title: "Hello, world", Slug: "hello-world", Body: "<p>First post body.</p>", PostedAt: time.Date(2026, 4, 22, 10, 0, 0, 0, time.UTC)},
			{ID: 2, Title: "Second", Body: "Plain text body without HTML.", PostedAt: time.Date(2026, 4, 21, 9, 0, 0, 0, time.UTC)},
		},
	}
}

func TestIndexRendersHeaderAndLinks(t *testing.T) {
	out := Index(sampleInput())
	for _, want := range []string{
		"# Example",
		"> A test blog.",
		"> Second line.",
		"## Recent posts",
		"- [Hello, world](https://example.com/entry/hello-world/): First post body.",
		"- [Second](https://example.com/entry/2/): Plain text body without HTML.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Index missing %q; full output:\n%s", want, out)
		}
	}
}

func TestFullIncludesBodies(t *testing.T) {
	out := Full(sampleInput())
	for _, want := range []string{
		"# Example",
		"## Hello, world",
		"<https://example.com/entry/hello-world/>",
		"First post body.",
		"## Second",
		"<https://example.com/entry/2/>",
		"Plain text body without HTML.",
		"_Posted: 2026-04-22_",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Full missing %q; full output:\n%s", want, out)
		}
	}
}

func TestFullHonoursMoreSeparator(t *testing.T) {
	in := Input{
		Weblog:  domain.Weblog{Title: "x", BaseURL: "https://x.example/"},
		Entries: []domain.Entry{{ID: 1, Title: "t", Body: "main", More: "extra"}},
	}
	out := Full(in)
	if !strings.Contains(out, "main") || !strings.Contains(out, "extra") {
		t.Errorf("Full should include both body and more; got:\n%s", out)
	}
	// The --- separator only appears between body and more.
	if strings.Count(out, "\n---\n") != 1 {
		t.Errorf("expected exactly one --- separator; got:\n%s", out)
	}
}

func TestIndexHandlesEmptyEntries(t *testing.T) {
	in := Input{Weblog: domain.Weblog{Title: "empty"}}
	out := Index(in)
	if !strings.Contains(out, "_No published entries yet._") {
		t.Errorf("empty entries should show the placeholder; got:\n%s", out)
	}
}

func TestIndexSummaryTruncatesLongBodies(t *testing.T) {
	long := strings.Repeat("日本語テキスト", 50) // ~400 runes
	in := Input{
		Weblog:  domain.Weblog{Title: "t", BaseURL: "https://x/"},
		Entries: []domain.Entry{{ID: 1, Title: "long", Body: long}},
	}
	out := Index(in)
	// Summary should truncate with …; line length well under the full body.
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "- [long]") && len([]rune(line)) > 200 {
			t.Errorf("summary not truncated; line has %d runes: %q", len([]rune(line)), line)
		}
	}
}

func TestEscapeInlineNeutralisesSquareBrackets(t *testing.T) {
	in := Input{
		Weblog:  domain.Weblog{Title: "t", BaseURL: "https://x/"},
		Entries: []domain.Entry{{ID: 1, Title: "has ] bracket", Body: "body with `backtick`"}},
	}
	out := Index(in)
	// Title's ] needs to be escaped so the link's [ ... ] doesn't close early.
	if !strings.Contains(out, `has \] bracket`) {
		t.Errorf("expected escaped ]; got:\n%s", out)
	}
	if !strings.Contains(out, "\\`backtick\\`") {
		t.Errorf("expected escaped backticks in summary; got:\n%s", out)
	}
}
