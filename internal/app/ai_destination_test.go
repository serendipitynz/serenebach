package app_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// The connection test is the shortest way for an author to make the server
// send a request somewhere, so it is where the destination rule has to
// hold: a non-admin pointing at a loopback endpoint gets refused before
// anything is sent, while an admin's local LLM keeps working.
func TestAIConnectionTestDestinationRuleByRole(t *testing.T) {
	t.Setenv("SB_AI_SECRET", "test-secret-ai-destination")

	a := newTestApp(t)
	adminCookie := login(t, a.Handler(), "admin", "changeme")
	createForm := url.Values{
		"name":             {"alice"},
		"display_name":     {"Alice"},
		"password":         {"pa55word"},
		"password_confirm": {"pa55word"},
		"email":            {"alice@example.com"},
		"role":             {"3"}, // RoleRegular
	}
	if w := authedPOSTForm(t, a.Handler(), "/admin/users/new", createForm, adminCookie); w.Code != http.StatusFound {
		t.Fatalf("user create status = %d, body:\n%s", w.Code, w.Body.String())
	}
	aliceCookie := login(t, a.Handler(), "alice", "pa55word")

	local, captured := fakeComposeServer(t, "OK")
	defer local.Close()

	connectionTest := func(cookies []*http.Cookie) map[string]any {
		t.Helper()
		configureAIForTest(t, a.Handler(), cookies, local.URL, "local-model")
		*captured = nil
		w := postJSON(t, a.Handler(), "/admin/settings/ai/test", map[string]any{}, cookies)
		if w.Code != http.StatusOK {
			t.Fatalf("connection test = %d; body=%s", w.Code, w.Body.String())
		}
		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v; body=%s", err, w.Body.String())
		}
		return resp
	}

	resp := connectionTest(aliceCookie)
	if ok, _ := resp["ok"].(bool); ok {
		t.Fatalf("non-admin reached a loopback endpoint: %v", resp)
	}
	if msg, _ := resp["error"].(string); !strings.Contains(msg, "公開アドレス") {
		t.Errorf("error = %q, want the localised destination message", msg)
	}
	if *captured != nil {
		t.Errorf("loopback endpoint received a request from a non-admin: %s", *captured)
	}

	resp = connectionTest(adminCookie)
	if ok, _ := resp["ok"].(bool); !ok {
		t.Fatalf("admin's local endpoint refused: %v", resp)
	}
	if *captured == nil {
		t.Error("admin's request never reached the local endpoint")
	}
}
