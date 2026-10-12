package format

import (
	"strings"
	"testing"
)

func renderMarkdownT(t *testing.T, body string) string {
	t.Helper()
	out, err := Render(body, "markdown")
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestMarkdownInlineHTMLAllowedTags(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"q", `彼は<q>はい</q>と言った`, `<p>彼は<q>はい</q>と言った</p>`},
		{"q with https cite", `<q cite="https://example.com/a?x=1&amp;y=2">x</q>`, `<q cite="https://example.com/a?x=1&amp;y=2">x</q>`},
		{"q with http cite, uppercase, single quotes", `<Q CITE='http://example.com/'>x</Q>`, `<q cite="http://example.com/">x</q>`},
		{"cite surrounding spaces trimmed", `<q cite="  https://example.com/  ">x</q>`, `<q cite="https://example.com/">x</q>`},
		{"unquoted cite", `<q cite=https://example.com/>x</q>`, `<q cite="https://example.com/">x</q>`},
		{"cite element", `<cite>吾輩は猫である</cite>`, `<cite>吾輩は猫である</cite>`},
		{"abbr title re-escaped", `<abbr title="a &quot;b&quot; &lt;c&gt;">ABC</abbr>`, `<abbr title="a &#34;b&#34; &lt;c&gt;">ABC</abbr>`},
		{"dfn title", `<dfn title="定義">用語</dfn>`, `<dfn title="定義">用語</dfn>`},
		{"kbd", `<kbd>Ctrl</kbd>`, `<kbd>Ctrl</kbd>`},
		{"sub and sup", `H<sub>2</sub>O と x<sup>2</sup>`, `H<sub>2</sub>O と x<sup>2</sup>`},
		{"mark", `<mark>重要</mark>`, `<mark>重要</mark>`},
		{"ins and del", `<del datetime="2026-10-13" cite="https://example.com/">旧</del><ins datetime="2026-10-13">新</ins>`,
			`<del datetime="2026-10-13" cite="https://example.com/">旧</del><ins datetime="2026-10-13">新</ins>`},
		{"semicolonless reference before = stays literal", `<q cite="https://example.com/?a=1&copy=2">x</q>`, `<q cite="https://example.com/?a=1&amp;copy=2">x</q>`},
		{"semicolonless reference before alphanumeric stays literal", `<abbr title="&notin &copyx">x</abbr>`, `<abbr title="&amp;notin &amp;copyx">x</abbr>`},
		{"semicolonless reference at a boundary is decoded", `<abbr title="&copy 2026">x</abbr>`, `<abbr title="© 2026">x</abbr>`},
		{"references with semicolons are decoded", `<abbr title="&copy;=&notin;&#169;">x</abbr>`, `<abbr title="©=∉©">x</abbr>`},
		{"tag spanning lines", "<q\ncite=\"https://example.com/\">x</q>", `<q cite="https://example.com/">x</q>`},
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

func TestMarkdownInlineHTMLDropsAttributes(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"javascript cite", `<q cite="javascript:alert(1)">x</q>`, `<q>x</q>`},
		{"cite with entity-encoded newline in scheme", `<q cite="java&#x0A;script:alert(1)">x</q>`, `<q>x</q>`},
		{"relative cite", `<q cite="/entry/1">x</q>`, `<q>x</q>`},
		{"protocol-relative cite", `<q cite="//example.com/">x</q>`, `<q>x</q>`},
		{"data cite", `<q cite="data:text/html,x">x</q>`, `<q>x</q>`},
		{"cite without value", `<q cite>x</q>`, `<q>x</q>`},
		{"event handler, class and style", `<q onclick="alert(1)" class="c" style="color:red">x</q>`, `<q>x</q>`},
		{"attribute not allowed on this tag", `<q title="t">x</q><kbd title="t">k</kbd>`, `<q>x</q><kbd>k</kbd>`},
		{"first duplicate wins even when rejected", `<q cite="javascript:x" cite="https://example.com/">x</q>`, `<q>x</q>`},
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

func TestMarkdownInlineHTMLOmitsOtherTags(t *testing.T) {
	cases := []struct {
		name, in, leaked string
	}{
		{"script", `a <script>alert(1)</script> b`, "<script"},
		{"img with handler", `a <img src="x" onerror="alert(1)"> b`, "<img"},
		{"span", `a <span>x</span> b`, "<span"},
		{"formatting element small", `a <small>x</small> b`, "<small"},
		{"formatting element b", `a <b>x</b> b`, "<b>"},
		{"self-closing allowed tag", `a <q/> b`, "<q"},
		{"self-closing allowed tag with attribute", `a <q cite="https://example.com/"/> b`, "<q"},
		{"comment", `a <!-- note --> b`, "note"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := renderMarkdownT(t, c.in)
			if strings.Contains(out, c.leaked) {
				t.Errorf("%q leaked into output: %q", c.leaked, out)
			}
			if !strings.Contains(out, omittedRawHTML) {
				t.Errorf("expected the omitted-HTML marker, got %q", out)
			}
		})
	}
}

func TestMarkdownInlineHTMLLeavesCodeAndBlocksAlone(t *testing.T) {
	out := renderMarkdownT(t, "`<q>code</q>`")
	if !strings.Contains(out, "<code>&lt;q&gt;code&lt;/q&gt;</code>") {
		t.Errorf("code span should stay escaped: %q", out)
	}

	// A line holding nothing but the tag is a CommonMark HTML block, not
	// inline HTML; the allowlist covers only tags inside running text.
	out = renderMarkdownT(t, "<q>\nquoted\n</q>")
	if strings.Contains(out, "<q>") {
		t.Errorf("HTML block should stay omitted: %q", out)
	}
}
