package format

import (
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// noteParagraphs turns lines starting with `// ` into a `<p class="note">`,
// the markup SB3's sbtext emitted for its `//` notes, so the `.note` rule in
// existing SB3 templates styles them unchanged. Consecutive lines share one
// paragraph with a `<br />` after each line, as sbtext did.
//
// The space after `//` is required so a protocol-relative URL such as
// `//example.com` at the start of a line stays text.
type noteParagraphs struct{}

var kindNote = ast.NewNodeKind("Note")

type noteNode struct {
	ast.BaseBlock
}

func (n *noteNode) Kind() ast.NodeKind { return kindNote }

func (n *noteNode) Dump(source []byte, level int) { ast.DumpHelper(n, source, level, nil, nil) }

func (noteParagraphs) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(
		// Ahead of the paragraph parser (1000) so the line is claimed here
		// first; blockquote and list markers never start with `/`, so the
		// exact value among the other block parsers does not matter.
		parser.WithBlockParsers(util.Prioritized(noteParser{}, 750)),
		parser.WithASTTransformers(util.Prioritized(noteLineBreaks{}, 100)),
	)
	m.Renderer().AddOptions(renderer.WithNodeRenderers(
		util.Prioritized(noteRenderer{}, 100),
	))
}

type noteParser struct{}

func (noteParser) Trigger() []byte { return []byte{'/'} }

func (noteParser) Open(parent ast.Node, reader text.Reader, pc parser.Context) (ast.Node, parser.State) {
	seg, ok := noteLine(reader)
	if !ok {
		return nil, parser.NoChildren
	}
	n := &noteNode{}
	n.Lines().Append(seg)
	reader.AdvanceToEOL()
	return n, parser.NoChildren
}

func (noteParser) Continue(node ast.Node, reader text.Reader, pc parser.Context) parser.State {
	seg, ok := noteLine(reader)
	if !ok {
		return parser.Close
	}
	node.Lines().Append(seg)
	reader.AdvanceToEOL()
	return parser.Continue | parser.NoChildren
}

func (noteParser) Close(node ast.Node, reader text.Reader, pc parser.Context) {
	lines := node.Lines()
	if n := lines.Len(); n > 0 {
		last := lines.At(n - 1)
		lines.Set(n-1, last.TrimRightSpace(reader.Source()))
	}
}

// CanInterruptParagraph lets a note follow a paragraph line directly, the
// way a blockquote can; sbtext needed no blank line before `//` either.
func (noteParser) CanInterruptParagraph() bool { return true }

func (noteParser) CanAcceptIndentedLine() bool { return false }

// noteLine reports whether the current line is a note line and returns the
// segment of its content after the `// ` marker.
func noteLine(reader text.Reader) (text.Segment, bool) {
	line, seg := reader.PeekLine()
	w, pos := util.IndentWidth(line, reader.LineOffset())
	if w > 3 || pos+2 >= len(line) || line[pos] != '/' || line[pos+1] != '/' {
		return text.Segment{}, false
	}
	if c := line[pos+2]; c != ' ' && c != '\t' {
		return text.Segment{}, false
	}
	content := seg.WithStart(seg.Start + pos + 3)
	return content.TrimLeftSpace(reader.Source()), true
}

// noteLineBreaks turns each line ending inside a note into a hard break, so
// the note renders a `<br />` per source line like sbtext instead of joining
// the lines as an ordinary paragraph would.
type noteLineBreaks struct{}

func (noteLineBreaks) Transform(doc *ast.Document, reader text.Reader, pc parser.Context) {
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || n.Kind() != kindNote {
			return ast.WalkContinue, nil
		}
		_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
			if t, ok := c.(*ast.Text); ok && entering && t.SoftLineBreak() {
				t.SetSoftLineBreak(false)
				t.SetHardLineBreak(true)
			}
			return ast.WalkContinue, nil
		})
		return ast.WalkSkipChildren, nil
	})
}

type noteRenderer struct{}

func (noteRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(kindNote, func(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			_, _ = w.WriteString(`<p class="note">`)
		} else {
			_, _ = w.WriteString("</p>\n")
		}
		return ast.WalkContinue, nil
	})
}
