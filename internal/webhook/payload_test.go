package webhook

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/serendipitynz/serenebach/internal/domain"
)

func TestEntryPayloadShape(t *testing.T) {
	weblog := domain.Weblog{ID: 1, Title: "My Blog", BaseURL: "https://example.com/"}
	entry := domain.Entry{
		ID:       42,
		Slug:     "hello",
		Title:    "Hello",
		Status:   domain.EntryPublished,
		PostedAt: time.Date(2026, 5, 6, 12, 34, 56, 0, time.UTC),
	}
	author := domain.User{ID: 7, Name: "admin", DisplayName: "Admin"}
	p := EntryPayload(weblog, entry, author, "https://example.com/entry/hello/", []string{"雑記"}, []string{"go"}, EventEntryPublished)
	if p.Event != EventEntryPublished {
		t.Errorf("Event = %q, want %q", p.Event, EventEntryPublished)
	}
	if p.ID == "" {
		t.Errorf("delivery ID should be populated")
	}
	if p.Weblog.Title != "My Blog" {
		t.Errorf("weblog title = %q", p.Weblog.Title)
	}
	if p.Data["id"].(int64) != 42 {
		t.Errorf("data.id = %v, want 42", p.Data["id"])
	}
	if p.Data["status"].(string) != "published" {
		t.Errorf("data.status = %v, want published", p.Data["status"])
	}
	if got := p.Data["author"].(AuthorRef).Name; got != "Admin" {
		t.Errorf("author.name = %q, want Admin", got)
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), `"event":"entry.published"`) {
		t.Errorf("payload JSON missing event field: %s", b)
	}
}

func TestCommentPayloadOmitsSensitiveFields(t *testing.T) {
	weblog := domain.Weblog{ID: 1, Title: "Blog", BaseURL: "https://example.com/"}
	msg := domain.Message{
		ID:          101,
		EntryID:     42,
		Status:      domain.MessageWaiting,
		AuthorName:  "Alice",
		AuthorEmail: "alice@example.com",
		Body:        "Great post!",
		IPAddress:   "203.0.113.4",
	}
	p := CommentPayload(weblog, msg, "https://example.com/entry/42/", "https://example.com/admin/comments?status=waiting", EventCommentReceived)
	b, _ := json.Marshal(p)
	js := string(b)
	for _, leak := range []string{"alice@example.com", "203.0.113.4"} {
		if strings.Contains(js, leak) {
			t.Errorf("payload leaked %q: %s", leak, js)
		}
	}
	if !strings.Contains(js, `"commenter":"Alice"`) {
		t.Errorf("commenter name missing: %s", js)
	}
	if !strings.Contains(js, `"status":"waiting"`) {
		t.Errorf("status missing: %s", js)
	}
}

func TestExcerptTruncatesUTF8Safely(t *testing.T) {
	s := strings.Repeat("あ", 300)
	got := excerpt(s, 240)
	runeCount := 0
	for range got {
		runeCount++
	}
	if runeCount != 241 { // 240 + ellipsis
		t.Errorf("excerpt rune count = %d, want 241", runeCount)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("excerpt should end with …, got suffix %q", got[len(got)-3:])
	}
}
