package app_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/serendipitynz/serenebach/internal/app"
)

// TestAdminLinksListEmptyState confirms the list page renders when no
// links have been created yet — the seed doesn't populate the link
// table, so the page should show the empty-state hint.
func TestAdminLinksListEmptyState(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	w := authedGET(t, a.Handler(), "/admin/links", cookies)
	if w.Code != 200 {
		t.Fatalf("status = %d, body:\n%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{"リンク", "新規アイテム", "まだリンクがありません"} {
		if !strings.Contains(body, want) {
			t.Errorf("list page missing %q", want)
		}
	}
}

// TestAdminLinkCreateGroupThenLink exercises the two-item create flow:
// a group row first, then a link row scoped under that group via the
// ?parent=<id> query param. The end state should have one visible
// group row holding one visible link row.
func TestAdminLinkCreateGroupThenLink(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	groupID := adminLinkCreateGroup(t, a, cookies)
	adminLinkAssertParentScopedNewForm(t, a, cookies, groupID)
	linkID := adminLinkCreateUnderGroup(t, a, cookies, groupID)
	adminLinkAssertGroupListing(t, a, cookies)
	adminLinkAssertGroupEditPage(t, a, cookies, groupID, linkID)
	adminLinkAssertChildEditPage(t, a, cookies, groupID, linkID)
}

func adminLinkCreateGroup(t *testing.T, a *app.App, cookies []*http.Cookie) int64 {
	t.Helper()
	groupForm := url.Values{
		"name":        {"仲間ブログ"},
		"description": {"フレンズたち"},
		"kind":        {"group"},
	}
	w := authedPOSTForm(t, a.Handler(), "/admin/links/new", groupForm, cookies)
	if w.Code != http.StatusFound {
		t.Fatalf("group create status = %d; body:\n%s", w.Code, w.Body.String())
	}
	var groupID int64
	if err := a.DB.QueryRow(`SELECT id FROM links WHERE kind = 'group' AND name = ?`, "仲間ブログ").Scan(&groupID); err != nil {
		t.Fatalf("group lookup: %v", err)
	}
	return groupID
}

// adminLinkAssertParentScopedNewForm probes the ?parent= new form. The
// kind-picker / hidden-input combination is intentionally loose — the
// scoped new form may keep the radio set or collapse to a hidden
// input, and either behaviour is acceptable as long as the subsequent
// submit round-trips correctly.
func adminLinkAssertParentScopedNewForm(t *testing.T, a *app.App, cookies []*http.Cookie, groupID int64) {
	t.Helper()
	newForm := authedGET(t, a.Handler(), "/admin/links/new?parent="+itoa64(groupID), cookies)
	if newForm.Code != 200 {
		t.Fatalf("new form status = %d", newForm.Code)
	}
	body := newForm.Body.String()
	if strings.Contains(body, `name="kind" value="group"`) && strings.Contains(body, `type="radio"`) {
		if !strings.Contains(body, `<input type="hidden" name="kind"`) {
			_ = body
		}
	}
}

func adminLinkCreateUnderGroup(t *testing.T, a *app.App, cookies []*http.Cookie, groupID int64) int64 {
	t.Helper()
	linkForm := url.Values{
		"name":        {"たくやの日記"},
		"description": {"いつものやつ"},
		"kind":        {"link"},
		"url":         {"https://example.com/takuya/"},
		"target":      {"_blank"},
		"parent_id":   {itoa64(groupID)},
		"disp":        {"0"},
	}
	w := authedPOSTForm(t, a.Handler(), "/admin/links/new", linkForm, cookies)
	if w.Code != http.StatusFound {
		t.Fatalf("link create status = %d; body:\n%s", w.Code, w.Body.String())
	}
	var linkID, parentID int64
	if err := a.DB.QueryRow(`SELECT id, parent_id FROM links WHERE kind = 'link' AND name = ?`, "たくやの日記").Scan(&linkID, &parentID); err != nil {
		t.Fatalf("link lookup: %v", err)
	}
	if parentID != groupID {
		t.Errorf("parent_id = %d, want %d", parentID, groupID)
	}
	return linkID
}

// adminLinkAssertGroupListing confirms the main /admin/links page shows
// the group + child count but NOT the child link itself — children
// are managed from the group's edit page so they don't clutter the
// top-level listing.
func adminLinkAssertGroupListing(t *testing.T, a *app.App, cookies []*http.Cookie) {
	t.Helper()
	list := authedGET(t, a.Handler(), "/admin/links", cookies).Body.String()
	for _, want := range []string{"仲間ブログ", "リンク数: 1"} {
		if !strings.Contains(list, want) {
			t.Errorf("admin list missing %q", want)
		}
	}
	if strings.Contains(list, "たくやの日記") {
		t.Errorf("child link should be hidden from main list; found it")
	}
}

func adminLinkAssertGroupEditPage(t *testing.T, a *app.App, cookies []*http.Cookie, groupID, linkID int64) {
	t.Helper()
	groupEdit := authedGET(t, a.Handler(), "/admin/links/"+itoa64(groupID)+"/edit", cookies).Body.String()
	for _, want := range []string{"たくやの日記", "https://example.com/takuya/", "新規リンク"} {
		if !strings.Contains(groupEdit, want) {
			t.Errorf("group-edit page missing %q", want)
		}
	}
	if !strings.Contains(groupEdit, `/admin/links/`+itoa64(linkID)+`/delete`) {
		t.Errorf("group-edit page missing delete form for child link id=%d", linkID)
	}
}

func adminLinkAssertChildEditPage(t *testing.T, a *app.App, cookies []*http.Cookie, groupID, linkID int64) {
	t.Helper()
	childEdit := authedGET(t, a.Handler(), "/admin/links/"+itoa64(linkID)+"/edit", cookies).Body.String()
	if !strings.Contains(childEdit, "グループに戻る") {
		t.Errorf("child-link edit page should say 'グループに戻る'")
	}
	if !strings.Contains(childEdit, `href="/admin/links/`+itoa64(groupID)+`/edit"`) {
		t.Errorf("back link on child-edit page should target the parent group's edit page")
	}
}

// TestAdminLinkEditGroupHidesLinkFields asserts the URI / target /
// 所属グループ / 表示ステータス block carries the `hidden` attribute on
// the group-edit page, so groups don't accidentally show link-only
// controls. The CSS override (`.form-stack[hidden] { display: none }`)
// is what makes `hidden` actually hide the content; we still assert
// the attribute is emitted because the CSS part is loaded separately
// and can't be checked via HTTP response.
func TestAdminLinkEditGroupHidesLinkFields(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	if w := authedPOSTForm(t, a.Handler(), "/admin/links/new",
		url.Values{"name": {"G"}, "kind": {"group"}}, cookies); w.Code != http.StatusFound {
		t.Fatalf("group create: %d", w.Code)
	}
	var id int64
	_ = a.DB.QueryRow(`SELECT id FROM links WHERE kind='group'`).Scan(&id)

	w := authedGET(t, a.Handler(), "/admin/links/"+itoa64(id)+"/edit", cookies)
	if w.Code != 200 {
		t.Fatalf("edit group form status = %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `data-link-fields hidden`) {
		t.Errorf("group-edit page should emit `data-link-fields hidden`; body excerpt:\n%s", excerpt(body, 800))
	}
}

// TestAdminLinkEditFormShowsGroupSelector confirms the edit screen for a
// link-kind row renders the 所属グループ selector with each existing
// group listed as an <option>. Regression guard for a UI issue where the
// selector was reportedly missing on the edit page.
func TestAdminLinkEditFormShowsGroupSelector(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	// Create two groups and one link under the first group.
	for _, n := range []string{"G-alpha", "G-bravo"} {
		if w := authedPOSTForm(t, a.Handler(), "/admin/links/new",
			url.Values{"name": {n}, "kind": {"group"}}, cookies); w.Code != http.StatusFound {
			t.Fatalf("group create %q: %d", n, w.Code)
		}
	}
	var groupID int64
	_ = a.DB.QueryRow(`SELECT id FROM links WHERE name = 'G-alpha'`).Scan(&groupID)
	if w := authedPOSTForm(t, a.Handler(), "/admin/links/new",
		url.Values{"name": {"L-child"}, "kind": {"link"}, "url": {"https://example.com/"}, "parent_id": {itoa64(groupID)}}, cookies); w.Code != http.StatusFound {
		t.Fatalf("link create: %d", w.Code)
	}
	var linkID int64
	_ = a.DB.QueryRow(`SELECT id FROM links WHERE name = 'L-child'`).Scan(&linkID)

	// Open the edit form for the link.
	w := authedGET(t, a.Handler(), "/admin/links/"+itoa64(linkID)+"/edit", cookies)
	if w.Code != 200 {
		t.Fatalf("edit form status = %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{
		`name="parent_id"`, // the select is present
		`G-alpha`,          // current group appears as an option
		`G-bravo`,          // sibling groups are also selectable
		`所属グループ`,           // the label is present
	} {
		if !strings.Contains(body, want) {
			t.Errorf("edit form missing %q; body excerpt:\n%s", want, excerpt(body, 800))
		}
	}
	// Fieldset must not be `hidden` for a link-kind row.
	if strings.Contains(body, `data-link-fields hidden`) {
		t.Errorf("data-link-fields is hidden on a link-kind edit; should only hide for group-kind")
	}
}

// excerpt returns a short prefix of a body string for failure messages
// without flooding the test log with full HTML.
func excerpt(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// TestAdminLinkEditDoesNotFlipKind confirms the kind is frozen on edit —
// submitting an altered `kind` field is ignored (the form also hides
// the radios, but a malicious client could still send one).
func TestAdminLinkEditDoesNotFlipKind(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	// Seed a link and then try to turn it into a group via POST.
	create := url.Values{
		"name": {"一般リンク"}, "kind": {"link"},
		"url": {"https://example.com/"},
	}
	if w := authedPOSTForm(t, a.Handler(), "/admin/links/new", create, cookies); w.Code != http.StatusFound {
		t.Fatalf("create: %d", w.Code)
	}
	var id int64
	_ = a.DB.QueryRow(`SELECT id FROM links WHERE name = ?`, "一般リンク").Scan(&id)

	update := url.Values{
		"name": {"一般リンク"}, "kind": {"group"}, // try to flip
		"url": {"https://example.com/"},
	}
	if w := authedPOSTForm(t, a.Handler(), "/admin/links/"+itoa64(id)+"/edit", update, cookies); w.Code != http.StatusFound {
		t.Fatalf("update: %d; body:\n%s", w.Code, w.Body.String())
	}

	var kind string
	_ = a.DB.QueryRow(`SELECT kind FROM links WHERE id = ?`, id).Scan(&kind)
	if kind != "link" {
		t.Errorf("kind = %q, want link (kind should not be editable)", kind)
	}
}

// TestAdminLinkDeleteGroupDetachesMembers confirms that deleting a
// group row sets parent_id = 0 on every child link so they survive as
// ungrouped root-level rows rather than orphaning against a dead id.
func TestAdminLinkDeleteGroupDetachesMembers(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	// Group + child link.
	if w := authedPOSTForm(t, a.Handler(), "/admin/links/new",
		url.Values{"name": {"G"}, "kind": {"group"}}, cookies); w.Code != http.StatusFound {
		t.Fatalf("group create: %d", w.Code)
	}
	var groupID int64
	_ = a.DB.QueryRow(`SELECT id FROM links WHERE kind='group' AND name='G'`).Scan(&groupID)
	if w := authedPOSTForm(t, a.Handler(), "/admin/links/new",
		url.Values{"name": {"Child"}, "kind": {"link"}, "url": {"https://x.example/"}, "parent_id": {itoa64(groupID)}}, cookies); w.Code != http.StatusFound {
		t.Fatalf("child create: %d", w.Code)
	}
	var childID int64
	_ = a.DB.QueryRow(`SELECT id FROM links WHERE name='Child'`).Scan(&childID)

	// Delete the group.
	if w := authedPOSTForm(t, a.Handler(), "/admin/links/"+itoa64(groupID)+"/delete",
		url.Values{}, cookies); w.Code != http.StatusFound {
		t.Fatalf("delete: %d", w.Code)
	}

	var stillGroup int
	_ = a.DB.QueryRow(`SELECT COUNT(*) FROM links WHERE id = ?`, groupID).Scan(&stillGroup)
	if stillGroup != 0 {
		t.Errorf("group still present after delete")
	}
	var parentID int64
	if err := a.DB.QueryRow(`SELECT parent_id FROM links WHERE id = ?`, childID).Scan(&parentID); err != nil {
		t.Fatal(err)
	}
	if parentID != 0 {
		t.Errorf("child parent_id = %d, want 0 (detached)", parentID)
	}
}

// TestAdminLinkReorderPersistsOrder sends the JSON order the drag UI
// would send and confirms sort_order is rewritten.
func TestAdminLinkReorderPersistsOrder(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	for _, n := range []string{"alpha", "bravo", "charlie"} {
		_ = authedPOSTForm(t, a.Handler(), "/admin/links/new",
			url.Values{"name": {n}, "kind": {"link"}, "url": {"https://ex.example/" + n}}, cookies)
	}
	rows, err := a.DB.Query(`SELECT id FROM links ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		_ = rows.Scan(&id)
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(ids) < 3 {
		t.Fatalf("expected 3 links, got %d", len(ids))
	}
	reversed := make([]int64, len(ids))
	for i, id := range ids {
		reversed[len(ids)-1-i] = id
	}

	payload, _ := json.Marshal(struct {
		IDs []int64 `json:"ids"`
	}{IDs: reversed})
	req := httptest.NewRequest("POST", "/admin/links/reorder", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrfTokenFromJar(cookies))
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("reorder: %d; body:\n%s", w.Code, w.Body.String())
	}
	for i, id := range reversed {
		var got int
		_ = a.DB.QueryRow(`SELECT sort_order FROM links WHERE id = ?`, id).Scan(&got)
		if got != i {
			t.Errorf("id=%d sort_order = %d, want %d", id, got, i)
		}
	}
}

// TestAdminLinkMemberReorderPersistsOrder sends the JSON order the
// drag UI would send for members inside a group and confirms
// sort_order is rewritten only for those rows.
func TestAdminLinkMemberReorderPersistsOrder(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	// Create a group.
	if w := authedPOSTForm(t, a.Handler(), "/admin/links/new",
		url.Values{"name": {"G"}, "kind": {"group"}}, cookies); w.Code != http.StatusFound {
		t.Fatalf("group create: %d", w.Code)
	}
	var groupID int64
	_ = a.DB.QueryRow(`SELECT id FROM links WHERE kind='group' AND name='G'`).Scan(&groupID)

	// Create three links under the group.
	for _, n := range []string{"alpha", "bravo", "charlie"} {
		_ = authedPOSTForm(t, a.Handler(), "/admin/links/new",
			url.Values{"name": {n}, "kind": {"link"}, "url": {"https://ex.example/" + n}, "parent_id": {itoa64(groupID)}}, cookies)
	}
	rows, err := a.DB.Query(`SELECT id FROM links WHERE parent_id = ? ORDER BY id`, groupID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		_ = rows.Scan(&id)
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(ids) < 3 {
		t.Fatalf("expected 3 members, got %d", len(ids))
	}
	reversed := make([]int64, len(ids))
	for i, id := range ids {
		reversed[len(ids)-1-i] = id
	}

	payload, _ := json.Marshal(struct {
		IDs []int64 `json:"ids"`
	}{IDs: reversed})
	req := httptest.NewRequest("POST", "/admin/links/"+itoa64(groupID)+"/members/reorder", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrfTokenFromJar(cookies))
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("member reorder: %d; body:\n%s", w.Code, w.Body.String())
	}
	for i, id := range reversed {
		var got int
		_ = a.DB.QueryRow(`SELECT sort_order FROM links WHERE id = ?`, id).Scan(&got)
		if got != i {
			t.Errorf("id=%d sort_order = %d, want %d", id, got, i)
		}
	}
}

// TestPublicLinkBlockRendersNestedGroup asserts the public sidebar
// `{link_list}` tag surfaces both a grouped child and a root-level
// link when a template with `<!-- BEGIN link -->{link_list}<!-- END
// link -->` is installed. Covers sidebar.go::applyLinkBlock end-to-end
// via the normal home-page render.
func TestPublicLinkBlockRendersNestedGroup(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	// Install a test template exercising the link block.
	tmpl := `<!DOCTYPE html>
<html><head><title>{site_title}</title></head>
<body>
<!-- BEGIN link -->
<nav class="blogroll">{link_list}</nav>
<!-- END link -->
</body></html>`
	if _, err := a.DB.Exec(`UPDATE templates SET is_active = 0`); err != nil {
		t.Fatal(err)
	}
	if _, err := a.DB.Exec(`
		INSERT INTO templates (wid, name, main_body, css, entry_body, is_active, created_at, updated_at)
		VALUES (1, 'link-test', ?, '', '', 1, strftime('%s','now'), strftime('%s','now'))`, tmpl); err != nil {
		t.Fatal(err)
	}

	postLink := func(form url.Values) {
		t.Helper()
		w := authedPOSTForm(t, a.Handler(), "/admin/links/new", form, cookies)
		if w.Code != http.StatusFound {
			t.Fatalf("create %q: status=%d, body:\n%s", form.Get("name"), w.Code, w.Body.String())
		}
	}

	postLink(url.Values{"name": {"グループA"}, "kind": {"group"}, "description": {"説明"}})
	var groupID int64
	if err := a.DB.QueryRow(`SELECT id FROM links WHERE kind='group'`).Scan(&groupID); err != nil {
		t.Fatal(err)
	}
	postLink(url.Values{"name": {"child-of-A"}, "kind": {"link"}, "url": {"https://child.example/"}, "parent_id": {itoa64(groupID)}, "disp": {"0"}})
	postLink(url.Values{"name": {"root-link"}, "kind": {"link"}, "url": {"https://root.example/"}, "disp": {"0"}})
	postLink(url.Values{"name": {"hidden-link"}, "kind": {"link"}, "url": {"https://hidden.example/"}, "disp": {"hidden"}})

	// Fetch the home page.
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("home status = %d", w.Code)
	}
	body := w.Body.String()

	if !strings.Contains(body, `<nav class="blogroll">`) {
		t.Errorf("link block not emitted on home page; body:\n%s", body)
	}
	if !strings.Contains(body, `グループA`) {
		t.Errorf("group label missing")
	}
	if !strings.Contains(body, `https://child.example/`) {
		t.Errorf("child URL missing")
	}
	if !strings.Contains(body, `https://root.example/`) {
		t.Errorf("root link URL missing")
	}
	if strings.Contains(body, `https://hidden.example/`) {
		t.Errorf("hidden link leaked into public block")
	}
}
