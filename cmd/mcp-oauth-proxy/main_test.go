package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestProductionGuardsRequired(t *testing.T) {
	const loopbackBase = "http://localhost:8080"
	const loopbackAddr = "127.0.0.1:8080"
	cases := []struct {
		name             string
		baseURL          string
		listenAddr       string
		allowInsecureDev string
		want             bool
	}{
		// Pure BASE_URL axis (listen addr held loopback)
		{"loopback-everywhere", loopbackBase, loopbackAddr, "", false},
		{"baseurl-127-0-0-1", "http://127.0.0.1:8080", loopbackAddr, "", false},
		{"baseurl-ipv6-loopback", "http://[::1]:8080", loopbackAddr, "", false},
		{"baseurl-public-without-override", "https://mcp.example.com", loopbackAddr, "", true},
		{"baseurl-public-with-override", "https://mcp.example.com", loopbackAddr, "1", false},
		{"baseurl-empty-fails-safe", "", loopbackAddr, "", true},
		{"baseurl-unparseable-fails-safe", "::not a url::", loopbackAddr, "", true},
		{"baseurl-localhost-uppercase", "http://LOCALHOST:8080", loopbackAddr, "", false},
		// Pure listen-addr axis (BASE_URL held loopback) — covers the
		// "BASE_URL omitted but :8080 binds every interface" case the
		// reviewer surfaced on PR #97.
		{"addr-wildcard-empty-host", loopbackBase, ":8080", "", true},
		{"addr-0-0-0-0", loopbackBase, "0.0.0.0:8080", "", true},
		{"addr-ipv6-wildcard", loopbackBase, "[::]:8080", "", true},
		{"addr-localhost", loopbackBase, "localhost:8080", "", false},
		{"addr-ipv6-loopback", loopbackBase, "[::1]:8080", "", false},
		{"addr-public-ip", loopbackBase, "203.0.113.5:8080", "", true},
		{"addr-unparseable-fails-safe", loopbackBase, "garbage", "", true},
		// Override applies regardless of axis.
		{"addr-public-with-override", loopbackBase, ":8080", "1", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := productionGuardsRequired(c.baseURL, c.listenAddr, c.allowInsecureDev); got != c.want {
				t.Errorf("productionGuardsRequired(%q, %q, %q) = %v, want %v",
					c.baseURL, c.listenAddr, c.allowInsecureDev, got, c.want)
			}
		})
	}
}

func TestIsLoopbackListenAddr(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{"127.0.0.1:8080", true},
		{"localhost:8080", true},
		{"LOCALHOST:8080", true},
		{"[::1]:8080", true},
		{":8080", false},
		{"0.0.0.0:8080", false},
		{"[::]:8080", false},
		{"203.0.113.5:8080", false},
		{"example.com:8080", false},
		{"", false},
		{"no-port", false},
	}
	for _, c := range cases {
		t.Run(c.addr, func(t *testing.T) {
			if got := isLoopbackListenAddr(c.addr); got != c.want {
				t.Errorf("isLoopbackListenAddr(%q) = %v, want %v", c.addr, got, c.want)
			}
		})
	}
}

func TestValidateProductionConfig(t *testing.T) {
	if err := validateProductionConfig("", []string{"https://chatgpt.com/cb"}); err == nil {
		t.Error("expected error when AUTH_PIN is empty")
	}
	if err := validateProductionConfig("1234", nil); err == nil {
		t.Error("expected error when OAUTH_REDIRECT_URIS is empty")
	}
	if err := validateProductionConfig("1234", []string{}); err == nil {
		t.Error("expected error when OAUTH_REDIRECT_URIS is empty slice")
	}
	if err := validateProductionConfig("1234", []string{"https://chatgpt.com/cb"}); err != nil {
		t.Errorf("unexpected error for valid config: %v", err)
	}
}

// withAuthorizeGlobals overrides the package-level config used by
// handleAuthorize for the duration of a test and restores the previous
// values on cleanup. handleAuthorize reads global state directly so we
// have to mutate these vars rather than passing values in.
func withAuthorizeGlobals(t *testing.T, pin, clientID string, allowed []string) {
	t.Helper()
	prevPIN, prevAllowed, prevClient := authPIN, allowedRedirectURIs, oauthClientID
	t.Cleanup(func() {
		authPIN, allowedRedirectURIs, oauthClientID = prevPIN, prevAllowed, prevClient
	})
	authPIN, allowedRedirectURIs, oauthClientID = pin, allowed, clientID
}

func newAuthorizePINPost(t *testing.T, form url.Values) *http.Request {
	t.Helper()
	// The original query (rendered by the GET path) carries the canonical
	// values; the POST body is what the attacker would try to forge.
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", "test-client")
	q.Set("redirect_uri", "https://chatgpt.com/callback")
	q.Set("code_challenge", "ch")
	q.Set("code_challenge_method", "S256")
	req := httptest.NewRequest(http.MethodPost, "/authorize?"+q.Encode(), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

func TestHandleAuthorizePINPostRejectsForeignRedirect(t *testing.T) {
	withAuthorizeGlobals(t, "1234", "test-client", []string{"https://chatgpt.com/callback"})

	form := url.Values{}
	form.Set("pin", "1234")
	form.Set("state", "abc")
	form.Set("redirect_uri", "https://attacker.example.com/cb") // outside allowlist
	form.Set("code_challenge", "ch")
	form.Set("code_challenge_method", "S256")

	w := httptest.NewRecorder()
	handleAuthorize(w, newAuthorizePINPost(t, form))

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 when PIN POST adopts a foreign redirect_uri, got %d (body=%q)",
			w.Code, w.Body.String())
	}
}

func TestHandleAuthorizePINPostRejectsDowngradedPKCE(t *testing.T) {
	withAuthorizeGlobals(t, "1234", "test-client", []string{"https://chatgpt.com/callback"})

	form := url.Values{}
	form.Set("pin", "1234")
	form.Set("state", "abc")
	form.Set("redirect_uri", "https://chatgpt.com/callback")
	form.Set("code_challenge", "ch")
	form.Set("code_challenge_method", "plain") // downgrade attempt

	w := httptest.NewRecorder()
	handleAuthorize(w, newAuthorizePINPost(t, form))

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 when PIN POST downgrades PKCE method, got %d (body=%q)",
			w.Code, w.Body.String())
	}
}

func TestHandleAuthorizePINPostRejectsEmptyRedirect(t *testing.T) {
	withAuthorizeGlobals(t, "1234", "test-client", []string{"https://chatgpt.com/callback"})

	form := url.Values{}
	form.Set("pin", "1234")
	form.Set("state", "abc")
	form.Set("redirect_uri", "")
	form.Set("code_challenge", "ch")
	form.Set("code_challenge_method", "S256")

	w := httptest.NewRecorder()
	handleAuthorize(w, newAuthorizePINPost(t, form))

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 when PIN POST clears redirect_uri, got %d (body=%q)",
			w.Code, w.Body.String())
	}
}

func TestHandleAuthorizePINPostSuccess(t *testing.T) {
	withAuthorizeGlobals(t, "1234", "test-client", []string{"https://chatgpt.com/callback"})

	form := url.Values{}
	form.Set("pin", "1234")
	form.Set("state", "abc")
	form.Set("redirect_uri", "https://chatgpt.com/callback")
	form.Set("code_challenge", "ch")
	form.Set("code_challenge_method", "S256")

	w := httptest.NewRecorder()
	handleAuthorize(w, newAuthorizePINPost(t, form))

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302 on valid PIN POST, got %d (body=%q)", w.Code, w.Body.String())
	}
	loc := w.Header().Get("Location")
	if !strings.HasPrefix(loc, "https://chatgpt.com/callback?") {
		t.Errorf("unexpected Location: %s", loc)
	}
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if u.Query().Get("code") == "" {
		t.Error("expected code= in redirect, got empty")
	}
	if u.Query().Get("state") != "abc" {
		t.Errorf("expected state=abc, got %q", u.Query().Get("state"))
	}
}

func TestHandleAuthorizeGetRejectsForeignRedirect(t *testing.T) {
	// Confirm the existing GET-path guard still rejects foreign redirects
	// when an allowlist is configured. Regression coverage paired with the
	// PIN POST tests above.
	withAuthorizeGlobals(t, "", "test-client", []string{"https://chatgpt.com/callback"})

	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", "test-client")
	q.Set("redirect_uri", "https://attacker.example.com/cb")
	q.Set("code_challenge", "ch")
	q.Set("code_challenge_method", "S256")
	req := httptest.NewRequest(http.MethodGet, "/authorize?"+q.Encode(), nil)
	w := httptest.NewRecorder()
	handleAuthorize(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for foreign redirect on GET, got %d", w.Code)
	}
}
