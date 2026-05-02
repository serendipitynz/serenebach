package app_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAnalyticsRecordsPublicGET(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (reader)")
	req.RemoteAddr = "10.0.0.1:1"
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)

	var n int
	if err := a.DB.QueryRow(`SELECT COUNT(*) FROM page_views WHERE path = '/'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("page_views count for / = %d, want 1", n)
	}
}

func TestAnalyticsExtractsEntryID(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)

	req := httptest.NewRequest("GET", "/entry/1/", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (reader)")
	req.RemoteAddr = "10.0.0.1:1"
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)

	var entryID int64
	if err := a.DB.QueryRow(`SELECT entry_id FROM page_views WHERE path = '/entry/1/'`).Scan(&entryID); err != nil {
		t.Fatal(err)
	}
	if entryID != 1 {
		t.Errorf("entry_id = %d, want 1", entryID)
	}
}

func TestAnalyticsSkipsAdmin(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)

	req := httptest.NewRequest("GET", "/admin/login", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (reader)")
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)

	var n int
	if err := a.DB.QueryRow(
		`SELECT COUNT(*) FROM page_views WHERE path LIKE '/admin/%'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("admin paths should not be tracked; got %d", n)
	}
}

func TestAnalyticsSkipsBotUA(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("User-Agent", "Googlebot/2.1")
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)

	var n int
	if err := a.DB.QueryRow(`SELECT COUNT(*) FROM page_views`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("bot UA should not record a pageview; got %d", n)
	}
}

func TestAnalyticsSkipsPOSTs(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)

	req := httptest.NewRequest("POST", "/entry/1/like", nil)
	req.Header.Set("Origin", testPublicOrigin)
	req.Header.Set("User-Agent", "Mozilla/5.0 (reader)")
	req.RemoteAddr = "10.0.0.1:1"
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)

	var n int
	if err := a.DB.QueryRow(`SELECT COUNT(*) FROM page_views`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("POST requests should not record a pageview; got %d", n)
	}
}

func TestAnalyticsSetsVisitorCookie(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (reader)")
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)

	found := false
	for _, c := range w.Result().Cookies() {
		if c.Name == "sb_visitor_id" && c.Value != "" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("sb_visitor_id cookie should be set on first visit")
	}
}

func TestAnalyticsSameVisitorReused(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)

	// first request: cookie is minted
	req1 := httptest.NewRequest("GET", "/", nil)
	req1.Header.Set("User-Agent", "Mozilla/5.0 (reader)")
	req1.RemoteAddr = "10.0.0.1:1"
	w1 := httptest.NewRecorder()
	a.Handler().ServeHTTP(w1, req1)

	var cookie *http.Cookie
	for _, c := range w1.Result().Cookies() {
		if c.Name == "sb_visitor_id" {
			cookie = c
			break
		}
	}
	if cookie == nil {
		t.Fatal("visitor cookie not minted")
	}

	// second request: replays the cookie, hits different path
	req2 := httptest.NewRequest("GET", "/entry/1/", nil)
	req2.Header.Set("User-Agent", "Mozilla/5.0 (reader)")
	req2.AddCookie(cookie)
	req2.RemoteAddr = "10.0.0.1:1"
	w2 := httptest.NewRecorder()
	a.Handler().ServeHTTP(w2, req2)

	// Both views should be attributed to the same visitor_id.
	var uniques int
	if err := a.DB.QueryRow(
		`SELECT COUNT(DISTINCT visitor_id) FROM page_views`).Scan(&uniques); err != nil {
		t.Fatal(err)
	}
	if uniques != 1 {
		t.Errorf("distinct visitors = %d, want 1 (same cookie across two GETs)", uniques)
	}
}

func TestAdminAnalyticsDashboardRenders(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)

	// seed a couple of pageviews so the dashboard has content
	if err := a.Analytics.Record(context.Background(), "v1", "/entry/1/", 1); err != nil {
		t.Fatal(err)
	}
	if err := a.Analytics.Record(context.Background(), "v2", "/entry/1/", 1); err != nil {
		t.Fatal(err)
	}

	cookie := login(t, a.Handler(), "admin", "changeme")
	w := authedGET(t, a.Handler(), "/admin/analytics", cookie)
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{
		"アクセス解析",
		"ページビュー",
		"ユニーク訪問者",
		"人気記事",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard missing %q", want)
		}
	}
}
