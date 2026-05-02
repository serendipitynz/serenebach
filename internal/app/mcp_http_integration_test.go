package app_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/serendipitynz/serenebach/internal/app"
)

// postMCP issues a JSON-RPC POST to /mcp with optional Bearer token.
// Returns the recorder so the test can assert Code / body.
func postMCP(t *testing.T, h http.Handler, token string, req map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	r := httptest.NewRequest("POST", "/mcp", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// issueMCPToken runs the full admin UI flow (POST /admin/settings/mcp/new)
// and scrapes the newly-minted raw token out of the rendered
// "new token" panel. Returns the raw token + its DB id. Defaults to
// read scope — call issueMCPTokenWithScope for write-scope mints.
func issueMCPToken(t *testing.T, a *app.App, cookies []*http.Cookie, name string) (string, int64) {
	return issueMCPTokenWithScope(t, a, cookies, name, "read")
}

func issueMCPTokenWithScope(t *testing.T, a *app.App, cookies []*http.Cookie, name, scope string) (string, int64) {
	return issueMCPTokenFull(t, a, cookies, name, scope, 1)
}

// issueMCPTokenFull is the most specific variant: picks the scope +
// the author_id the token is bound to. Existing callers default
// author_id=1 (seed admin).
func issueMCPTokenFull(t *testing.T, a *app.App, cookies []*http.Cookie, name, scope string, authorID int64) (string, int64) {
	t.Helper()
	w := authedPOSTForm(t, a.Handler(), "/admin/settings/mcp/new",
		url.Values{
			"name":      {name},
			"scope":     {scope},
			"author_id": {strconv.FormatInt(authorID, 10)},
		}, cookies)
	if w.Code != 200 {
		t.Fatalf("issue token status = %d, body:\n%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	start := strings.Index(body, `data-mcp-new-token>`)
	if start < 0 {
		t.Fatalf("new-token panel not found in response body; body:\n%s", body)
	}
	tail := body[start+len("data-mcp-new-token>"):]
	end := strings.Index(tail, "</code>")
	if end < 0 {
		t.Fatalf("new-token panel malformed")
	}
	raw := strings.TrimSpace(tail[:end])
	if !strings.HasPrefix(raw, "sb_mcp_") {
		t.Fatalf("new token lacks sb_mcp_ prefix: %q", raw)
	}
	var id int64
	_ = a.DB.QueryRow(`SELECT id FROM mcp_tokens WHERE name = ?`, name).Scan(&id)
	if id == 0 {
		t.Fatalf("token %q not found in DB after create", name)
	}
	return raw, id
}

func TestMCPHTTPRequiresBearerToken(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)

	// No Authorization header → 401.
	w := postMCP(t, a.Handler(), "", map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{},
	})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("no-token status = %d, want 401; body=%s", w.Code, w.Body.String())
	}

	// Bogus token → 401.
	w = postMCP(t, a.Handler(), "sb_mcp_not_a_real_token", map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "initialize",
	})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("bad-token status = %d, want 401", w.Code)
	}
}

func TestMCPHTTPWithValidTokenHandshake(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	token, _ := issueMCPToken(t, a, cookies, "test-laptop")

	// initialize
	w := postMCP(t, a.Handler(), token, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{"protocolVersion": "2024-11-05"},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("initialize status = %d; body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["error"] != nil {
		t.Fatalf("initialize returned error: %v", resp["error"])
	}

	// tools/list should return 5 tools
	w = postMCP(t, a.Handler(), token, map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/list",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("tools/list status = %d", w.Code)
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	result := resp["result"].(map[string]any)
	tools := result["tools"].([]any)
	if len(tools) < 5 {
		t.Errorf("expected ≥5 tools, got %d", len(tools))
	}
}

func TestMCPHTTPCallListEntries(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	token, _ := issueMCPToken(t, a, cookies, "list-probe")

	w := postMCP(t, a.Handler(), token, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{
			"name":      "list_entries",
			"arguments": map[string]any{"limit": 5},
		},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("tools/call status = %d; body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	result := resp["result"].(map[string]any)
	if isErr, _ := result["isError"].(bool); isErr {
		t.Fatalf("tools/call isError; content=%v", result["content"])
	}
	content := result["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, `"entries"`) {
		t.Errorf("response text should be a JSON object with `entries`; got %q", text)
	}
}

func TestMCPHTTPRevokedTokenRejected(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	token, id := issueMCPToken(t, a, cookies, "revokable")

	// Revoke via admin endpoint.
	rev := authedPOSTForm(t, a.Handler(), "/admin/settings/mcp/"+itoa64(id)+"/revoke",
		url.Values{}, cookies)
	if rev.Code != http.StatusSeeOther {
		t.Fatalf("revoke status = %d", rev.Code)
	}

	// Now the same token must be rejected.
	w := postMCP(t, a.Handler(), token, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
	})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("revoked-token status = %d, want 401", w.Code)
	}
}

func TestMCPHTTPTouchesLastUsedAt(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	token, id := issueMCPToken(t, a, cookies, "last-used-probe")

	// last_used_at should start at 0 before any /mcp call.
	var before int64
	_ = a.DB.QueryRow(`SELECT last_used_at FROM mcp_tokens WHERE id = ?`, id).Scan(&before)
	if before != 0 {
		t.Fatalf("expected last_used_at=0 before first call, got %d", before)
	}
	w := postMCP(t, a.Handler(), token, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/list",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("call status = %d", w.Code)
	}
	var after int64
	_ = a.DB.QueryRow(`SELECT last_used_at FROM mcp_tokens WHERE id = ?`, id).Scan(&after)
	if after == 0 {
		t.Errorf("last_used_at should advance after successful call")
	}
}

func TestAdminMCPTokensUIRequiresAuth(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	// Unauthenticated GET is bounced to the login page by the
	// RequireUser middleware — MCP token management is admin-only
	// territory so the route must never serve directly.
	req := httptest.NewRequest("GET", "/admin/settings/mcp", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	// 302/303/307 redirect to /admin/login, or outright 401/403.
	if w.Code == http.StatusOK {
		t.Errorf("unauth request should not reach MCP settings; status=%d", w.Code)
	}
}
