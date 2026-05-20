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
	"errors"
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
	// DefaultMultipartMaxBytes is the conservative cap used for multipart
	// bodies the middleware has to parse itself (no-JS form fallback).
	// Large uploads should send the token via X-CSRF-Token so the
	// middleware doesn't touch the body at all. 1 MiB covers small
	// no-JS forms (text-only template imports, settings panels) without
	// letting an attacker with a cookie/token pair force unbounded
	// ParseMultipartForm work pre-authentication.
	DefaultMultipartMaxBytes int64 = 1 << 20
)

// MultipartMaxBytes caps how much of a multipart body the middleware
// will read while extracting the form-encoded token. Defaults to
// DefaultMultipartMaxBytes; the process should set it once at startup
// (e.g. from SB_CSRF_MULTIPART_MAX_BYTES) before the middleware first
// runs. Real upload paths bypass this entirely by sending the token in
// the X-CSRF-Token header so the body is never parsed here.
var MultipartMaxBytes = DefaultMultipartMaxBytes

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
			// Cap the multipart body we are willing to parse before
			// authentication. Anyone holding a CSRF cookie+token pair
			// (cheap to obtain from a GET) could otherwise force a
			// 32 MiB ParseMultipartForm via the no-JS fallback path.
			// The X-CSRF-Token path skips this entirely because verify
			// returns before the body is touched.
			if r.Header.Get("X-CSRF-Token") == "" && isMultipartForm(r) {
				r.Body = http.MaxBytesReader(w, r.Body, MultipartMaxBytes)
			}
			if err := verify(r, token); err != nil {
				status := http.StatusForbidden
				if errors.Is(err, errTooLarge) {
					status = http.StatusRequestEntityTooLarge
				}
				http.Error(w, "CSRF: "+err.Error(), status)
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
	// ParseMultipartForm explicitly so PostForm gets populated. The
	// MaxBytesReader applied in Middleware bounds how much body we are
	// willing to read here pre-authentication.
	if isMultipartForm(r) {
		if err := r.ParseMultipartForm(MultipartMaxBytes); err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				return errTooLarge
			}
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

// Sentinel errors so the middleware's 403 (or 413 for errTooLarge) message
// says *why* in a way that helps ops diagnose a flaky deployment without
// leaking secrets.
var (
	errMissingCookie    = newStatic("cookie missing")
	errInvalidForm      = newStatic("bad form body")
	errMissingFormToken = newStatic("form token missing")
	errTokenMismatch    = newStatic("token mismatch")
	errTooLarge         = newStatic("multipart body too large")
)

type staticErr string

func newStatic(s string) staticErr { return staticErr(s) }
func (e staticErr) Error() string  { return string(e) }
