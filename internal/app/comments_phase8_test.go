package app_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// fakeVerifier lets integration tests pretend Turnstile is configured without
// touching Cloudflare. It returns whatever the test configured.
type fakeVerifier struct {
	enabled   bool
	pass      bool
	err       error
	seenToken string
}

func (f *fakeVerifier) Enabled() bool { return f.enabled }
func (f *fakeVerifier) SiteKey() string {
	if !f.enabled {
		return ""
	}
	return "FAKE-SITE-KEY"
}
func (f *fakeVerifier) Verify(ctx context.Context, token, ip string) (bool, error) {
	f.seenToken = token
	return f.pass, f.err
}
func (f *fakeVerifier) WidgetHTML() string {
	if !f.enabled {
		return ""
	}
	return `<div data-fake-turnstile="1"></div>`
}

// Approve a comment by id via the admin UI so the following submission can
// inherit the "trust memory" auto-approval.
func approveComment(t *testing.T, h http.Handler, cookies []*http.Cookie, id int64) {
	t.Helper()
	w := authedPOSTForm(t, h, "/admin/comments/"+itoa(int(id))+"/approve", url.Values{}, cookies)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("approve status = %d", w.Code)
	}
}

func TestTrustMemoryAutoApprovesReturningEmail(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)

	// 1. first comment from trusted@example.com → waiting (default moderated)
	submitComment(t, a.Handler(), 1, url.Values{
		"name":        {"Alice"},
		"email":       {"trusted@example.com"},
		"description": {"first comment"},
	})

	// 2. admin approves that first comment, so trust memory kicks in
	cookie := login(t, a.Handler(), "admin", "changeme")
	firstID := extractFirstCommentID(t, authedGET(t, a.Handler(), "/admin/comments?status=waiting", cookie).Body.String())
	if firstID == 0 {
		t.Fatal("couldn't find first comment id")
	}
	approveComment(t, a.Handler(), cookie, firstID)

	// 3. same email posts again — should auto-publish without admin touch
	submitComment(t, a.Handler(), 1, url.Values{
		"name":        {"Alice"},
		"email":       {"trusted@example.com"},
		"description": {"second comment — should auto-approve"},
	})

	// 4. public entry shows both without further moderation
	body := httpGet(t, a.Handler(), "/entry/1/")
	if !strings.Contains(body, "second comment — should auto-approve") {
		t.Errorf("auto-approved second comment missing; body:\n%s", body)
	}
}

func TestTrustMemoryStillQueuesStrangers(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)

	// only one email has been approved
	submitComment(t, a.Handler(), 1, url.Values{
		"name":        {"Alice"},
		"email":       {"trusted@example.com"},
		"description": {"anchor"},
	})
	cookie := login(t, a.Handler(), "admin", "changeme")
	firstID := extractFirstCommentID(t, authedGET(t, a.Handler(), "/admin/comments?status=waiting", cookie).Body.String())
	approveComment(t, a.Handler(), cookie, firstID)

	// different email submits — must still be queued
	submitComment(t, a.Handler(), 1, url.Values{
		"name":        {"Mallory"},
		"email":       {"stranger@example.com"},
		"description": {"new commenter — stays in queue"},
	})

	body := httpGet(t, a.Handler(), "/entry/1/")
	if strings.Contains(body, "new commenter — stays in queue") {
		t.Errorf("stranger comment leaked as approved; body:\n%s", body)
	}
}

func TestSpamWordsSilentReject(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)

	if _, err := a.DB.ExecContext(context.Background(),
		`UPDATE weblogs SET spam_words = 'casino' || X'0A' || 'viagra' WHERE id = 1`); err != nil {
		t.Fatal(err)
	}

	w := submitComment(t, a.Handler(), 1, url.Values{
		"name":        {"Spammer"},
		"description": {"Cheap VIAGRA here!"},
	})
	if w.Code != http.StatusSeeOther {
		t.Fatalf("spam status = %d, want 303 silent redirect", w.Code)
	}

	var n int
	if err := a.DB.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("spam filter saved %d comments, want 0", n)
	}
}

func TestTurnstileEnabledRejectsMissingToken(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	fake := &fakeVerifier{enabled: true, pass: false}
	a.Public.Turnstile = fake

	w := submitComment(t, a.Handler(), 1, url.Values{
		"name":        {"Visitor"},
		"description": {"no challenge answered"},
	})
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303 redirect", w.Code)
	}

	// Nothing reached the database.
	var n int
	if err := a.DB.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("submission stored despite failed turnstile; n = %d", n)
	}
}

func TestTurnstileEnabledAcceptsValidToken(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	fake := &fakeVerifier{enabled: true, pass: true}
	a.Public.Turnstile = fake

	fields := url.Values{
		"name":                  {"Visitor"},
		"description":           {"passes the challenge"},
		"cf-turnstile-response": {"valid-token-from-client"},
	}
	submitComment(t, a.Handler(), 1, fields)

	if fake.seenToken != "valid-token-from-client" {
		t.Errorf("verifier saw token %q, want 'valid-token-from-client'", fake.seenToken)
	}
	var n int
	if err := a.DB.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("comment count = %d, want 1", n)
	}
}

func TestTurnstileDisabledKeepsOldBehaviour(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	// Default: newTestApp leaves Turnstile disabled. A submission should
	// still succeed without a token.
	submitComment(t, a.Handler(), 1, url.Values{
		"name":        {"NoChallenge"},
		"description": {"runs unchallenged"},
	})

	var n int
	if err := a.DB.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("comment count = %d, want 1 (turnstile off should not block)", n)
	}
}

func TestCookiePrefillRoundtrip(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)

	// submit with set_cookie=1 so the server persists our name/email/url
	csrfCookie, token := fetchCSRF(t, a.Handler())
	fields := url.Values{
		"name":        {"Returning User"},
		"email":       {"return@example.com"},
		"url":         {"https://example.com/returning"},
		"description": {"first visit"},
		"set_cookie":  {"1"},
		"csrf_token":  {token},
	}
	fields.Set("_ts", formatUnix(time.Now().Add(-10*time.Second)))
	req := httptest.NewRequest("POST", "/entry/1/comment", strings.NewReader(fields.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", testPublicOrigin)
	req.AddCookie(csrfCookie)
	req.RemoteAddr = "127.0.0.1:1234"
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("submit status = %d", w.Code)
	}

	// Server set the three prefill cookies — replay them on the next GET.
	resp := w.Result()
	var prefill []*http.Cookie
	for _, c := range resp.Cookies() {
		if strings.HasPrefix(c.Name, "sb_") && c.Name != "sb_session" {
			prefill = append(prefill, c)
		}
	}
	if len(prefill) < 3 {
		t.Fatalf("expected 3 prefill cookies, got %d: %v", len(prefill), prefill)
	}

	// Revisit the entry page carrying the cookies; form fields should be
	// prefilled via the {cookie_*} tags.
	getReq := httptest.NewRequest("GET", "/entry/1/", nil)
	for _, c := range prefill {
		getReq.AddCookie(c)
	}
	gw := httptest.NewRecorder()
	a.Handler().ServeHTTP(gw, getReq)
	body := gw.Body.String()
	for _, want := range []string{
		`value="Returning User"`,
		`value="return@example.com"`,
		`value="https://example.com/returning"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("cookie prefill missing %q in form; body:\n%s", want, body)
			return
		}
	}
}
