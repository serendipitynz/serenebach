package app_test

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/serendipitynz/serenebach/internal/app"
	"github.com/serendipitynz/serenebach/internal/auth"
)

func TestAdminPagesRequiresLogin(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	w := authedGET(t, a.Handler(), "/admin/pages", nil)
	if w.Code != http.StatusFound || !strings.Contains(w.Header().Get("Location"), "/admin/login") {
		t.Errorf("expected redirect to login, got status=%d loc=%q", w.Code, w.Header().Get("Location"))
	}
}

func TestAdminPageCreateUpdateDelete(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	pageID := adminPageCreatePublished(t, a, cookies)
	adminPageAssertEditForm(t, a, cookies, pageID)
	adminPageAssertPublicVisible(t, a)
	adminPageUpdateToDraft(t, a, cookies, pageID)
	adminPageAssertSlugRotated(t, a)
	adminPageDelete(t, a, cookies, pageID)
	adminPageAssertRemovedFromList(t, a, cookies)
}

// adminPageCreatePublished POSTs the published "About Us" page and
// returns the redirect-derived page id. Failure to extract the id is
// fatal because every later step keys off it.
func adminPageCreatePublished(t *testing.T, a *app.App, cookies []*http.Cookie) string {
	t.Helper()
	form := url.Values{
		"title":       {"About Us"},
		"body":        {"<p>hello</p>"},
		"slug":        {"/about"},
		"status":      {"1"},
		"format":      {"html"},
		"template_id": {"0"},
	}
	w := authedPOSTForm(t, a.Handler(), "/admin/pages/new", form, cookies)
	if w.Code != http.StatusFound {
		t.Fatalf("create status = %d; body:\n%s", w.Code, w.Body.String())
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "ok=saved") {
		t.Errorf("expected ok=saved in Location, got %q", loc)
	}
	var pageID string
	for _, part := range strings.Split(loc, "/") {
		if part != "" && part != "admin" && part != "pages" && part != "edit" {
			pageID = part
			break
		}
	}
	if pageID == "" {
		t.Fatal("could not extract page id from redirect")
	}
	return pageID
}

func adminPageAssertEditForm(t *testing.T, a *app.App, cookies []*http.Cookie, pageID string) {
	t.Helper()
	edit := authedGET(t, a.Handler(), "/admin/pages/"+pageID+"/edit", cookies)
	if edit.Code != 200 {
		t.Fatalf("edit form status = %d", edit.Code)
	}
	body := edit.Body.String()
	for _, want := range []string{"About Us", "&lt;p&gt;hello&lt;/p&gt;", "/about", `selected`, `name="template_id"`} {
		if !strings.Contains(body, want) {
			t.Errorf("edit form missing %q", want)
		}
	}
}

func adminPageAssertPublicVisible(t *testing.T, a *app.App) {
	t.Helper()
	pub := authedGET(t, a.Handler(), "/about", nil)
	if pub.Code != 200 {
		t.Errorf("public page status = %d, want 200", pub.Code)
	}
	if !strings.Contains(pub.Body.String(), "<p>hello</p>") {
		t.Errorf("public page missing body")
	}
}

func adminPageUpdateToDraft(t *testing.T, a *app.App, cookies []*http.Cookie, pageID string) {
	t.Helper()
	up := url.Values{
		"title":       {"About Us Updated"},
		"body":        {"<p>updated</p>"},
		"slug":        {"/about-updated"},
		"status":      {"0"},
		"format":      {"html"},
		"template_id": {"0"},
	}
	w := authedPOSTForm(t, a.Handler(), "/admin/pages/"+pageID+"/edit", up, cookies)
	if w.Code != http.StatusFound {
		t.Fatalf("update status = %d; body:\n%s", w.Code, w.Body.String())
	}
}

// adminPageAssertSlugRotated confirms both the previous and the new
// slug 404 after the draft update — the old slug because it no longer
// resolves, the new one because the page status flipped to draft.
func adminPageAssertSlugRotated(t *testing.T, a *app.App) {
	t.Helper()
	if old := authedGET(t, a.Handler(), "/about", nil); old.Code != 404 {
		t.Errorf("old slug status = %d, want 404", old.Code)
	}
	if newURL := authedGET(t, a.Handler(), "/about-updated", nil); newURL.Code != 404 {
		t.Errorf("draft page status = %d, want 404", newURL.Code)
	}
}

func adminPageDelete(t *testing.T, a *app.App, cookies []*http.Cookie, pageID string) {
	t.Helper()
	del := authedPOSTForm(t, a.Handler(), "/admin/pages/"+pageID+"/delete", url.Values{}, cookies)
	if del.Code != http.StatusFound {
		t.Fatalf("delete status = %d; body:\n%s", del.Code, del.Body.String())
	}
}

func adminPageAssertRemovedFromList(t *testing.T, a *app.App, cookies []*http.Cookie) {
	t.Helper()
	list := authedGET(t, a.Handler(), "/admin/pages", cookies)
	if strings.Contains(list.Body.String(), "About Us Updated") {
		t.Errorf("deleted page still on admin list")
	}
}

func TestAdminPageValidation(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	cases := []struct {
		name   string
		fields url.Values
	}{
		{
			name: "empty title",
			fields: url.Values{
				"title": {""}, "body": {"b"}, "slug": {"/x"}, "status": {"1"}, "format": {"html"},
			},
		},
		{
			name: "empty slug",
			fields: url.Values{
				"title": {"t"}, "body": {"b"}, "slug": {""}, "status": {"1"}, "format": {"html"},
			},
		},
		{
			name: "invalid slug",
			fields: url.Values{
				"title": {"t"}, "body": {"b"}, "slug": {"/hello world"}, "status": {"1"}, "format": {"html"},
			},
		},
		{
			name: "reserved path admin",
			fields: url.Values{
				"title": {"t"}, "body": {"b"}, "slug": {"/admin"}, "status": {"1"}, "format": {"html"},
			},
		},
		{
			name: "reserved path entry",
			fields: url.Values{
				"title": {"t"}, "body": {"b"}, "slug": {"/entry"}, "status": {"1"}, "format": {"html"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := authedPOSTForm(t, a.Handler(), "/admin/pages/new", tc.fields, cookies)
			if w.Code != 200 {
				t.Fatalf("expected stay on form, got status=%d", w.Code)
			}
			if !strings.Contains(w.Body.String(), `class="alert error"`) {
				t.Errorf("missing error alert; body:\n%s", w.Body.String())
			}
		})
	}
}

func TestAdminPageSlugPrefixConflict(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	// Create /service
	form := url.Values{
		"title": {"Service"}, "body": {"b"}, "slug": {"/service"},
		"status": {"1"}, "format": {"html"}, "template_id": {"0"},
	}
	w := authedPOSTForm(t, a.Handler(), "/admin/pages/new", form, cookies)
	if w.Code != http.StatusFound {
		t.Fatalf("create /service status = %d; body:\n%s", w.Code, w.Body.String())
	}

	// Try to create /service/pricing — should fail with prefix conflict
	form2 := url.Values{
		"title": {"Pricing"}, "body": {"b"}, "slug": {"/service/pricing"},
		"status": {"1"}, "format": {"html"}, "template_id": {"0"},
	}
	w2 := authedPOSTForm(t, a.Handler(), "/admin/pages/new", form2, cookies)
	if w2.Code != 200 {
		t.Fatalf("expected stay on form, got status=%d", w2.Code)
	}
	if !strings.Contains(w2.Body.String(), "親または子") {
		t.Errorf("expected slugPrefixConflict error; body:\n%s", w2.Body.String())
	}

	// Try to create /service from update of another page — should also fail
	// First create a second page /other
	form3 := url.Values{
		"title": {"Other"}, "body": {"b"}, "slug": {"/other"},
		"status": {"1"}, "format": {"html"}, "template_id": {"0"},
	}
	w3 := authedPOSTForm(t, a.Handler(), "/admin/pages/new", form3, cookies)
	if w3.Code != http.StatusFound {
		t.Fatalf("create /other status = %d", w3.Code)
	}
	// Extract other page id
	locParts := strings.Split(w3.Header().Get("Location"), "/")
	otherID := locParts[len(locParts)-2]
	// Update /other -> /service
	up := url.Values{
		"title": {"Other"}, "body": {"b"}, "slug": {"/service"},
		"status": {"1"}, "format": {"html"}, "template_id": {"0"},
	}
	w4 := authedPOSTForm(t, a.Handler(), "/admin/pages/"+otherID+"/edit", up, cookies)
	if w4.Code != 200 {
		t.Fatalf("expected stay on form, got status=%d", w4.Code)
	}
	if !strings.Contains(w4.Body.String(), "既に使われています") && !strings.Contains(w4.Body.String(), "親または子") {
		t.Errorf("expected slug conflict error; body:\n%s", w4.Body.String())
	}
}

func TestAdminPageRegularUserCannotEditOthers(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	adminCookies := login(t, a.Handler(), "admin", "changeme")

	// Create a page as admin
	form := url.Values{
		"title": {"Admin Page"}, "body": {"b"}, "slug": {"/admin-page"},
		"status": {"1"}, "format": {"html"}, "template_id": {"0"},
	}
	w := authedPOSTForm(t, a.Handler(), "/admin/pages/new", form, adminCookies)
	if w.Code != http.StatusFound {
		t.Fatalf("create status = %d", w.Code)
	}
	locParts := strings.Split(w.Header().Get("Location"), "/")
	pageID := locParts[len(locParts)-2]

	// Create a regular user
	hash, _ := auth.HashPassword("secret")
	if _, err := a.DB.Exec(`INSERT INTO users (wid, name, display_name, email, password_hash, role, created_at, updated_at, list_visible, description_format)
		VALUES (1, 'regular', 'Reg', '', ?, 3, strftime('%s','now'), strftime('%s','now'), 1, 'html')`, hash); err != nil {
		t.Fatal(err)
	}
	regularCookies := login(t, a.Handler(), "regular", "secret")

	// Regular user cannot edit
	edit := url.Values{
		"title": {"Hacked"}, "body": {"b"}, "slug": {"/admin-page"},
		"status": {"1"}, "format": {"html"}, "template_id": {"0"},
	}
	w2 := authedPOSTForm(t, a.Handler(), "/admin/pages/"+pageID+"/edit", edit, regularCookies)
	if w2.Code != 403 {
		t.Errorf("edit by regular user status = %d, want 403", w2.Code)
	}

	// Regular user cannot delete
	w3 := authedPOSTForm(t, a.Handler(), "/admin/pages/"+pageID+"/delete", url.Values{}, regularCookies)
	if w3.Code != 403 {
		t.Errorf("delete by regular user status = %d, want 403", w3.Code)
	}
}

// TestAdminPageEditMissingID confirms the edit form returns 404 for
// an unknown page id rather than rendering a blank form. Symmetric
// with the OG endpoint case to lock in the contract across the
// admin-pages surface.
func TestAdminPageEditMissingID(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	w := authedGET(t, a.Handler(), "/admin/pages/9999/edit", cookies)
	if w.Code != http.StatusNotFound {
		t.Errorf("GET edit status = %d, want 404", w.Code)
	}

	form := url.Values{
		"title": {"X"}, "body": {"b"}, "slug": {"/x"},
		"status": {"1"}, "format": {"html"}, "template_id": {"0"},
	}
	w2 := authedPOSTForm(t, a.Handler(), "/admin/pages/9999/edit", form, cookies)
	if w2.Code != http.StatusNotFound {
		t.Errorf("POST edit status = %d, want 404", w2.Code)
	}

	w3 := authedPOSTForm(t, a.Handler(), "/admin/pages/9999/delete", url.Values{}, cookies)
	if w3.Code != http.StatusNotFound {
		t.Errorf("POST delete status = %d, want 404", w3.Code)
	}
}

func TestAdminPageOGEndpoint(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	// Create a page
	form := url.Values{
		"title": {"OG Page"}, "body": {"b"}, "slug": {"/og-page"},
		"status": {"1"}, "format": {"html"}, "template_id": {"0"},
	}
	w := authedPOSTForm(t, a.Handler(), "/admin/pages/new", form, cookies)
	if w.Code != http.StatusFound {
		t.Fatalf("create status = %d", w.Code)
	}
	locParts := strings.Split(w.Header().Get("Location"), "/")
	pageID := locParts[len(locParts)-2]

	// POST OG endpoint
	og := authedPOSTForm(t, a.Handler(), "/admin/pages/"+pageID+"/og", url.Values{}, cookies)
	if og.Code != 200 {
		t.Fatalf("og status = %d; body:\n%s", og.Code, og.Body.String())
	}
	if !strings.Contains(og.Body.String(), `"ok":true`) {
		t.Errorf("expected ok:true in body; got:\n%s", og.Body.String())
	}

	// File exists on disk
	ogPath := filepath.Join(a.Config.ImageDir, "og", "page_"+pageID+".png")
	if _, err := os.Stat(ogPath); err != nil {
		t.Errorf("OG card missing at %s: %v", ogPath, err)
	}

	// Non-existent page ID returns 404
	bad := authedPOSTForm(t, a.Handler(), "/admin/pages/99999/og", url.Values{}, cookies)
	if bad.Code != 404 {
		t.Errorf("bad id status = %d, want 404", bad.Code)
	}

	// Regular user cannot generate OG
	hash, _ := auth.HashPassword("secret")
	if _, err := a.DB.Exec(`INSERT INTO users (wid, name, display_name, email, password_hash, role, created_at, updated_at, list_visible, description_format)
		VALUES (1, 'regular', 'Reg', '', ?, 3, strftime('%s','now'), strftime('%s','now'), 1, 'html')`, hash); err != nil {
		t.Fatal(err)
	}
	regularCookies := login(t, a.Handler(), "regular", "secret")
	noPerm := authedPOSTForm(t, a.Handler(), "/admin/pages/"+pageID+"/og", url.Values{}, regularCookies)
	if noPerm.Code != 403 {
		t.Errorf("regular user og status = %d, want 403", noPerm.Code)
	}
}
