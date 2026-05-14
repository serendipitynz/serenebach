package content

import (
	"strconv"

	"github.com/serendipitynz/serenebach/internal/domain"
)

// EntryPermalink is the single source of truth for entry URLs. When an
// entry has a custom slug the canonical URL is /entry/<slug>/;
// otherwise it falls back to the numeric id form. The
// public router accepts either key shape and 301s id → slug when a slug
// exists, so previously-cached reader links never rot.
//
// All list/permalink URLs end in "/" so the same paths resolve both in
// dynamic mode (chi + StripSlashes middleware) and in static mode (served
// as `<path>/index.html` by any static host).
func (s Site) EntryPermalink(e domain.Entry) string {
	return s.TopURL() + "entry/" + entryKey(e) + "/"
}

// entryKey returns the slug when set, falling back to the numeric id.
// Exposed package-private so rebuild can place the static output at the
// same path the handler resolves. Not exported: callers should go
// through EntryPermalink to stay consistent.
func entryKey(e domain.Entry) string {
	if e.Slug != "" {
		return e.Slug
	}
	return strconv.FormatInt(e.ID, 10)
}

// EntryStaticPath returns the static-snapshot filesystem segment for an
// entry — "entry/<key>" without a leading or trailing slash — so rebuild
// can write `<out>/<EntryStaticPath(e)>/index.html`. Keeping this next
// to EntryPermalink guarantees the two never drift.
func (s Site) EntryStaticPath(e domain.Entry) string {
	return "entry/" + entryKey(e)
}

// CategoryPermalink returns the URL for a category listing page. Same
// "change here and router at once" discipline as EntryPermalink — when
// a category has a custom slug the canonical URL is
// /category/<slug>/; otherwise it falls back to the numeric id form.
// The public router accepts either key shape and 301s id → slug when a
// slug exists.
func (s Site) CategoryPermalink(c domain.Category) string {
	return s.TopURL() + "category/" + categoryKey(c) + "/"
}

// categoryKey returns the slug when set, falling back to the numeric
// id. Exposed package-private so rebuild can place the static output at
// the same path the handler resolves. Not exported: callers should go
// through CategoryPermalink to stay consistent.
func categoryKey(c domain.Category) string {
	if c.Slug != "" {
		return c.Slug
	}
	return strconv.FormatInt(c.ID, 10)
}

// CategoryStaticPath returns the static-snapshot filesystem segment for
// a category — "category/<key>" without a leading or trailing slash —
// so rebuild can write `<out>/<CategoryStaticPath(c)>/index.html`.
// Mirrors EntryStaticPath so the two never drift.
func (s Site) CategoryStaticPath(c domain.Category) string {
	return "category/" + categoryKey(c)
}

// TagPermalink returns the URL for a tag listing page. Slug is the
// addressing identifier (tags have no id-based fallback — they're
// created by authors, not auto-seeded).
func (s Site) TagPermalink(t domain.Tag) string {
	return s.TopURL() + "tag/" + t.Slug + "/"
}

// ArchivePermalink returns the URL for a year or year+month archive page.
// Pass 0 for month to get the full-year URL.
func (s Site) ArchivePermalink(year, month int) string {
	if month == 0 {
		return s.TopURL() + "archive/" + strconv.Itoa(year) + "/"
	}
	return s.TopURL() + "archive/" + strconv.Itoa(year) + "/" + padMonth(month) + "/"
}

func padMonth(m int) string {
	if m < 10 {
		return "0" + strconv.Itoa(m)
	}
	return strconv.Itoa(m)
}
