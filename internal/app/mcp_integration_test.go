package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/serendipitynz/serenebach/internal/app"
	"github.com/serendipitynz/serenebach/internal/mcp"
)

// rpcClient speaks JSON-RPC to an mcp.Server over a pair of in-memory
// pipes. Tests enqueue requests via `call` and read decoded responses
// back; notifications short-circuit via `notify`.
type rpcClient struct {
	t      *testing.T
	in     io.Writer
	out    *safeBuffer
	nextID int64
}

// safeBuffer is a mutex-guarded bytes.Buffer so the server goroutine
// can Write while the test goroutine reads / advances the read cursor
// without tripping -race. The production stdio path uses os.Stdout
// which is kernel-serialised, so this indirection only exists for
// the in-process test harness.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) readFirstLine() ([]byte, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	raw := b.buf.Bytes()
	idx := bytes.IndexByte(raw, '\n')
	if idx < 0 {
		return nil, false
	}
	line := append([]byte(nil), raw[:idx]...)
	b.buf.Next(idx + 1)
	return line, true
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func startMCPServer(t *testing.T, a *app.App) *rpcClient {
	t.Helper()
	clientOut, serverIn := io.Pipe()
	serverOut := &safeBuffer{}

	srv := &mcp.Server{
		Store: a.Store,
		WID:   1,
		In:    clientOut,
		Out:   serverOut,
	}
	done := make(chan struct{})
	go func() {
		_ = srv.Serve(context.Background())
		close(done)
	}()
	t.Cleanup(func() {
		_ = serverIn.Close() // triggers EOF, Serve returns
		<-done
	})
	return &rpcClient{
		t:   t,
		in:  serverIn,
		out: serverOut,
	}
}

// call sends a request and reads exactly one JSON response back. The
// server writes newline-terminated JSON, so we look for the first
// newline after the request is flushed and decode it.
func (c *rpcClient) call(method string, params any) map[string]any {
	c.t.Helper()
	c.nextID++
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      c.nextID,
		"method":  method,
	}
	if params != nil {
		req["params"] = params
	}
	payload, err := json.Marshal(req)
	if err != nil {
		c.t.Fatalf("marshal: %v", err)
	}
	payload = append(payload, '\n')
	if _, err := c.in.Write(payload); err != nil {
		c.t.Fatalf("write: %v", err)
	}
	// Server responses come through a bytes.Buffer; poll until we have
	// a line (server runs on another goroutine).
	line := c.readLine()
	var resp map[string]any
	if err := json.Unmarshal(line, &resp); err != nil {
		c.t.Fatalf("decode response %q: %v", line, err)
	}
	return resp
}

// readLine waits for the server goroutine to flush at least one full
// response line into the shared buffer. The safeBuffer takes a mutex
// on every access so the server Write and the test read can't race
// under -race.
func (c *rpcClient) readLine() []byte {
	c.t.Helper()
	for i := 0; i < 200; i++ {
		if line, ok := c.out.readFirstLine(); ok {
			return line
		}
		// busy-wait briefly; test runs are local + fast
		time.Sleep(5 * time.Millisecond)
	}
	c.t.Fatalf("timed out waiting for server response; buffer: %q", c.out.String())
	return nil
}

func TestMCPInitializeAndToolsList(t *testing.T) {
	a := newTestApp(t)
	c := startMCPServer(t, a)

	resp := c.call("initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "test", "version": "0"},
	})
	if resp["error"] != nil {
		t.Fatalf("initialize returned error: %v", resp["error"])
	}
	result := resp["result"].(map[string]any)
	if result["protocolVersion"] == nil {
		t.Errorf("initialize result missing protocolVersion")
	}
	if server, ok := result["serverInfo"].(map[string]any); !ok || server["name"] != "serenebach" {
		t.Errorf("serverInfo.name = %v, want serenebach", server)
	}

	// tools/list
	list := c.call("tools/list", nil)
	tools := list["result"].(map[string]any)["tools"].([]any)
	if len(tools) < 5 {
		t.Fatalf("expected at least 5 tools, got %d", len(tools))
	}
	names := map[string]bool{}
	for _, t := range tools {
		m := t.(map[string]any)
		names[m["name"].(string)] = true
	}
	for _, want := range []string{"list_entries", "get_entry", "search_entries", "list_categories", "list_tags"} {
		if !names[want] {
			t.Errorf("tools/list missing %q", want)
		}
	}
}

// parseToolText extracts the JSON text payload from a tools/call
// response so each tool's assertions can work on structured data.
func parseToolText(t *testing.T, resp map[string]any) map[string]any {
	t.Helper()
	if resp["error"] != nil {
		t.Fatalf("rpc error: %v", resp["error"])
	}
	result := resp["result"].(map[string]any)
	if isErr, _ := result["isError"].(bool); isErr {
		t.Fatalf("tool returned isError; content: %v", result["content"])
	}
	content := result["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	var out map[string]any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("decode tool text: %v; got %q", err, text)
	}
	return out
}

func TestMCPListEntries(t *testing.T) {
	a := newTestApp(t)
	c := startMCPServer(t, a)
	resp := c.call("tools/call", map[string]any{
		"name":      "list_entries",
		"arguments": map[string]any{"limit": 50},
	})
	data := parseToolText(t, resp)
	entries := data["entries"].([]any)
	if len(entries) == 0 {
		t.Fatalf("expected seeded entries in list_entries, got 0")
	}
	first := entries[0].(map[string]any)
	for _, want := range []string{"id", "title", "status", "format", "posted_at"} {
		if _, ok := first[want]; !ok {
			t.Errorf("entry summary missing field %q", want)
		}
	}
	if first["status"].(string) != "published" {
		t.Errorf("list_entries should only show published; got status=%v", first["status"])
	}
}

func TestMCPGetEntryByIDAndSlug(t *testing.T) {
	a := newTestApp(t)
	c := startMCPServer(t, a)

	var id int64
	var slug string
	if err := a.DB.QueryRow(`SELECT id, slug FROM entries WHERE status = 1 LIMIT 1`).Scan(&id, &slug); err != nil {
		t.Fatal(err)
	}

	// by id
	resp := c.call("tools/call", map[string]any{
		"name":      "get_entry",
		"arguments": map[string]any{"id": id},
	})
	data := parseToolText(t, resp)
	if int64(data["id"].(float64)) != id {
		t.Errorf("by-id lookup: got id=%v, want %d", data["id"], id)
	}
	if _, ok := data["body"]; !ok {
		t.Errorf("get_entry should include body field")
	}

	// by slug (when seeded entries have one)
	if slug != "" {
		resp2 := c.call("tools/call", map[string]any{
			"name":      "get_entry",
			"arguments": map[string]any{"slug": slug},
		})
		data2 := parseToolText(t, resp2)
		if int64(data2["id"].(float64)) != id {
			t.Errorf("by-slug lookup: got id=%v, want %d", data2["id"], id)
		}
	}
}

func TestMCPGetEntryMissingReturnsIsError(t *testing.T) {
	a := newTestApp(t)
	c := startMCPServer(t, a)
	resp := c.call("tools/call", map[string]any{
		"name":      "get_entry",
		"arguments": map[string]any{"id": 999999},
	})
	result := resp["result"].(map[string]any)
	if isErr, _ := result["isError"].(bool); !isErr {
		t.Errorf("expected isError=true for missing entry; result=%v", result)
	}
}

func TestMCPSearchEntries(t *testing.T) {
	a := newTestApp(t)
	c := startMCPServer(t, a)
	// Pick a word from a seeded entry's title so we know the match exists.
	var needle string
	if err := a.DB.QueryRow(`SELECT title FROM entries WHERE status = 1 LIMIT 1`).Scan(&needle); err != nil {
		t.Fatal(err)
	}
	// Use the first whitespace-bounded word to keep the LIKE cheap.
	if sp := strings.IndexAny(needle, " \t"); sp > 0 {
		needle = needle[:sp]
	}
	if needle == "" {
		t.Skip("seeded entry had no usable title word for search")
	}
	resp := c.call("tools/call", map[string]any{
		"name":      "search_entries",
		"arguments": map[string]any{"query": needle},
	})
	data := parseToolText(t, resp)
	if matched, _ := data["matched"].(float64); matched < 1 {
		t.Errorf("search_entries returned 0 matches for %q; data=%v", needle, data)
	}
}

func TestMCPListCategoriesAndTags(t *testing.T) {
	a := newTestApp(t)
	c := startMCPServer(t, a)

	cats := parseToolText(t, c.call("tools/call", map[string]any{
		"name":      "list_categories",
		"arguments": map[string]any{},
	}))
	if _, ok := cats["categories"]; !ok {
		t.Errorf("list_categories result missing `categories` key")
	}

	tags := parseToolText(t, c.call("tools/call", map[string]any{
		"name":      "list_tags",
		"arguments": map[string]any{},
	}))
	if _, ok := tags["tags"]; !ok {
		t.Errorf("list_tags result missing `tags` key")
	}
}

func TestMCPUnknownMethodReturnsError(t *testing.T) {
	a := newTestApp(t)
	c := startMCPServer(t, a)
	resp := c.call("unknown/method", nil)
	if resp["error"] == nil {
		t.Errorf("expected JSON-RPC error for unknown method; got %v", resp)
	}
}
