package analytics

import (
	"log"
	"net/http"
	"strings"
)

const visitorCookieName = "sb_visitor_id"

// botPatterns is a cheap case-insensitive check against the User-Agent.
// It's not meant to be exhaustive — the goal is to skip the obvious
// crawlers so the numbers on the dashboard reflect readers, not scrapes.
var botPatterns = []string{
	"bot", "crawler", "spider", "slurp", "scrape", "wget", "curl", "headlesschrome",
}

// skipPrefixes are paths that must never count toward public analytics.
// Admin pages, static assets, and non-GET "action" endpoints land here.
var skipPrefixes = []string{
	"/admin/",
	"/admin",
	"/static/",
	"/favicon.ico",
	"/style.css",
	"/template/", // per-template CSS + template assets (/template/<id>/...)
	"/robots.txt",
}

// Middleware returns a chi-compatible middleware that records one pageview
// per qualifying request. Nil Store or Record() error = silent drop; we
// never break a page render over analytics bookkeeping.
func (s *Store) Middleware(next http.Handler) http.Handler {
	if s == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Ensure the visitor cookie is set even on skipped paths so that
		// returning visitors on the next GET are recognised immediately.
		visitorID, fresh := s.ensureVisitorCookie(w, r)

		if !s.shouldRecord(r) {
			next.ServeHTTP(w, r)
			return
		}

		// Best effort: log, never block the response.
		if err := s.Record(r.Context(), visitorID, r.URL.Path, s.entryIDForRequest(r.Context(), r.URL.Path)); err != nil {
			log.Printf("analytics: record: %v", err)
		}
		_ = fresh
		next.ServeHTTP(w, r)
	})
}

// ensureVisitorCookie reads the existing visitor cookie or mints a new one
// and sets it on the response. Returns (id, freshlyMinted).
func (s *Store) ensureVisitorCookie(w http.ResponseWriter, r *http.Request) (string, bool) {
	if c, err := r.Cookie(visitorCookieName); err == nil && c.Value != "" {
		return c.Value, false
	}
	id, err := NewVisitorID()
	if err != nil {
		return "", false
	}
	http.SetCookie(w, &http.Cookie{
		Name:     visitorCookieName,
		Value:    id,
		Path:     "/",
		MaxAge:   365 * 24 * 60 * 60,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
	})
	return id, true
}

func (s *Store) shouldRecord(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	p := r.URL.Path
	for _, pref := range skipPrefixes {
		if p == pref || strings.HasPrefix(p, pref) {
			return false
		}
	}
	ua := strings.ToLower(r.UserAgent())
	for _, pat := range botPatterns {
		if strings.Contains(ua, pat) {
			return false
		}
	}
	return true
}
