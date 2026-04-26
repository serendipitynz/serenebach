package format

import (
	"html"
	"regexp"
	"strings"
)

// renderSbtext is the minimal SB3-sbtext subset shipped with the Go
// port. It covers the three features that matter for imported
// entries: paragraph splitting on blank lines (autobreak),
// URL autolinking, and a small pair of inline emphasis markers.
// Anything richer from SB3's sbTextFormat.pm (headings, tables,
// footnotes, shortcut links) is intentionally out of scope — the
// reference SB3 parser is 800+ lines of Perl and most authors in
// 2026 reach for Markdown before sbtext.
//
// Processing order matters:
//
//  1. Escape the whole body so HTML in the source is neutralised.
//  2. Split into paragraphs on blank lines; inside each paragraph
//     collapse newlines to `<br>` when autobreak is on, otherwise
//     join with a single space.
//  3. Apply URL autolink on the already-escaped text so we only
//     touch what looks like a URL.
//  4. Apply the inline markers (`”…”` → strong, `”'…”'` → em).
//
// `[text|URL]` SB3 link shortcuts are translated when autolink is on
// so imported entries with inline links keep their anchors.
func renderSbtext(body string, autobreak, autolink bool) string {
	escaped := html.EscapeString(body)

	paragraphs := splitParagraphs(escaped)
	for i, p := range paragraphs {
		if autobreak {
			p = strings.ReplaceAll(p, "\n", "<br>\n")
		} else {
			p = strings.ReplaceAll(p, "\n", " ")
		}
		if autolink {
			// Bracket link first. It produces `<a href="URL">label</a>`,
			// so we capture the URL under a sentinel afterwards to
			// keep the bare-URL pass from re-linking it.
			p = sbtextBracketLink.ReplaceAllString(p, `<a href="$2">$1</a>`)
			// Bare URL only when the char before is a safe boundary —
			// start of line, whitespace, or a closing `>`. Anything
			// inside an `href="…"` attribute starts with `"` and is
			// skipped. Go's regexp lacks lookbehind, so we match the
			// boundary char and put it back in the replacement.
			p = sbtextBareURL.ReplaceAllString(p, `$1<a href="$2">$2</a>`)
		}
		p = applyInlineEmphasis(p)
		paragraphs[i] = p
	}

	// Keep the single-paragraph shortcut for the common case (a short
	// comment, an SB2 one-liner) — wrapping in `<p>` when there's no
	// blank-line split would diverge from what most SB3 themes
	// expect for top-level descriptions.
	if len(paragraphs) == 1 {
		return paragraphs[0]
	}
	var b strings.Builder
	for _, p := range paragraphs {
		if p == "" {
			continue
		}
		b.WriteString("<p>")
		b.WriteString(p)
		b.WriteString("</p>\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// splitParagraphs splits body on sequences of blank lines (one or
// more). Lines are normalised on `\n`; the escape step above leaves
// line-terminators untouched.
func splitParagraphs(body string) []string {
	raw := sbtextParaSplit.Split(body, -1)
	out := make([]string, 0, len(raw))
	for _, p := range raw {
		trimmed := strings.Trim(p, "\n")
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return []string{""}
	}
	return out
}

// applyInlineEmphasis translates SB3's triple/double quote markers.
// The input text has already been passed through html.EscapeString
// which rewrites `'` to `&#39;`, so the regex matches the escaped
// form. Triple is handled before double so `”'word”'` doesn't
// match as two `”` spans with a stray `word` in between.
func applyInlineEmphasis(p string) string {
	p = sbtextTripleQuote.ReplaceAllString(p, `<em>$1</em>`)
	p = sbtextDoubleQuote.ReplaceAllString(p, `<strong>$1</strong>`)
	return p
}

var (
	// Paragraph split: one or more blank lines. Matches against
	// already-escaped input, so `\n` survives EscapeString unchanged.
	sbtextParaSplit = regexp.MustCompile(`\n\s*\n`)

	// Bare URL autolink. Captures a preceding boundary char (start
	// of line, whitespace, or a closing `>`) so we don't re-link a
	// URL that already sits inside an `href="…"` attribute produced
	// by the bracket-link pass.
	sbtextBareURL = regexp.MustCompile(`(^|[\s>])(https?://[^\s<>"']+)`)

	// SB3 `[label|URL]` bracket-link notation. Brackets aren't
	// escaped by html.EscapeString so the pattern survives intact.
	sbtextBracketLink = regexp.MustCompile(`\[([^\]|]+)\|(https?://[^\]\s]+)\]`)

	// Emphasis markers — the `'` characters get turned into `&#39;`
	// by html.EscapeString, so the regex matches that HTML entity
	// form.
	sbtextTripleQuote = regexp.MustCompile(`(?:&#39;){3}([^&]+?)(?:&#39;){3}`)
	sbtextDoubleQuote = regexp.MustCompile(`(?:&#39;){2}([^&]+?)(?:&#39;){2}`)
)
