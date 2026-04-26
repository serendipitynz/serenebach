package content

import (
	"html"
	"strings"
)

// formatCommentBody turns a raw visitor-submitted comment body into
// safe HTML: every character is html-escaped first so tag-like bytes
// can never construct markup, then newlines are replaced with <br>
// so multi-line comments keep their shape on render. Templates consume
// the returned string via `{comment_description}` without further
// escaping, which is why the helper must guarantee escape itself.
func formatCommentBody(body string) string {
	normalised := strings.ReplaceAll(body, "\r\n", "\n")
	escaped := html.EscapeString(normalised)
	return strings.ReplaceAll(escaped, "\n", "<br>")
}

// safeExternalURL returns s only when it parses to a scheme we trust
// enough to let into an `<a href>`. Rejects `javascript:`, `data:`,
// `vbscript:`, any unknown scheme, and anything that looks like a
// script payload. Also returns "" for schemeless strings starting
// with whitespace / control bytes. Safe values are http, https,
// mailto, and protocol-relative (`//example.com/…`) + path-only
// (`/about`) forms for same-site links.
//
// Note: the comment submit handler does its own validation up front;
// this is a belt-and-braces render-time check so a stale DB row
// (imported from an older SB3 install, for example) can't leak past.
func safeExternalURL(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// Protocol-relative or site-relative links are safe — no scheme to
	// smuggle a script into.
	if strings.HasPrefix(s, "//") || strings.HasPrefix(s, "/") {
		return s
	}
	// Scheme extraction: up to the first ":". A colon-less value is
	// treated as a relative URL (safe).
	colon := strings.Index(s, ":")
	if colon < 0 {
		return s
	}
	scheme := strings.ToLower(s[:colon])
	switch scheme {
	case "http", "https", "mailto":
		return s
	}
	return ""
}
