package format

import (
	"strings"
	"testing"
)

func TestRenderSbtextEscapesHTML(t *testing.T) {
	out, err := Render("<script>alert(1)</script>", "sbtext")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "<script>") {
		t.Errorf("sbtext must escape raw HTML: %q", out)
	}
	if !strings.Contains(out, "&lt;script&gt;") {
		t.Errorf("expected escaped script tag: %q", out)
	}
}

func TestRenderSbtextParagraphsSplitOnBlankLine(t *testing.T) {
	out, err := Render("one\ntwo\n\nthree", "sbtext")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "<p>") || strings.Count(out, "<p>") != 2 {
		t.Errorf("expected two paragraphs, got: %q", out)
	}
	if !strings.Contains(out, "one<br>\ntwo") {
		t.Errorf("expected autobreak within paragraph: %q", out)
	}
}

func TestRenderSbtextAutolink(t *testing.T) {
	out, err := Render("visit https://example.com/ok", "sbtext")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `<a href="https://example.com/ok">https://example.com/ok</a>`) {
		t.Errorf("expected autolinked URL: %q", out)
	}
}

func TestRenderSbtextBracketLink(t *testing.T) {
	out, err := Render("see [docs|https://example.com/guide] for more", "sbtext")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `<a href="https://example.com/guide">docs</a>`) {
		t.Errorf("expected bracket link: %q", out)
	}
}

func TestRenderSbtextInlineEmphasis(t *testing.T) {
	out, err := Render("mix ''strong'' and '''italic''' styles", "sbtext")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "<strong>strong</strong>") {
		t.Errorf("expected strong: %q", out)
	}
	if !strings.Contains(out, "<em>italic</em>") {
		t.Errorf("expected em: %q", out)
	}
}

func TestRenderSbtextFormat1OnlyAutobreak(t *testing.T) {
	// format=1 is SB3's "autobreak only" mode — URLs stay as plain text.
	out, err := Render("visit https://example.com please", "1")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, `<a href=`) {
		t.Errorf("format=1 should not autolink: %q", out)
	}
}

func TestRenderSbtextFormat2OnlyAutolink(t *testing.T) {
	// format=2 is SB3's "autolink only" mode — newlines don't become <br>.
	out, err := Render("line one\nline two", "2")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "<br>") {
		t.Errorf("format=2 should not autobreak: %q", out)
	}
}
