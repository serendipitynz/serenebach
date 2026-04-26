package app_test

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// TestPublicStyleCSSServesActiveTemplateCSS confirms /style.css hits
// the dev-mode handler and returns the active template's CSS with a
// text/css content type — the readme guarantees this URL works both
// after a static rebuild and in `task dev`, so the dynamic handler
// can't 404 out.
func TestPublicStyleCSSServesActiveTemplateCSS(t *testing.T) {
	a := newTestApp(t)

	// Plant a known marker in the active template's CSS so we can tell
	// from the response that the right row was served.
	if _, err := a.DB.Exec(`UPDATE templates SET css = 'body{color:#c0ffee}/*SB-CSS-MARKER*/' WHERE is_active = 1`); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/style.css", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/css") {
		t.Errorf("Content-Type = %q, want text/css", ct)
	}
	if !strings.Contains(w.Body.String(), "SB-CSS-MARKER") {
		t.Errorf("served body did not include the active template CSS: %q", w.Body.String())
	}
}

// TestPerTemplateCSSServesCorrectBody confirms /template/<id>/style.css
// returns the CSS for that specific template row, not the active one.
// Category / archive / profile pages point their <link> at this URL
// when pinned to a non-active template.
func TestPerTemplateCSSServesCorrectBody(t *testing.T) {
	a := newTestApp(t)
	// Create an inactive template with a distinct CSS marker.
	res, err := a.DB.Exec(`INSERT INTO templates (wid, name, is_active, main_body, entry_body, css, info, sort_order, created_at, updated_at)
		VALUES (1, 'pin', 0, '<!-- BEGIN entry --><!-- END entry -->', '', 'body{background:#abcdef}/*PIN-ONLY*/', '', 99, strftime('%s','now'), strftime('%s','now'))`)
	if err != nil {
		t.Fatal(err)
	}
	pinID, _ := res.LastInsertId()

	// Plant a different marker in the active template so we can tell
	// the two apart.
	if _, err := a.DB.Exec(`UPDATE templates SET css = 'body{color:#c0ffee}/*ACTIVE-ONLY*/' WHERE is_active = 1`); err != nil {
		t.Fatal(err)
	}

	// /template/<pin>/style.css must return PIN-ONLY, not ACTIVE-ONLY.
	req := httptest.NewRequest("GET", "/template/"+itoa64str(pinID)+"/style.css", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("pin css status = %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "PIN-ONLY") {
		t.Errorf("pin CSS missing: %q", body)
	}
	if strings.Contains(body, "ACTIVE-ONLY") {
		t.Errorf("active CSS leaked into pin URL: %q", body)
	}

	// /template/<unknown>/style.css → 404.
	req = httptest.NewRequest("GET", "/template/99999/style.css", nil)
	w = httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != 404 {
		t.Errorf("unknown template css status = %d, want 404", w.Code)
	}
}

// TestPublicStyleCSSExpandsSiteParts confirms /style.css expands
// {site_parts} into the active template's `/template/<id>/` prefix,
// matching the behaviour the SB3 sb2.css fixture depends on (every
// background image URL uses {site_parts}).
func TestPublicStyleCSSExpandsSiteParts(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.DB.Exec(`UPDATE templates SET css = '@charset "{site_encoding}";' || X'0a' || 'body{background:url({site_parts}bg.gif)}' WHERE is_active = 1`); err != nil {
		t.Fatal(err)
	}
	var activeID int64
	_ = a.DB.QueryRow(`SELECT id FROM templates WHERE is_active = 1`).Scan(&activeID)

	req := httptest.NewRequest("GET", "/style.css", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, "{site_parts}") {
		t.Errorf("raw {site_parts} marker leaked: %s", body)
	}
	if !strings.Contains(body, "url(/template/"+itoa64str(activeID)+"/bg.gif)") {
		t.Errorf("expected expanded site_parts url; body: %s", body)
	}
	if !strings.Contains(body, `@charset "utf-8";`) {
		t.Errorf("expected expanded site_encoding; body: %s", body)
	}
}

// TestPerTemplateCSSExpandsSiteParts mirrors the active-template
// case for `/template/{id}/style.css`. site_parts has to resolve to
// THIS template's id, not the active one.
func TestPerTemplateCSSExpandsSiteParts(t *testing.T) {
	a := newTestApp(t)
	res, err := a.DB.Exec(`INSERT INTO templates (wid, name, is_active, main_body, entry_body, css, info, sort_order, created_at, updated_at)
		VALUES (1, 'pin', 0, '<!-- BEGIN entry --><!-- END entry -->', '', 'body{background:url({site_parts}pinbg.gif)}', '', 99, strftime('%s','now'), strftime('%s','now'))`)
	if err != nil {
		t.Fatal(err)
	}
	pinID, _ := res.LastInsertId()

	req := httptest.NewRequest("GET", "/template/"+itoa64str(pinID)+"/style.css", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "url(/template/"+itoa64str(pinID)+"/pinbg.gif)") {
		t.Errorf("expected pin-template site_parts; body: %s", body)
	}
}

// TestCategoryPageEmitsPinnedCSSURL confirms that when a category
// pins a non-active template, the rendered page's {site_css} tag
// points at the pinned template's CSS URL — closing the loop on the
// feature so readers actually load the right stylesheet.
func TestCategoryPageEmitsPinnedCSSURL(t *testing.T) {
	a := newTestApp(t)

	// Create a pinned template whose main_body echoes {site_css} so
	// we can assert on what the renderer emits.
	pinBody := "<!doctype html>\n<html><head><link href=\"{site_css}\" rel=\"stylesheet\"></head><body>\n<!-- BEGIN entry --><!-- END entry -->\n</body></html>\n"
	res, err := a.DB.Exec(`INSERT INTO templates (wid, name, is_active, main_body, entry_body, css, info, sort_order, created_at, updated_at)
		VALUES (1, 'pin', 0, ?, '', 'body{}', '', 99, strftime('%s','now'), strftime('%s','now'))`, pinBody)
	if err != nil {
		t.Fatal(err)
	}
	pinID, _ := res.LastInsertId()
	if _, err := a.DB.Exec(`UPDATE categories SET template_id = ? WHERE id = 1`, pinID); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/category/1/", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("category status = %d", w.Code)
	}
	wantHref := "/template/" + itoa64str(pinID) + "/style.css"
	if !strings.Contains(w.Body.String(), `href="`+wantHref+`"`) {
		t.Errorf("expected {site_css} to emit %q\nbody: %s", wantHref, w.Body.String())
	}
}
