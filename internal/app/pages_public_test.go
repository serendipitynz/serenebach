package app_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/serendipitynz/serenebach/internal/domain"
)

func TestPublicFlatPageRenders(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	// Create a published flat page
	form := newPageForm("About", "<p>about us</p>", "/about", domain.PagePublished)
	w := authedPOSTForm(t, a.Handler(), "/admin/pages/new", form, cookies)
	if w.Code != 302 {
		t.Fatalf("create status = %d", w.Code)
	}

	req := httptest.NewRequest("GET", "/about", nil)
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "<p>about us</p>") {
		t.Errorf("missing page body; got:\n%s", body)
	}
}

func TestPublicNestedFlatPageRenders(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	form := newPageForm("Pricing", "<p>pricing info</p>", "/service/pricing", domain.PagePublished)
	w := authedPOSTForm(t, a.Handler(), "/admin/pages/new", form, cookies)
	if w.Code != 302 {
		t.Fatalf("create status = %d", w.Code)
	}

	req := httptest.NewRequest("GET", "/service/pricing", nil)
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "<p>pricing info</p>") {
		t.Errorf("missing nested page body")
	}
}

func TestPublicFlatPageDoesNotLeakIntoFeeds(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	form := newPageForm("About", "<p>about us</p>", "/about", domain.PagePublished)
	w := authedPOSTForm(t, a.Handler(), "/admin/pages/new", form, cookies)
	if w.Code != 302 {
		t.Fatalf("create status = %d", w.Code)
	}

	// RSS must not contain the flat page title
	rss := httptest.NewRequest("GET", "/rss.xml", nil)
	rr := httptest.NewRecorder()
	a.Handler().ServeHTTP(rr, rss)
	if rr.Code != 200 {
		t.Fatalf("rss status = %d", rr.Code)
	}
	if strings.Contains(rr.Body.String(), "About") {
		t.Errorf("flat page title leaked into rss.xml")
	}

	// Atom must not contain the flat page title
	atom := httptest.NewRequest("GET", "/atom.xml", nil)
	ra := httptest.NewRecorder()
	a.Handler().ServeHTTP(ra, atom)
	if ra.Code != 200 {
		t.Fatalf("atom status = %d", ra.Code)
	}
	if strings.Contains(ra.Body.String(), "About") {
		t.Errorf("flat page title leaked into atom.xml")
	}
}

func TestPublicFlatPageDoesNotLeakIntoHomeOrCategory(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	form := newPageForm("About", "<p>about us</p>", "/about", domain.PagePublished)
	w := authedPOSTForm(t, a.Handler(), "/admin/pages/new", form, cookies)
	if w.Code != 302 {
		t.Fatalf("create status = %d", w.Code)
	}

	// Home page must not show the flat page in the main entry list
	home := httptest.NewRequest("GET", "/", nil)
	rh := httptest.NewRecorder()
	a.Handler().ServeHTTP(rh, home)
	if rh.Code != 200 {
		t.Fatalf("home status = %d", rh.Code)
	}
	main := mainArea(rh.Body.String())
	if strings.Contains(main, "About") {
		t.Errorf("flat page leaked into home entry list")
	}

	// Category page must not show the flat page
	cat := httptest.NewRequest("GET", "/category/news", nil)
	rc := httptest.NewRecorder()
	a.Handler().ServeHTTP(rc, cat)
	if rc.Code != 200 {
		t.Fatalf("category status = %d", rc.Code)
	}
	if strings.Contains(mainArea(rc.Body.String()), "About") {
		t.Errorf("flat page leaked into category page")
	}
}

func TestPublicSystemRoutesWinOverFlatPageCatchAll(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	// Reserved system paths are rejected at creation time, so they can
	// never shadow the real routes. Verify each reserved slug is refused.
	reserved := []string{"/entry/1", "/admin", "/rss.xml", "/style.css"}
	for _, slug := range reserved {
		form := newPageForm("Shadow", "body", slug, domain.PagePublished)
		w := authedPOSTForm(t, a.Handler(), "/admin/pages/new", form, cookies)
		if w.Code != 200 {
			t.Fatalf("create %s: expected stay on form, got status=%d", slug, w.Code)
		}
		if !strings.Contains(w.Body.String(), `class="alert error"`) {
			t.Errorf("create %s: expected error alert", slug)
		}
	}

	// The real system routes still work because no page was created.
	routes := []struct {
		route  string
		wantOK int
	}{
		{"/entry/1", 200},
		{"/admin", 302},
		{"/rss.xml", 200},
		{"/style.css", 200},
	}
	for _, s := range routes {
		req := httptest.NewRequest("GET", s.route, nil)
		rec := httptest.NewRecorder()
		a.Handler().ServeHTTP(rec, req)
		if rec.Code != s.wantOK {
			t.Errorf("GET %s = %d, want %d", s.route, rec.Code, s.wantOK)
		}
	}
}

func newPageForm(title, body, slug string, status domain.PageStatus) map[string][]string {
	return map[string][]string{
		"title":       {title},
		"body":        {body},
		"slug":        {slug},
		"status":      {string(rune('0' + status))},
		"format":      {"html"},
		"template_id": {"0"},
	}
}
