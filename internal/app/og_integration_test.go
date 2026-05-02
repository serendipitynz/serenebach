package app_test

import (
	"bytes"
	"image/png"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEntrySaveGeneratesOGCard walks the golden path: creating or
// updating an entry writes an /img/og/<id>.png file to disk, and the
// rendered permalink carries a matching <meta property="og:image">.
func TestEntrySaveGeneratesOGCard(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	form := url.Values{
		"title":       {"og test entry"},
		"body":        {"body"},
		"more":        {""},
		"category_id": {"-1"},
		"status":      {"1"},
		"format":      {"html"},
		"posted_at":   {"2026-04-21T12:00"},
	}
	w := authedPOSTForm(t, a.Handler(), "/admin/entries/new", form, cookies)
	if w.Code != http.StatusFound {
		t.Fatalf("create status = %d; body:\n%s", w.Code, w.Body.String())
	}
	loc := w.Header().Get("Location")
	parts := strings.Split(loc, "/")
	if len(parts) < 4 {
		t.Fatalf("unexpected Location %q", loc)
	}
	id := parts[3]

	// File on disk under <ImageDir>/og/<id>.png
	absPath := filepath.Join(a.Config.ImageDir, "og", id+".png")
	data, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatalf("OG card not written to disk: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(data)); err != nil {
		t.Errorf("OG card is not a valid PNG: %v", err)
	}

	// Public /img/og/<id>.png serves it.
	resp := authedGET(t, a.Handler(), "/img/og/"+id+".png", cookies)
	if resp.Code != 200 {
		t.Errorf("/img/og/%s.png status = %d", id, resp.Code)
	}

	// Permalink HTML carries og:image meta.
	pub := authedGET(t, a.Handler(), "/entry/"+id+"/", cookies).Body.String()
	if !strings.Contains(pub, `property="og:image"`) {
		t.Errorf("permalink missing og:image meta")
	}
	wantSub := `/img/og/` + id + `.png`
	if !strings.Contains(pub, wantSub) {
		t.Errorf("permalink meta missing %q; body:\n%s", wantSub, pub)
	}
}

// TestEntryDeleteRemovesOGCard — delete → file on disk is gone.
func TestEntryDeleteRemovesOGCard(t *testing.T) {
	t.Parallel()
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")

	// Create an entry so there's a card to remove.
	form := url.Values{
		"title": {"delete me"}, "body": {"x"}, "more": {""},
		"category_id": {"-1"}, "status": {"1"}, "format": {"html"},
		"posted_at": {"2026-04-21T12:00"},
	}
	w := authedPOSTForm(t, a.Handler(), "/admin/entries/new", form, cookies)
	if w.Code != http.StatusFound {
		t.Fatalf("create status = %d", w.Code)
	}
	id := strings.Split(w.Header().Get("Location"), "/")[3]

	absPath := filepath.Join(a.Config.ImageDir, "og", id+".png")
	if _, err := os.Stat(absPath); err != nil {
		t.Fatalf("OG card missing after create: %v", err)
	}

	del := authedPOSTForm(t, a.Handler(), "/admin/entries/"+id+"/delete",
		url.Values{}, cookies)
	if del.Code != http.StatusFound {
		t.Fatalf("delete status = %d", del.Code)
	}

	if _, err := os.Stat(absPath); !os.IsNotExist(err) {
		t.Errorf("OG card still present after entry delete")
	}
}
