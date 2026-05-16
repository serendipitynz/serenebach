package webhook

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/serendipitynz/serenebach/internal/domain"
)

// Payload is the JSON envelope every webhook receives. Data is
// event-specific; see entryData / commentData / imageData.
type Payload struct {
	ID        string         `json:"id"`
	Event     string         `json:"event"`
	Timestamp string         `json:"timestamp"`
	Weblog    WeblogRef      `json:"weblog"`
	Data      map[string]any `json:"data"`
}

// WeblogRef is the per-payload weblog descriptor. Title + URL come from
// the Weblog row at dispatch time so subscribers don't have to query
// back into Serene Bach to get readable context.
type WeblogRef struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

// AuthorRef is the per-payload author descriptor. Name resolves to
// DisplayName when set, otherwise the login name — same convention as
// the public template layer.
type AuthorRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// EntryPayload assembles an entry.* payload. Pass the full URL
// (already resolved against the weblog's BaseURL) so subscribers get a
// link they can hit without further composition.
func EntryPayload(weblog domain.Weblog, entry domain.Entry, author domain.User, entryURL string, categoryNames, tagNames []string, event string) Payload {
	return Payload{
		ID:        NewDeliveryID(),
		Event:     event,
		Timestamp: nowISO(),
		Weblog:    weblogRef(weblog),
		Data: map[string]any{
			"id":           entry.ID,
			"slug":         entry.Slug,
			"title":        entry.Title,
			"url":          entryURL,
			"status":       entryStatusLabel(entry.Status),
			"author":       authorRef(author),
			"published_at": iso8601(entry.PostedAt),
			"categories":   ensureStringSlice(categoryNames),
			"tags":         ensureStringSlice(tagNames),
		},
	}
}

// CommentPayload assembles a comment.* payload. entryURL is the link
// readers land on; adminURL is a deep link into the moderation queue.
// commenter is the visitor-supplied name (no email / IP — design §10).
func CommentPayload(weblog domain.Weblog, msg domain.Message, entryURL, adminURL, event string) Payload {
	return Payload{
		ID:        NewDeliveryID(),
		Event:     event,
		Timestamp: nowISO(),
		Weblog:    weblogRef(weblog),
		Data: map[string]any{
			"id":           msg.ID,
			"entry_id":     msg.EntryID,
			"entry_url":    entryURL,
			"admin_url":    adminURL,
			"status":       messageStatusLabel(msg.Status),
			"commenter":    msg.AuthorName,
			"body_excerpt": excerpt(msg.Body, 240),
		},
	}
}

// ImagePayload is reserved for the future image.uploaded event. Kept
// here so the JSON shape stays consistent with the entry / comment
// payloads.
func ImagePayload(weblog domain.Weblog, image domain.Image, imageURL string) Payload {
	return Payload{
		ID:        NewDeliveryID(),
		Event:     EventImageUploaded,
		Timestamp: nowISO(),
		Weblog:    weblogRef(weblog),
		Data: map[string]any{
			"id":         image.ID,
			"filename":   image.Filename,
			"mime_type":  image.MimeType,
			"size_bytes": image.SizeBytes,
			"url":        imageURL,
		},
	}
}

// nowISO returns the current UTC instant in RFC3339 (the design's
// "2026-05-06T12:34:56Z" shape).
func nowISO() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func iso8601(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func weblogRef(w domain.Weblog) WeblogRef {
	return WeblogRef{ID: w.ID, Title: w.Title, URL: w.BaseURL}
}

func authorRef(u domain.User) AuthorRef {
	name := u.DisplayName
	if name == "" {
		name = u.Name
	}
	return AuthorRef{ID: u.ID, Name: name}
}

func entryStatusLabel(s domain.EntryStatus) string {
	switch s {
	case domain.EntryPublished:
		return "published"
	case domain.EntryDraft:
		return "draft"
	case domain.EntryClosed:
		return "closed"
	}
	return "unknown"
}

func messageStatusLabel(s domain.MessageStatus) string {
	switch s {
	case domain.MessageApproved:
		return "approved"
	case domain.MessageWaiting:
		return "waiting"
	case domain.MessageHidden:
		return "hidden"
	}
	return "unknown"
}

// excerpt returns up to `limit` runes from s, suffixing "…" when
// truncated. Operates on runes so multi-byte UTF-8 isn't cut mid-
// character.
func excerpt(s string, limit int) string {
	trimmed := strings.TrimSpace(s)
	if utf8.RuneCountInString(trimmed) <= limit {
		return trimmed
	}
	runes := []rune(trimmed)
	return string(runes[:limit]) + "…"
}

// ensureStringSlice converts a nil slice to an empty (non-nil) slice so
// the JSON serialisation emits `[]` rather than `null`. Subscribers
// have an easier time when categories / tags are always arrays.
func ensureStringSlice(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}
