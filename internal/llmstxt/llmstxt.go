// Package llmstxt generates the two files LLM agents (and crawler-
// style AI pipelines) read to discover + ingest a blog:
//
//   - /llms.txt      — a short Markdown index of title / description
//   - per-entry anchor links. Follows the llmstxt.org convention so
//     agents that only scrape the index still see the shape of the
//     site.
//   - /llms-full.txt — full-body Markdown dump of every published
//     entry, concatenated with H2-level headings. The intended
//     consumer is an agent that wants to pull the whole knowledge
//     base in one request and chunk client-side.
//
// Generation is pure (no side effects beyond the returned string) so
// both the dynamic HTTP route and the static rebuild can share the
// exact same output.
package llmstxt

import (
	"fmt"
	"strings"

	"github.com/serendipitynz/serenebach/internal/domain"
)

// Input captures everything the generators need. Callers populate
// this from repo queries; llmstxt itself never touches the DB so the
// package stays trivial to test.
type Input struct {
	Weblog  domain.Weblog
	Entries []domain.Entry
}

// Index returns the Markdown body for `/llms.txt`. Format mirrors
// the llmstxt.org example:
//
//	# Blog Title
//	> description
//
//	## Recent posts
//	- [Entry Title](https://site/entry/slug/): first-line summary
func Index(in Input) string {
	var b strings.Builder
	writeHeader(&b, in.Weblog)
	if len(in.Entries) == 0 {
		b.WriteString("\n_No published entries yet._\n")
		return b.String()
	}
	b.WriteString("\n## Recent posts\n\n")
	base := siteBase(in.Weblog)
	for _, e := range in.Entries {
		link := base + "entry/" + entryKey(e) + "/"
		summary := summarise(e, 140)
		if summary == "" {
			fmt.Fprintf(&b, "- [%s](%s)\n", escapeInline(e.Title), link)
		} else {
			fmt.Fprintf(&b, "- [%s](%s): %s\n", escapeInline(e.Title), link, summary)
		}
	}
	return b.String()
}

// Full returns the Markdown body for `/llms-full.txt`. Every entry's
// full raw body is emitted under an H2 heading with its permalink
// so the agent can resolve ambiguous references back to a URL. If
// the entry body is Markdown it flows through untouched; HTML entries
// keep their tags (most agents can parse either).
func Full(in Input) string {
	var b strings.Builder
	writeHeader(&b, in.Weblog)
	if len(in.Entries) == 0 {
		b.WriteString("\n_No published entries yet._\n")
		return b.String()
	}
	base := siteBase(in.Weblog)
	for _, e := range in.Entries {
		link := base + "entry/" + entryKey(e) + "/"
		b.WriteString("\n## ")
		b.WriteString(e.Title)
		b.WriteByte('\n')
		fmt.Fprintf(&b, "\n<%s>\n\n", link)
		if !e.PostedAt.IsZero() {
			fmt.Fprintf(&b, "_Posted: %s_\n\n", e.PostedAt.UTC().Format("2006-01-02"))
		}
		body := strings.TrimSpace(e.Body)
		if body != "" {
			b.WriteString(body)
			b.WriteByte('\n')
		}
		if more := strings.TrimSpace(e.More); more != "" {
			b.WriteString("\n---\n\n")
			b.WriteString(more)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func writeHeader(b *strings.Builder, w domain.Weblog) {
	fmt.Fprintf(b, "# %s\n", w.Title)
	if w.Description != "" {
		// llms.txt's convention: quote the blurb with `>` so agents
		// can lift the description without parsing the whole doc.
		for _, line := range strings.Split(w.Description, "\n") {
			line = strings.TrimRight(line, "\r")
			if line == "" {
				b.WriteString(">\n")
				continue
			}
			fmt.Fprintf(b, "> %s\n", line)
		}
	}
}

// siteBase returns the weblog's canonical base URL with a trailing
// slash. Falls back to "/" when BaseURL is unset so generated links
// stay relative rather than pointing at an empty host.
func siteBase(w domain.Weblog) string {
	if w.BaseURL == "" {
		return "/"
	}
	if strings.HasSuffix(w.BaseURL, "/") {
		return w.BaseURL
	}
	return w.BaseURL + "/"
}

// entryKey prefers slug over numeric id so links land on the
// human-readable permalink when available.
func entryKey(e domain.Entry) string {
	if e.Slug != "" {
		return e.Slug
	}
	return fmt.Sprintf("%d", e.ID)
}

// summarise compresses an entry body to a single-line Markdown
// summary at most n runes long. Strips HTML tags coarsely (good
// enough for llms.txt — it's a hint, not a render) and collapses
// inner whitespace.
func summarise(e domain.Entry, n int) string {
	body := strings.TrimSpace(e.Body)
	if body == "" {
		return ""
	}
	// Coarse HTML strip: remove everything between < and >.
	var out strings.Builder
	inTag := false
	for _, r := range body {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			if r == '\n' || r == '\r' || r == '\t' {
				out.WriteByte(' ')
				continue
			}
			out.WriteRune(r)
		}
	}
	collapsed := strings.Join(strings.Fields(out.String()), " ")
	if len(collapsed) == 0 {
		return ""
	}
	runes := []rune(collapsed)
	if len(runes) <= n {
		return escapeInline(collapsed)
	}
	return escapeInline(string(runes[:n-1])) + "…"
}

// escapeInline neutralises the Markdown characters that would
// fracture a one-line list item: `]` terminates the link text, and
// unbalanced backticks would swallow later text. Keep it surgical —
// we don't want to HTML-escape Japanese content.
func escapeInline(s string) string {
	s = strings.ReplaceAll(s, "]", "\\]")
	s = strings.ReplaceAll(s, "`", "\\`")
	return s
}
