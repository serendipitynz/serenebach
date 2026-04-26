package public

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// guardWith builds a guard plus a tiny "200 OK" backend so a request
// that survives the middleware lands here visibly.
func guardWith(origins ...string) http.Handler {
	g := NewSameOriginGuard(origins)
	return g.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
}

func TestSafeMethodsPassWithoutHeaders(t *testing.T) {
	// GET / HEAD / OPTIONS shouldn't get rejected — a fail-closed
	// policy on safe methods would 403 every reader page load.
	for _, m := range []string{"GET", "HEAD", "OPTIONS"} {
		req := httptest.NewRequest(m, "/anything", nil)
		w := httptest.NewRecorder()
		guardWith("http://example.com").ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", m, w.Code)
		}
	}
}

func TestPostRejectedWhenAllowListEmpty(t *testing.T) {
	// Fail-closed when the operator never configured BaseURL or
	// SB_PUBLIC_ALLOWED_ORIGINS — silently accepting would defeat the
	// whole point of moving these endpoints out of CSRF.
	req := httptest.NewRequest("POST", "/", strings.NewReader(""))
	req.Header.Set("Origin", "http://example.com")
	w := httptest.NewRecorder()
	guardWith().ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 (no allowed origins configured)", w.Code)
	}
}

func TestOriginExactMatchPermitsPost(t *testing.T) {
	req := httptest.NewRequest("POST", "/", strings.NewReader(""))
	req.Header.Set("Origin", "http://example.com")
	w := httptest.NewRecorder()
	guardWith("http://example.com").ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; same-origin POST must pass", w.Code)
	}
}

func TestOriginMismatchRejected(t *testing.T) {
	req := httptest.NewRequest("POST", "/", strings.NewReader(""))
	req.Header.Set("Origin", "https://attacker.example")
	w := httptest.NewRecorder()
	guardWith("http://example.com").ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
}

func TestOriginNullRejected(t *testing.T) {
	// Sandboxed iframes / data URIs send Origin: null. There's no
	// legitimate reason to accept these for an authenticated-by-
	// session-cookie endpoint.
	req := httptest.NewRequest("POST", "/", strings.NewReader(""))
	req.Header.Set("Origin", "null")
	w := httptest.NewRecorder()
	guardWith("http://example.com").ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 (Origin: null must be rejected)", w.Code)
	}
}

func TestRefererFallbackOnlyWhenOriginAbsent(t *testing.T) {
	// Origin missing → Referer takes over.
	{
		req := httptest.NewRequest("POST", "/", strings.NewReader(""))
		req.Header.Set("Referer", "http://example.com/some/page")
		w := httptest.NewRecorder()
		guardWith("http://example.com").ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("Origin absent + matching Referer: status = %d, want 200", w.Code)
		}
	}
	// Origin present + mismatch → Referer must NOT rescue the
	// request; otherwise an attacker just sets a benign Referer and
	// keeps an attack-Origin.
	{
		req := httptest.NewRequest("POST", "/", strings.NewReader(""))
		req.Header.Set("Origin", "https://attacker.example")
		req.Header.Set("Referer", "http://example.com/")
		w := httptest.NewRecorder()
		guardWith("http://example.com").ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Errorf("attacker Origin + benign Referer: status = %d, want 403", w.Code)
		}
	}
}

func TestPostWithNeitherHeaderRejected(t *testing.T) {
	// Browsers always send one; CLI tools do not. Fail-closed.
	req := httptest.NewRequest("POST", "/", strings.NewReader(""))
	w := httptest.NewRecorder()
	guardWith("http://example.com").ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 when neither Origin nor Referer set", w.Code)
	}
}

func TestMultiOriginAllowList(t *testing.T) {
	// Split-origin deployment: static HTML on a CDN host POSTing back
	// to the dynamic backend host.
	cases := []struct {
		origin string
		want   int
	}{
		{"http://blog.example.com", http.StatusOK},
		{"https://static.example.net", http.StatusOK},
		{"https://attacker.example", http.StatusForbidden},
	}
	for _, tc := range cases {
		req := httptest.NewRequest("POST", "/", strings.NewReader(""))
		req.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		guardWith("http://blog.example.com", "https://static.example.net").ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Errorf("origin %q: status = %d, want %d", tc.origin, w.Code, tc.want)
		}
	}
}

func TestPortIsPartOfOriginIdentity(t *testing.T) {
	// http://example.com and http://example.com:8080 are different
	// origins per the standard — the guard must respect that.
	req := httptest.NewRequest("POST", "/", strings.NewReader(""))
	req.Header.Set("Origin", "http://example.com:8080")
	w := httptest.NewRecorder()
	guardWith("http://example.com").ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 (port mismatch is a different origin)", w.Code)
	}
}

func TestCanonicalOriginLowercasesSchemeAndHost(t *testing.T) {
	// Browsers normalise scheme + host but server-side comparisons
	// occasionally hit mixed-case Origin (e.g. tests, custom proxies).
	req := httptest.NewRequest("POST", "/", strings.NewReader(""))
	req.Header.Set("Origin", "HTTP://Example.COM")
	w := httptest.NewRecorder()
	guardWith("http://example.com").ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (case-insensitive match expected)", w.Code)
	}
}

func TestNewSameOriginGuardDropsGarbage(t *testing.T) {
	// Typos in SB_PUBLIC_ALLOWED_ORIGINS shouldn't crash startup or
	// silently widen the allow-list.
	g := NewSameOriginGuard([]string{"", "not-a-url", "://broken", "http://good.example"})
	if len(g.allowed) != 1 {
		t.Errorf("expected only 1 valid origin, got %d (%v)", len(g.allowed), g.allowed)
	}
}
