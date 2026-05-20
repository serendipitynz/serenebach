package csrf

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
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

// withSmallMultipartCap temporarily shrinks MultipartMaxBytes so the
// regression tests can exercise oversize rejection without writing a
// MiB-class fixture into a hot test. Restores the previous value on
// cleanup so the package var stays at its production default for any
// subsequent tests in the run.
func withSmallMultipartCap(t *testing.T, limit int64) {
	t.Helper()
	prev := MultipartMaxBytes
	t.Cleanup(func() { MultipartMaxBytes = prev })
	MultipartMaxBytes = limit
}

// buildMultipartBody encodes a single text field as a multipart body
// using the standard library writer. The first part name matches the
// CSRF form field, with optional padding appended after the token so
// individual cases can grow the body past the cap without touching the
// token plumbing.
func buildMultipartBody(t *testing.T, token string, padBytes int) (string, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField(FormField, token); err != nil {
		t.Fatalf("WriteField csrf: %v", err)
	}
	if padBytes > 0 {
		fw, err := mw.CreateFormField("pad")
		if err != nil {
			t.Fatalf("CreateFormField pad: %v", err)
		}
		if _, err := io.CopyN(fw, zeroReader{}, int64(padBytes)); err != nil {
			t.Fatalf("write pad: %v", err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("multipart close: %v", err)
	}
	return mw.FormDataContentType(), &buf
}

// zeroReader is an infinite reader of NUL bytes, used to inflate
// multipart payloads without allocating a giant in-memory slice.
type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

func TestMultipartWithMatchingTokenPasses(t *testing.T) {
	// Sanity check: a no-JS form whose body fits well under the cap
	// still verifies via the multipart parsing path.
	withSmallMultipartCap(t, 64<<10) // 64 KiB

	const token = "match-multipart"
	ct, body := buildMultipartBody(t, token, 0)
	req := httptest.NewRequest("POST", "/admin/upload", body)
	req.Header.Set("Content-Type", ct)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: token})

	w := httptest.NewRecorder()
	Middleware(okHandler()).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
}

func TestMultipartOversizedRejectedWith413(t *testing.T) {
	// The SAST-002 case: a holder of any csrf cookie+token pair (cheap
	// to obtain from a GET) cannot force ParseMultipartForm to keep
	// reading past the cap.
	const limit = 4 << 10 // 4 KiB
	withSmallMultipartCap(t, limit)

	const token = "oversize-token"
	// padBytes well above the cap so MaxBytesReader trips before
	// ParseMultipartForm finishes assembling the body.
	ct, body := buildMultipartBody(t, token, limit*4)
	req := httptest.NewRequest("POST", "/admin/upload", body)
	req.Header.Set("Content-Type", ct)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: token})

	w := httptest.NewRecorder()
	Middleware(okHandler()).ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body=%s", w.Code, w.Body.String())
	}
}

func TestMultipartHeaderBypassDoesNotParseBody(t *testing.T) {
	// X-CSRF-Token short-circuits verify before MaxBytesReader is
	// installed, so JS-uploader paths can still send bodies larger than
	// the middleware cap without being rejected here.
	withSmallMultipartCap(t, 4<<10) // 4 KiB

	const token = "header-token"
	// Body would blow past the cap if the middleware tried to parse
	// it, but the header path returns first.
	ct, body := buildMultipartBody(t, "", 64<<10)
	req := httptest.NewRequest("POST", "/admin/upload", body)
	req.Header.Set("Content-Type", ct)
	req.Header.Set("X-CSRF-Token", token)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: token})

	bodyParsed := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try to parse the body in the handler to prove the middleware
		// didn't drain it; a successful ParseMultipartForm here means
		// the body is still available downstream.
		if err := r.ParseMultipartForm(1 << 20); err == nil {
			bodyParsed = true
		}
		w.WriteHeader(http.StatusOK)
	})

	w := httptest.NewRecorder()
	Middleware(next).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if !bodyParsed {
		t.Error("expected handler to be able to parse body itself")
	}
}

func TestMultipartHeaderMismatchRejected(t *testing.T) {
	// Even with the header path, a mismatching token must still be
	// rejected — header bypass is not a "no verification" mode.
	withSmallMultipartCap(t, 4<<10)

	ct, body := buildMultipartBody(t, "", 0)
	req := httptest.NewRequest("POST", "/admin/upload", body)
	req.Header.Set("Content-Type", ct)
	req.Header.Set("X-CSRF-Token", "not-the-cookie")
	req.AddCookie(&http.Cookie{Name: CookieName, Value: "real-cookie"})

	w := httptest.NewRecorder()
	Middleware(okHandler()).ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
}
