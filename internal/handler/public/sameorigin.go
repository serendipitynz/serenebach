package public

import (
	"net/http"
	"net/url"
	"strings"
)

// SameOriginGuard rejects unsafe-method requests whose Origin (or
// Referer fallback) doesn't match a configured allow-list. Used for
// reader-facing POSTs (comment / like / stamp) that intentionally sit
// outside the CSRF middleware so static-rendered HTML can post to the
// dynamic backend.
//
// Origin/Referer is NOT as strong as a per-session CSRF token — a
// determined attacker scripting curl or a non-browser HTTP client can
// forge these headers. What it does block is the realistic
// browser-based attack: a malicious site embedding a hidden form that
// targets the comment endpoint with the visitor's cookies. For
// anonymous public POSTs that's the only attack vector worth
// defending against; spam from non-browser clients is the spam-filter
// + Turnstile + IP-blocklist's job, not CSRF's.
type SameOriginGuard struct {
	allowed []string // canonical scheme://host[:port], lowercased
}

// NewSameOriginGuard normalises the allow-list. Values that don't
// parse as scheme + host are dropped so a typo in
// SB_PUBLIC_ALLOWED_ORIGINS doesn't silently widen the policy. An
// empty result is fail-closed: the middleware rejects every
// non-safe request.
func NewSameOriginGuard(origins []string) SameOriginGuard {
	canon := make([]string, 0, len(origins))
	seen := map[string]struct{}{}
	for _, o := range origins {
		c := canonicalOrigin(o)
		if c == "" {
			continue
		}
		if _, dup := seen[c]; dup {
			continue
		}
		seen[c] = struct{}{}
		canon = append(canon, c)
	}
	return SameOriginGuard{allowed: canon}
}

// canonicalOrigin reduces an input to "scheme://host[:port]" with the
// scheme + host lowercased. Returns empty string when the input is
// not a valid absolute URL — used by the allow-list normaliser to
// drop garbage and by the runtime check to compare request headers
// uniformly.
func canonicalOrigin(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	u, err := url.Parse(s)
	if err != nil {
		return ""
	}
	if u.Scheme == "" || u.Host == "" {
		return ""
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host)
}

// Middleware enforces the policy on every non-safe method (POST, PUT,
// PATCH, DELETE). Safe methods pass through untouched so reader page
// loads aren't penalised.
func (g SameOriginGuard) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isSafeMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		if !g.permits(r) {
			http.Error(w, "cross-origin request rejected", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// permits returns true when the request's Origin (or, only when
// Origin is absent, Referer) matches the allow-list. Origin: null
// — sent by sandboxed iframes and some data: URIs — is always
// rejected. A request with neither header on an unsafe method is
// also rejected: legitimate browsers always send one.
func (g SameOriginGuard) permits(r *http.Request) bool {
	if origin := r.Header.Get("Origin"); origin != "" {
		if origin == "null" {
			return false
		}
		return g.matches(origin)
	}
	if ref := r.Header.Get("Referer"); ref != "" {
		return g.matches(ref)
	}
	return false
}

func (g SameOriginGuard) matches(raw string) bool {
	canon := canonicalOrigin(raw)
	if canon == "" {
		return false
	}
	for _, a := range g.allowed {
		if a == canon {
			return true
		}
	}
	return false
}

func isSafeMethod(m string) bool {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	}
	return false
}
