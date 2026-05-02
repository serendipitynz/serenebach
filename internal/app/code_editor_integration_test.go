package app_test

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTemplateFormMarksTextareasForCodeEditor(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	var activeID int64
	_ = a.DB.QueryRow(`SELECT id FROM templates WHERE is_active = 1`).Scan(&activeID)

	w := authedGET(t, a.Handler(), "/admin/templates/"+itoa64(activeID)+"/edit", cookies)
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{
		`name="main_body"`,
		// Template editors run in the custom sbtemplate mode so {tag} and
		// <!-- BEGIN/END block --> get highlighted on top of plain HTML.
		`data-code-editor="sbtemplate"`,
		`name="css"`,
		`data-code-editor="css"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("template edit form missing %q", want)
		}
	}
}

func TestEntryFormMarksBodyTextareasForCodeEditor(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	w := authedGET(t, a.Handler(), "/admin/entries/1/edit", cookies)
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{
		`name="body"`,
		`name="more"`,
		// Default-format (html) entry should start in HTML mode. If this
		// assertion drifts, double-check entry_form.html's $aceMode block.
		`data-code-editor="html"`,
		// Lives-on-every-Ace-body hook the JS watches to re-mode when the
		// format <select> changes.
		`data-code-editor-dynamic`,
		// Format select carries the picker marker the shared JS wiring
		// uses to resolve the Ace mode on change.
		`data-code-editor-format`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("entry form missing %q", want)
		}
	}
	// Both textareas must carry the dynamic hook — otherwise 追記 keeps
	// the initial mode forever when the user switches format.
	if strings.Count(body, `data-code-editor-dynamic`) < 2 {
		t.Errorf("expected dynamic hook on both body and more textareas")
	}
}

func TestAceStaticAssetsServe(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	for _, path := range []string{
		"/admin/static/ace/ace.js",
		"/admin/static/ace/mode-html.js",
		"/admin/static/ace/mode-css.js",
		"/admin/static/ace/mode-markdown.js",
		"/admin/static/ace/mode-text.js",
		"/admin/static/ace/theme-solarized_light.js",
		"/admin/static/ace/theme-solarized_dark.js",
	} {
		req := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, req)
		if w.Code != 200 {
			t.Errorf("GET %s = %d", path, w.Code)
		}
		if w.Body.Len() < 100 {
			t.Errorf("GET %s body too small: %d bytes", path, w.Body.Len())
		}
	}
}
