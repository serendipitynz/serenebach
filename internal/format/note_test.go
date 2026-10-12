package format

import (
	"strings"
	"testing"
)

func TestMarkdownNoteParagraph(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"single line", "// 注釈です", `<p class="note">注釈です</p>`},
		{"consecutive lines share one paragraph", "// 一行目\n// 二行目", "<p class=\"note\">一行目<br />\n二行目</p>"},
		{"inline markdown inside", "// *強調* と [リンク](https://example.com/)", `<p class="note"><em>強調</em> と <a href="https://example.com/">リンク</a></p>`},
		{"interrupts a paragraph", "本文\n// 注釈", "<p>本文</p>\n<p class=\"note\">注釈</p>"},
		{"ends at a non-note line", "// 注釈\n本文", "<p class=\"note\">注釈</p>\n<p>本文</p>"},
		{"indented up to three spaces", "   // 注釈", `<p class="note">注釈</p>`},
		{"tab after the slashes", "//\t注釈", `<p class="note">注釈</p>`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := renderMarkdownT(t, c.in)
			if !strings.Contains(out, c.want) {
				t.Errorf("want %q in output\n got: %q", c.want, out)
			}
		})
	}
}

func TestMarkdownNoteNeedsSpaceAfterSlashes(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"protocol-relative URL", "//example.com/path", "<p>//example.com/path</p>"},
		{"bare slashes", "//", "<p>//</p>"},
		{"slashes mid-line", "本文 // 注釈ではない", "<p>本文 // 注釈ではない</p>"},
		{"four-space indent is code", "    // code", "<pre><code>// code"},
		{"fenced code", "```\n// code\n```", "<pre><code>// code"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := renderMarkdownT(t, c.in)
			if !strings.Contains(out, c.want) {
				t.Errorf("want %q in output\n got: %q", c.want, out)
			}
			if strings.Contains(out, `class="note"`) {
				t.Errorf("unexpected note paragraph: %q", out)
			}
		})
	}
}
