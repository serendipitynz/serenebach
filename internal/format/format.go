// Package format turns a raw entry body (the bytes the author typed) into
// the HTML that lands in the rendered page. The author picks the input
// syntax via `entries.format`; each syntax is a small renderer plugged
// into the same Render() entry point.
//
// Three formats ship: HTML (pass-through, the historical default),
// Markdown (goldmark), and sbtext — a minimal subset of the SB3
// Hatena-style notation for import compatibility.
package format

import (
	"bytes"
	"fmt"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer/html"
)

// Kind is the stored identifier for an entry's body format. Strings are
// used (not an int enum) so the importer can carry SB3's values straight
// through without remapping, and so future formats can land as additive
// constants rather than a migration.
type Kind string

const (
	// HTML: body already contains HTML; passed through unchanged. This is
	// the default when the stored format field is empty, preserving the
	// behaviour every entry was saved with before the format field was
	// introduced.
	HTML Kind = "html"
	// Markdown: body rendered by goldmark with GFM extensions + hard wraps
	// off (so stray newlines don't become unwanted <br>).
	Markdown Kind = "markdown"
	// Sbtext is the minimal SB3 Hatena-style parser (paragraph split,
	// URL autolink, inline emphasis). See sbtext.go for the supported
	// subset.
	Sbtext Kind = "sbtext"
)

// Supported lists every format keyword the admin UI knows how to present.
// Keep the display labels near the constants so a new format only needs
// an entry here and a branch in Render.
var Supported = []struct {
	Kind  Kind
	Label string
	Hint  string
}{
	{HTML, "HTML", "そのまま HTML として埋め込みます。"},
	{Markdown, "Markdown", "# 見出し / **強調** / [リンク](...) などを HTML に展開します。"},
	{Sbtext, "sbtext", "SB3 互換: 空行で段落化、URL を自動リンク、''強調'' / '''斜体''' を展開します。"},
}

// Normalize maps the raw value coming from storage (possibly an SB3
// legacy string) to the canonical Kind used for dispatch. Empty and
// unknown values both collapse to HTML so nothing silently disappears.
func Normalize(raw string) Kind {
	switch raw {
	case "", "html":
		return HTML
	case "markdown", "md":
		return Markdown
	case "sbtext", "1", "2":
		// SB3's "1" (autobreak only) and "2" (autolink only) also map
		// to sbtext; the renderer toggles flags off the raw value.
		return Sbtext
	}
	return HTML
}

// Render turns `body` into the HTML fragment the template engine emits
// for tags like `{entry_description}` and `{entry_sequel}`. An empty
// input yields an empty output — callers don't need to short-circuit.
func Render(body, rawKind string) (string, error) {
	if body == "" {
		return "", nil
	}
	switch Normalize(rawKind) {
	case Markdown:
		return renderMarkdown(body)
	case Sbtext:
		// SB3's format field was `1` (autobreak only), `2` (autolink
		// only), or `sbtext` (Hatena-style, both). We collapse all
		// three through the same renderer, toggling flags off the raw
		// stored value so imported entries render the way SB3 did.
		autobreak, autolink := true, true
		switch rawKind {
		case "1":
			autolink = false
		case "2":
			autobreak = false
		}
		return renderSbtext(body, autobreak, autolink), nil
	case HTML:
		fallthrough
	default:
		return body, nil
	}
}

// markdownRenderer is shared across calls — goldmark.Markdown is safe for
// concurrent use once constructed.
var markdownRenderer = goldmark.New(
	goldmark.WithExtensions(
		extension.GFM,     // tables + strikethrough + task lists + autolink
		extension.Linkify, // bare URL → <a>
	),
	goldmark.WithRendererOptions(
		// The default XHTML mode emits self-closing <br /> which is fine,
		// but html.WithUnsafe is off by default — raw HTML in the source
		// will be escaped. We leave it off so user-submitted comments or
		// sloppy drafts can't inject arbitrary markup; admin-authored
		// entries that need literal HTML should store as Kind=HTML.
		html.WithXHTML(),
	),
)

func renderMarkdown(body string) (string, error) {
	var buf bytes.Buffer
	if err := markdownRenderer.Convert([]byte(body), &buf); err != nil {
		return "", fmt.Errorf("format: markdown: %w", err)
	}
	return buf.String(), nil
}
