package app_test

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// likeOnce POSTs to /entry/{id}/like with a fresh cookie jar and returns
// the response + any cookie the server set so the caller can simulate a
// returning browser. A fresh CSRF cookie + token pair is always attached
// so the middleware accepts the POST; replayCookie (when non-nil) lets
// callers replay the sb_liked_<id> cookie from a previous like.
func likeOnce(t *testing.T, h http.Handler, entryID int64, userAgent string, replayCookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	csrfCookie, token := fetchCSRF(t, h)
	body := url.Values{"csrf_token": {token}}.Encode()
	req := httptest.NewRequest("POST", "/entry/"+itoa(int(entryID))+"/like", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", testPublicOrigin)
	req.Header.Set("User-Agent", userAgent)
	req.RemoteAddr = "10.0.0.1:1234"
	req.AddCookie(csrfCookie)
	if replayCookie != nil {
		req.AddCookie(replayCookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func entryLikesCount(t *testing.T, db *sql.DB, entryID int64) int64 {
	t.Helper()
	var n int64
	if err := db.QueryRow(`SELECT likes_count FROM entries WHERE id = ?`, entryID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestLikeIncrementsCounter(t *testing.T) {
	a := newTestApp(t)

	w := likeOnce(t, a.Handler(), 1, "Mozilla/5.0 (Macintosh)", nil)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", w.Code)
	}
	if n := entryLikesCount(t, a.DB, 1); n != 1 {
		t.Errorf("likes_count = %d, want 1", n)
	}

	// Server should set the short-circuit cookie so a returning browser can
	// skip the DB round-trip.
	var set *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "sb_liked_1" {
			set = c
			break
		}
	}
	if set == nil {
		t.Fatal("expected sb_liked_1 cookie to be set")
	}
}

func TestLikeCookieShortCircuitsRepeat(t *testing.T) {
	a := newTestApp(t)

	first := likeOnce(t, a.Handler(), 1, "Mozilla/5.0 (Macintosh)", nil)
	var cookie *http.Cookie
	for _, c := range first.Result().Cookies() {
		if c.Name == "sb_liked_1" {
			cookie = c
			break
		}
	}
	if cookie == nil {
		t.Fatal("no cookie from first like")
	}

	// Replaying the cookie must NOT bump the counter again.
	likeOnce(t, a.Handler(), 1, "Mozilla/5.0 (Macintosh)", cookie)
	if n := entryLikesCount(t, a.DB, 1); n != 1 {
		t.Errorf("likes_count = %d, want 1 (cookie replay)", n)
	}
}

func TestLikeFingerprintBlocksCookielessRepeat(t *testing.T) {
	a := newTestApp(t)

	likeOnce(t, a.Handler(), 1, "Mozilla/5.0 (Macintosh)", nil)
	// Same IP + UA but no cookie (simulates incognito / cleared cookies)
	likeOnce(t, a.Handler(), 1, "Mozilla/5.0 (Macintosh)", nil)

	if n := entryLikesCount(t, a.DB, 1); n != 1 {
		t.Errorf("likes_count = %d, want 1 (fingerprint should block repeat)", n)
	}
}

func TestLikeAllowsDifferentVisitors(t *testing.T) {
	a := newTestApp(t)

	likeOnce(t, a.Handler(), 1, "Mozilla/5.0 (Macintosh)", nil)
	likeOnce(t, a.Handler(), 1, "Mozilla/5.0 (Windows NT 10)", nil) // different UA → different fingerprint

	if n := entryLikesCount(t, a.DB, 1); n != 2 {
		t.Errorf("likes_count = %d, want 2", n)
	}
}

func TestLikeUnknownEntryIs404(t *testing.T) {
	a := newTestApp(t)

	csrfCookie, token := fetchCSRF(t, a.Handler())
	body := url.Values{"csrf_token": {token}}.Encode()
	req := httptest.NewRequest("POST", "/entry/9999/like", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", testPublicOrigin)
	req.AddCookie(csrfCookie)
	req.RemoteAddr = "10.0.0.1:1234"
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestLikeCountAppearsInPublicPages(t *testing.T) {
	a := newTestApp(t)

	likeOnce(t, a.Handler(), 1, "Mozilla/5.0 (Macintosh)", nil)

	homeBody := httpGet(t, a.Handler(), "/")
	entryBody := httpGet(t, a.Handler(), "/entry/1/")

	if !strings.Contains(homeBody, "❤ 1") {
		t.Errorf("home page should show like count; body:\n%s", homeBody)
	}
	if !strings.Contains(entryBody, "❤ 1") {
		t.Errorf("permalink should show like count")
	}
	if !strings.Contains(entryBody, `action="/entry/1/like"`) {
		t.Errorf("permalink should have the like form")
	}
}
