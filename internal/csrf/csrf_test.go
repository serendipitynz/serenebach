package csrf

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// pass-through handler used below; records that the middleware actually
// forwarded the request after successful verification.
func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok token=" + Token(r.Context())))
	})
}

func TestGETSetsCookieAndExposesTokenInContext(t *testing.T) {
	h := Middleware(okHandler())

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var set *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == CookieName {
			set = c
			break
		}
	}
	if set == nil || set.Value == "" {
		t.Fatal("expected csrf cookie to be set on GET")
	}
	if !strings.Contains(w.Body.String(), "token="+set.Value) {
		t.Errorf("handler context token should match cookie value")
	}
}

func TestPOSTWithoutCookieIsRejected(t *testing.T) {
	h := Middleware(okHandler())

	form := url.Values{FormField: {"anything"}}
	req := httptest.NewRequest("POST", "/admin/whatever", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	// Middleware set a cookie this turn, but PostForm token can't match
	// because no cookie was replayed by the client.
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
}

func TestPOSTWithMismatchingTokenRejected(t *testing.T) {
	h := Middleware(okHandler())

	form := url.Values{FormField: {"not-the-right-value"}}
	req := httptest.NewRequest("POST", "/admin/whatever", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: CookieName, Value: "real-cookie-token"})

	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
}

func TestPOSTWithMatchingTokenPasses(t *testing.T) {
	h := Middleware(okHandler())

	const token = "matching-token-value"
	form := url.Values{FormField: {token}}
	req := httptest.NewRequest("POST", "/admin/whatever", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: CookieName, Value: token})

	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
}

func TestPOSTWithoutFormTokenRejected(t *testing.T) {
	h := Middleware(okHandler())

	// Empty form body (nothing in PostForm)
	req := httptest.NewRequest("POST", "/admin/whatever", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: "some-token"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
}

func TestTokenContextHelperReturnsEmptyOnBareRequest(t *testing.T) {
	ctx := context.Background()
	if Token(ctx) != "" {
		t.Errorf("Token on bare context should be empty")
	}
}
