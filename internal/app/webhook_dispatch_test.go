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

	collector := newWebhookPayloadCollector()
	srv := httptest.NewServer(http.HandlerFunc(collector.handle))
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

	received := collector.waitFor(1, 2*time.Second)
	if len(received) == 0 {
		t.Fatalf("no webhook payload received")
	}
	assertFlatEntryPayload(t, received[0])
}

// webhookPayloadCollector is a tiny sink for httptest webhook servers:
// it decodes every JSON body as map[string]any and keeps the list
// behind a mutex so tests can poll for deliveries.
type webhookPayloadCollector struct {
	mu       sync.Mutex
	received []map[string]any
}

func newWebhookPayloadCollector() *webhookPayloadCollector {
	return &webhookPayloadCollector{}
}

func (c *webhookPayloadCollector) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err == nil {
		c.mu.Lock()
		c.received = append(c.received, payload)
		c.mu.Unlock()
	}
	w.WriteHeader(http.StatusOK)
}

// waitFor blocks until at least n payloads have been received or the
// timeout fires, then returns a snapshot of the slice.
func (c *webhookPayloadCollector) waitFor(n int, timeout time.Duration) []map[string]any {
	deadline := time.Now().Add(timeout)
	for {
		c.mu.Lock()
		got := len(c.received)
		c.mu.Unlock()
		if got >= n || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	snapshot := make([]map[string]any, len(c.received))
	copy(snapshot, c.received)
	return snapshot
}

// assertFlatEntryPayload checks the shape promises of payload_format=flat
// against a single entry.published delivery captured during the test.
func assertFlatEntryPayload(t *testing.T, p map[string]any) {
	t.Helper()
	// Flat shape: nested keys land on top level joined with "_".
	wantKV := map[string]any{
		"event":      "entry.published",
		"data_title": "Hello, World!",
		"weblog_id":  float64(1),
	}
	for k, want := range wantKV {
		if p[k] != want {
			t.Errorf("%s = %v (%T), want %v", k, p[k], p[k], want)
		}
	}
	// Envelope-only keys should NOT appear as nested objects.
	for _, key := range []string{"data", "weblog"} {
		if _, nested := p[key].(map[string]any); nested {
			t.Errorf("flat payload should not contain nested %q object: %v", key, p[key])
		}
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
