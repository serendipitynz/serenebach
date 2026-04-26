package app_test

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestAdminEntryKeywordsRoundtrip confirms the keywords field saves and
// re-renders correctly, with whitespace normalised to ", " separators.
func TestAdminEntryKeywordsRoundtrip(t *testing.T) {
	a := newTestApp(t)
	cookies := login(t, a.Handler(), "admin", "changeme")
	csrfCookie, token := fetchCSRF(t, a.Handler())

	form := url.Values{
		"csrf_token":  {token},
		"title":       {"existing"},
		"body":        {"x"},
		"format":      {"html"},
		"status":      {"1"},
		"posted_at":   {"2026-04-21T10:00"},
		"category_id": {"-1"},
		// Mixed spacing in on purpose — the server should normalise.
		"keywords": {"go,  sqlite  ,blog"},
	}
	req := httptest.NewRequest("POST", "/admin/entries/1/edit", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrfCookie)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != 302 && w.Code != 303 {
		t.Fatalf("status = %d, want redirect", w.Code)
	}

	// Re-open the edit form and check the normalised value renders in
	// the input. The trimmed-and-rejoined form ("go, sqlite, blog") is
	// the contract — not whatever raw input was typed.
	w = authedGET(t, a.Handler(), "/admin/entries/1/edit", cookies)
	if !strings.Contains(w.Body.String(), `value="go, sqlite, blog"`) {
		t.Errorf("normalised keywords not rendered back into form\n%s", w.Body.String())
	}
}
