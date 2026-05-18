package admin

import (
	"strings"
	"testing"
)

// TestComposeActionsHaveCatalogueEntries fails if a verb wired into
// composeActions has no corresponding entry in
// ai_compose_prompts.jsonc. Without this guard, adding a new action
// to the dispatch map (or misspelling an existing one) would let
// the HTTP handler reach the provider with an empty system prompt,
// because resolveComposePrompt currently only flags fully-unknown
// actions. We cannot run this check at package init time without
// creating an initialization cycle, so the invariant is enforced by
// CI instead.
func TestComposeActionsHaveCatalogueEntries(t *testing.T) {
	for action := range composeActions {
		key := action
		if key == "summarize" {
			key = "summarise"
		}
		if _, ok := composePrompts[key]; !ok {
			t.Errorf("action %q is wired into composeActions but has no entry in ai_compose_prompts.jsonc", action)
		}
	}
}

// TestStripJSONCComments exercises the custom JSONC stripper's
// state machine. The risk addressed here is regression in a piece
// of hand-rolled parsing — the canonical catalogue file would
// continue to parse cleanly even if e.g. the string-literal guard
// broke, so the integration tests do not catch these edges.
func TestStripJSONCComments(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "no comments are passed through verbatim",
			in:   `{"a":1}`,
			want: `{"a":1}`,
		},
		{
			name: "line comment is removed but newline kept for line numbers",
			in:   "{\n// drop me\n\"a\":1}",
			want: "{\n\n\"a\":1}",
		},
		{
			name: "block comment is removed inline",
			in:   `{/* drop */"a":1}`,
			want: `{"a":1}`,
		},
		{
			name: "multi-line block comment is removed in one span",
			in:   "{\n/* line one\nline two */\"a\":1}",
			want: "{\n\"a\":1}",
		},
		{
			name: "double-slash inside a string literal is preserved",
			in:   `{"a":"http://example.com"}`,
			want: `{"a":"http://example.com"}`,
		},
		{
			name: "slash-star inside a string literal is preserved",
			in:   `{"a":"see /* not a comment */ here"}`,
			want: `{"a":"see /* not a comment */ here"}`,
		},
		{
			name: "escaped quote does not terminate the string",
			in:   `{"a":"he said \"hi\" // still in string"}`,
			want: `{"a":"he said \"hi\" // still in string"}`,
		},
		{
			name: "unterminated block comment consumes the rest of the input",
			in:   `{"a":1/* never ends`,
			want: `{"a":1`,
		},
		{
			name: "line comment at EOF without trailing newline",
			in:   `{"a":1} // tail`,
			want: `{"a":1} `,
		},
		{
			name: "lone slash outside a comment is preserved",
			in:   `{"a":"x", "b":"1/2"}`,
			want: `{"a":"x", "b":"1/2"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := string(stripJSONCComments([]byte(tc.in)))
			if got != tc.want {
				t.Errorf("got %q\nwant %q", got, tc.want)
			}
		})
	}
}

// TestComposePromptSystem covers placeholder substitution, the
// summarize/summarise alias, and the unknown-action signal that
// resolveComposePrompt depends on to surface drift at request time.
func TestComposePromptSystem(t *testing.T) {
	t.Run("substitutes both {format} and {lang}", func(t *testing.T) {
		got, ok := composePromptSystem("rewrite", "markdown", "English")
		if !ok {
			t.Fatalf("rewrite should be a known action")
		}
		if strings.Contains(got, "{format}") || strings.Contains(got, "{lang}") {
			t.Errorf("placeholders still present: %q", got)
		}
		if !strings.Contains(got, "markdown markup") {
			t.Errorf("{format} not substituted into prompt: %q", got)
		}
		if !strings.Contains(got, "Reply in English") {
			t.Errorf("{lang} not substituted into prompt: %q", got)
		}
	})

	t.Run("summarize alias resolves to the summarise entry", func(t *testing.T) {
		a, okA := composePromptSystem("summarise", "html", "Japanese")
		b, okB := composePromptSystem("summarize", "html", "Japanese")
		if !okA || !okB {
			t.Fatalf("both summarise and summarize should be known (okA=%v okB=%v)", okA, okB)
		}
		if a != b {
			t.Errorf("summarize alias produced different text:\nsummarise: %q\nsummarize: %q", a, b)
		}
	})

	t.Run("unknown action returns ok=false and an empty system prompt", func(t *testing.T) {
		got, ok := composePromptSystem("no_such_verb", "markdown", "English")
		if ok {
			t.Errorf("expected ok=false for unknown action, got system=%q", got)
		}
		if got != "" {
			t.Errorf("expected empty system for unknown action, got %q", got)
		}
	})
}
