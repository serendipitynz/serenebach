package app_test

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestCategoryDescriptionSurfacesOnPublicPage confirms the admin form
// saves the description + the public /category/<id>/ page renders it
// via {category_description}.
func TestCategoryDescriptionSurfacesOnPublicPage(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	// Edit seed category #1 to carry a description.
	csrfCookie, token := fetchCSRF(t, a.Handler())
	form := url.Values{
		"csrf_token":  {token},
		"name":        {"お知らせ"},
		"slug":        {"news"},
		"parent_id":   {"0"},
		"sort_order":  {"0"},
		"description": {"運営のお知らせ専用カテゴリーです。"},
		"template_id": {"0"},
	}
	req := httptest.NewRequest("POST", "/admin/categories/1/edit", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrfCookie)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != 302 {
		t.Fatalf("save status = %d", w.Code)
	}

	// DB roundtrip check.
	var stored string
	if err := a.DB.QueryRow(`SELECT description FROM categories WHERE id = 1`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != "運営のお知らせ専用カテゴリーです。" {
		t.Errorf("description persisted = %q", stored)
	}

	// Inject a category-template directive into the active template so
	// the description actually appears in the rendered output — default
	// seed template may not reference the tag.
	if _, err := a.DB.Exec(`UPDATE templates SET main_body = main_body || '<div class="cat-desc">{category_description}</div>' WHERE is_active = 1`); err != nil {
		t.Fatal(err)
	}

	w = httptest.NewRecorder()
	a.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/category/1/", nil))
	if w.Code != 200 {
		t.Fatalf("category page status = %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `<div class="cat-desc">運営のお知らせ専用カテゴリーです。</div>`) {
		t.Errorf("{category_description} tag did not surface on category page\nbody snippet: %s", body)
	}
}

// TestCategoryTemplatePinOverridesArchivePin sets categories.template_id
// to a specific template and confirms the category page renders from
// that template — overriding both the archive pin and the active
// template.
func TestCategoryTemplatePinOverridesArchivePin(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	// Create a second template with a distinct marker string so we can
	// tell from the output which one rendered the page.
	pinBody := "<!doctype html>\n<html><body>\n<!-- BEGIN entry -->\n<article>{entry_title}</article>\n<!-- END entry -->\n<marker>CAT-PIN-RAN</marker>\n</body></html>\n"
	res, err := a.DB.Exec(`INSERT INTO templates (wid, name, is_active, main_body, entry_body, css, info, sort_order, created_at, updated_at)
		VALUES (1, 'cat-pin', 0, ?, '', '', '', 99, strftime('%s','now'), strftime('%s','now'))`, pinBody)
	if err != nil {
		t.Fatal(err)
	}
	pinID, _ := res.LastInsertId()

	// Point category #1 at the new template via the admin form.
	csrfCookie, token := fetchCSRF(t, a.Handler())
	form := url.Values{
		"csrf_token":  {token},
		"name":        {"お知らせ"},
		"slug":        {"news"},
		"parent_id":   {"0"},
		"sort_order":  {"0"},
		"description": {""},
		"template_id": {itoa64str(pinID)},
	}
	req := httptest.NewRequest("POST", "/admin/categories/1/edit", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrfCookie)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != 302 {
		t.Fatalf("save status = %d", w.Code)
	}

	// The category page now uses the pinned template.
	w = httptest.NewRecorder()
	a.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/category/1/", nil))
	if w.Code != 200 {
		t.Fatalf("category status = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "CAT-PIN-RAN") {
		t.Errorf("pinned template did not render the category page\nbody: %s", w.Body.String())
	}

	// Sanity: home page still uses the active template, not the pin.
	w = httptest.NewRecorder()
	a.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if strings.Contains(w.Body.String(), "CAT-PIN-RAN") {
		t.Errorf("pinned template leaked onto home page")
	}
}

// itoa64str formats an int64 without pulling strconv into this test
// file — one use.
func itoa64str(n int64) string {
	if n == 0 {
		return "0"
	}
	buf := []byte{}
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	if neg {
		buf = append([]byte{'-'}, buf...)
	}
	return string(buf)
}
