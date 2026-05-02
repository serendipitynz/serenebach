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

// submitComment posts to /entry/{id}/comment with the honeypot empty, a
// sufficiently old _ts, and a valid CSRF token/cookie pair sourced from a
// prior GET. Returns the ResponseRecorder for inspection.
func submitComment(t *testing.T, h http.Handler, entryID int64, fields url.Values) *httptest.ResponseRecorder {
	t.Helper()
	csrfCookie, token := fetchCSRF(t, h)
	fields.Set("_ts", formatUnix(time.Now().Add(-10*time.Second)))
	if fields.Get("csrf_token") == "" {
		fields.Set("csrf_token", token)
	}
	req := httptest.NewRequest("POST", "/entry/"+itoa(int(entryID))+"/comment", strings.NewReader(fields.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", testPublicOrigin)
	req.AddCookie(csrfCookie)
	// A stable remote addr so IP-based rate-limit accounting is predictable.
	req.RemoteAddr = "127.0.0.1:1234"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func formatUnix(t time.Time) string { return itoa(int(t.Unix())) }

func TestCommentRequiresModeration(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)

	fields := url.Values{
		"name":        {"visitor"},
		"description": {"hello from the test suite"},
	}
	w := submitComment(t, a.Handler(), 1, fields)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("submit status = %d, want 303", w.Code)
	}

	// Default comment_mode is "moderated" so the comment should NOT appear
	// on the public entry yet.
	entryBody := httpGet(t, a.Handler(), "/entry/1/")
	if strings.Contains(entryBody, "hello from the test suite") {
		t.Errorf("moderated comment leaked to public before approval:\n%s", entryBody)
	}
}

func TestCommentModerationFlow(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)

	// 1. submit a comment (moderated mode → waiting)
	fields := url.Values{
		"name":        {"moderation-test"},
		"description": {"awaiting approval"},
	}
	submitComment(t, a.Handler(), 1, fields)

	// 2. admin approves it
	cookie := login(t, a.Handler(), "admin", "changeme")
	list := authedGET(t, a.Handler(), "/admin/comments?status=waiting", cookie)
	if !strings.Contains(list.Body.String(), "awaiting approval") {
		t.Fatalf("waiting comment not visible in admin list:\n%s", list.Body.String())
	}
	// Grab the comment id from the first delete form action.
	id := extractFirstCommentID(t, list.Body.String())
	if id == 0 {
		t.Fatalf("couldn't find comment id in admin list")
	}
	authedPOSTForm(t, a.Handler(), "/admin/comments/"+itoa(int(id))+"/approve", url.Values{}, cookie)

	// 3. public entry now shows it
	entryBody := httpGet(t, a.Handler(), "/entry/1/")
	if !strings.Contains(entryBody, "awaiting approval") {
		t.Errorf("approved comment missing from public entry:\n%s", entryBody)
	}
}

func TestCommentOpenModeAutoPublishes(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)

	// flip comment_mode to open so the POST handler publishes directly
	if _, err := a.DB.ExecContext(context.Background(),
		`UPDATE weblogs SET comment_mode = 'open' WHERE id = 1`); err != nil {
		t.Fatal(err)
	}

	submitComment(t, a.Handler(), 1, url.Values{
		"name":        {"open-mode"},
		"description": {"published immediately"},
	})
	entryBody := httpGet(t, a.Handler(), "/entry/1/")
	if !strings.Contains(entryBody, "published immediately") {
		t.Errorf("open-mode comment should be public on first render; got:\n%s", entryBody)
	}
}

func TestCommentClosedModeRejects(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)

	if _, err := a.DB.ExecContext(context.Background(),
		`UPDATE weblogs SET comment_mode = 'closed' WHERE id = 1`); err != nil {
		t.Fatal(err)
	}

	w := submitComment(t, a.Handler(), 1, url.Values{
		"name":        {"visitor"},
		"description": {"should be rejected"},
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("closed-mode status = %d, want 403", w.Code)
	}

	// And the public entry must not render the comment form either.
	entryBody := httpGet(t, a.Handler(), "/entry/1/")
	if strings.Contains(entryBody, `name="description"`) {
		t.Errorf("comment form should not render in closed mode")
	}
}

func TestCommentHoneypotRejectedSilently(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)

	fields := url.Values{
		"name":        {"bot"},
		"description": {"<a href=\"https://spam\">spam</a>"},
		"website":     {"https://spam.example"}, // the honeypot field
	}
	w := submitComment(t, a.Handler(), 1, fields)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("honeypot status = %d, want 303 silent redirect", w.Code)
	}

	// Nothing got written to the messages table.
	var n int
	if err := a.DB.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("honeypot trip saved %d comments, want 0", n)
	}
}

func TestCommentRejectedWhenFormTooFresh(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)

	csrfCookie, token := fetchCSRF(t, a.Handler())
	fields := url.Values{
		"name":        {"fast-bot"},
		"description": {"instant submit"},
		"csrf_token":  {token},
		// _ts just now = form submitted in 0s
		"_ts": {formatUnix(time.Now())},
	}
	req := httptest.NewRequest("POST", "/entry/1/comment", strings.NewReader(fields.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", testPublicOrigin)
	req.AddCookie(csrfCookie)
	req.RemoteAddr = "127.0.0.1:1234"
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", w.Code)
	}

	var n int
	if err := a.DB.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("fresh-form submission saved %d comments, want 0", n)
	}
}

func TestCommentBlankFieldsRejected(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)

	w := submitComment(t, a.Handler(), 1, url.Values{
		"name":        {"   "},
		"description": {""},
	})
	if w.Code != http.StatusSeeOther {
		t.Fatalf("blank status = %d, want 303", w.Code)
	}
	var n int
	if err := a.DB.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("blank submission saved %d comments, want 0", n)
	}
}

// ---- tiny helpers -------------------------------------------------------

func httpGet(t *testing.T, h http.Handler, path string) string {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w.Body.String()
}

// extractFirstCommentID scans the admin comment list HTML for the first
// "/admin/comments/{N}/..." path segment where N is numeric, and returns
// N. Skips non-numeric segments like "/admin/comments/settings" so the
// helper keeps targeting real comment rows.
func extractFirstCommentID(t *testing.T, html string) int64 {
	t.Helper()
	const marker = "/admin/comments/"
	search := html
	for {
		i := strings.Index(search, marker)
		if i < 0 {
			return 0
		}
		rest := search[i+len(marker):]
		end := strings.IndexByte(rest, '/')
		if end < 0 {
			return 0
		}
		if n, err := parseInt(rest[:end]); err == nil {
			return n
		}
		// Move past this hit and keep looking.
		search = rest[end:]
	}
}

func parseInt(s string) (int64, error) {
	var n int64
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, errInvalidInt
		}
		n = n*10 + int64(c-'0')
	}
	return n, nil
}

var errInvalidInt = &stringError{"invalid int"}

type stringError struct{ s string }

func (e *stringError) Error() string { return e.s }
