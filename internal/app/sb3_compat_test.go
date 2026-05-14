package app_test

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// TestSB3CompatEntryAliasTags confirms the entry permalink page emits
// every SB3-spec tag alias the Go port needs for existing SB3
// templates to resolve without gaps. These assertions double as a
// regression net — if someone renames a tag, the SB3 spelling has to
// keep working.
func TestSB3CompatEntryAliasTags(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	// Swap the active template to exercise every alias the fix touches.
	main := "<!doctype html><html><body>\n" +
		"<!-- BEGIN entry -->\n" +
		`<article data-id="{entry_id}" data-catid="{category_id}" data-catname="{category_disp_name}"` + "\n" +
		`  data-userlogin="{user_login}" data-userdisp="{user_disp_name}" data-userid="{user_id}"` + "\n" +
		`  data-keyword="{entry_keyword}" data-keywords="{entry_keywords}"` + "\n" +
		`  data-permalink="{permalink}">{entry_title}</article>` + "\n" +
		"<!-- END entry -->\n" +
		"<!-- BEGIN sequel -->\n" +
		`<nav data-prev="{prev_permalink}" data-prev-title="{prev_title}"` + "\n" +
		`  data-next="{next_permalink}" data-next-title="{next_title}"></nav>` + "\n" +
		"<!-- END sequel -->\n" +
		"</body></html>\n"
	if _, err := a.DB.Exec(`UPDATE templates SET main_body = ? WHERE is_active = 1`, main); err != nil {
		t.Fatal(err)
	}
	// Seed a second entry so prev/next resolve on the single-entry
	// page. Seed already ships two entries; entry 1 has a neighbor.
	if _, err := a.DB.Exec(`UPDATE entries SET keywords = 'go, sqlite' WHERE id = 1`); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/entry/1/", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()

	for _, want := range []string{
		`data-id="1"`,
		`data-catid="1"`,
		`data-userid="1"`,
		`data-userlogin="admin"`,     // login name
		`data-userdisp="admin"`,      // display name (seed puts both equal)
		`data-keyword="go, sqlite"`,  // singular SB3 tag
		`data-keywords="go, sqlite"`, // plural alias kept
		`data-permalink="/entry/1/"`, // {permalink} alias
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q\nbody: %s", want, body)
		}
	}
	// Sequel block: prev-or-next title should land in either
	// prev_title or next_title slot depending on which neighbor
	// exists. Entry 1 is the newest; next is empty, prev points at
	// the older entry.
	if !strings.Contains(body, `data-prev="/entry/2/"`) && !strings.Contains(body, `data-next="/entry/2/"`) {
		t.Errorf("neither prev_permalink nor next_permalink resolved for entry 1\nbody: %s", body)
	}
}

// TestSB3CompatSiteLevelTags confirms the site-level tag catalogue
// SB3's Common::_main emits resolves correctly on each page kind —
// {script_name}/{script_version}/{script_webpage}, {mode_name} +
// {mode_id}, {blog_name_only}, {site_mobile}, {site_rsd}. The
// assertions focus on what's specific to the route so a later
// refactor that accidentally drops a tag surfaces here.
func TestSB3CompatSiteLevelTags(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	main := "<!doctype html><html><body>\n" +
		`<div data-mode-name="{mode_name}" data-mode-id="{mode_id}"` + "\n" +
		`  data-blog-only="{blog_name_only}" data-script="{script_name}"` + "\n" +
		`  data-version="{script_version}" data-mobile="{site_mobile}"` + "\n" +
		`  data-rsd="{site_rsd}" data-archive="{selected_archive}"></div>` + "\n" +
		"<!-- BEGIN entry -->\n<article></article>\n<!-- END entry -->\n" +
		"</body></html>\n"
	if _, err := a.DB.Exec(`UPDATE templates SET main_body = ? WHERE is_active = 1`, main); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		url        string
		wantMode   string
		wantModeID string
		wantArc    string
	}{
		{"/", "page", "", ""},
		{"/entry/1/", "entry", "1", "ようこそ Serene Bach へ"},
		{"/category/news/", "category", "1", "Category: お知らせ"},
		{"/archive/2026/04/", "archive", "202604", "Archive: 2026/04"},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, httptest.NewRequest("GET", tc.url, nil))
		if w.Code != 200 {
			t.Errorf("%s: status = %d", tc.url, w.Code)
			continue
		}
		body := w.Body.String()
		for _, want := range []string{
			`data-mode-name="` + tc.wantMode + `"`,
			`data-mode-id="` + tc.wantModeID + `"`,
			`data-blog-only="Serene Bach"`,
			`data-script="Serene Bach"`,
			`data-mobile=""`,
			`data-rsd="/rsd.xml"`,
		} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: missing %q", tc.url, want)
			}
		}
		if tc.wantArc != "" && !strings.Contains(body, `data-archive="`+tc.wantArc+`"`) {
			t.Errorf("%s: selected_archive missing %q", tc.url, tc.wantArc)
		}
	}
}

// TestSB3CompatOptionBlock confirms SB3's `option` block — "render
// these bits only on entry pages" gate — fires 1 on /entry/<id>/ and
// 0 on list views.
func TestSB3CompatOptionBlock(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	main := "<!doctype html><html><body>\n" +
		"<!-- BEGIN entry -->\n<article></article>\n<!-- END entry -->\n" +
		"<!-- BEGIN option -->\n<div class=\"only-on-entry\"></div>\n<!-- END option -->\n" +
		"</body></html>\n"
	if _, err := a.DB.Exec(`UPDATE templates SET main_body = ? WHERE is_active = 1`, main); err != nil {
		t.Fatal(err)
	}

	// Entry page should render the block.
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/entry/1/", nil))
	if w.Code != 200 {
		t.Fatalf("entry status = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `class="only-on-entry"`) {
		t.Errorf("option block not rendered on entry page")
	}

	// Home should strip it.
	w = httptest.NewRecorder()
	a.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if strings.Contains(w.Body.String(), `class="only-on-entry"`) {
		t.Errorf("option block leaked onto home page")
	}
}

// TestSB3CompatSidebarBlocks confirms the SB3 sidebar callbacks the
// Go port was previously stripping now emit their respective list
// fragments when data is available. Uses the seed content (2 entries,
// 1 category, 1 approved comment if we seed one).
func TestSB3CompatSidebarBlocks(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	// Plant one approved comment so recent_comment has something.
	if _, err := a.DB.Exec(`INSERT INTO messages (wid, entry_id, status, posted_at, author_name, author_email, author_url, body, ip_address, user_agent, created_at, updated_at)
		VALUES (1, 1, 1, strftime('%s','now'), 'Visitor', '', '', 'nice post', '', '', strftime('%s','now'), strftime('%s','now'))`); err != nil {
		t.Fatal(err)
	}

	main := "<!doctype html><html><body>\n" +
		"<!-- BEGIN entry -->\n<article></article>\n<!-- END entry -->\n" +
		"<!-- BEGIN archives -->\n<section class=\"arc\">{archives_list}</section>\n<!-- END archives -->\n" +
		"<!-- BEGIN category -->\n<section class=\"cat\">{category_list}</section>\n<!-- END category -->\n" +
		"<!-- BEGIN recent_comment -->\n<section class=\"rc\">{recent_comment_list}</section>\n<!-- END recent_comment -->\n" +
		"<!-- BEGIN latest_entry -->\n<section class=\"le\">{latest_entry_list}</section>\n<!-- END latest_entry -->\n" +
		"</body></html>\n"
	if _, err := a.DB.Exec(`UPDATE templates SET main_body = ? WHERE is_active = 1`, main); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	// Each of the four sidebar block families should produce a <ul>.
	// Specific content varies by seed; we just check the shape landed.
	for _, marker := range []string{
		`class="arc"><ul>`,
		`class="cat"><ul>`,
		`class="rc"><ul>`,
		`class="le"><ul>`,
	} {
		if !strings.Contains(body, marker) {
			t.Errorf("missing sidebar marker %q\nbody snippet: %s", marker, body)
		}
	}
	// Latest entry block should link to a seed entry.
	if !strings.Contains(body, `ようこそ Serene Bach へ</a>`) {
		t.Errorf("latest_entry_list does not link to seeded entry")
	}
	// Recent comment block should carry the planted author name.
	if !strings.Contains(body, `Visitor</li>`) {
		t.Errorf("recent_comment_list does not show planted comment")
	}
}

// TestSB3CompatCategoryAreaBlock confirms /category/<id>/ renders
// with the SB3 `category_area` block populated — block count = 1
// with the expected tags inside.
func TestSB3CompatCategoryAreaBlock(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	// Swap the active template to include the block.
	main := "<!doctype html><html><body>\n" +
		"<!-- BEGIN entry -->\n<article></article>\n<!-- END entry -->\n" +
		"<!-- BEGIN category_area -->\n" +
		`<header><h1>{category_pagename}</h1><p>{category_fullname}</p><div>{category_description}</div></header>` + "\n" +
		"<!-- END category_area -->\n" +
		"</body></html>\n"
	if _, err := a.DB.Exec(`UPDATE templates SET main_body = ? WHERE is_active = 1`, main); err != nil {
		t.Fatal(err)
	}
	// Give the seed category an explicit description so we can
	// tell the block actually populated.
	if _, err := a.DB.Exec(`UPDATE categories SET description = 'announcements channel' WHERE id = 1`); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/category/news/", nil))
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `<h1>お知らせ</h1>`) {
		t.Errorf("category_pagename missing\nbody: %s", body)
	}
	if !strings.Contains(body, `<div>announcements channel</div>`) {
		t.Errorf("category_description missing\nbody: %s", body)
	}

	// Home page should NOT render the category_area block (it's
	// gated — only category pages populate it).
	w = httptest.NewRecorder()
	a.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if strings.Contains(w.Body.String(), `announcements channel`) {
		t.Errorf("category_area leaked onto home page")
	}
}
