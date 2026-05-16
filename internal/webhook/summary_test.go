package webhook

import (
	"strings"
	"testing"
	"time"

	"github.com/serendipitynz/serenebach/internal/domain"
)

func TestSummariseEntryPublished(t *testing.T) {
	p := EntryPayload(
		domain.Weblog{ID: 1, Title: "My Blog", BaseURL: "https://example.com/"},
		domain.Entry{ID: 42, Slug: "hello", Title: "Hello, World!", Status: domain.EntryPublished, PostedAt: time.Now()},
		domain.User{ID: 1, Name: "admin"},
		"https://example.com/entry/hello/",
		nil, nil, EventEntryPublished,
	)
	got := summarise(p)
	for _, want := range []string{"[My Blog]", "New entry", "Hello, World!", "https://example.com/entry/hello/"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing %q: %s", want, got)
		}
	}
}

func TestSummariseCommentReceivedUsesAdminURL(t *testing.T) {
	p := CommentPayload(
		domain.Weblog{ID: 1, Title: "My Blog", BaseURL: "https://example.com/"},
		domain.Message{ID: 10, EntryID: 42, Status: domain.MessageWaiting, AuthorName: "Alice", Body: "Nice post!\n\n  multi\n  line"},
		"https://example.com/entry/hello/",
		"https://example.com/admin/comments?status=waiting",
		EventCommentReceived,
	)
	got := summarise(p)
	for _, want := range []string{"Comment received", "Alice", "Nice post! multi line", "/admin/comments?status=waiting"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing %q: %s", want, got)
		}
	}
	if strings.Contains(got, "\n") {
		t.Errorf("summary should be single-line (whitespace collapsed): %q", got)
	}
}

func TestSummariseCommentApprovedUsesEntryURL(t *testing.T) {
	p := CommentPayload(
		domain.Weblog{ID: 1, Title: "My Blog", BaseURL: "https://example.com/"},
		domain.Message{ID: 11, EntryID: 42, Status: domain.MessageApproved, AuthorName: "Bob"},
		"https://example.com/entry/hello/",
		"https://example.com/admin/comments?status=waiting",
		EventCommentApproved,
	)
	got := summarise(p)
	if !strings.Contains(got, "/entry/hello/") {
		t.Errorf("approved comment summary should link to entry, got %q", got)
	}
	if strings.Contains(got, "/admin/comments") {
		t.Errorf("approved comment summary should NOT link to the moderation queue, got %q", got)
	}
}

func TestSummariseImageUploaded(t *testing.T) {
	p := ImagePayload(
		domain.Weblog{ID: 1, Title: "My Blog", BaseURL: "https://example.com/"},
		domain.Image{ID: 7, Filename: "hello.png"},
		"https://example.com/img/2026/05/hello.png",
	)
	got := summarise(p)
	if !strings.Contains(got, "hello.png") {
		t.Errorf("image summary should mention filename, got %q", got)
	}
}

func TestSummariseFallsBackToGenericForUnknownEvent(t *testing.T) {
	got := summarise(Payload{Event: "future.event"})
	if !strings.HasPrefix(got, "Serene Bach:") {
		t.Errorf("unknown event should produce generic fallback, got %q", got)
	}
}

func TestFlattenInjectsTextAndContent(t *testing.T) {
	p := EntryPayload(
		domain.Weblog{ID: 1, Title: "My Blog", BaseURL: "https://example.com/"},
		domain.Entry{ID: 42, Slug: "hello", Title: "Hello", Status: domain.EntryPublished, PostedAt: time.Now()},
		domain.User{ID: 1, Name: "admin"},
		"https://example.com/entry/hello/",
		nil, nil, EventEntryPublished,
	)
	flat, err := flattenPayload(p)
	if err != nil {
		t.Fatalf("flattenPayload: %v", err)
	}
	textVal, ok := flat["text"].(string)
	if !ok || textVal == "" {
		t.Fatalf("flat[\"text\"] missing or wrong type: %v", flat["text"])
	}
	contentVal, ok := flat["content"].(string)
	if !ok || contentVal == "" {
		t.Fatalf("flat[\"content\"] missing or wrong type: %v", flat["content"])
	}
	if textVal != contentVal {
		t.Errorf("text and content should match: text=%q content=%q", textVal, contentVal)
	}
}

func TestEnvelopeFormatDoesNotIncludeSummary(t *testing.T) {
	// Sanity: the existing envelope shape stays unchanged so receivers
	// already consuming it are not broken by accidentally inheriting
	// the new "text" / "content" keys.
	p := EntryPayload(
		domain.Weblog{ID: 1, Title: "My Blog", BaseURL: "https://example.com/"},
		domain.Entry{ID: 42, Slug: "hello", Title: "Hello", Status: domain.EntryPublished, PostedAt: time.Now()},
		domain.User{ID: 1, Name: "admin"},
		"https://example.com/entry/hello/",
		nil, nil, EventEntryPublished,
	)
	envelope, err := encodeForFormat(p, PayloadFormatEnvelope)
	if err != nil {
		t.Fatalf("encodeForFormat envelope: %v", err)
	}
	js := string(envelope)
	if strings.Contains(js, `"text":`) || strings.Contains(js, `"content":`) {
		t.Errorf("envelope payload should not contain text/content keys: %s", js)
	}
}
