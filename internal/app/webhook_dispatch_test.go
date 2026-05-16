package app_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"
)

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
