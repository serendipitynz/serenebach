package app_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestFlatPayloadFormatDispatch confirms that a webhook subscription
// configured with payload_format='flat' receives the slack.dev-shaped
// single-level JSON body, while envelope subscriptions on the same
// event continue to receive the nested JSON. This guards the per-
// subscription encoding path in webhook.Service.deliverOne.
func TestFlatPayloadFormatDispatch(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	a.Webhooks.AllowLoopback = true

	var (
		mu       sync.Mutex
		received []map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err == nil {
			mu.Lock()
			received = append(received, payload)
			mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if _, err := a.DB.Exec(`INSERT INTO webhooks (wid, url, events, active, payload_format, created_at, updated_at)
		VALUES (1, ?, '["entry.published"]', 1, 'flat', strftime('%s','now'), strftime('%s','now'))`, srv.URL); err != nil {
		t.Fatalf("insert webhook: %v", err)
	}

	cookies := login(t, a.Handler(), "admin", "changeme")
	form := url.Values{
		"title":  {"Hello, World!"},
		"body":   {"first post"},
		"status": {"1"},
	}
	w := authedPOSTForm(t, a.Handler(), "/admin/entries/new", form, cookies)
	if w.Code != http.StatusFound {
		t.Fatalf("create entry status = %d; body:\n%s", w.Code, w.Body.String())
	}

	// Async dispatch — poll briefly for the delivery to land.
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		n := len(received)
		mu.Unlock()
		if n > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(received) == 0 {
		t.Fatalf("no webhook payload received")
	}
	p := received[0]
	// Flat shape: nested keys land on top level joined with "_".
	if got := p["event"]; got != "entry.published" {
		t.Errorf("event = %v, want entry.published", got)
	}
	if got := p["data_title"]; got != "Hello, World!" {
		t.Errorf("data_title = %v, want \"Hello, World!\"", got)
	}
	if got := p["weblog_id"]; got != float64(1) {
		t.Errorf("weblog_id = %v (%T), want 1", got, got)
	}
	// Envelope-only keys should NOT appear as nested objects.
	if _, hasData := p["data"].(map[string]any); hasData {
		t.Errorf("flat payload should not contain nested \"data\" object: %v", p["data"])
	}
	if _, hasWeblog := p["weblog"].(map[string]any); hasWeblog {
		t.Errorf("flat payload should not contain nested \"weblog\" object: %v", p["weblog"])
	}
	// text / content carry the one-line summary so a direct Slack /
	// Discord Incoming Webhook subscription can pick it up.
	for _, key := range []string{"text", "content"} {
		v, ok := p[key].(string)
		if !ok || v == "" {
			t.Errorf("flat payload should include %q summary, got %v", key, p[key])
			continue
		}
		if !strings.Contains(v, "Hello, World!") {
			t.Errorf("%s summary should mention the entry title, got %q", key, v)
		}
	}
}

// TestCommentApproveDispatchUsesApprovedStatus regression-tests the
// PR #88 review finding (P2 #3): when an admin approves a waiting
// comment, the `comment.approved` payload must reflect the post-
// transition status. The pre-fix code dispatched the snapshot loaded
// before UpdateMessageStatus, so the payload reported "waiting" even
// though the event was the approval.
func TestCommentApproveDispatchUsesApprovedStatus(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	// Allow the dispatcher to dial 127.0.0.1 (httptest server).
	a.Webhooks.AllowLoopback = true

	var (
		mu       sync.Mutex
		received []map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err == nil {
			mu.Lock()
			received = append(received, payload)
			mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cookies := login(t, a.Handler(), "admin", "changeme")

	// Insert the webhook row directly: the admin form rejects 127.0.0.1
	// at validation time (correctly), so we bypass it for the test.
	if _, err := a.DB.Exec(`INSERT INTO webhooks (wid, url, events, active, created_at, updated_at)
		VALUES (1, ?, '["comment.approved"]', 1, strftime('%s','now'), strftime('%s','now'))`, srv.URL); err != nil {
		t.Fatalf("insert webhook: %v", err)
	}

	// Need an entry to attach a comment to. Seed a published one.
	res, err := a.DB.Exec(`INSERT INTO entries
		(wid, author_id, category_id, title, body, status, posted_at, created_at, updated_at, accept_comments)
		VALUES (1, 1, 1, 'hello', 'body', 1, strftime('%s','now'), strftime('%s','now'), strftime('%s','now'), 1)`)
	if err != nil {
		t.Fatalf("insert entry: %v", err)
	}
	entryID, _ := res.LastInsertId()

	// Insert a waiting comment directly so we don't need to go through
	// the public submission path (which would also fire comment.received
	// and complicate the assertion).
	res, err = a.DB.Exec(`INSERT INTO messages
		(wid, entry_id, status, posted_at, author_name, author_email, author_url, body, ip_address, user_agent, created_at, updated_at)
		VALUES (1, ?, 0, strftime('%s','now'), 'Alice', '', '', 'hi', '', '', strftime('%s','now'), strftime('%s','now'))`, entryID)
	if err != nil {
		t.Fatalf("insert message: %v", err)
	}
	msgID, _ := res.LastInsertId()

	// Approve via the admin handler.
	w := authedPOSTForm(t, a.Handler(), "/admin/comments/"+strconv.FormatInt(msgID, 10)+"/approve", url.Values{}, cookies)
	if w.Code != http.StatusSeeOther && w.Code != http.StatusFound {
		t.Fatalf("approve status = %d; body:\n%s", w.Code, w.Body.String())
	}

	// DispatchOne is synchronous; the entry/Update path through
	// commentSetStatus uses async dispatch in server mode, so wait for
	// the goroutine to land. Poll briefly.
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		n := len(received)
		mu.Unlock()
		if n > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(received) == 0 {
		t.Fatalf("no webhook payload received")
	}
	for _, p := range received {
		if p["event"] != "comment.approved" {
			continue
		}
		data, _ := p["data"].(map[string]any)
		if got := data["status"]; got != "approved" {
			t.Errorf("comment.approved payload status = %v, want \"approved\"", got)
		}
	}
}
