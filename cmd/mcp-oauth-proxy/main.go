// mcp-oauth-proxy sits between ChatGPT (or any MCP client that requires
// OAuth) and an existing Serene Bach MCP server that only accepts a fixed
// Bearer token.
//
// The proxy speaks OAuth 2.0 with PKCE on the front side, and attaches a
// configured static Bearer token on the back side when forwarding JSON-RPC
// requests to the upstream Serene Bach /mcp endpoint.
//
// Required env vars:
//
//	UPSTREAM_URL       Base URL of the Serene Bach instance (e.g. https://blog.example.com)
//	MCP_BEARER_TOKEN   Static token the proxy attaches to upstream requests
//	OAUTH_CLIENT_ID    Client ID that ChatGPT is configured with
//
// Optional env vars:
//
//	PROXY_LISTEN_ADDR   Listen address (default ":8080")
//	BASE_URL            Public URL of this proxy, used in metadata (default "http://localhost:8080")
//	AUTH_PIN            If set, the /authorize page asks for this PIN before issuing a code
//	OAUTH_REDIRECT_URIS Comma-separated allowlist of redirect_uris. When empty, any uri is accepted (dev only).
//	TOKEN_TTL           Access-token lifetime (default "24h")
//
// Usage:
//
//	go run ./cmd/mcp-oauth-proxy
//	MCP_BEARER_TOKEN=sb_tok_xxx UPSTREAM_URL=https://blog.example.com OAUTH_CLIENT_ID=chatgpt_mcp go run ./cmd/mcp-oauth-proxy
package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	maxBodyBytes      = 1 << 20 // 1 MiB cap on MCP JSON-RPC payloads
	upstreamTimeout   = 30 * time.Second
	readHeaderTimeout = 10 * time.Second
	readTimeout       = 30 * time.Second
)

var (
	upstreamURL         string
	bearerToken         string
	oauthClientID       string
	baseURL             string
	authPIN             string
	tokenTTL            time.Duration
	allowedRedirectURIs []string
	upstreamClient      *http.Client
)

func main() {
	upstreamURL = os.Getenv("UPSTREAM_URL")
	bearerToken = os.Getenv("MCP_BEARER_TOKEN")
	oauthClientID = os.Getenv("OAUTH_CLIENT_ID")
	baseURL = os.Getenv("BASE_URL")
	authPIN = os.Getenv("AUTH_PIN")
	if v := os.Getenv("OAUTH_REDIRECT_URIS"); v != "" {
		allowedRedirectURIs = strings.Split(v, ",")
		for i := range allowedRedirectURIs {
			allowedRedirectURIs[i] = strings.TrimSpace(allowedRedirectURIs[i])
		}
	}

	if upstreamURL == "" || bearerToken == "" || oauthClientID == "" {
		fmt.Fprintln(os.Stderr, "env UPSTREAM_URL, MCP_BEARER_TOKEN and OAUTH_CLIENT_ID are required")
		os.Exit(1)
	}
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	tokenTTL = 24 * time.Hour
	if d, err := time.ParseDuration(os.Getenv("TOKEN_TTL")); err == nil {
		tokenTTL = d
	}

	upstreamClient = &http.Client{
		Timeout: upstreamTimeout,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/oauth-authorization-server", handleMetadata)
	mux.HandleFunc("/authorize", handleAuthorize)
	mux.HandleFunc("/token", handleToken)
	mux.HandleFunc("/mcp", handleMCPProxy)

	addr := os.Getenv("PROXY_LISTEN_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	log.Printf("mcp-oauth-proxy listening on %s", addr)
	log.Printf("upstream: %s  client_id: %s  pin_required: %v  redirect_uris: %d",
		upstreamURL, oauthClientID, authPIN != "", len(allowedRedirectURIs))

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
	}
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

// ---------------------------------------------------------------------------
// OAuth metadata
// ---------------------------------------------------------------------------

func handleMetadata(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"issuer":                                baseURL,
		"authorization_endpoint":                baseURL + "/authorize",
		"token_endpoint":                        baseURL + "/token",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"},
	})
}

// ---------------------------------------------------------------------------
// Authorization endpoint
// ---------------------------------------------------------------------------

type authCode struct {
	CodeChallenge       string
	CodeChallengeMethod string
	RedirectURI         string
	ClientID            string
	ExpiresAt           time.Time
}

var (
	codeMu    sync.RWMutex
	codeStore = map[string]*authCode{}
)

func handleAuthorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("response_type") != "code" {
		http.Error(w, "unsupported response_type", http.StatusBadRequest)
		return
	}
	if q.Get("client_id") != oauthClientID {
		http.Error(w, "invalid client_id", http.StatusBadRequest)
		return
	}
	redirectURI := q.Get("redirect_uri")
	if redirectURI == "" {
		http.Error(w, "missing redirect_uri", http.StatusBadRequest)
		return
	}
	// When an allowlist is configured, reject unknown redirect_uris.
	if len(allowedRedirectURIs) > 0 && !sliceContains(allowedRedirectURIs, redirectURI) {
		http.Error(w, "invalid redirect_uri", http.StatusBadRequest)
		return
	}
	state := q.Get("state")
	codeChallenge := q.Get("code_challenge")
	codeChallengeMethod := q.Get("code_challenge_method")
	if codeChallenge == "" || codeChallengeMethod != "S256" {
		http.Error(w, "PKCE S256 required", http.StatusBadRequest)
		return
	}

	// If an AUTH_PIN is configured, show a tiny HTML form instead of auto-approving.
	if authPIN != "" {
		if r.Method == http.MethodGet {
			renderPINForm(w, r, state, redirectURI, codeChallenge)
			return
		}
		if r.Method == http.MethodPost {
			if err := r.ParseForm(); err != nil {
				http.Error(w, "bad form", http.StatusBadRequest)
				return
			}
			if r.FormValue("pin") != authPIN {
				http.Error(w, "invalid PIN", http.StatusUnauthorized)
				return
			}
			// Adopt form values so the hidden fields carry the original query params through.
			state = r.FormValue("state")
			redirectURI = r.FormValue("redirect_uri")
			codeChallenge = r.FormValue("code_challenge")
			codeChallengeMethod = r.FormValue("code_challenge_method")
			// Fall through to code issuance below.
		} else {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
	} else if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	code := randomString(32)
	codeMu.Lock()
	codeStore[code] = &authCode{
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: codeChallengeMethod,
		RedirectURI:         redirectURI,
		ClientID:            oauthClientID,
		ExpiresAt:           time.Now().Add(10 * time.Minute),
	}
	codeMu.Unlock()

	u, _ := url.Parse(redirectURI)
	qq := u.Query()
	qq.Set("code", code)
	if state != "" {
		qq.Set("state", state)
	}
	u.RawQuery = qq.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

func renderPINForm(w http.ResponseWriter, r *http.Request, state, redirectURI, codeChallenge string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html>
<html>
<head><title>MCP OAuth Authorization</title></head>
<body style="font-family:system-ui;max-width:400px;margin:40px auto">
<h2>Authorize MCP Access</h2>
<p>Enter the PIN configured on the proxy to allow ChatGPT to access your Serene Bach blog.</p>
<form method="POST">
<input type="hidden" name="state" value="%s">
<input type="hidden" name="redirect_uri" value="%s">
<input type="hidden" name="code_challenge" value="%s">
<input type="hidden" name="code_challenge_method" value="S256">
<p><input type="password" name="pin" placeholder="PIN" style="padding:8px;width:100%%;font-size:16px"></p>
<p><button type="submit" style="padding:10px 20px;font-size:16px">Authorize</button></p>
</form>
</body>
</html>
`, htmlEsc(state), htmlEsc(redirectURI), htmlEsc(codeChallenge))
}

func htmlEsc(s string) string {
	return strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
	).Replace(s)
}

// ---------------------------------------------------------------------------
// Token endpoint
// ---------------------------------------------------------------------------

type tokenRecord struct {
	Token     string
	ExpiresAt time.Time
}

var (
	tokenMu    sync.RWMutex
	tokenStore = map[string]*tokenRecord{} // key = access token
)

func handleToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if r.FormValue("grant_type") != "authorization_code" {
		writeTokenError(w, "unsupported_grant_type", "only authorization_code is supported")
		return
	}
	codeVal := r.FormValue("code")
	redirectURI := r.FormValue("redirect_uri")
	codeVerifier := r.FormValue("code_verifier")
	clientID := r.FormValue("client_id")

	codeMu.Lock()
	ac, ok := codeStore[codeVal]
	if ok {
		delete(codeStore, codeVal)
	}
	codeMu.Unlock()

	if !ok || time.Now().After(ac.ExpiresAt) {
		writeTokenError(w, "invalid_grant", "code expired or invalid")
		return
	}
	if ac.RedirectURI != redirectURI || ac.ClientID != clientID {
		writeTokenError(w, "invalid_grant", "redirect_uri or client_id mismatch")
		return
	}
	// Verify PKCE
	sum := sha256.Sum256([]byte(codeVerifier))
	expectedChallenge := base64.RawURLEncoding.EncodeToString(sum[:])
	if ac.CodeChallenge != expectedChallenge {
		writeTokenError(w, "invalid_grant", "PKCE verification failed")
		return
	}

	accessToken := randomString(32)
	tokenMu.Lock()
	tokenStore[accessToken] = &tokenRecord{
		Token:     accessToken,
		ExpiresAt: time.Now().Add(tokenTTL),
	}
	tokenMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token": accessToken,
		"token_type":   "Bearer",
		"expires_in":   int(tokenTTL.Seconds()),
	})
}

func writeTokenError(w http.ResponseWriter, err, desc string) {
	w.WriteHeader(http.StatusBadRequest)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error":             err,
		"error_description": desc,
	})
}

// ---------------------------------------------------------------------------
// MCP reverse proxy
// ---------------------------------------------------------------------------

func handleMCPProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST, OPTIONS")
		http.Error(w, "only POST is supported", http.StatusMethodNotAllowed)
		return
	}

	// Extract and validate the OAuth access token from the Authorization header.
	auth := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(auth, prefix) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="mcp-oauth-proxy"`)
		http.Error(w, "authorization required", http.StatusUnauthorized)
		return
	}
	accessToken := strings.TrimSpace(auth[len(prefix):])
	if accessToken == "" {
		http.Error(w, "empty token", http.StatusUnauthorized)
		return
	}

	tokenMu.RLock()
	tr, ok := tokenStore[accessToken]
	tokenMu.RUnlock()
	if !ok || time.Now().After(tr.ExpiresAt) {
		http.Error(w, "invalid or expired token", http.StatusUnauthorized)
		return
	}

	// Best-effort cleanup of expired tokens (probabilistic sweep).
	if len(tokenStore) > 1000 && randInt(10) == 0 {
		go sweepExpiredTokens()
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	r.Body.Close()

	upstream, err := url.Parse(upstreamURL + "/mcp")
	if err != nil {
		http.Error(w, "upstream misconfiguration", http.StatusInternalServerError)
		return
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, upstream.String(), bytes.NewReader(body))
	if err != nil {
		http.Error(w, "build upstream request", http.StatusInternalServerError)
		return
	}
	req.Header.Set("Authorization", "Bearer "+bearerToken)
	if ct := r.Header.Get("Content-Type"); ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	if ae := r.Header.Get("Accept"); ae != "" {
		req.Header.Set("Accept", ae)
	}

	resp, err := upstreamClient.Do(req)
	if err != nil {
		log.Printf("proxy upstream error: %v", err)
		http.Error(w, "upstream error", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Forward headers.
	for k, vv := range resp.Header {
		if k == "WWW-Authenticate" {
			continue // never leak upstream auth hints
		}
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		log.Printf("proxy copy response: %v", err)
	}
}

func sweepExpiredTokens() {
	tokenMu.Lock()
	defer tokenMu.Unlock()
	now := time.Now()
	for k, v := range tokenStore {
		if now.After(v.ExpiresAt) {
			delete(tokenStore, k)
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func randomString(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func randInt(n int) int {
	b := make([]byte, 1)
	_, _ = rand.Read(b)
	return int(b[0]) % n
}

func sliceContains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
