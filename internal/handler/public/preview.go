package public

import (
	"net/http"
	"strconv"

	"github.com/serendipitynz/serenebach/internal/session"
)

// Query-param names driving admin preview mode. Match SB3's `tid=`
// spirit (a single URL knob forces a dynamic render) but live under
// a `__sb_` namespace so they can't collide with author-chosen slugs
// or template tag values.
const (
	previewDraftParam    = "__sb_preview"
	previewTemplateParam = "__sb_template"
)

// previewOverride carries the two knobs the admin preview buttons
// can flip on a public request. Zero values mean "no override" — the
// page renders exactly as an anonymous visitor would see it.
type previewOverride struct {
	// AllowDraft lifts the EntryDraft / EntryClosed rejections in
	// the /entry/{key} handler so an admin can review an unpublished
	// entry before flipping its status.
	AllowDraft bool
	// TemplateID swaps the active template for this one request —
	// the authoritative DB row is not touched. Admin confirms a
	// template candidate against the live site without having to
	// mark it "use" first.
	TemplateID int64
}

// Active reports whether any preview knob is flipped, so callers can
// decide to set no-store / noindex response headers.
func (p previewOverride) Active() bool { return p.AllowDraft || p.TemplateID > 0 }

// previewFromRequest parses the preview query params and validates
// that the caller is logged in as an admin. Any preview knob set
// by an anonymous (or unauthenticated) request silently collapses
// to the zero value so a leaked `?__sb_preview=1` link can never
// expose drafts.
func previewFromRequest(r *http.Request) previewOverride {
	// Short-circuit when nothing is on the URL to avoid the ctx lookup
	// for every public page render.
	q := r.URL.Query()
	draftRaw := q.Get(previewDraftParam)
	tmplRaw := q.Get(previewTemplateParam)
	if draftRaw == "" && tmplRaw == "" {
		return previewOverride{}
	}
	if session.UserFrom(r.Context()) == nil {
		return previewOverride{}
	}
	var p previewOverride
	if draftRaw == "1" || draftRaw == "true" {
		p.AllowDraft = true
	}
	if tmplRaw != "" {
		if id, err := strconv.ParseInt(tmplRaw, 10, 64); err == nil && id > 0 {
			p.TemplateID = id
		}
	}
	return p
}

// markPreviewResponse stamps no-store + noindex on a response known
// to carry preview data. Keeps admin drafts out of shared caches and
// search indexes even if a preview URL accidentally leaks.
func markPreviewResponse(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store, private")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
}
