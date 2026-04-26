// Package turnstile wraps Cloudflare Turnstile's server-side siteverify API
// so the public comment handler can accept or reject a form's challenge
// token. The whole feature is gated on SB_TURNSTILE_SITEKEY and
// SB_TURNSTILE_SECRET being set — empty secret means Enabled() is false and
// no verification happens, keeping the "place it anywhere" story intact.
package turnstile

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultVerifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

// Verifier is the minimal behaviour the public handler needs; its only
// non-trivial implementation is Client, but keeping an interface here keeps
// the handler and tests decoupled from `net/http`.
type Verifier interface {
	Enabled() bool
	SiteKey() string
	Verify(ctx context.Context, token, remoteIP string) (bool, error)
}

// Client is the production Verifier. Construct one with New and inject into
// handlers.
type Client struct {
	siteKey   string
	secret    string
	http      *http.Client
	verifyURL string
}

// New returns a Client. When siteKey or secret is empty the resulting client
// is "disabled" — Enabled() returns false and Verify() is a no-op success.
func New(siteKey, secret string) *Client {
	return &Client{
		siteKey:   siteKey,
		secret:    secret,
		http:      &http.Client{Timeout: 5 * time.Second},
		verifyURL: defaultVerifyURL,
	}
}

// Enabled reports whether this Verifier should be exercised. Handlers call
// this to decide whether to render the widget and whether to check tokens.
func (c *Client) Enabled() bool {
	return strings.TrimSpace(c.siteKey) != "" && strings.TrimSpace(c.secret) != ""
}

// SiteKey returns the public key the template renders into the challenge
// widget. Empty string when Enabled() is false.
func (c *Client) SiteKey() string {
	if !c.Enabled() {
		return ""
	}
	return c.siteKey
}

// Verify calls Cloudflare's siteverify endpoint with the submitted token.
// When Enabled() is false the method returns success immediately so handlers
// can call it unconditionally without branching.
func (c *Client) Verify(ctx context.Context, token, remoteIP string) (bool, error) {
	if !c.Enabled() {
		return true, nil
	}
	if strings.TrimSpace(token) == "" {
		return false, nil
	}

	form := url.Values{
		"secret":   {c.secret},
		"response": {token},
	}
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.verifyURL, strings.NewReader(form.Encode()))
	if err != nil {
		return false, fmt.Errorf("turnstile: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.http.Do(req)
	if err != nil {
		return false, fmt.Errorf("turnstile: siteverify: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return false, fmt.Errorf("turnstile: read body: %w", err)
	}

	var out struct {
		Success bool `json:"success"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return false, fmt.Errorf("turnstile: decode response: %w", err)
	}
	return out.Success, nil
}

// WidgetHTML returns the <div> + <script> the template should drop into the
// comment form when the feature is enabled. Empty when disabled so templates
// can inject this unconditionally.
func (c *Client) WidgetHTML() string {
	if !c.Enabled() {
		return ""
	}
	return `<div class="cf-turnstile" data-sitekey="` + c.siteKey + `"></div>
<script src="https://challenges.cloudflare.com/turnstile/v0/api.js" async defer></script>`
}
