package app_test

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestProfileDescriptionRoundtripsThroughSave confirms the profile
// form persists a markdown description + format select. The actual
// {profile_description} render happens in profile_area (the user-
// profile page block) which isn't wired up yet; this test just proves
// the stored values are right so that downstream render will be
// correct once that route lands.
func TestProfileDescriptionRoundtripsThroughSave(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	csrfCookie, token := fetchCSRF(t, a.Handler())

	form := url.Values{
		"csrf_token":         {token},
		"name":               {"admin"},
		"display_name":       {"Admin"},
		"email":              {""},
		"description":        {"# Heading\n\n**bold** text"},
		"description_format": {"markdown"},
		"list_visible":       {"on"},
	}
	req := httptest.NewRequest("POST", "/admin/profile", strings.NewReader(form.Encode()))
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

	var desc, descFmt string
	if err := a.DB.QueryRow(`SELECT description, description_format FROM users WHERE name = 'admin'`).
		Scan(&desc, &descFmt); err != nil {
		t.Fatal(err)
	}
	if desc != "# Heading\n\n**bold** text" {
		t.Errorf("description = %q", desc)
	}
	if descFmt != "markdown" {
		t.Errorf("description_format = %q, want markdown", descFmt)
	}
}

// TestCategoryDescriptionMarkdownFormat confirms the same pipeline
// works for categories.
func TestCategoryDescriptionMarkdownFormat(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	csrfCookie, token := fetchCSRF(t, a.Handler())

	form := url.Values{
		"csrf_token":         {token},
		"name":               {"お知らせ"},
		"slug":               {"news"},
		"parent_id":          {"0"},
		"sort_order":         {"0"},
		"description":        {"**important** announcement"},
		"description_format": {"markdown"},
		"template_id":        {"0"},
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

	// Inject {category_description} into the active template so we
	// can assert on the rendered output.
	if _, err := a.DB.Exec(`UPDATE templates SET main_body = main_body || '<div class="cat">{category_description}</div>' WHERE is_active = 1`); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	a.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/category/1/", nil))
	if w.Code != 200 {
		t.Fatalf("category status = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "<strong>important</strong>") {
		t.Errorf("category markdown not rendered\nbody: %s", w.Body.String())
	}
}

// TestUsersListColumnsOrder confirms the admin user list renders the
// columns in the spec order (drag / ID / name / display / email /
// role / delete — no dedicated edit column, since the username
// itself is the edit link).
func TestUsersListColumnsOrder(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	w := authedGET(t, a.Handler(), "/admin/users", cookies)
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()

	// Header order check: each column name appears once, in the
	// expected sequence.
	headers := []string{"ユーザー名", "表示名", "メール", "権限", "削除"}
	last := -1
	for _, h := range headers {
		idx := strings.Index(body, h)
		if idx < 0 {
			t.Errorf("missing header %q", h)
			continue
		}
		if idx < last {
			t.Errorf("header %q appears before a prior header (out of order)", h)
		}
		last = idx
	}
	if strings.Contains(body, "<th class=\"col-action\">編集</th>") {
		t.Errorf("dedicated 編集 column should be gone — username itself is the edit link now")
	}
	if !strings.Contains(body, `<a href="/admin/users/1/edit">admin</a>`) {
		t.Errorf("username cell should render as the edit anchor")
	}
}
