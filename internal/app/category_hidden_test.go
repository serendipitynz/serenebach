package app_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestPublicHomeHidesEntriesInHiddenCategory plants a published entry
// in a hidden category and asserts it does not appear on the home page
// (which lists recent published entries irrespective of category).
func TestPublicHomeHidesEntriesInHiddenCategory(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)

	ctx := context.Background()
	now := time.Now().Unix()
	res, err := a.DB.ExecContext(ctx, `
		INSERT INTO categories (wid, parent_id, name, slug, sort_order, hidden, created_at, updated_at)
		VALUES (1, 0, 'Internal', 'internal', 0, 1, ?, ?)`, now, now)
	if err != nil {
		t.Fatal(err)
	}
	hiddenCat, _ := res.LastInsertId()
	if _, err := a.DB.ExecContext(ctx, `
		INSERT INTO entries (wid, author_id, category_id, title, body, more, format, status, posted_at, created_at, updated_at)
		VALUES (1, 1, ?, 'HIDDEN-ENTRY', '<p>hidden</p>', '', '', 1, ?, ?, ?)`,
		hiddenCat, now, now, now); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "HIDDEN-ENTRY") {
		t.Errorf("hidden-category entry leaked into home page")
	}
}

// TestPublicArchiveHidesEntriesInHiddenCategory plants the same fixture
// and asserts the entry drops out of the year-archive surface too.
func TestPublicArchiveHidesEntriesInHiddenCategory(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)

	ctx := context.Background()
	now := time.Now()
	posted := now.Unix()
	res, err := a.DB.ExecContext(ctx, `
		INSERT INTO categories (wid, parent_id, name, slug, sort_order, hidden, created_at, updated_at)
		VALUES (1, 0, 'Internal', 'internal', 0, 1, ?, ?)`, posted, posted)
	if err != nil {
		t.Fatal(err)
	}
	hiddenCat, _ := res.LastInsertId()
	if _, err := a.DB.ExecContext(ctx, `
		INSERT INTO entries (wid, author_id, category_id, title, body, more, format, status, posted_at, created_at, updated_at)
		VALUES (1, 1, ?, 'HIDDEN-IN-ARCHIVE', '<p>x</p>', '', '', 1, ?, ?, ?)`,
		hiddenCat, posted, posted, posted); err != nil {
		t.Fatal(err)
	}

	url := "/archive/" + itoa(now.Year())
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, httptest.NewRequest("GET", url, nil))
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	if strings.Contains(mainArea(w.Body.String()), "HIDDEN-IN-ARCHIVE") {
		t.Errorf("hidden-category entry leaked into archive page")
	}
}

// TestHiddenCategoryArchivePageStillResponds confirms the archive page
// of a hidden category is reachable by direct URL. The dynamic route
// keeps responding 200; only listing surfaces drop it. The slug-aware
// route is the canonical surface (the id form 301-redirects to it), so
// the assertion targets /category/<slug>/ directly.
func TestHiddenCategoryArchivePageStillResponds(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)

	ctx := context.Background()
	now := time.Now().Unix()
	if _, err := a.DB.ExecContext(ctx, `
		INSERT INTO categories (wid, parent_id, name, slug, sort_order, hidden, created_at, updated_at)
		VALUES (1, 0, 'Internal', 'internal', 0, 1, ?, ?)`, now, now); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/category/internal/", nil))
	if w.Code != 200 {
		t.Fatalf("hidden category archive page status = %d, want 200", w.Code)
	}
}

// TestHiddenCategoryEntryPermalinkStillResponds confirms an entry that
// lives in a hidden category remains reachable at its individual
// permalink — the design explicitly keeps the entry URL live.
func TestHiddenCategoryEntryPermalinkStillResponds(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)

	ctx := context.Background()
	now := time.Now().Unix()
	res, err := a.DB.ExecContext(ctx, `
		INSERT INTO categories (wid, parent_id, name, slug, sort_order, hidden, created_at, updated_at)
		VALUES (1, 0, 'Internal', 'internal', 0, 1, ?, ?)`, now, now)
	if err != nil {
		t.Fatal(err)
	}
	hiddenCat, _ := res.LastInsertId()
	er, err := a.DB.ExecContext(ctx, `
		INSERT INTO entries (wid, author_id, category_id, title, body, more, format, status, posted_at, created_at, updated_at)
		VALUES (1, 1, ?, 'STILL-LIVE', '<p>still live</p>', '', '', 1, ?, ?, ?)`,
		hiddenCat, now, now, now)
	if err != nil {
		t.Fatal(err)
	}
	entryID, _ := er.LastInsertId()

	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/entry/"+itoa64(entryID)+"/", nil))
	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "STILL-LIVE") {
		t.Errorf("entry body missing; surface should keep hidden-category permalinks live")
	}
}

// TestHiddenCategoryDropsFromRecentCommentList plants approved comments
// on a visible-category entry and a hidden-category entry, then
// renders the sidebar {recent_comment_list} block. The visible entry's
// comment must appear and the hidden one must not — otherwise the
// sidebar re-exposes a permalink the listing surfaces deliberately
// dropped.
func TestHiddenCategoryDropsFromRecentCommentList(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)

	ctx := context.Background()
	now := time.Now().Unix()
	// Render the sidebar block. BEGIN/END markers must sit on their
	// own lines for the line-based sbtemplate parser.
	mainBody := "<!-- BEGIN entry -->\n" +
		"<article></article>\n" +
		"<!-- END entry -->\n" +
		"<!-- BEGIN recent_comment -->\n" +
		"<section class=\"rc\">{recent_comment_list}</section>\n" +
		"<!-- END recent_comment -->\n"
	if _, err := a.DB.ExecContext(ctx,
		`UPDATE templates SET main_body = ? WHERE is_active = 1`, mainBody); err != nil {
		t.Fatal(err)
	}

	// Visible category + entry + approved comment.
	visRes, err := a.DB.ExecContext(ctx, `
		INSERT INTO categories (wid, parent_id, name, slug, sort_order, hidden, created_at, updated_at)
		VALUES (1, 0, 'Public', 'public', 0, 0, ?, ?)`, now, now)
	if err != nil {
		t.Fatal(err)
	}
	visCat, _ := visRes.LastInsertId()
	visEntry, err := a.DB.ExecContext(ctx, `
		INSERT INTO entries (wid, author_id, category_id, title, body, more, format, status, posted_at, created_at, updated_at)
		VALUES (1, 1, ?, 'VISIBLE-ENTRY', '<p>x</p>', '', '', 1, ?, ?, ?)`,
		visCat, now, now, now)
	if err != nil {
		t.Fatal(err)
	}
	visEntryID, _ := visEntry.LastInsertId()
	if _, err := a.DB.ExecContext(ctx, `
		INSERT INTO messages (wid, entry_id, status, posted_at, author_name, author_email, author_url, body, ip_address, user_agent, created_at, updated_at)
		VALUES (1, ?, 1, ?, 'VisibleCommenter', '', '', 'visible-comment-body', '', '', ?, ?)`,
		visEntryID, now, now, now); err != nil {
		t.Fatal(err)
	}

	// Hidden category + entry + approved comment.
	hidRes, err := a.DB.ExecContext(ctx, `
		INSERT INTO categories (wid, parent_id, name, slug, sort_order, hidden, created_at, updated_at)
		VALUES (1, 0, 'Internal', 'internal', 0, 1, ?, ?)`, now, now)
	if err != nil {
		t.Fatal(err)
	}
	hidCat, _ := hidRes.LastInsertId()
	hidEntry, err := a.DB.ExecContext(ctx, `
		INSERT INTO entries (wid, author_id, category_id, title, body, more, format, status, posted_at, created_at, updated_at)
		VALUES (1, 1, ?, 'INTERNAL-ENTRY', '<p>x</p>', '', '', 1, ?, ?, ?)`,
		hidCat, now+1, now+1, now+1) // posted slightly later so it would rank above otherwise
	if err != nil {
		t.Fatal(err)
	}
	hidEntryID, _ := hidEntry.LastInsertId()
	if _, err := a.DB.ExecContext(ctx, `
		INSERT INTO messages (wid, entry_id, status, posted_at, author_name, author_email, author_url, body, ip_address, user_agent, created_at, updated_at)
		VALUES (1, ?, 1, ?, 'HiddenCommenter', '', '', 'hidden-comment-body', '', '', ?, ?)`,
		hidEntryID, now+1, now+1, now+1); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "VisibleCommenter") {
		t.Fatalf("visible-category comment missing — block did not render, hidden assertion would be vacuous\nbody:\n%s", body)
	}
	if strings.Contains(body, "HiddenCommenter") {
		t.Errorf("recent_comment_list leaked a hidden-category comment\nbody:\n%s", body)
	}
	if strings.Contains(body, "INTERNAL-ENTRY") {
		t.Errorf("recent_comment_list leaked hidden-category entry title\nbody:\n%s", body)
	}
}

// TestHiddenCategoryDropsFromSidebarList plants a hidden category and
// asserts the sidebar {category_list} fragment never offers a link to
// it on home, even though it is a top-level category.
func TestHiddenCategoryDropsFromSidebarList(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)

	ctx := context.Background()
	now := time.Now().Unix()
	// Inject a {category_list} renderer into the active template's main
	// body so the assertion can look for the actual rendered fragment.
	if _, err := a.DB.ExecContext(ctx,
		`UPDATE templates SET main_body = '<!-- BEGIN category --><nav class="cats">{category_list}</nav><!-- END category --><!-- BEGIN entry --><article></article><!-- END entry -->' WHERE is_active = 1`); err != nil {
		t.Fatal(err)
	}
	// Seed a hidden category with an entry so its sidebar count would
	// otherwise be > 0 and it would render.
	res, err := a.DB.ExecContext(ctx, `
		INSERT INTO categories (wid, parent_id, name, slug, sort_order, hidden, created_at, updated_at)
		VALUES (1, 0, 'Internal', 'internal', 0, 1, ?, ?)`, now, now)
	if err != nil {
		t.Fatal(err)
	}
	hiddenCat, _ := res.LastInsertId()
	if _, err := a.DB.ExecContext(ctx, `
		INSERT INTO entries (wid, author_id, category_id, title, body, more, format, status, posted_at, created_at, updated_at)
		VALUES (1, 1, ?, 'INTERNAL-ENTRY', '<p>x</p>', '', '', 1, ?, ?, ?)`,
		hiddenCat, now, now, now); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, "/category/internal/") {
		t.Errorf("sidebar category_list leaked hidden category link\nbody:\n%s", body)
	}
}
