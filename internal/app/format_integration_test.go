package app_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestMarkdownEntryRendersAsHTML walks the full round-trip: admin creates
// an entry with format=markdown, the raw Markdown is stored, and the
// public permalink plus the home page show the rendered HTML (not the
// source). Backstop against regressions in the content → format wiring.
func TestMarkdownEntryRendersAsHTML(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	form := url.Values{
		"title":       {"md entry"},
		"body":        {"# hello\n\nsome **bold** copy with https://example.com link"},
		"more":        {""},
		"category_id": {"-1"},
		"status":      {"1"},
		"format":      {"markdown"},
		"posted_at":   {"2026-04-19T12:00"},
	}
	w := authedPOSTForm(t, a.Handler(), "/admin/entries/new", form, cookies)
	if w.Code != http.StatusFound {
		t.Fatalf("create status = %d; body:\n%s", w.Code, w.Body.String())
	}

	// Edit form should round-trip the raw markdown source back into the
	// textarea — the author must see their bytes, not the rendered HTML.
	editLoc := w.Header().Get("Location")
	edit := authedGET(t, a.Handler(), editLoc, cookies)
	if !strings.Contains(edit.Body.String(), "# hello") {
		t.Errorf("edit form lost the raw markdown source; body:\n%s", edit.Body.String())
	}
	if !strings.Contains(edit.Body.String(), `value="markdown" selected`) {
		t.Errorf("format select should remember markdown choice; body:\n%s", edit.Body.String())
	}

	// Public permalink: markdown rendered, source gone.
	pub := authedGET(t, a.Handler(), editPathToPublic(editLoc), cookies)
	if pub.Code != 200 {
		t.Fatalf("permalink status = %d", pub.Code)
	}
	pubBody := pub.Body.String()
	for _, want := range []string{
		"<h1>hello</h1>",
		"<strong>bold</strong>",
		`<a href="https://example.com">`,
	} {
		if !strings.Contains(pubBody, want) {
			t.Errorf("permalink missing %q in rendered output", want)
		}
	}
	if strings.Contains(pubBody, "# hello") {
		t.Errorf("raw markdown leaked into rendered HTML:\n%s", pubBody)
	}
}

// TestHTMLEntryStaysPassThrough confirms the historical default (empty /
// "html" format) still works — no goldmark in the path, the stored bytes
// land verbatim in the rendered page.
func TestHTMLEntryStaysPassThrough(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	form := url.Values{
		"title":       {"html entry"},
		"body":        {`<p class="lead">verbatim <em>HTML</em></p>`},
		"more":        {""},
		"category_id": {"-1"},
		"status":      {"1"},
		"format":      {"html"},
		"posted_at":   {"2026-04-19T12:00"},
	}
	w := authedPOSTForm(t, a.Handler(), "/admin/entries/new", form, cookies)
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d", w.Code)
	}
	pubLoc := editPathToPublic(w.Header().Get("Location"))
	pub := authedGET(t, a.Handler(), pubLoc, cookies)
	if !strings.Contains(pub.Body.String(), `<p class="lead">verbatim <em>HTML</em></p>`) {
		t.Errorf("html body should land verbatim; got:\n%s", pub.Body.String())
	}
}

// editPathToPublic turns "/admin/entries/123/edit" into "/entry/123/" so the
// permalink tests can reuse the Location the create handler hands back.
func editPathToPublic(editPath string) string {
	// editPath looks like "/admin/entries/<id>/edit"
	parts := strings.Split(editPath, "/")
	if len(parts) < 4 {
		return "/"
	}
	return "/entry/" + parts[3] + "/"
}
