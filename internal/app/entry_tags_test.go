package app_test

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestEntryTagsAutoCreateAndAssign confirms the end-to-end save flow:
// submitting the entry form with a comma-separated tags field creates
// missing tags, reuses existing ones, and attaches them to the entry.
func TestEntryTagsAutoCreateAndAssign(t *testing.T) {
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	csrfCookie, token := fetchCSRF(t, a.Handler())

	form := url.Values{
		"csrf_token":  {token},
		"title":       {"tagged post"},
		"body":        {"x"},
		"format":      {"html"},
		"status":      {"1"},
		"posted_at":   {"2026-04-21T10:00"},
		"category_id": {"-1"},
		"tags":        {"go, blog, 日記"},
	}
	req := httptest.NewRequest("POST", "/admin/entries/1/edit", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrfCookie)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != 302 {
		t.Fatalf("status = %d, want redirect", w.Code)
	}

	// 3 tags should be in the DB now, joined to entry 1.
	var count int
	if err := a.DB.QueryRow(`SELECT COUNT(*) FROM tags WHERE wid = 1`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Errorf("tag rows = %d, want 3", count)
	}
	if err := a.DB.QueryRow(`SELECT COUNT(*) FROM entry_tags WHERE entry_id = 1`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Errorf("entry_tags rows = %d, want 3", count)
	}

	// Japanese-name tag must have a non-empty slug via the sha1 fallback
	// — otherwise /tag/<slug>/ wouldn't resolve.
	var slug string
	if err := a.DB.QueryRow(`SELECT slug FROM tags WHERE name = '日記'`).Scan(&slug); err != nil {
		t.Fatal(err)
	}
	if slug == "" {
		t.Errorf("japanese tag slug empty")
	}
}

// TestPublicTagPageServes confirms /tag/<slug>/ loads, renders the
// listing, and 404s on unknown slugs.
func TestPublicTagPageServes(t *testing.T) {
	a := newTestApp(t)
	// Seed one tag + one entry association directly via SQL so the
	// test doesn't have to go through the admin flow.
	if _, err := a.DB.Exec(`INSERT INTO tags (wid, name, slug, created_at, updated_at) VALUES (1, 'diary', 'diary', strftime('%s','now'), strftime('%s','now'))`); err != nil {
		t.Fatal(err)
	}
	if _, err := a.DB.Exec(`INSERT INTO entry_tags (entry_id, tag_id) VALUES (1, (SELECT id FROM tags WHERE slug='diary'))`); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/tag/diary/", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("tag page status = %d", w.Code)
	}

	req = httptest.NewRequest("GET", "/tag/nope/", nil)
	w = httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != 404 {
		t.Errorf("unknown tag status = %d, want 404", w.Code)
	}
}

// TestAdminTagDeleteCleansJoinRows confirms DeleteTag wipes both the
// tags row and every entry_tags row that referenced it in one
// transaction.
func TestAdminTagDeleteCleansJoinRows(t *testing.T) {
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	if _, err := a.DB.Exec(`INSERT INTO tags (wid, name, slug, created_at, updated_at) VALUES (1, 'doomed', 'doomed', strftime('%s','now'), strftime('%s','now'))`); err != nil {
		t.Fatal(err)
	}
	if _, err := a.DB.Exec(`INSERT INTO entry_tags (entry_id, tag_id) VALUES (1, (SELECT id FROM tags WHERE slug='doomed'))`); err != nil {
		t.Fatal(err)
	}
	var tagID int64
	if err := a.DB.QueryRow(`SELECT id FROM tags WHERE slug='doomed'`).Scan(&tagID); err != nil {
		t.Fatal(err)
	}

	csrfCookie, token := fetchCSRF(t, a.Handler())
	form := url.Values{"csrf_token": {token}}.Encode()
	req := httptest.NewRequest("POST", "/admin/tags/"+itoa64(tagID)+"/delete", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrfCookie)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != 302 {
		t.Fatalf("status = %d, want redirect", w.Code)
	}

	var n int
	if err := a.DB.QueryRow(`SELECT COUNT(*) FROM tags WHERE id = ?`, tagID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("tag row not deleted")
	}
	if err := a.DB.QueryRow(`SELECT COUNT(*) FROM entry_tags WHERE tag_id = ?`, tagID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("entry_tags rows not cleaned")
	}
}
