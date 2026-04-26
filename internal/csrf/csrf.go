// Package csrf defends every non-GET request with a double-submit cookie
// pattern. Server renders each form with a hidden `csrf_token` input whose
// value matches the `sb_csrf` cookie; the middleware rejects POSTs where
// the two don't line up. Works without server-side storage so it applies
// equally to admin POSTs (which have sessions) and public POSTs (comments,
// likes) that don't.
//
// SameSite=Lax on the cookie is the primary defence; the form token is
// belt-and-suspenders against edge cases (cookie leaks via sub-domains,
// older browsers, iframes etc).
package csrf

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"mime"
	"net/http"
)

const (
	// CookieName is the double-submit cookie carrying the CSRF token.
	CookieName = "sb_csrf"
	// FormField is the hidden input name expected on every POST body.
	FormField = "csrf_token"
	// tokenBytes governs how long the random token is before encoding.
	tokenBytes = 32
	// cookieTTL is generous on purpose — the token is just a random
	// secret, not an auth credential. Rotating forces users to refresh
	// half-written forms, which isn't the goal.
	cookieTTL = 30 * 24 * 60 * 60
)

type ctxKey struct{}

// Token returns the token already established on the request (by
// Middleware) so handlers can embed it in rendered forms.
func Token(ctx context.Context) string {
	if v := ctx.Value(ctxKey{}); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// newToken returns a fresh random token encoded with base64url.
func newToken() (string, error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func isSafeMethod(m string) bool {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	}
	return false
}

// Middleware is the CSRF enforcement point. On every request it makes sure
// the sb_csrf cookie is present (minting one when missing) and exposes
// the token to downstream handlers via the request context. On non-safe
// methods (POST, PUT, PATCH, DELETE) it additionally requires the form
// body to carry the same token value.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, refreshed := ensureToken(w, r)

		if !isSafeMethod(r.Method) {
			if err := verify(r, token); err != nil {
				http.Error(w, "CSRF: "+err.Error(), http.StatusForbidden)
				return
			}
		}

		ctx := context.WithValue(r.Context(), ctxKey{}, token)
		_ = refreshed
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ensureToken returns the existing token from the cookie or mints + sets a
// fresh one. Returns (token, createdNow).
func ensureToken(w http.ResponseWriter, r *http.Request) (string, bool) {
	if c, err := r.Cookie(CookieName); err == nil && c.Value != "" {
		return c.Value, false
	}
	t, err := newToken()
	if err != nil {
		// Extremely unlikely — crypto/rand failure. Surface via an empty
		// token so downstream renders break obviously.
		return "", false
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    t,
		Path:     "/",
		MaxAge:   cookieTTL,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
	})
	return t, true
}

// verify checks that the token the client echoed in the form body — or in
// the X-CSRF-Token header for fetch / multipart uploads — matches the value
// we set in the cookie. ParseForm is called defensively so the subsequent
// handlers still see the parsed values.
func verify(r *http.Request, cookieToken string) error {
	if cookieToken == "" {
		return errMissingCookie
	}
	// Header path first: JS fetches (including multipart uploads) use this
	// to avoid stuffing the token into the body.
	if hv := r.Header.Get("X-CSRF-Token"); hv != "" {
		if subtle.ConstantTimeCompare([]byte(cookieToken), []byte(hv)) == 1 {
			return nil
		}
		return errTokenMismatch
	}

	// For multipart bodies (no-JS upload form with a hidden csrf_token),
	// ParseForm doesn't touch the body — we have to call
	// ParseMultipartForm explicitly so PostForm gets populated.
	if isMultipartForm(r) {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			return errInvalidForm
		}
	} else if err := r.ParseForm(); err != nil {
		return errInvalidForm
	}
	sent := r.PostForm.Get(FormField)
	if sent == "" {
		return errMissingFormToken
	}
	if subtle.ConstantTimeCompare([]byte(cookieToken), []byte(sent)) != 1 {
		return errTokenMismatch
	}
	return nil
}

func isMultipartForm(r *http.Request) bool {
	ct := r.Header.Get("Content-Type")
	if ct == "" {
		return false
	}
	mt, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return false
	}
	return mt == "multipart/form-data"
}

// Sentinel errors so the middleware's 403 message says *why* in a way
// that helps ops diagnose a flaky deployment without leaking secrets.
var (
	errMissingCookie    = newStatic("cookie missing")
	errInvalidForm      = newStatic("bad form body")
	errMissingFormToken = newStatic("form token missing")
	errTokenMismatch    = newStatic("token mismatch")
)

type staticErr string

func newStatic(s string) staticErr { return staticErr(s) }
func (e staticErr) Error() string  { return string(e) }
