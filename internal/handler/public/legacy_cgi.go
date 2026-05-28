package public

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/serendipitynz/serenebach/internal/storage/repo"
)

// legacyCGI fronts the `/sb.cgi` URL emitted by SB3 in two distinct
// flavours:
//
//  1. Internal template URLs ({site_cgi}?mode=…&eid=…). These appear
//     inside SB3-generated HTML — comment forms, search actions,
//     cross-page navigation. Imported templates render the SB3 spelling
//     until they're rewritten, and live SB3 admins still produce them.
//
//  2. External permalinks (?eid=N / ?cid=N / ?month=YYYYMM, no `mode`
//     param). SB3's permalink() emits these for dynamic-only blogs;
//     external links and bookmarks landed on this shape.
//
// We don't run the old Perl dispatcher — we translate the request into
// the Go port's canonical URL and let chi route from there.
//
// Translations:
//
//	?eid=N (no mode)        → 301 /entry/{key}/    (legacy_id lookup)
//	?cid=N (no mode)        → 301 /category/{id}/  (legacy_id lookup)
//	?month=YYYYMM (no mode) → 301 /archive/YYYY/MM/
//	mode=entry&eid=N        → 301 /entry/{key}/    (legacy_id lookup)
//	mode=category&cid=N     → 301 /category/{id}/  (legacy_id lookup)
//	mode=archive&cond=YYYYMM[DD]
//	                        → 301 /archive/YYYY[/MM]/
//	mode=user&pid=N         → 301 /profile/N/      (id passthrough; user
//	                                                import is out of scope
//	                                                for the SB3 port)
//	mode=comment&eid=N, POST → 307 /entry/N/comment (body preserved)
//	mode=comment&eid=N, GET  → 301 /entry/N/#comment-form
//	mode=search&search=<q>   → 301 /search?q=<q>  (term also accepted via q=<q>)
//	?search=<q> (no mode)    → 301 /search?q=<q>  (SB3 native — sb::App::Main
//	                                               infers srch mode purely
//	                                               from the search param)
//	(empty / unknown)       → 301 /
//
// 307 preserves the POST body + method so the existing commentSubmit
// handler still owns CSRF / Turnstile / spam checks — this shim only
// smooths out the URL shape.
func (h *Handler) legacyCGI(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	mode := q.Get("mode")

	// Mode-less external permalinks. SB3's permalink() emits these for
	// dynamic blogs and they're the form most likely to live in
	// external bookmarks / search indexes.
	if mode == "" {
		h.serveLegacyModelessRedirect(w, r, q)
		return
	}

	switch mode {
	case "entry":
		h.serveLegacyEntryRedirect(w, r, q)
	case "category":
		h.serveLegacyCategoryRedirect(w, r, q)
	case "archive":
		serveLegacyArchiveRedirect(w, r, q)
	case "user":
		serveLegacyUserRedirect(w, r, q)
	case "comment":
		serveLegacyCommentRedirect(w, r, q)
	case "search":
		serveLegacySearchRedirect(w, r, q)
	case "page":
		http.Redirect(w, r, root(r)+"/", http.StatusMovedPermanently)
	default:
		// Unknown mode values (mobile, rsd, trackback…) land on the
		// home page rather than 404 so a misconfigured imported
		// template doesn't drop the reader off a cliff.
		http.Redirect(w, r, root(r)+"/", http.StatusMovedPermanently)
	}
}

func (h *Handler) serveLegacyModelessRedirect(w http.ResponseWriter, r *http.Request, q url.Values) {
	// SB3's sb::App::Main upgrades the request to search mode whenever
	// `search` is non-empty (last-assignment-wins, so it overrides eid /
	// cid / month / day). Mirror that priority here so imported
	// templates and old paginator links keep their query string.
	if s := q.Get("search"); s != "" {
		redirectLegacySearch(w, r, s)
		return
	}
	if eid := q.Get("eid"); eid != "" {
		h.redirectLegacyEntryID(w, r, eid)
		return
	}
	if cid := q.Get("cid"); cid != "" {
		h.redirectLegacyCategoryID(w, r, cid)
		return
	}
	if m := q.Get("month"); m != "" {
		redirectLegacyMonth(w, r, m)
		return
	}
	http.Redirect(w, r, root(r)+"/", http.StatusMovedPermanently)
}

// serveLegacySearchRedirect handles `mode=search`. SB3 templates carry
// the term in `search=<q>`, but the current `mode=search&q=<q>` form
// is also accepted because existing tests / templates may produce it.
// Empty term forwards to bare `/search`, where the public handler
// renders the guidance page.
func serveLegacySearchRedirect(w http.ResponseWriter, r *http.Request, q url.Values) {
	term := q.Get("search")
	if term == "" {
		term = q.Get("q")
	}
	redirectLegacySearch(w, r, term)
}

// redirectLegacySearch 301s to the Go-native /search route. term is
// url-encoded so spaces and Japanese pass through cleanly; an empty
// term still forwards (to the bare /search) so the modern handler can
// render its guidance page instead of dropping the visitor on the
// home page with no explanation.
func redirectLegacySearch(w http.ResponseWriter, r *http.Request, term string) {
	dest := root(r) + "/search"
	if term != "" {
		dest += "?q=" + url.QueryEscape(term)
	}
	http.Redirect(w, r, dest, http.StatusMovedPermanently)
}

func (h *Handler) serveLegacyEntryRedirect(w http.ResponseWriter, r *http.Request, q url.Values) {
	if eid := q.Get("eid"); eid != "" {
		h.redirectLegacyEntryID(w, r, eid)
		return
	}
	http.Redirect(w, r, root(r)+"/", http.StatusMovedPermanently)
}

func (h *Handler) serveLegacyCategoryRedirect(w http.ResponseWriter, r *http.Request, q url.Values) {
	if cid := q.Get("cid"); cid != "" {
		h.redirectLegacyCategoryID(w, r, cid)
		return
	}
	http.NotFound(w, r)
}

func serveLegacyArchiveRedirect(w http.ResponseWriter, r *http.Request, q url.Values) {
	cond := q.Get("cond")
	switch len(cond) {
	case 4:
		http.Redirect(w, r, root(r)+"/archive/"+cond+"/", http.StatusMovedPermanently)
	case 6:
		http.Redirect(w, r, root(r)+"/archive/"+cond[:4]+"/"+cond[4:]+"/", http.StatusMovedPermanently)
	default:
		http.NotFound(w, r)
	}
}

// serveLegacyUserRedirect honours the pid passthrough rather than 404
// because user import is out of scope (SB3 stored crypt() hashes
// incompatible with bcrypt) so SB3 user_id == Go user.id is never
// guaranteed. Imported templates that emit {site_cgi}?mode=user&pid=N
// still land somewhere useful.
func serveLegacyUserRedirect(w http.ResponseWriter, r *http.Request, q url.Values) {
	if pid := q.Get("pid"); pid != "" {
		http.Redirect(w, r, root(r)+"/profile/"+pid+"/", http.StatusMovedPermanently)
		return
	}
	http.NotFound(w, r)
}

// serveLegacyCommentRedirect forwards SB3's comment-form URLs. We keep
// the eid as id-passthrough rather than legacy_id lookup because the
// URL is generated by an imported template and goes away once that
// template is replaced; the rare post-import comment hit is acceptable
// collateral. 307 preserves the POST body + method so the canonical
// commentSubmit handler still owns CSRF / Turnstile / spam checks.
func serveLegacyCommentRedirect(w http.ResponseWriter, r *http.Request, q url.Values) {
	eid := q.Get("eid")
	if eid == "" {
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodPost {
		http.Redirect(w, r, root(r)+"/entry/"+eid+"/comment", http.StatusTemporaryRedirect)
		return
	}
	http.Redirect(w, r, root(r)+"/entry/"+eid+"/#comment-form", http.StatusMovedPermanently)
}

func (h *Handler) redirectLegacyEntryID(w http.ResponseWriter, r *http.Request, eid string) {
	legacyID, err := strconv.ParseInt(eid, 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ref, err := h.Store.EntryByLegacyID(r.Context(), h.WID, legacyID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "lookup failed", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, root(r)+"/entry/"+entryKeyForRef(ref)+"/", http.StatusMovedPermanently)
}

func (h *Handler) redirectLegacyCategoryID(w http.ResponseWriter, r *http.Request, cid string) {
	legacyID, err := strconv.ParseInt(cid, 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ref, err := h.Store.CategoryByLegacyID(r.Context(), h.WID, legacyID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "lookup failed", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, root(r)+"/category/"+categoryKeyForRef(ref)+"/", http.StatusMovedPermanently)
}

// redirectLegacyMonth handles ?month=YYYYMM. The Go archive route is
// /archive/YYYY/MM/, so we just split + redirect — no DB lookup needed.
// Bad shapes 404 rather than guess.
func redirectLegacyMonth(w http.ResponseWriter, r *http.Request, m string) {
	if len(m) != 6 {
		http.NotFound(w, r)
		return
	}
	if _, err := strconv.Atoi(m); err != nil {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, root(r)+"/archive/"+m[:4]+"/"+m[4:]+"/", http.StatusMovedPermanently)
}

// entryKeyForRef mirrors entryKeyFor() but takes the redirect-only
// repo.LegacyEntryRef so we don't have to load the full domain.Entry
// just to pick between slug and id.
func entryKeyForRef(ref repo.LegacyEntryRef) string {
	if ref.Slug != "" {
		return ref.Slug
	}
	return strconv.FormatInt(ref.ID, 10)
}
