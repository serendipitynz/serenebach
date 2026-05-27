package content

import (
	"html"
	"regexp"
	"strings"

	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/template/sbtemplate"
)

// summaryLength mirrors SB3's sb::Data::Entry::SUMMARY_LENGTH: the
// {entry_excerpt} fallback clips the plain-text body to this many
// characters. SB3 counts bytes; the Go port counts runes so multi-byte
// Japanese text is never cut mid-character or shortened to a third.
const summaryLength = 200

// excerptEllipsis is SB3's CLIPPING_TAIL, appended when the body is
// clipped. SB3 subtracts its length from the clip budget so the final
// string stays at summaryLength runes; we mirror that.
const excerptEllipsis = "..."

// tagStripRe matches HTML tags for SB3's clip(removetag=1) behaviour
// (sb::Text::_remove_tag_fast does `s/<[^>]*>//g`). Imperfect but fast,
// and identical to what SB3 itself ran on the summary fallback.
var tagStripRe = regexp.MustCompile(`<[^>]*>`)

// applySEO fills the SB3-compatible {entry_excerpt} tag plus the
// Go-native entry_canonical / entry_noindex blocks. Written like
// applyPinned so the template DSL needs no extension: the conditional
// blocks emit with count 1 only when the field is set and 0-stripe
// otherwise — an empty <link rel="canonical"> / robots meta is harmful,
// so "emit nothing when empty" is the contract.
func (v EntryView) applySEO(c *sbtemplate.Context) {
	c.Tag("entry_excerpt", entryExcerpt(v.Entry))

	// single_meta gates the per-item head metadata (description / OG) so
	// it renders on single-content views (entry permalink + flat page)
	// but not on list pages, where {entry_excerpt} would resolve to the
	// first entry's excerpt. The sequel block stays entry-only (prev/next
	// nav); see PageView.applySEO for the flat-page side.
	c.Block("single_meta", 1)

	// canonical: c.Tag runs html.EscapeString on the value, so pass the
	// raw URL — pre-escaping would double-encode `?a=1&b=2` into
	// `&amp;amp;` and break the link.
	if v.Entry.CanonicalURL != "" {
		c.Tag("entry_canonical_url", v.Entry.CanonicalURL)
		c.Block("entry_canonical", 1)
	} else {
		c.Block("entry_canonical", 0)
	}

	if v.Entry.NoIndex {
		c.Block("entry_noindex", 1)
	} else {
		c.Block("entry_noindex", 0)
	}
}

// applySEO (flat pages) mirrors EntryView.applySEO so pages share the
// exact same {entry_excerpt} tag and entry_canonical / entry_noindex
// blocks. Pages have no SB3 `sum` heritage, so the excerpt falls back to
// a body clip then the title.
func (v PageView) applySEO(c *sbtemplate.Context) {
	c.Tag("entry_excerpt", seoExcerpt(v.Page.Summary, v.Page.Body, v.Page.Format, v.Page.Title))

	// A flat page is a single-content view, so the head metadata block
	// renders here too (the entry-only sequel block stays 0 for pages).
	c.Block("single_meta", 1)

	if v.Page.CanonicalURL != "" {
		c.Tag("entry_canonical_url", v.Page.CanonicalURL)
		c.Block("entry_canonical", 1)
	} else {
		c.Block("entry_canonical", 0)
	}

	if v.Page.NoIndex {
		c.Block("entry_noindex", 1)
	} else {
		c.Block("entry_noindex", 0)
	}
}

// entryExcerpt computes the {entry_excerpt} value shared by the entry
// and list renderers, mirroring SB3's $entry->sum.
func entryExcerpt(e domain.Entry) string {
	return seoExcerpt(e.Summary, e.Body, e.Format, e.Title)
}

// seoExcerpt is the {entry_excerpt} fallback chain shared by entries and
// flat pages: the stored summary when set, else a plain-text clip of the
// body, else the title (a Go enhancement so the tag is never blank —
// SB3 would emit empty here, but Body is optional while Title is
// required).
func seoExcerpt(summary, body, format, title string) string {
	if summary != "" {
		return summary
	}
	if s := excerptPlain(body, format); s != "" {
		return s
	}
	return title
}

// excerptPlain renders the body, strips tags, and clips to summaryLength
// runes — SB3's clip(length=200, removetag=1, ellipsis). The rendered
// HTML may still carry entities (e.g. `&amp;`); we unescape them back to
// raw text so the caller's c.Tag escape runs exactly once instead of
// doubling them.
func excerptPlain(body, format string) string {
	if body == "" {
		return ""
	}
	rendered := formatBody(body, format, "seo.excerpt")
	stripped := tagStripRe.ReplaceAllString(rendered, "")
	text := html.UnescapeString(stripped)
	// SB3 only strips linefeeds; collapsing every whitespace run keeps
	// the one-line description readable when block tags butted words
	// together (e.g. "</p><p>" → "wordword").
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= summaryLength {
		return text
	}
	limit := summaryLength - len([]rune(excerptEllipsis))
	return string(runes[:limit]) + excerptEllipsis
}
