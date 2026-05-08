package sbtemplate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// helper: parse with no callback (keeps assertions free of injected tags).
func mustParse(t *testing.T, src string) *Template {
	t.Helper()
	tmpl, err := Parse(src, NoCallback)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return tmpl
}

func TestPlainText(t *testing.T) {
	got := mustParse(t, "hello world\n").New().Render()
	if got != "hello world\n" {
		t.Errorf("got %q", got)
	}
}

func TestSimpleTagSubstitution(t *testing.T) {
	c := mustParse(t, "hello {who}\n").New()
	c.Tag("who", "world")
	if got := c.Render(); got != "hello world\n" {
		t.Errorf("got %q", got)
	}
}

func TestTagAutoEscapesPlainText(t *testing.T) {
	// Tag is the safe-by-default setter — anything user-controlled must
	// land in the rendered HTML as escaped text, never as live markup.
	c := mustParse(t, "{x}\n").New()
	c.Tag("x", `<script>alert('xss')</script>`)
	got := c.Render()
	if strings.Contains(got, "<script>") {
		t.Errorf("Tag must escape HTML, raw <script> leaked: %q", got)
	}
	if !strings.Contains(got, "&lt;script&gt;") {
		t.Errorf("Tag should produce &lt;script&gt;, got %q", got)
	}
}

func TestTagHTMLPassesThroughFragment(t *testing.T) {
	// TagHTML is the explicit raw-fragment setter for SB3-style tags
	// like {entry_time} that ship pre-built anchor markup.
	c := mustParse(t, "{x}\n").New()
	c.TagHTML("x", `<a href="https://example.com/">link</a>`)
	got := c.Render()
	if !strings.Contains(got, `<a href="https://example.com/">link</a>`) {
		t.Errorf("TagHTML must pass markup through unchanged, got %q", got)
	}
}

func TestUnsetTagBecomesEmpty(t *testing.T) {
	got := mustParse(t, "[{missing}]\n").New().Render()
	if got != "[]\n" {
		t.Errorf("got %q", got)
	}
}

func TestRepeatedTagInOneIteration(t *testing.T) {
	c := mustParse(t, "{x} {x} {x}\n").New()
	c.Tag("x", "hi")
	if got := c.Render(); got != "hi hi hi\n" {
		t.Errorf("got %q", got)
	}
}

func TestBlockHiddenWhenCountZero(t *testing.T) {
	src := "before\n<!-- BEGIN item -->\nX\n<!-- END item -->\nafter\n"
	got := mustParse(t, src).New().Render()
	if !strings.Contains(got, "before") || !strings.Contains(got, "after") {
		t.Errorf("bookends missing in %q", got)
	}
	if strings.Contains(got, "X") {
		t.Errorf("block body leaked: %q", got)
	}
}

func TestBlockRepeatsWithoutPerIterationValues(t *testing.T) {
	src := "<!-- BEGIN item -->\n- item\n<!-- END item -->\n"
	c := mustParse(t, src).New()
	c.Block("item", 3)
	got := c.Render()
	if count := strings.Count(got, "- item"); count != 3 {
		t.Errorf("repeat count = %d, want 3. got=%q", count, got)
	}
}

func TestPerIterationValuesAndFallback(t *testing.T) {
	src := "<!-- BEGIN row -->\n{label}: {n}\n<!-- END row -->\n"
	c := mustParse(t, src).New()
	c.Num(0)
	c.Tag("label", "default")
	c.Tag("n", "first")
	c.Num(1)
	c.Tag("n", "second")
	// index 2 has no per-iteration value for either → both fall back to [0]
	c.Num(2)
	c.Block("row", 3)

	got := c.Render()
	want := "default: first\ndefault: second\ndefault: first\n"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestNestedBlocks(t *testing.T) {
	src := "<!-- BEGIN outer -->\nO\n<!-- BEGIN inner -->\nI\n<!-- END inner -->\n<!-- END outer -->\n"
	c := mustParse(t, src).New()
	c.Block("outer", 2)
	c.Block("inner", 3)
	got := c.Render()
	// outer repeats 2× each time emitting its own "O\n" plus inner's 3× "I\n".
	want := "O\nI\nI\nI\nO\nI\nI\nI\n"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestDuplicateBlockNameAutoSuffix(t *testing.T) {
	src := `<!-- BEGIN menu -->
A
<!-- END menu -->
<!-- BEGIN menu -->
B
<!-- END menu -->
`
	tmpl := mustParse(t, src)
	if !tmpl.HasBlock("menu") {
		t.Fatal("menu missing")
	}
	if !tmpl.HasBlock("menu.") {
		t.Fatal("menu. (second occurrence) missing")
	}
}

func TestDividedBlocksShareBaseCount(t *testing.T) {
	src := `<!-- BEGIN row -->
A
<!-- END row -->
<!-- BEGIN row -->
B
<!-- END row -->
`
	c := mustParse(t, src).New()
	c.Block("row", 2)
	got := c.Render()
	// Both occurrences should render twice each → 2×A + 2×B.
	if a, b := strings.Count(got, "A\n"), strings.Count(got, "B\n"); a != 2 || b != 2 {
		t.Errorf("want A×2, B×2, got A×%d B×%d in %q", a, b, got)
	}
}

func TestDeleteBlockBlanksOutput(t *testing.T) {
	src := "<!-- BEGIN item -->\nX\n<!-- END item -->\n"
	c := mustParse(t, src).New()
	c.Block("item", 5)
	c.DeleteBlock("item")
	if got := c.Render(); strings.Contains(got, "X") {
		t.Errorf("block not blanked: %q", got)
	}
}

func TestClearResetsStateButNotTemplate(t *testing.T) {
	src := "<!-- BEGIN item -->\nX {v}\n<!-- END item -->\n"
	c := mustParse(t, src).New()
	c.Block("item", 2)
	c.Tag("v", "once")
	_ = c.Render()
	c.Clear()
	if got := c.Render(); got != "" {
		t.Errorf("clear should reset counts, got %q", got)
	}
	// rebinding after Clear should work
	c.Block("item", 1)
	c.Tag("v", "twice")
	if got := c.Render(); got != "X twice\n" {
		t.Errorf("rebind after clear: %q", got)
	}
}

func TestDefaultCallbackInjectsEntryMarking(t *testing.T) {
	src := "<!-- BEGIN entry -->\nbody\n<!-- END entry -->\n"
	tmpl, err := Parse(src, DefaultCallback)
	if err != nil {
		t.Fatal(err)
	}
	c := tmpl.New()
	c.Block("entry", 1)
	c.Tag("sb_entry_marking", "MARK")
	if got := c.Render(); !strings.Contains(got, "MARK") {
		t.Errorf("sb_entry_marking not injected: %q", got)
	}
}

func TestNoCallbackAddsNothing(t *testing.T) {
	src := "<!-- BEGIN entry -->\nbody\n<!-- END entry -->\n"
	tmpl, _ := Parse(src, NoCallback)
	c := tmpl.New()
	c.Block("entry", 1)
	c.Tag("sb_entry_marking", "MARK")
	if got := c.Render(); strings.Contains(got, "MARK") {
		t.Errorf("unexpected injection under NoCallback: %q", got)
	}
}

func TestRealAtomFeedTemplate(t *testing.T) {
	// Parse the unmodified template shipped with the original Perl SB and
	// confirm it renders against live data without error. Any regression
	// in the parser's handling of real-world whitespace or structure shows
	// up here before it can break the Feed app.
	path := filepath.Join("..", "..", "..", "_base", "lib", "resource", "default_atomfeed.xml")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("reference template not available: %v", err)
	}
	tmpl, err := Parse(string(src), DefaultCallback)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	c := tmpl.New()
	c.Tag("feed_site_encoding", "utf-8")
	c.Tag("site_lang", "en")
	c.Block("title", 1)
	c.Tag("site_title", "Example")
	c.Tag("site_top", "https://example.com/")
	c.Tag("feed_date", "2026-04-19T00:00:00Z")
	c.Tag("blog_description", "example blog")
	c.Tag("script_webpage", "https://serenebach.net/")
	c.Tag("script_name", "Serene Bach")

	c.Num(0)
	c.Tag("feed_entry_title", "First")
	c.Tag("feed_entry_url", "https://example.com/1")
	c.Tag("feed_entry_date", "2026-04-18T12:00:00Z")
	c.Tag("feed_entry_modified", "2026-04-18T12:00:00Z")
	c.Tag("feed_entry_summary", "first summary")
	c.Tag("feed_entry_author", "ootani")
	c.Tag("feed_entry_category", "test")
	c.Tag("feed_entry_description", "<p>first body</p>")
	c.Block("feed_entry", 1)

	got := c.Render()
	for _, want := range []string{
		`<title>Example</title>`,
		`<title>First</title>`,
		`<id>https://example.com/1</id>`,
		`<name>ootani</name>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("real template output missing %q\nfull output:\n%s", want, got)
			return
		}
	}
}

func TestAtomFeedGolden(t *testing.T) {
	// Smoke test against the shape of SB3's default_atomfeed.xml.
	src := `<?xml version="1.0" encoding="{feed_site_encoding}" ?>
<feed>
	<!-- BEGIN title -->
	<title>{site_title}</title>
	<modified>{feed_date}</modified>
	<!-- END title -->
	<!-- BEGIN feed_entry -->
	<entry>
		<title>{feed_entry_title}</title>
		<id>{feed_entry_url}</id>
	</entry>
	<!-- END feed_entry -->
</feed>
`
	c := mustParse(t, src).New()
	c.Tag("feed_site_encoding", "utf-8")
	c.Block("title", 1)
	c.Tag("site_title", "Hello")
	c.Tag("feed_date", "2026-04-19T00:00:00Z")

	c.Num(0)
	c.Tag("feed_entry_title", "First post")
	c.Tag("feed_entry_url", "https://example.com/1")
	c.Num(1)
	c.Tag("feed_entry_title", "Second post")
	c.Tag("feed_entry_url", "https://example.com/2")
	c.Block("feed_entry", 2)

	got := c.Render()
	for _, want := range []string{
		`encoding="utf-8"`,
		`<title>Hello</title>`,
		`<modified>2026-04-19T00:00:00Z</modified>`,
		`<title>First post</title>`,
		`<id>https://example.com/1</id>`,
		`<title>Second post</title>`,
		`<id>https://example.com/2</id>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\nfull output:\n%s", want, got)
		}
	}
}

func TestUsedTagsExcludesChildPlaceholders(t *testing.T) {
	src := "<!-- BEGIN outer -->\n{alpha}\n<!-- BEGIN inner -->\n{beta}\n<!-- END inner -->\n<!-- END outer -->\n"
	tmpl := mustParse(t, src)
	tags := tmpl.UsedTags()
	haveAlpha, haveBeta, haveChildPlaceholder := false, false, false
	for _, tg := range tags {
		switch tg {
		case "alpha":
			haveAlpha = true
		case "beta":
			haveBeta = true
		case "-inner":
			haveChildPlaceholder = true
		}
	}
	if !haveAlpha || !haveBeta {
		t.Errorf("UsedTags should surface author tags alpha + beta, got %v", tags)
	}
	if haveChildPlaceholder {
		t.Errorf("UsedTags must strip `{-name}` child placeholders, got %v", tags)
	}
}

func TestUsedBlocksExcludesRoot(t *testing.T) {
	src := "<!-- BEGIN foo -->a<!-- END foo -->\n<!-- BEGIN bar -->b<!-- END bar -->\n"
	tmpl := mustParse(t, src)
	blocks := tmpl.UsedBlocks()
	if len(blocks) != 2 || blocks[0] != "bar" || blocks[1] != "foo" {
		t.Errorf("expected sorted [bar foo], got %v", blocks)
	}
}

func TestParseUnmatchedEndReturnsError(t *testing.T) {
	// An END at the root level used to underflow the block stack and panic.
	// It must now surface as a syntax error so lint/import/render report it.
	src := "hello\n<!-- END outer -->\n"
	tmpl, err := Parse(src, NoCallback)
	if err == nil {
		t.Fatalf("Parse: want error for stray END, got nil (tmpl=%v)", tmpl)
	}
	if !strings.Contains(err.Error(), "unexpected END") {
		t.Errorf("error %q should mention 'unexpected END'", err.Error())
	}
}
