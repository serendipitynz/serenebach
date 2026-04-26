package app_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestLLMsRoutes404WhenDisabled(t *testing.T) {
	a := newTestApp(t)
	// Seed default: llms_enabled = 0. Neither route exposes content.
	for _, path := range []string{"/llms.txt", "/llms-full.txt"} {
		req := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Errorf("%s status = %d when disabled, want 404", path, w.Code)
		}
	}
}

func TestLLMsRoutes200WhenEnabled(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.DB.Exec(`UPDATE weblogs SET llms_enabled = 1 WHERE id = 1`); err != nil {
		t.Fatal(err)
	}

	// /llms.txt
	req := httptest.NewRequest("GET", "/llms.txt", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("/llms.txt status = %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("/llms.txt content-type = %q, want text/plain*", ct)
	}
	body := w.Body.String()
	if !strings.HasPrefix(body, "# ") {
		t.Errorf("/llms.txt should start with a Markdown H1; got:\n%s", body[:min2(200, len(body))])
	}
	if !strings.Contains(body, "## Recent posts") {
		t.Errorf("/llms.txt should include the Recent posts section")
	}

	// /llms-full.txt
	req = httptest.NewRequest("GET", "/llms-full.txt", nil)
	w = httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("/llms-full.txt status = %d", w.Code)
	}
	body = w.Body.String()
	if !strings.HasPrefix(body, "# ") {
		t.Errorf("/llms-full.txt should start with a Markdown H1")
	}
	if !strings.Contains(body, "## ") {
		t.Errorf("/llms-full.txt should include H2 entry headings")
	}
}

func TestAdminSettingsTogglePersistsLLMSEnabled(t *testing.T) {
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	// Form should render the toggle radios on the 基本設定 tab.
	w := authedGET(t, a.Handler(), "/admin/settings/basic", cookies)
	if !strings.Contains(w.Body.String(), `name="llms_enabled"`) {
		t.Errorf("settings form missing llms_enabled radios")
	}

	// Submit with llms_enabled=1.
	form := url.Values{
		"title":        {"T"},
		"description":  {"d"},
		"base_url":     {"https://example.com/"},
		"lang":         {"ja"},
		"llms_enabled": {"1"},
	}
	res := authedPOSTForm(t, a.Handler(), "/admin/settings/basic", form, cookies)
	if res.Code != http.StatusFound {
		t.Fatalf("save status = %d; body:\n%s", res.Code, res.Body.String())
	}
	var v int
	_ = a.DB.QueryRow(`SELECT llms_enabled FROM weblogs WHERE id = 1`).Scan(&v)
	if v != 1 {
		t.Errorf("llms_enabled = %d after save, want 1", v)
	}

	// Flip back to 0.
	form.Set("llms_enabled", "0")
	res = authedPOSTForm(t, a.Handler(), "/admin/settings/basic", form, cookies)
	if res.Code != http.StatusFound {
		t.Fatalf("save-off status = %d", res.Code)
	}
	_ = a.DB.QueryRow(`SELECT llms_enabled FROM weblogs WHERE id = 1`).Scan(&v)
	if v != 0 {
		t.Errorf("llms_enabled = %d after toggle-off, want 0", v)
	}
}

func min2(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestMCPHTTPGetAnalyticsAndListImages(t *testing.T) {
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	token, _ := issueMCPToken(t, a, cookies, "analytics-probe")

	// get_analytics
	w := postMCP(t, a.Handler(), token, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{
			"name":      "get_analytics",
			"arguments": map[string]any{"days": 7, "top": 5},
		},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("get_analytics status = %d; body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String() // The window_days field is inside a quoted JSON string, so the
	// body carries the backslash-escaped form — just look for the
	// field name without quotes.
	if !strings.Contains(body, "window_days") {
		t.Errorf("get_analytics response missing window_days; got:\n%s", body)
	}

	// list_images
	w = postMCP(t, a.Handler(), token, map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]any{
			"name":      "list_images",
			"arguments": map[string]any{"limit": 5},
		},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("list_images status = %d", w.Code)
	}
	body = w.Body.String()
	if !strings.Contains(body, "images") {
		t.Errorf("list_images response missing images key; got:\n%s", body)
	}
}
