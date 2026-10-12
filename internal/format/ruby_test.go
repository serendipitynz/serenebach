package format

import (
	"strings"
	"testing"
)

func ruby(base, reading string) string {
	return "<ruby>" + base + "<rp>（</rp><rt>" + reading + "</rt><rp>）</rp></ruby>"
}

func TestMarkdownRubyConverts(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"fullwidth bar", "｜漢字《かんじ》", "<p>" + ruby("漢字", "かんじ") + "</p>"},
		{"halfwidth bar", "|漢字《かんじ》", "<p>" + ruby("漢字", "かんじ") + "</p>"},
		{"bar lets the base be any text", "｜ルビ付き《るびつき》です", ruby("ルビ付き", "るびつき") + "です"},
		{"bar base stops at the bar", "長い｜文章《ぶんしょう》", "長い" + ruby("文章", "ぶんしょう")},
		{"no bar takes only the kanji run", "長い文章《ぶんしょう》", "長い" + ruby("文章", "ぶんしょう")},
		{"no bar after katakana", "カタカナ漢字《かんじ》", "カタカナ" + ruby("漢字", "かんじ")},
		{"kanji class includes 々 and ヶ", "佐々木《ささき》と三ヶ月《さんかげつ》", ruby("佐々木", "ささき") + "と" + ruby("三ヶ月", "さんかげつ")},
		{"emphasis dots per character", "《《傍点》》", ruby("傍", "・") + ruby("点", "・")},
		{"emphasis splits decoded characters", "《《A&amp;B》》", ruby("A", "・") + ruby("&amp;", "・") + ruby("B", "・")},
		{"several on one line", "｜一《いち》と二《に》", ruby("一", "いち") + "と" + ruby("二", "に")},
		{"line break after ruby is kept", "漢字《かんじ》\n次の行", ruby("漢字", "かんじ") + "\n次の行"},
		{"inside emphasis and link text", "*漢字《かんじ》* [語《ご》](https://example.com/)", "<em>" + ruby("漢字", "かんじ") + `</em> <a href="https://example.com/">` + ruby("語", "ご") + "</a>"},
		{"fullwidth bar inside a table", "| a |\n|---|\n| ｜漢字《かんじ》 |", "<td>" + ruby("漢字", "かんじ") + "</td>"},
		{"reading with a bracket pair after a bar", "｜漢字《かん《じ》》", ruby("漢字", "かん《じ》")},
		{"reading with a bracket pair without a bar", "漢字《かん《じ》》", ruby("漢字", "かん《じ》")},
		{"bar before double brackets is ruby, not emphasis", "｜傍点《《・》》", ruby("傍点", "《・》")},
		{"kanji before double brackets is emphasis", "漢字《《傍点》》", "漢字" + ruby("傍", "・") + ruby("点", "・")},
		{"emphasis body may start with a bracket", "《《《x》》", ruby("《", "・") + ruby("x", "・")},
		{"emphasis retries after a failed start", "《《a》b《《c》》", "《《a》b" + ruby("c", "・")},
		{"reading is escaped", "｜a《1 < 2》", ruby("a", "1 &lt; 2")},
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

func TestMarkdownRubyLeavesText(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"no kanji before 《", "ひらがな《るび》", "<p>ひらがな《るび》</p>"},
		{"bar with empty base", "｜《るび》", "<p>｜《るび》</p>"},
		{"empty reading", "漢字《》", "<p>漢字《》</p>"},
		{"unclosed reading", "漢字《かんじ", "<p>漢字《かんじ</p>"},
		{"reading across lines", "漢字《かん\nじ》", "<p>漢字《かん\nじ》</p>"},
		{"empty emphasis", "《《》》", "<p>《《》》</p>"},
		{"halfwidth bar before a space is text", "| 漢字《かんじ》", "<p>| " + ruby("漢字", "かんじ") + "</p>"},
		{"halfwidth bar before an ideographic space is text", "|　かな《よみ》", "<p>|　かな《よみ》</p>"},
		{"halfwidth bar before a no-break space is text", "|\u00a0かな《よみ》", "<p>|\u00a0かな《よみ》</p>"},
		{"ruby inside double brackets leaves no emphasis", "《《漢字《かんじ》》》", "<p>《《" + ruby("漢字", "かんじ") + "》》</p>"},
		{"escaped halfwidth bar is text", `ab\|漢字《かんじ》`, "<p>ab|" + ruby("漢字", "かんじ") + "</p>"},
		{"code span", "`漢字《かんじ》`", "<code>漢字《かんじ》</code>"},
		{"code block", "    漢字《かんじ》", "<pre><code>漢字《かんじ》"},
		{"image alt", "![漢字《かんじ》](https://example.com/a.png)", `alt="漢字《かんじ》"`},
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
