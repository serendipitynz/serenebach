package public

import (
	"crypto/sha256"
	"encoding/hex"
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/serendipitynz/serenebach/internal/domain"
)

// likedCookieName returns the short-circuit cookie name scoped to one entry.
// The cookie exists purely for UX — the DB fingerprint row is what actually
// enforces uniqueness.
func likedCookieName(entryID int64) string {
	return "sb_liked_" + strconv.FormatInt(entryID, 10)
}

// fingerprintFor hashes the client IP + User-Agent into a short hex string
// that we can uniquely-index per entry. It's not a security boundary —
// determined attackers rotate IPs — but it catches the obvious "click 10
// times" case even when cookies are cleared.
func (h *Handler) fingerprintFor(r *http.Request) string {
	sum := sha256.Sum256([]byte(h.clientIP(r) + "|" + r.UserAgent()))
	return hex.EncodeToString(sum[:16])
}

func (h *Handler) entryLike(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	entry, _, err := h.resolveEntryKey(ctx, chi.URLParam(r, "key"))
	if err != nil || entry.Status != domain.EntryPublished {
		http.NotFound(w, r)
		return
	}
	entryID := entry.ID

	back := root(r) + "/entry/" + entryKeyFor(entry) + "/"

	// Cookie short-circuit — avoids a needless DB round-trip when a browser
	// has already liked this entry.
	if _, err := r.Cookie(likedCookieName(entryID)); err == nil {
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}

	newLike, err := h.Store.LikeEntry(ctx, entryID, h.fingerprintFor(r))
	if err != nil {
		log.Printf("public.entryLike: %v", err)
		http.Error(w, "failed to record like", http.StatusInternalServerError)
		return
	}

	// Always set the cookie so future attempts short-circuit cheaply, even
	// when the fingerprint rejected the repeat.
	http.SetCookie(w, &http.Cookie{
		Name:     likedCookieName(entryID),
		Value:    "1",
		Path:     "/",
		MaxAge:   365 * 24 * 60 * 60,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
	})
	_ = newLike
	http.Redirect(w, r, back, http.StatusSeeOther)
}

// stampedCookieName is the scoped short-circuit cookie set once per
// (entry, kind) reaction. Matches the per-entry like cookie's naming
// so the browser devtools cookie list stays scannable.
func stampedCookieName(entryID int64, kind domain.StampKind) string {
	return "sb_stamped_" + strconv.FormatInt(entryID, 10) + "_" + string(kind)
}

func (h *Handler) entryStamp(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	entry, _, err := h.resolveEntryKey(ctx, chi.URLParam(r, "key"))
	if err != nil || entry.Status != domain.EntryPublished {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	kind := domain.StampKind(r.PostFormValue("kind"))
	if !kind.Valid() {
		http.Error(w, "invalid stamp kind", http.StatusBadRequest)
		return
	}
	entryID := entry.ID

	back := root(r) + "/entry/" + entryKeyFor(entry) + "/"

	// Cookie short-circuit per (entry, kind) so re-clicking the same
	// reaction button doesn't need a DB round-trip.
	if _, err := r.Cookie(stampedCookieName(entryID, kind)); err == nil {
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}

	if _, err := h.Store.StampEntry(ctx, entryID, kind, h.fingerprintFor(r)); err != nil {
		log.Printf("public.entryStamp: %v", err)
		http.Error(w, "failed to record stamp", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     stampedCookieName(entryID, kind),
		Value:    "1",
		Path:     "/",
		MaxAge:   365 * 24 * 60 * 60,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
	})
	http.Redirect(w, r, back, http.StatusSeeOther)
}
