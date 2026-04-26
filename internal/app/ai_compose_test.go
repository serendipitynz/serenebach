package app_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// fakeComposeServer returns a canned text reply for any
// chat-completions request. Lets the compose integration test drive
// /admin/ai/compose end-to-end without a live model.
func fakeComposeServer(t *testing.T, reply string) (*httptest.Server, *[]byte) {
	t.Helper()
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captured = append(captured[:0], body...)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"choices":[{"message":{"role":"assistant","content":"` + reply + `"}}],
			"usage":{"prompt_tokens":20,"completion_tokens":8,"total_tokens":28}
		}`))
	}))
	return srv, &captured
}

func configureAIForTest(t *testing.T, h http.Handler, cookies []*http.Cookie, baseURL, model string) {
	t.Helper()
	w := authedPOSTForm(t, h, "/admin/settings/ai", url.Values{
		"ai_enabled":  {"on"},
		"ai_kind":     {"openai-compat"},
		"ai_base_url": {baseURL},
		"ai_model":    {model},
	}, cookies)
	if w.Code != 302 {
		t.Fatalf("configure AI = %d, body=%s", w.Code, w.Body.String())
	}
}

func postJSON(t *testing.T, h http.Handler, path string, payload any, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(payload)
	r := httptest.NewRequest("POST", path, bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if _, tok := csrfFromJar(cookies); tok != "" {
		r.Header.Set("X-CSRF-Token", tok)
	}
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestAIComposeRewriteRoundtrip(t *testing.T) {
	t.Setenv("SB_AI_SECRET", "test-secret-ai-compose")

	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	fake, captured := fakeComposeServer(t, "Rewritten passage.")
	defer fake.Close()
	configureAIForTest(t, a.Handler(), cookies, fake.URL, "fake-model")

	w := postJSON(t, a.Handler(), "/admin/ai/compose", map[string]any{
		"action":   "rewrite",
		"text":     "original passage that needs polishing",
		"format":   "markdown",
		"language": "en",
	}, cookies)
	if w.Code != 200 {
		t.Fatalf("compose = %d; body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if ok, _ := resp["ok"].(bool); !ok {
		t.Fatalf("response !ok: %v", resp)
	}
	if got, _ := resp["text"].(string); !strings.Contains(got, "Rewritten") {
		t.Errorf("text = %q", got)
	}

	// Request body the fake server saw should carry the original
	// text as the user message — otherwise the provider never got
	// the passage to work on.
	if !bytes.Contains(*captured, []byte("original passage")) {
		t.Errorf("provider body missing user text; got:\n%s", *captured)
	}

	// Audit: one ai.rewrite row.
	var count int
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM mcp_audit_log WHERE tool = 'ai.rewrite'`).Scan(&count)
	if count != 1 {
		t.Errorf("audit count = %d, want 1", count)
	}
}

func TestAIComposeRewriteRequiresSelection(t *testing.T) {
	t.Setenv("SB_AI_SECRET", "test-secret-compose-sel")

	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	fake, _ := fakeComposeServer(t, "irrelevant")
	defer fake.Close()
	configureAIForTest(t, a.Handler(), cookies, fake.URL, "fake-model")

	w := postJSON(t, a.Handler(), "/admin/ai/compose", map[string]any{
		"action": "rewrite",
		"text":   "",
	}, cookies)
	if w.Code != 400 {
		t.Fatalf("expected 400 for empty rewrite, got %d", w.Code)
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if got, _ := resp["error"].(string); got != "selection_required" {
		t.Errorf("error = %q, want selection_required", got)
	}
}

func TestAIComposeTitleSuggestion(t *testing.T) {
	t.Setenv("SB_AI_SECRET", "test-secret-compose-title")

	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	fake, _ := fakeComposeServer(t, "Test driven development in Go")
	defer fake.Close()
	configureAIForTest(t, a.Handler(), cookies, fake.URL, "fake-model")

	w := postJSON(t, a.Handler(), "/admin/ai/compose", map[string]any{
		"action":   "title",
		"text":     "A post about writing tests before implementation in Go, using the standard testing package.",
		"language": "en",
	}, cookies)
	if w.Code != 200 {
		t.Fatalf("title suggest = %d", w.Code)
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if got, _ := resp["text"].(string); got == "" {
		t.Errorf("expected non-empty title, got %q", got)
	}
}

func TestAIComposeTagsSuggestion(t *testing.T) {
	t.Setenv("SB_AI_SECRET", "test-secret-compose-tags")

	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	fake, _ := fakeComposeServer(t, "go, testing, tdd")
	defer fake.Close()
	configureAIForTest(t, a.Handler(), cookies, fake.URL, "fake-model")

	w := postJSON(t, a.Handler(), "/admin/ai/compose", map[string]any{
		"action":   "tags",
		"text":     "Content about TDD in Go",
		"language": "en",
	}, cookies)
	if w.Code != 200 {
		t.Fatalf("tags suggest = %d", w.Code)
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if got, _ := resp["text"].(string); !strings.Contains(got, "go") {
		t.Errorf("expected tags containing 'go', got %q", got)
	}
}

func TestAIComposeUnconfiguredReturnsStableCode(t *testing.T) {
	t.Setenv("SB_AI_SECRET", "test-secret-compose-unconfig")

	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	w := postJSON(t, a.Handler(), "/admin/ai/compose", map[string]any{
		"action": "continue",
		"text":   "some text",
	}, cookies)
	if w.Code != 400 {
		t.Fatalf("expected 400 for unconfigured, got %d", w.Code)
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if got, _ := resp["error"].(string); got != "unconfigured" {
		t.Errorf("error = %q, want unconfigured", got)
	}
}
