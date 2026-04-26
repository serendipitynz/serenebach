package app_test

import (
	"encoding/xml"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestPublicRSSFeedServes confirms /rss.xml responds with RSS 2.0 XML and
// includes the seeded entries. Guards against regressions in both the
// route mounting and the feed builder.
func TestPublicRSSFeedServes(t *testing.T) {
	a := newTestApp(t)
	req := httptest.NewRequest("GET", "/rss.xml", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/rss+xml") {
		t.Errorf("Content-Type = %q, want application/rss+xml", ct)
	}
	body := w.Body.Bytes()
	if err := xml.Unmarshal(body, new(interface{})); err != nil {
		t.Errorf("feed not well-formed XML: %v\n%s", err, body)
	}
	if !strings.Contains(string(body), `<rss version="2.0"`) {
		t.Errorf("missing RSS 2.0 root element")
	}
	if !strings.Contains(string(body), "<item>") {
		t.Errorf("no <item> in feed — seeded entry should have appeared")
	}
}

// TestPublicAtomFeedServes is the Atom counterpart.
func TestPublicAtomFeedServes(t *testing.T) {
	a := newTestApp(t)
	req := httptest.NewRequest("GET", "/atom.xml", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/atom+xml") {
		t.Errorf("Content-Type = %q, want application/atom+xml", ct)
	}
	body := w.Body.Bytes()
	if err := xml.Unmarshal(body, new(interface{})); err != nil {
		t.Errorf("feed not well-formed XML: %v\n%s", err, body)
	}
	if !strings.Contains(string(body), `xmlns="http://www.w3.org/2005/Atom"`) {
		t.Errorf("missing Atom 1.0 namespace")
	}
	if !strings.Contains(string(body), "<entry>") {
		t.Errorf("no <entry> in feed — seeded entry should have appeared")
	}
}
