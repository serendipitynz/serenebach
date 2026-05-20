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
//	PROXY_LISTEN_ADDR        Listen address (default ":8080")
//	BASE_URL                 Public URL of this proxy, used in metadata (default "http://localhost:8080")
//	AUTH_PIN                 If set, the /authorize page asks for this PIN before issuing a code
//	OAUTH_REDIRECT_URIS      Comma-separated allowlist of redirect_uris. When empty, any uri is accepted (dev only).
//	TOKEN_TTL                Access-token lifetime (default "24h")
//	PROXY_ALLOW_INSECURE_DEV Set to "1" to skip the production-mode guards (AUTH_PIN + OAUTH_REDIRECT_URIS required when BASE_URL or PROXY_LISTEN_ADDR is non-loopback). Development only.
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
	"net"
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
	addr := os.Getenv("PROXY_LISTEN_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	if productionGuardsRequired(baseURL, addr, os.Getenv("PROXY_ALLOW_INSECURE_DEV")) {
		if err := validateProductionConfig(authPIN, allowedRedirectURIs); err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(1)
		}
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

// authorizeParams is the set of OAuth fields the authorize endpoint needs
// to issue a code. They start out from the initial query and are replaced
// by the PIN form body when AUTH_PIN is configured.
type authorizeParams struct {
	state               string
	redirectURI         string
	codeChallenge       string
	codeChallengeMethod string
}

func handleAuthorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if msg, ok := validateAuthorizeQuery(q); !ok {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}
	params := authorizeParams{
		state:               q.Get("state"),
		redirectURI:         q.Get("redirect_uri"),
		codeChallenge:       q.Get("code_challenge"),
		codeChallengeMethod: q.Get("code_challenge_method"),
	}

	next, ok := runAuthorizeMethod(w, r, params)
	if !ok {
		return
	}
	issueAuthCode(w, r, next)
}

// validateAuthorizeQuery returns ("", true) when the initial GET query
// satisfies the OAuth + PKCE rules. Otherwise it returns a short error
// message and false.
func validateAuthorizeQuery(q url.Values) (string, bool) {
	if q.Get("response_type") != "code" {
		return "unsupported response_type", false
	}
	if q.Get("client_id") != oauthClientID {
		return "invalid client_id", false
	}
	return validateRedirectAndPKCE(q.Get("redirect_uri"), q.Get("code_challenge"), q.Get("code_challenge_method"))
}

// validateRedirectAndPKCE checks the redirect allowlist and PKCE method.
// Used for both the initial GET query and the adopted PIN form body so
// the latter cannot bypass the former.
func validateRedirectAndPKCE(redirectURI, codeChallenge, codeChallengeMethod string) (string, bool) {
	if redirectURI == "" {
		return "missing redirect_uri", false
	}
	if len(allowedRedirectURIs) > 0 && !sliceContains(allowedRedirectURIs, redirectURI) {
		return "invalid redirect_uri", false
	}
	if codeChallenge == "" || codeChallengeMethod != "S256" {
		return "PKCE S256 required", false
	}
	return "", true
}

// runAuthorizeMethod dispatches the request to the right phase of the
// PIN flow. It returns the params to issue a code with, or false when
// the response has already been written (render-form / errors).
func runAuthorizeMethod(w http.ResponseWriter, r *http.Request, params authorizeParams) (authorizeParams, bool) {
	if authPIN == "" {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return authorizeParams{}, false
		}
		return params, true
	}
	switch r.Method {
	case http.MethodGet:
		renderPINForm(w, params.state, params.redirectURI, params.codeChallenge)
		return authorizeParams{}, false
	case http.MethodPost:
		return acceptPINPost(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return authorizeParams{}, false
	}
}

// acceptPINPost parses and authenticates a PIN-form submission, then
// revalidates the adopted redirect_uri and PKCE method against the same
// rules used for the initial query. Returns the adopted params on
// success; on failure it has already written an HTTP error.
func acceptPINPost(w http.ResponseWriter, r *http.Request) (authorizeParams, bool) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return authorizeParams{}, false
	}
	if r.FormValue("pin") != authPIN {
		http.Error(w, "invalid PIN", http.StatusUnauthorized)
		return authorizeParams{}, false
	}
	p := authorizeParams{
		state:               r.FormValue("state"),
		redirectURI:         r.FormValue("redirect_uri"),
		codeChallenge:       r.FormValue("code_challenge"),
		codeChallengeMethod: r.FormValue("code_challenge_method"),
	}
	if msg, ok := validateRedirectAndPKCE(p.redirectURI, p.codeChallenge, p.codeChallengeMethod); !ok {
		http.Error(w, msg, http.StatusBadRequest)
		return authorizeParams{}, false
	}
	return p, true
}

// issueAuthCode mints a one-shot authorization code and redirects the
// user agent to the (already-validated) redirect_uri with ?code=… &state=….
func issueAuthCode(w http.ResponseWriter, r *http.Request, p authorizeParams) {
	code := randomString(32)
	codeMu.Lock()
	codeStore[code] = &authCode{
		CodeChallenge:       p.codeChallenge,
		CodeChallengeMethod: p.codeChallengeMethod,
		RedirectURI:         p.redirectURI,
		ClientID:            oauthClientID,
		ExpiresAt:           time.Now().Add(10 * time.Minute),
	}
	codeMu.Unlock()

	u, _ := url.Parse(p.redirectURI)
	qq := u.Query()
	qq.Set("code", code)
	if p.state != "" {
		qq.Set("state", p.state)
	}
	u.RawQuery = qq.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

func renderPINForm(w http.ResponseWriter, state, redirectURI, codeChallenge string) {
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
		writeMCPProxyPreflight(w)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST, OPTIONS")
		http.Error(w, "only POST is supported", http.StatusMethodNotAllowed)
		return
	}
	if !authenticateMCPProxy(w, r) {
		return // response already written
	}
	body, ok := readMCPProxyBody(w, r)
	if !ok {
		return // response already written
	}
	req, err := buildMCPUpstreamRequest(r, body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resp, err := upstreamClient.Do(req)
	if err != nil {
		log.Printf("proxy upstream error: %v", err)
		http.Error(w, "upstream error", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	forwardMCPUpstreamResponse(w, resp)
}

// writeMCPProxyPreflight responds to the CORS preflight that ChatGPT
// (and other MCP clients) send before POSTing JSON-RPC payloads.
func writeMCPProxyPreflight(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
	w.WriteHeader(http.StatusNoContent)
}

// authenticateMCPProxy validates the Bearer access token and writes the
// appropriate 401 response when it doesn't check out. Returns true when
// the request is authorised. A side effect: a probabilistic expired-
// token sweep is kicked off when the store grows past 1000 entries.
func authenticateMCPProxy(w http.ResponseWriter, r *http.Request) bool {
	const prefix = "Bearer "
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, prefix) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="mcp-oauth-proxy"`)
		http.Error(w, "authorization required", http.StatusUnauthorized)
		return false
	}
	accessToken := strings.TrimSpace(auth[len(prefix):])
	if accessToken == "" {
		http.Error(w, "empty token", http.StatusUnauthorized)
		return false
	}
	tokenMu.RLock()
	tr, ok := tokenStore[accessToken]
	tokenMu.RUnlock()
	if !ok || time.Now().After(tr.ExpiresAt) {
		http.Error(w, "invalid or expired token", http.StatusUnauthorized)
		return false
	}
	// Best-effort cleanup of expired tokens (probabilistic sweep).
	if len(tokenStore) > 1000 && randInt(10) == 0 {
		go sweepExpiredTokens()
	}
	return true
}

// readMCPProxyBody pulls the JSON-RPC body up to the size cap. Returns
// (nil, false) and writes a 400 when reading fails.
func readMCPProxyBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return nil, false
	}
	r.Body.Close()
	return body, true
}

// buildMCPUpstreamRequest constructs the POST that forwards the
// caller's JSON-RPC payload to the upstream Serene Bach /mcp endpoint
// with the static bearer token attached.
func buildMCPUpstreamRequest(r *http.Request, body []byte) (*http.Request, error) {
	upstream, err := url.Parse(upstreamURL + "/mcp")
	if err != nil {
		return nil, fmt.Errorf("upstream misconfiguration")
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, upstream.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build upstream request")
	}
	req.Header.Set("Authorization", "Bearer "+bearerToken)
	if ct := r.Header.Get("Content-Type"); ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	if ae := r.Header.Get("Accept"); ae != "" {
		req.Header.Set("Accept", ae)
	}
	return req, nil
}

// forwardMCPUpstreamResponse copies the upstream status, body, and
// headers (except WWW-Authenticate, which would leak upstream auth
// hints) back to the caller, plus the CORS allow-origin header so the
// browser-side MCP client can read it.
func forwardMCPUpstreamResponse(w http.ResponseWriter, resp *http.Response) {
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

// productionGuardsRequired reports whether the proxy must be started
// with both AUTH_PIN and OAUTH_REDIRECT_URIS in place. The check fires
// when either the advertised BASE_URL or the actual listen address can
// be reached from a non-loopback origin, unless the operator opts out
// via the insecure-dev escape hatch.
func productionGuardsRequired(baseURL, listenAddr, allowInsecureDev string) bool {
	if allowInsecureDev == "1" {
		return false
	}
	return !isLoopbackBaseURL(baseURL) || !isLoopbackListenAddr(listenAddr)
}

// isLoopbackBaseURL returns true when the URL host resolves to a loopback
// address by name (localhost) or literal (127.0.0.1, ::1). Unparseable URLs
// are treated as non-loopback so we fail safe toward enforcement.
func isLoopbackBaseURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	return isLoopbackHostLiteral(u.Hostname())
}

// isLoopbackListenAddr reports whether the given net.Listen-style address
// only accepts connections from the local machine. Empty host (":8080"),
// the IPv4 wildcard "0.0.0.0", and the IPv6 wildcard "::" all bind to
// every interface and are treated as public. Hostnames that are not
// literal loopback are also treated as public so we fail safe.
func isLoopbackListenAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		// Can't parse — treat as public-bound to err on the safer side.
		return false
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		return false
	}
	return isLoopbackHostLiteral(host)
}

// isLoopbackHostLiteral reports whether the given host string is one of
// the recognized loopback literals. Case-insensitive on the name form.
func isLoopbackHostLiteral(host string) bool {
	switch strings.ToLower(host) {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

// validateProductionConfig returns an error when the proxy would start in
// production mode without the safety controls. The caller decides whether
// production mode applies.
func validateProductionConfig(authPIN string, allowedRedirectURIs []string) error {
	const hint = " (set PROXY_ALLOW_INSECURE_DEV=1 to override for development only)"
	if authPIN == "" {
		return fmt.Errorf("AUTH_PIN must be set when BASE_URL or PROXY_LISTEN_ADDR is non-loopback%s", hint)
	}
	if len(allowedRedirectURIs) == 0 {
		return fmt.Errorf("OAUTH_REDIRECT_URIS must be set when BASE_URL or PROXY_LISTEN_ADDR is non-loopback%s", hint)
	}
	return nil
}
