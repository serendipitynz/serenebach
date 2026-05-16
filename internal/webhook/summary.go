package webhook

import (
	"fmt"
	"strings"
)

// summarise builds a one-line human-readable message describing the
// event. It is embedded as the "text" and "content" top-level keys of
// the flat payload so receivers that expect Slack's `{"text":"..."}`
// or Discord's `{"content":"..."}` schemas can pick something up
// without further transformation. Slack and Discord both auto-link
// bare URLs in plain text so the same string renders on either side.
//
// Falls back to a generic "Serene Bach: <event>" if the event id is
// unknown so a future event type still produces non-empty output
// instead of an empty message.
func summarise(p Payload) string {
	weblog := p.Weblog.Title
	switch p.Event {
	case EventEntryPublished:
		return entrySummary("📝 New entry", p, weblog)
	case EventEntryUpdated:
		return entrySummary("✏️ Entry updated", p, weblog)
	case EventEntryDeleted:
		title := dataString(p, "title")
		return prefixWithBlog(weblog, fmt.Sprintf("🗑 Entry deleted: %s", fallback(title, "(untitled)")))
	case EventCommentReceived:
		return commentSummary("💬 Comment received", p, weblog, "admin_url")
	case EventCommentApproved:
		return commentSummary("✅ Comment approved", p, weblog, "entry_url")
	case EventImageUploaded:
		filename := dataString(p, "filename")
		url := dataString(p, "url")
		body := fmt.Sprintf("🖼 Image uploaded: %s", fallback(filename, "(unknown)"))
		if url != "" {
			body += " — " + url
		}
		return prefixWithBlog(weblog, body)
	}
	return fmt.Sprintf("Serene Bach: %s", p.Event)
}

// entrySummary renders the entry.* events as
// "[BlogTitle] <prefix>: <entryTitle> — <entryURL>".
func entrySummary(prefix string, p Payload, weblog string) string {
	title := fallback(dataString(p, "title"), "(untitled)")
	url := dataString(p, "url")
	body := fmt.Sprintf("%s: %s", prefix, title)
	if url != "" {
		body += " — " + url
	}
	return prefixWithBlog(weblog, body)
}

// commentSummary renders the comment.* events. linkField names the
// data key to surface as the click-through URL: admin_url for the
// moderation-facing "received" event, entry_url for the public-facing
// "approved" event.
func commentSummary(prefix string, p Payload, weblog, linkField string) string {
	commenter := fallback(dataString(p, "commenter"), "anonymous")
	excerpt := dataString(p, "body_excerpt")
	body := fmt.Sprintf("%s from %s", prefix, commenter)
	if excerpt != "" {
		body += ": " + collapseWhitespace(excerpt)
	}
	if link := dataString(p, linkField); link != "" {
		body += " — " + link
	}
	return prefixWithBlog(weblog, body)
}

// prefixWithBlog optionally prepends "[BlogTitle] " when the weblog
// title is non-empty. Multi-blog deployments use it to disambiguate
// fan-out into shared channels; single-blog installs without a title
// stay terse.
func prefixWithBlog(weblog, body string) string {
	if weblog == "" {
		return body
	}
	return fmt.Sprintf("[%s] %s", weblog, body)
}

// dataString extracts a string value from Payload.Data, returning ""
// for missing keys or non-string values. Keeps the summariser tolerant
// of evolving data shapes.
func dataString(p Payload, key string) string {
	if p.Data == nil {
		return ""
	}
	v, ok := p.Data[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

func fallback(s, alt string) string {
	if s == "" {
		return alt
	}
	return s
}

// collapseWhitespace squashes consecutive whitespace runs into a
// single space so a multi-line comment body doesn't blow out a chat
// message. The 240-rune cap on body_excerpt limits final length.
func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
