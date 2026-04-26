package app_test

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestProfileRouteRendersProfileArea confirms the /profile/{id}/ page
// 200s and emits the profile_area block for a list_visible user.
func TestProfileRouteRendersProfileArea(t *testing.T) {
	a := newTestApp(t)

	main := "<!doctype html><html><body>\n" +
		"<!-- BEGIN entry -->\n<article></article>\n<!-- END entry -->\n" +
		"<!-- BEGIN profile_area -->\n" +
		`<section data-mode="{mode_name}" data-id="{mode_id}">` + "\n" +
		`<h2>{profile_name}</h2><p class="login">{user_name}</p>` + "\n" +
		"</section>\n<!-- END profile_area -->\n" +
		"</body></html>\n"
	if _, err := a.DB.Exec(`UPDATE templates SET main_body = ? WHERE is_active = 1`, main); err != nil {
		t.Fatal(err)
	}
	if _, err := a.DB.Exec(`UPDATE users SET display_name = 'Jane', description = 'hi' WHERE id = 1`); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/profile/1/", nil))
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{
		`data-mode="profile"`,
		`data-id="1"`,
		`<h2>Jane</h2>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
}

// TestProfileRouteHiddenWhenListVisibleFalse: a user with
// list_visible=0 should 404 on /profile/{id}/.
func TestProfileRouteHiddenWhenListVisibleFalse(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.DB.Exec(`UPDATE users SET list_visible = 0 WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/profile/1/", nil))
	if w.Code != 404 {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// TestLegacyCGIRedirectsPerMode verifies every mode= shape the shim
// supports resolves to the native URL. eid / cid resolution goes
// through the legacy_id lookup (Go autoincrement ids do not match SB3
// ids in general), so we seed concrete legacy_id values and assert the
// canonical Go id ends up in the Location header.
func TestLegacyCGIRedirectsPerMode(t *testing.T) {
	a := newTestApp(t)

	// Seed an entry whose Go id != SB3 legacy_id so the test would fail
	// if the shim skipped the lookup and just echoed the query value.
	if _, err := a.DB.Exec(`
		INSERT INTO entries (id, wid, author_id, category_id, title, status, posted_at, created_at, updated_at, legacy_id)
		VALUES (199, 1, 1, -1, 'imported entry', 1, 1700000000, 1700000000, 1700000000, 42)`); err != nil {
		t.Fatal(err)
	}
	if _, err := a.DB.Exec(`
		INSERT INTO categories (id, wid, parent_id, name, sort_order, created_at, updated_at, legacy_id)
		VALUES (88, 1, 0, 'imported cat', 0, 1700000000, 1700000000, 7)`); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		path     string
		wantCode int
		wantLoc  string
	}{
		{"/sb.cgi?mode=entry&eid=42", 301, "/entry/199/"},
		{"/sb.cgi?mode=entry&eid=9999", 404, ""},
		{"/sb.cgi?mode=category&cid=7", 301, "/category/88/"},
		{"/sb.cgi?mode=category&cid=9999", 404, ""},
		{"/sb.cgi?mode=archive&cond=2026", 301, "/archive/2026/"},
		{"/sb.cgi?mode=archive&cond=202604", 301, "/archive/2026/04/"},
		{"/sb.cgi?mode=user&pid=1", 301, "/profile/1/"},
		{"/sb.cgi?mode=comment&eid=5", 301, "/entry/5/#comment-form"},
		{"/sb.cgi?mode=search&q=hi", 404, ""},
		{"/sb.cgi?mode=", 301, "/"},
		// Mode-less external permalinks (SB3 permalink() output).
		{"/sb.cgi?eid=42", 301, "/entry/199/"},
		{"/sb.cgi?cid=7", 301, "/category/88/"},
		{"/sb.cgi?month=202604", 301, "/archive/2026/04/"},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
		if w.Code != tc.wantCode {
			t.Errorf("%s: code = %d want %d", tc.path, w.Code, tc.wantCode)
		}
		if tc.wantLoc != "" {
			if got := w.Result().Header.Get("Location"); got != tc.wantLoc {
				t.Errorf("%s: Location = %q want %q", tc.path, got, tc.wantLoc)
			}
		}
	}
}

// TestLegacyStaticRedirects covers the SB3 static-archive URL middleware:
// /log/eidN.html, /log/{file}.html, /{category_dir}/. The middleware
// reads weblogs.legacy_* on app construction, so the test seeds those
// values and rebuilds the app via newTestApp + a follow-up update +
// reopen pattern... actually simpler: we set the columns then build
// a second app pointed at the same DB so legacy_url is loaded into
// the Handler.
func TestLegacyStaticRedirects(t *testing.T) {
	a := newTestApp(t)
	// Configure the weblog as if an SB3 import had run: Individual
	// archive under /log/, default eid prefix, .html suffix.
	if _, err := a.DB.Exec(`
		UPDATE weblogs SET
			legacy_archive_type = 'Individual',
			legacy_log_path     = 'log/',
			legacy_base_path    = '/',
			legacy_cgi_name     = 'sb.cgi',
			legacy_id_prefix    = 'eid',
			legacy_suffix       = '.html'
		WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	// Seed an entry with a custom save-name + an entry without one,
	// plus a category whose dir differs from the global log path.
	if _, err := a.DB.Exec(`
		INSERT INTO entries (id, wid, author_id, category_id, title, status, posted_at, created_at, updated_at, legacy_id, legacy_file)
		VALUES
			(201, 1, 1, -1, 'imported one',  1, 1700000000, 1700000000, 1700000000, 42, ''),
			(202, 1, 1, -1, 'imported two',  1, 1700000000, 1700000000, 1700000000, 99, 'special-name')`); err != nil {
		t.Fatal(err)
	}
	if _, err := a.DB.Exec(`
		INSERT INTO categories (id, wid, parent_id, name, sort_order, created_at, updated_at, legacy_id, legacy_dir)
		VALUES
			(101, 1, 0, 'travel', 0, 1700000000, 1700000000, 5, 'travel/'),
			(102, 1, 0, 'plain',  0, 1700000000, 1700000000, 6, 'log/')`); err != nil {
		t.Fatal(err)
	}
	// Refresh the in-memory cache so LegacyStaticMiddleware sees the
	// newly-populated columns. (App.New loads it once at startup; the
	// test seeds the columns afterwards.)
	cfg, err := a.Store.WeblogLegacyURLByID(t.Context(), 1)
	if err != nil {
		t.Fatalf("reload legacy url: %v", err)
	}
	a.Public.LegacyURL = cfg

	cases := []struct {
		path     string
		wantCode int
		wantLoc  string
	}{
		// id-form: /log/eid42.html → entry 201 (legacy_id 42).
		{"/log/eid42.html", 301, "/entry/201/"},
		// name-form: /log/special-name.html → entry 202 (legacy_file).
		{"/log/special-name.html", 301, "/entry/202/"},
		// Category dir that differs from log_path → category 101.
		{"/travel/", 301, "/category/101/"},
		// "plain" category whose dir == log_path: the middleware must
		// NOT claim this — it's the archive root, not a category.
		{"/log/", 0, ""}, // pass-through; whatever the next handler does (likely 404)
		// Bare suffix and unknown name pass through.
		{"/log/.html", 0, ""},
		{"/log/unknown.html", 0, ""},
		// Sub-paths under log/ aren't SB3 entry shape.
		{"/log/sub/eid42.html", 0, ""},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
		if tc.wantCode != 0 && w.Code != tc.wantCode {
			t.Errorf("%s: code = %d want %d", tc.path, w.Code, tc.wantCode)
		}
		if tc.wantLoc != "" {
			if got := w.Result().Header.Get("Location"); got != tc.wantLoc {
				t.Errorf("%s: Location = %q want %q", tc.path, got, tc.wantLoc)
			}
		}
		if tc.wantCode == 0 && w.Code == 301 {
			t.Errorf("%s: expected pass-through, got 301 to %q", tc.path, w.Result().Header.Get("Location"))
		}
	}
}

// TestLegacyCGICommentPostUses307 — POST body + method must survive the
// redirect so the modern commentSubmit handler still owns the form.
func TestLegacyCGICommentPostUses307(t *testing.T) {
	a := newTestApp(t)
	body := strings.NewReader("name=alice&description=hello")
	req := httptest.NewRequest("POST", "/sb.cgi?mode=comment&eid=1", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != 307 {
		t.Fatalf("expected 307 Temporary Redirect, got %d", w.Code)
	}
	if got := w.Result().Header.Get("Location"); got != "/entry/1/comment" {
		t.Errorf("Location = %q", got)
	}
}

// TestRSDXMLServesDiscoveryDoc — /rsd.xml responds with an RSD XML
// body so imported templates' {site_rsd} tag points somewhere real.
func TestRSDXMLServesDiscoveryDoc(t *testing.T) {
	a := newTestApp(t)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/rsd.xml", nil))
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	out, _ := io.ReadAll(w.Result().Body)
	if !strings.Contains(string(out), "<rsd") {
		t.Errorf("body missing <rsd: %s", out)
	}
	if !strings.Contains(string(out), "Serene Bach") {
		t.Errorf("body missing engineName: %s", out)
	}
}
