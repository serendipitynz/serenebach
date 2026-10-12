package format

import (
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// rubyNotation implements the ruby notation of the japanese-novel-ruby
// Obsidian plugin (the one novel sites such as Aozora Bunko and Kakuyomu
// share), matching the plugin's own patterns:
//
//	｜親文字《るび》  |親文字《るび》  漢字《かんじ》  《《傍点》》
//
// It runs as an AST transformer over finished Text nodes rather than as an
// inline parser because goldmark only offers inline parsers ASCII
// punctuation and whitespace as triggers; `｜` and `《` never reach one.
// Working on Text nodes also keeps code spans, raw HTML and link
// destinations out of reach, as the plugin's DOM text-node walk does.
type rubyNotation struct{}

var kindRuby = ast.NewNodeKind("Ruby")

type rubyNode struct {
	ast.BaseInline
	base text.Segment
	ruby text.Segment
	// emphasis marks a 《《傍点》》 run: every character of base gets its
	// own ruby of rubyEmphasisDot, and ruby is unused.
	emphasis bool
}

func (n *rubyNode) Kind() ast.NodeKind { return kindRuby }

func (n *rubyNode) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{
		"Base": string(n.base.Value(source)),
		"Ruby": string(n.ruby.Value(source)),
	}, nil)
}

const rubyEmphasisDot = "・"

func (rubyNotation) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(parser.WithASTTransformers(util.Prioritized(rubyTransformer{}, 100)))
	m.Renderer().AddOptions(renderer.WithNodeRenderers(util.Prioritized(rubyRenderer{}, 100)))
}

type rubyTransformer struct{}

func (rubyTransformer) Transform(doc *ast.Document, reader text.Reader, pc parser.Context) {
	source := reader.Source()
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n.Kind() {
		case ast.KindCodeSpan, ast.KindRawHTML, ast.KindAutoLink, kindRuby,
			// An image's children become its alt attribute, which is
			// rendered as plain text and cannot hold <ruby>.
			ast.KindImage:
			return ast.WalkSkipChildren, nil
		}
		for c := n.FirstChild(); c != nil; {
			c = rewriteTextRun(n, c, source)
		}
		return ast.WalkContinue, nil
	})
}

// rewriteTextRun takes the run of adjacent Text nodes that starts at first
// and ends at the first line break, replaces it with text and ruby nodes
// when it holds any ruby notation, and returns the sibling after the run.
// A run is bounded by line breaks because the plugin's notation never
// spans lines.
func rewriteTextRun(parent, first ast.Node, source []byte) ast.Node {
	t, ok := first.(*ast.Text)
	if !ok || t.IsRaw() {
		return first.NextSibling()
	}
	run := []*ast.Text{t}
	for !run[len(run)-1].SoftLineBreak() && !run[len(run)-1].HardLineBreak() {
		next, ok := run[len(run)-1].NextSibling().(*ast.Text)
		// Siblings that are not adjacent in the source cannot form one
		// notation, and the run is read back as one source slice below.
		if !ok || next.IsRaw() || next.Segment.Start != run[len(run)-1].Segment.Stop {
			break
		}
		run = append(run, next)
	}
	last := run[len(run)-1]
	after := last.NextSibling()

	start, stop := run[0].Segment.Start, last.Segment.Stop
	matches := findRubies(source[start:stop])
	if len(matches) == 0 {
		return after
	}

	pos := start
	insert := func(node ast.Node) {
		parent.InsertBefore(parent, run[0], node)
	}
	for _, m := range matches {
		if start+m.start > pos {
			insert(ast.NewTextSegment(text.NewSegment(pos, start+m.start)))
		}
		insert(&rubyNode{
			base:     text.NewSegment(start+m.baseStart, start+m.baseStop),
			ruby:     text.NewSegment(start+m.rubyStart, start+m.rubyStop),
			emphasis: m.emphasis,
		})
		pos = start + m.stop
	}
	// The trailing text node — empty when the run ends in ruby — carries
	// the run's line break, which only a Text node can render.
	rest := ast.NewTextSegment(text.NewSegment(pos, stop))
	rest.SetSoftLineBreak(last.SoftLineBreak())
	rest.SetHardLineBreak(last.HardLineBreak())
	if pos < stop || last.SoftLineBreak() || last.HardLineBreak() {
		insert(rest)
	}
	for _, old := range run {
		parent.RemoveChild(parent, old)
	}
	return after
}

// rubyMatch holds byte offsets into the run passed to findRubies.
type rubyMatch struct {
	start, stop         int
	baseStart, baseStop int
	rubyStart, rubyStop int
	emphasis            bool
}

const (
	rubyOpen     = "《"
	rubyClose    = "》"
	rubyOpenLen  = len(rubyOpen)
	rubyCloseLen = len(rubyClose)
)

// findRubies scans one line of text left to right with the plugin's
// leftmost-match semantics:
//
//   - `《《x》》` is emphasis on x (x without `》`).
//   - `｜base《ruby》`, and `|base《ruby》` where `|` is unescaped and not
//     followed by whitespace, take everything back to the bar as base (base
//     without bars or `《`).
//   - otherwise `《ruby》` takes the kanji run directly before it as base.
//
// ruby is non-empty and holds neither `《` nor `》`.
func findRubies(s []byte) []rubyMatch {
	var out []rubyMatch
	lastEnd := 0
	// opener is the byte offset of the bar that would start an explicit
	// base, or -1. Any bar resets it, since a base cannot contain one.
	opener, openerLen := -1, 0
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRune(s[i:])
		switch r {
		case '｜':
			opener, openerLen = i, size
		case '|':
			if isEscaped(s, i) || i+1 >= len(s) || isASCIISpace(s[i+1]) {
				opener = -1
			} else {
				opener, openerLen = i, size
			}
		case '《':
			if m, ok := matchEmphasis(s, i); ok {
				out = append(out, m)
				i, lastEnd, opener = m.stop, m.stop, -1
				continue
			}
			if m, ok := matchRuby(s, i, lastEnd, opener, openerLen); ok {
				out = append(out, m)
				i, lastEnd, opener = m.stop, m.stop, -1
				continue
			}
			opener = -1
		}
		i += size
	}
	return out
}

func matchEmphasis(s []byte, open int) (rubyMatch, bool) {
	bodyStart := open + 2*rubyOpenLen
	if !hasPrefixAt(s, open+rubyOpenLen, rubyOpen) {
		return rubyMatch{}, false
	}
	end := indexFrom(s, bodyStart, rubyClose)
	if end <= bodyStart || !hasPrefixAt(s, end+rubyCloseLen, rubyClose) {
		return rubyMatch{}, false
	}
	return rubyMatch{
		start: open, stop: end + 2*rubyCloseLen,
		baseStart: bodyStart, baseStop: end,
		emphasis: true,
	}, true
}

func matchRuby(s []byte, open, lastEnd, opener, openerLen int) (rubyMatch, bool) {
	rubyStart := open + rubyOpenLen
	end := indexFrom(s, rubyStart, rubyClose)
	if end <= rubyStart || indexFrom(s[:end], rubyStart, rubyOpen) >= 0 {
		return rubyMatch{}, false
	}
	m := rubyMatch{baseStop: open, rubyStart: rubyStart, rubyStop: end, stop: end + rubyCloseLen}
	if opener >= 0 {
		// A bar directly before `《` leaves an empty base, which matches
		// neither form in the plugin, so the whole notation stays text.
		if opener+openerLen == open {
			return rubyMatch{}, false
		}
		m.start, m.baseStart = opener, opener+openerLen
		return m, true
	}
	baseStart := open
	for baseStart > lastEnd {
		r, size := utf8.DecodeLastRune(s[lastEnd:baseStart])
		if !isRubyKanji(r) {
			break
		}
		baseStart -= size
	}
	if baseStart == open {
		return rubyMatch{}, false
	}
	m.start, m.baseStart = baseStart, baseStart
	return m, true
}

// isRubyKanji is the plugin's `[一-龠々仝〆〇ヶ]`: the characters a base may
// consist of when no bar marks where it starts.
func isRubyKanji(r rune) bool {
	return ('一' <= r && r <= '龠') || r == '々' || r == '仝' || r == '〆' || r == '〇' || r == 'ヶ'
}

// isEscaped reports whether the byte at i is preceded by an odd number of
// backslashes, i.e. written as `\|` to keep it a literal bar.
func isEscaped(s []byte, i int) bool {
	n := 0
	for j := i - 1; j >= 0 && s[j] == '\\'; j-- {
		n++
	}
	return n%2 == 1
}

func isASCIISpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v'
}

func hasPrefixAt(s []byte, i int, prefix string) bool {
	return i+len(prefix) <= len(s) && string(s[i:i+len(prefix)]) == prefix
}

func indexFrom(s []byte, from int, sub string) int {
	for i := from; i+len(sub) <= len(s); i++ {
		if string(s[i:i+len(sub)]) == sub {
			return i
		}
	}
	return -1
}

type rubyRenderer struct{}

func (rubyRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(kindRuby, renderRuby)
}

// renderRuby wraps the reading in <rp> parentheses so a reader without
// ruby support — many feed readers — shows 漢字（かんじ） instead of running
// base and reading together.
func renderRuby(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkSkipChildren, nil
	}
	n := node.(*rubyNode)
	base := rubyPlainText(n.base.Value(source))
	if !n.emphasis {
		writeRuby(w, base, rubyPlainText(n.ruby.Value(source)))
		return ast.WalkSkipChildren, nil
	}
	for _, r := range string(base) {
		writeRuby(w, []byte(string(r)), []byte(rubyEmphasisDot))
	}
	return ast.WalkSkipChildren, nil
}

func writeRuby(w util.BufWriter, base, ruby []byte) {
	_, _ = w.WriteString("<ruby>")
	_, _ = w.Write(util.EscapeHTML(base))
	_, _ = w.WriteString("<rp>（</rp><rt>")
	_, _ = w.Write(util.EscapeHTML(ruby))
	_, _ = w.WriteString("</rt><rp>）</rp></ruby>")
}

// rubyPlainText resolves backslash escapes and character references the
// way a Text node's renderer would, so 《《…》》 splits what the reader sees
// into characters rather than the source spelling of `&amp;`.
func rubyPlainText(b []byte) []byte {
	return util.ResolveEntityNames(util.ResolveNumericReferences(util.UnescapePunctuations(b)))
}
