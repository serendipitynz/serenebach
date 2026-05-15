package app_test

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestEntryPageUsesCategoryTemplate verifies the SB3-compatible template
// priority for individual entry pages:
//
//  1. active template is the fallback
//  2. if the entry's main category has a template pin, that template wins
//  3. within the selected template, EntryBody beats MainBody
func TestEntryPageUsesCategroyTemplate(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	now := time.Now().Unix()

	// Insert a category template whose EntryBody is uniquely identifiable.
	res, err := a.DB.Exec(`
		INSERT INTO templates (wid, name, is_active, main_body, entry_body, css, info, sort_order, created_at, updated_at)
		VALUES (1, 'cat-tmpl', 0,
			'<!-- BEGIN entry -->' || char(10) || 'CATEGORY_MAIN:{entry_title}' || char(10) || '<!-- END entry -->' || char(10),
			'<!-- BEGIN entry -->' || char(10) || 'CATEGORY_ENTRY:{entry_title}' || char(10) || '<!-- END entry -->' || char(10),
			'', '', 0, ?, ?)`, now, now)
	if err != nil {
		t.Fatal(err)
	}
	catTmplID, _ := res.LastInsertId()

	// Update the active template so its marker is clearly distinct.
	if _, err := a.DB.Exec(`UPDATE templates SET
		main_body = '<!-- BEGIN entry -->' || char(10) || 'ACTIVE:{entry_title}' || char(10) || '<!-- END entry -->' || char(10),
		entry_body = ''
		WHERE is_active = 1`); err != nil {
		t.Fatal(err)
	}

	// Insert a category pinned to catTmplID.
	res, err = a.DB.Exec(`
		INSERT INTO categories (wid, parent_id, name, slug, sort_order, template_id, created_at, updated_at)
		VALUES (1, 0, 'Pinned Cat', 'pinned-cat', 0, ?, ?, ?)`, catTmplID, now, now)
	if err != nil {
		t.Fatal(err)
	}
	catID, _ := res.LastInsertId()

	// Insert a published entry in that category.
	res, err = a.DB.Exec(`
		INSERT INTO entries (wid, author_id, category_id, title, body, more, format, status, posted_at, created_at, updated_at)
		VALUES (1, 1, ?, 'PinnedTitle', '<p>body</p>', '', 'html', 1, ?, ?, ?)`,
		catID, now, now, now)
	if err != nil {
		t.Fatal(err)
	}
	entryID, _ := res.LastInsertId()

	req := httptest.NewRequest("GET", fmt.Sprintf("/entry/%d/", entryID), nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d; body:\n%s", w.Code, w.Body.String())
	}
	body := w.Body.String()

	if !strings.Contains(body, "CATEGORY_ENTRY:PinnedTitle") {
		t.Errorf("expected category EntryBody; body:\n%s", body)
	}
	if strings.Contains(body, "ACTIVE:") {
		t.Errorf("active template must not appear; body:\n%s", body)
	}
	if strings.Contains(body, "CATEGORY_MAIN:") {
		t.Errorf("EntryBody should beat MainBody; body:\n%s", body)
	}
}

// TestEntryPageCategoryTemplateFallsBackToMainBody verifies that when the
// category template has no EntryBody, MainBody is used as the fallback.
func TestEntryPageCategoryTemplateFallsBackToMainBody(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	now := time.Now().Unix()

	res, err := a.DB.Exec(`
		INSERT INTO templates (wid, name, is_active, main_body, entry_body, css, info, sort_order, created_at, updated_at)
		VALUES (1, 'cat-main-only', 0,
			'<!-- BEGIN entry -->' || char(10) || 'CATEGORY_MAIN:{entry_title}' || char(10) || '<!-- END entry -->' || char(10),
			'',
			'', '', 0, ?, ?)`, now, now)
	if err != nil {
		t.Fatal(err)
	}
	catTmplID, _ := res.LastInsertId()

	if _, err := a.DB.Exec(`UPDATE templates SET
		main_body = '<!-- BEGIN entry -->' || char(10) || 'ACTIVE:{entry_title}' || char(10) || '<!-- END entry -->' || char(10),
		entry_body = ''
		WHERE is_active = 1`); err != nil {
		t.Fatal(err)
	}

	res, err = a.DB.Exec(`
		INSERT INTO categories (wid, parent_id, name, slug, sort_order, template_id, created_at, updated_at)
		VALUES (1, 0, 'Main Only Cat', 'main-only-cat', 0, ?, ?, ?)`, catTmplID, now, now)
	if err != nil {
		t.Fatal(err)
	}
	catID, _ := res.LastInsertId()

	res, err = a.DB.Exec(`
		INSERT INTO entries (wid, author_id, category_id, title, body, more, format, status, posted_at, created_at, updated_at)
		VALUES (1, 1, ?, 'MainTitle', '<p>body</p>', '', 'html', 1, ?, ?, ?)`,
		catID, now, now, now)
	if err != nil {
		t.Fatal(err)
	}
	entryID, _ := res.LastInsertId()

	req := httptest.NewRequest("GET", fmt.Sprintf("/entry/%d/", entryID), nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d; body:\n%s", w.Code, w.Body.String())
	}
	body := w.Body.String()

	if !strings.Contains(body, "CATEGORY_MAIN:MainTitle") {
		t.Errorf("expected category MainBody fallback; body:\n%s", body)
	}
	if strings.Contains(body, "ACTIVE:") {
		t.Errorf("active template must not appear; body:\n%s", body)
	}
}

// TestEntryPageActiveFallbackWhenNoCategoryTemplate confirms that an entry
// whose category has no template pin renders with the active template.
func TestEntryPageActiveFallbackWhenNoCategoryTemplate(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)

	if _, err := a.DB.Exec(`UPDATE templates SET
		main_body = '<!-- BEGIN entry -->' || char(10) || 'ACTIVE:{entry_title}' || char(10) || '<!-- END entry -->' || char(10),
		entry_body = ''
		WHERE is_active = 1`); err != nil {
		t.Fatal(err)
	}

	// Use the existing seeded entry (id=1) which belongs to the seeded category
	// that has no template pin (template_id=0).
	req := httptest.NewRequest("GET", "/entry/1/", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d; body:\n%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "ACTIVE:") {
		t.Errorf("expected active template; body:\n%s", w.Body.String())
	}
}
