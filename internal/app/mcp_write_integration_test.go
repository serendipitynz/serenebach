package app_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"strings"
	"testing"
)

// helper: POST /mcp JSON-RPC and return the unmarshalled envelope.
func callMCP(t *testing.T, h http.Handler, token string, id int, method string, params map[string]any) map[string]any {
	t.Helper()
	req := map[string]any{
		"jsonrpc": "2.0", "id": id, "method": method,
	}
	if params != nil {
		req["params"] = params
	}
	w := postMCP(t, h, token, req)
	if w.Code != http.StatusOK {
		t.Fatalf("%s status = %d, body=%s", method, w.Code, w.Body.String())
	}
	var env map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, w.Body.String())
	}
	return env
}

// callTool wraps callMCP for the common tools/call method.
func callTool(t *testing.T, h http.Handler, token, name string, args map[string]any) map[string]any {
	t.Helper()
	return callMCP(t, h, token, 99, "tools/call", map[string]any{
		"name": name, "arguments": args,
	})
}

// toolCallResult pulls the first text content string out of a
// tools/call result envelope. Fails the test if isError is set so
// callers can assert on a successful call compactly.
func toolCallResult(t *testing.T, env map[string]any) string {
	t.Helper()
	result, _ := env["result"].(map[string]any)
	if result == nil {
		t.Fatalf("response has no result: %v", env)
	}
	if isErr, _ := result["isError"].(bool); isErr {
		t.Fatalf("tool reported error: %v", result["content"])
	}
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		t.Fatalf("result has no content: %v", result)
	}
	first, _ := content[0].(map[string]any)
	text, _ := first["text"].(string)
	return text
}

// toolCallErrorText returns the tool-error text when isError=true,
// empty otherwise. Used by scope-enforcement tests to assert the
// write tools refuse to run with a read-only token.
func toolCallErrorText(env map[string]any) string {
	result, _ := env["result"].(map[string]any)
	if result == nil {
		return ""
	}
	if isErr, _ := result["isError"].(bool); !isErr {
		return ""
	}
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		return ""
	}
	first, _ := content[0].(map[string]any)
	text, _ := first["text"].(string)
	return text
}

func TestMCPReadTokenCannotCallWriteTools(t *testing.T) {
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	token, _ := issueMCPToken(t, a, cookies, "read-only-probe")

	env := callTool(t, a.Handler(), token, "create_entry", map[string]any{
		"title": "nope", "body": "should never persist",
	})
	msg := toolCallErrorText(env)
	if !strings.Contains(msg, "requires write scope") {
		t.Fatalf("expected write-scope rejection, got %q", msg)
	}

	// Full-catalogue audit: tools/list must NOT expose write tools to a
	// read token, preventing the agent from even trying.
	env = callMCP(t, a.Handler(), token, 1, "tools/list", nil)
	result, _ := env["result"].(map[string]any)
	tools, _ := result["tools"].([]any)
	for _, tool := range tools {
		m, _ := tool.(map[string]any)
		name, _ := m["name"].(string)
		if name == "create_entry" || name == "update_entry" || name == "publish_entry" {
			t.Errorf("read-scope tools/list leaked write tool %q", name)
		}
	}
}

func TestMCPWriteTokenRoundtripsCreateUpdatePublish(t *testing.T) {
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	token, _ := issueMCPTokenWithScope(t, a, cookies, "write-probe", "write")

	// 1. create_entry — draft by default.
	env := callTool(t, a.Handler(), token, "create_entry", map[string]any{
		"title":  "From the MCP write test",
		"body":   "Hello from the MCP write test.",
		"format": "html",
		"tags":   []string{"mcp-write", "integration"},
	})
	created := parseEntryJSON(t, toolCallResult(t, env))
	if created.ID == 0 {
		t.Fatalf("create_entry returned zero id: %+v", created)
	}
	if created.Status != "draft" {
		t.Errorf("new entry status = %q, want draft", created.Status)
	}
	if len(created.Tags) != 2 {
		t.Errorf("tag count = %d, want 2 (got %v)", len(created.Tags), created.Tags)
	}

	// 2. update_entry — swap title + clear tags.
	env = callTool(t, a.Handler(), token, "update_entry", map[string]any{
		"id":    created.ID,
		"title": "From MCP, revised",
		"tags":  []string{},
	})
	updated := parseEntryJSON(t, toolCallResult(t, env))
	if updated.Title != "From MCP, revised" {
		t.Errorf("title not updated: %+v", updated)
	}
	if len(updated.Tags) != 0 {
		t.Errorf("tags not cleared: %v", updated.Tags)
	}

	// 3. publish_entry — status flips to published.
	env = callTool(t, a.Handler(), token, "publish_entry", map[string]any{
		"id": created.ID,
	})
	published := parseEntryJSON(t, toolCallResult(t, env))
	if published.Status != "published" {
		t.Errorf("publish_entry status = %q, want published", published.Status)
	}
}

func TestMCPCreateEntrySlugConflict(t *testing.T) {
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	token, _ := issueMCPTokenWithScope(t, a, cookies, "slug-probe", "write")

	env := callTool(t, a.Handler(), token, "create_entry", map[string]any{
		"title": "First",
		"body":  "first",
		"slug":  "dup-slug-test",
	})
	_ = toolCallResult(t, env)

	env = callTool(t, a.Handler(), token, "create_entry", map[string]any{
		"title": "Second",
		"body":  "second",
		"slug":  "dup-slug-test",
	})
	if msg := toolCallErrorText(env); !strings.Contains(msg, "already in use") {
		t.Fatalf("expected slug conflict error, got %q", msg)
	}
}

func TestMCPUpdateEntryNotFound(t *testing.T) {
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	token, _ := issueMCPTokenWithScope(t, a, cookies, "nf-probe", "write")

	env := callTool(t, a.Handler(), token, "update_entry", map[string]any{
		"id":    999999,
		"title": "ghost",
	})
	if msg := toolCallErrorText(env); !strings.Contains(msg, "not found") {
		t.Fatalf("expected not-found error, got %q", msg)
	}
}

func TestMCPCreateEntryAttributesToBoundAuthor(t *testing.T) {
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	// Seed a second user so we can tell apart "defaulted to admin" from
	// "honoured the token's bound author_id".
	var authorID int64
	res, err := a.DB.Exec(
		`INSERT INTO users (wid, name, display_name, email, password_hash, role, created_at, updated_at)
		 VALUES (1, 'agentbot', 'Agent Bot', 'bot@example.com', 'x', 2, strftime('%s','now'), strftime('%s','now'))`)
	if err != nil {
		t.Fatalf("seed bot user: %v", err)
	}
	authorID, _ = res.LastInsertId()
	if authorID <= 1 {
		t.Fatalf("unexpected bot user id %d", authorID)
	}

	token, _ := issueMCPTokenFull(t, a, cookies, "bot-bound", "write", authorID)
	env := callTool(t, a.Handler(), token, "create_entry", map[string]any{
		"title": "Drafted by the agent",
		"body":  "posted via write-scoped token",
	})
	created := parseEntryJSON(t, toolCallResult(t, env))

	var dbAuthor int64
	if err := a.DB.QueryRow(`SELECT author_id FROM entries WHERE id = ?`, created.ID).Scan(&dbAuthor); err != nil {
		t.Fatalf("load entry author: %v", err)
	}
	if dbAuthor != authorID {
		t.Fatalf("entry author_id = %d, want %d (token's bound author)", dbAuthor, authorID)
	}
}

func TestMCPUploadImageWriteScopedRoundtrip(t *testing.T) {
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	token, _ := issueMCPTokenWithScope(t, a, cookies, "upload-probe", "write")

	// Build a real 4x3 PNG so SaveUpload can decode dimensions + emit a
	// thumbnail — exercising the full path the admin UI takes.
	img := image.NewRGBA(image.Rect(0, 0, 4, 3))
	for x := 0; x < 4; x++ {
		for y := 0; y < 3; y++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 60), G: uint8(y * 80), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}

	env := callTool(t, a.Handler(), token, "upload_image", map[string]any{
		"data":     base64.StdEncoding.EncodeToString(buf.Bytes()),
		"filename": "probe.png",
	})
	text := toolCallResult(t, env)

	var resp map[string]any
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("decode upload_image result: %v; got %q", err, text)
	}
	id, _ := resp["id"].(float64)
	if id <= 0 {
		t.Fatalf("upload_image returned zero id: %v", resp)
	}
	mime, _ := resp["mime_type"].(string)
	if mime != "image/png" {
		t.Errorf("mime_type = %q, want image/png", mime)
	}
	width, _ := resp["width"].(float64)
	if int(width) != 4 {
		t.Errorf("width = %v, want 4", resp["width"])
	}
	storedURL, _ := resp["url"].(string)
	if !strings.HasPrefix(storedURL, "/img/") {
		t.Errorf("url = %q, want /img/ prefix", storedURL)
	}

	// DB row + on-disk bytes should both exist. A zero-row response means
	// the tool silently swallowed the write.
	var dbCount int
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM images WHERE id = ?`, int64(id)).Scan(&dbCount)
	if dbCount != 1 {
		t.Fatalf("images row not persisted for id=%d", int64(id))
	}
}

func TestMCPUploadImageRejectsUnsupportedMIME(t *testing.T) {
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	token, _ := issueMCPTokenWithScope(t, a, cookies, "bad-mime", "write")

	// Plain-text bytes → http.DetectContentType returns text/plain, which
	// the whitelist rejects. Exercise the default-sniff branch at the
	// same time by omitting mime_type.
	env := callTool(t, a.Handler(), token, "upload_image", map[string]any{
		"data": base64.StdEncoding.EncodeToString([]byte("hello, I am not an image")),
	})
	if msg := toolCallErrorText(env); !strings.Contains(msg, "unsupported mime") {
		t.Fatalf("expected unsupported mime error, got %q", msg)
	}
}

func TestMCPUploadImageRequiresWriteScope(t *testing.T) {
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	token, _ := issueMCPToken(t, a, cookies, "upload-read-probe")

	env := callTool(t, a.Handler(), token, "upload_image", map[string]any{
		"data": base64.StdEncoding.EncodeToString([]byte{0x89, 0x50, 0x4E, 0x47}),
	})
	if msg := toolCallErrorText(env); !strings.Contains(msg, "requires write scope") {
		t.Fatalf("expected write-scope rejection, got %q", msg)
	}
}

// entrySnapshot mirrors the JSON shape write tools return.
type entrySnapshot struct {
	ID     int64    `json:"id"`
	Title  string   `json:"title"`
	Slug   string   `json:"slug"`
	Status string   `json:"status"`
	Tags   []string `json:"tags"`
}

func parseEntryJSON(t *testing.T, s string) entrySnapshot {
	t.Helper()
	var e entrySnapshot
	if err := json.Unmarshal([]byte(s), &e); err != nil {
		t.Fatalf("decode entry payload: %v; got %q", err, s)
	}
	return e
}
