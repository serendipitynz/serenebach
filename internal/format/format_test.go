package format

import (
	"strings"
	"testing"
)

func TestNormalizeMapsKnownAliases(t *testing.T) {
	cases := map[string]Kind{
		"":         HTML, // legacy default before the column was populated
		"html":     HTML,
		"markdown": Markdown,
		"md":       Markdown,
		"sbtext":   Sbtext,
		"1":        Sbtext, // SB3 autobreak
		"2":        Sbtext, // SB3 autolink
		"bogus":    HTML,   // unknown → HTML, nothing disappears
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRenderHTMLIsPassThrough(t *testing.T) {
	body := `<p>Hello <strong>world</strong></p>`
	out, err := Render(body, "html")
	if err != nil {
		t.Fatal(err)
	}
	if out != body {
		t.Errorf("html passthrough changed the body:\n got: %q\nwant: %q", out, body)
	}
}

func TestRenderHTMLEmptyBodyStaysEmpty(t *testing.T) {
	out, err := Render("", "markdown")
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Errorf("empty body should render empty, got %q", out)
	}
}

func TestRenderMarkdownBasic(t *testing.T) {
	out, err := Render("# hello\n\nsome **bold** text", "markdown")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"<h1>hello</h1>",
		"<strong>bold</strong>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("markdown output missing %q\nfull:\n%s", want, out)
		}
	}
}

func TestRenderMarkdownEscapesEmbeddedHTML(t *testing.T) {
	// Security posture: by default goldmark does NOT pass raw HTML through.
	// A Markdown body that tries to inject <script> should be escaped so a
	// drive-by comment body pasted into a Markdown entry can't execute.
	out, err := Render(`hello <script>alert(1)</script>`, "markdown")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "<script>") {
		t.Errorf("markdown passed raw <script> through:\n%s", out)
	}
}

func TestRenderMarkdownAutolinkBareURL(t *testing.T) {
	out, err := Render("see https://example.com for details", "markdown")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `<a href="https://example.com">`) {
		t.Errorf("expected bare URL to be autolinked; got:\n%s", out)
	}
}

func TestSupportedListExposesMarkdown(t *testing.T) {
	found := false
	for _, s := range Supported {
		if s.Kind == Markdown {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Supported should include Markdown so the admin UI can show it")
	}
}
