package app_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestCommentErrorHonoursBlogLanguage drives the reader-facing i18n
// wiring by submitting an invalid comment against a blog whose Lang
// flips between "ja" and "en", and checking the redirect's ?err=
// carries the locale-appropriate copy.
func TestCommentErrorHonoursBlogLanguage(t *testing.T) {
	a := newTestApp(t)

	// Default seed = ja. Missing-body submission → JA copy.
	body := submitInvalid(t, a.Handler())
	if !strings.Contains(body, "お名前とコメント本文は必須です。") {
		t.Fatalf("expected JA error in redirect, got %q", body)
	}

	// Flip to English and re-submit.
	if _, err := a.DB.ExecContext(context.Background(),
		`UPDATE weblogs SET lang = 'en' WHERE id = 1`); err != nil {
		t.Fatal(err)
	}

	body = submitInvalid(t, a.Handler())
	if !strings.Contains(body, "Name and comment body are required.") {
		t.Fatalf("expected EN error in redirect, got %q", body)
	}
	if strings.Contains(body, "お名前") {
		t.Fatalf("JA string leaked into en response: %q", body)
	}
}

// submitInvalid posts a comment missing the required fields and
// returns the URL-decoded Location header so the caller can assert on
// the ?err= value directly.
func submitInvalid(t *testing.T, h http.Handler) string {
	t.Helper()
	csrfCookie, token := fetchCSRF(t, h)
	form := url.Values{
		"_ts":        {formatUnix(time.Now().Add(-10 * time.Second))},
		"csrf_token": {token},
	}
	req := httptest.NewRequest("POST", "/entry/1/comment", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", testPublicOrigin)
	req.AddCookie(csrfCookie)
	req.RemoteAddr = "127.0.0.1:1234"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("submit status = %d, want 303", w.Code)
	}
	loc := w.Header().Get("Location")
	decoded, err := url.QueryUnescape(loc)
	if err != nil {
		t.Fatalf("decode Location %q: %v", loc, err)
	}
	return decoded
}
