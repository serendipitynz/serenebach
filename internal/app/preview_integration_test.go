package app_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// /admin preview mode: `__sb_preview=1` unlocks drafts and closed
// entries for logged-in admins, `__sb_template=<id>` forces a template
// swap for the request. Both collapse silently on anonymous requests
// so a leaked URL can't expose unpublished content.

func TestPreviewDraftRequiresAdminSession(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)

	// Seeded entry 1 starts as published; flip to draft so the public
	// handler's normal 404 filter kicks in.
	if _, err := a.DB.ExecContext(context.Background(),
		`UPDATE entries SET status = 0 WHERE id = 1`); err != nil {
		t.Fatal(err)
	}

	// Anonymous request with the preview flag must still 404 — the
	// flag is ignored without a session cookie.
	req := httptest.NewRequest("GET", "/entry/1/?__sb_preview=1", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("anonymous preview status = %d, want 404", w.Code)
	}

	// Logged-in admin request with the flag sees the draft body.
	cookies := login(t, a.Handler(), "admin", "changeme")
	req = httptest.NewRequest("GET", "/entry/1/?__sb_preview=1", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w = httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("admin preview status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "ようこそ Serene Bach へ") {
		t.Errorf("admin preview missing entry body; body=%s", w.Body.String())
	}
	if got := w.Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Errorf("preview response missing no-store Cache-Control: %q", got)
	}
	if got := w.Header().Get("X-Robots-Tag"); !strings.Contains(got, "noindex") {
		t.Errorf("preview response missing noindex X-Robots-Tag: %q", got)
	}
}

func TestPreviewDraftWithoutFlagStill404(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	if _, err := a.DB.ExecContext(context.Background(),
		`UPDATE entries SET status = 0 WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	// Even an admin without the flag sees the canonical 404 — preview
	// is explicit, not implicit.
	cookies := login(t, a.Handler(), "admin", "changeme")
	req := httptest.NewRequest("GET", "/entry/1/", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("admin unflagged draft status = %d, want 404", w.Code)
	}
}

func TestPreviewTemplateOverrideRequiresAdminSession(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)

	// Seed a second template with a distinctive marker in its MainBody
	// so we can grep the response to tell which template rendered.
	// sbtemplate is strictly line-based — BEGIN/END blocks must sit on
	// their own lines, so the template body is multi-line rather than
	// a single string.
	marker := "PHASE-46-PREVIEW-TEMPLATE-MARKER"
	body := "<!doctype html>\n<html>\n<body>\n" + marker + "\n" +
		"<!-- BEGIN entry -->\n<h2>{entry_title}</h2>\n<!-- END entry -->\n" +
		"</body>\n</html>\n"
	res, err := a.DB.ExecContext(context.Background(), `
		INSERT INTO templates (wid, name, main_body, css, created_at, updated_at)
		VALUES (1, 'preview-candidate', ?, '', strftime('%s','now'), strftime('%s','now'))`, body)
	if err != nil {
		t.Fatalf("insert template: %v", err)
	}
	id, _ := res.LastInsertId()

	// Anonymous: the override must be ignored — marker must NOT appear.
	req := httptest.NewRequest("GET", "/?__sb_template="+itoa(int(id)), nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if strings.Contains(w.Body.String(), marker) {
		t.Fatalf("anonymous template override leaked marker into response")
	}

	// Admin: override takes effect — marker appears.
	cookies := login(t, a.Handler(), "admin", "changeme")
	req = httptest.NewRequest("GET", "/?__sb_template="+itoa(int(id)), nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w = httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("admin template preview status = %d, body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), marker) {
		t.Fatalf("admin template preview missing marker; body=%s", w.Body.String())
	}
	if got := w.Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Errorf("template preview response missing no-store: %q", got)
	}
}
