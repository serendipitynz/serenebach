package admin

import (
	"net/url"
	"strconv"
)

// listPagination turns the raw ?page= query value and the total row
// count into a clamped (page, totalPages, offset) triple. Bad input
// (non-numeric, < 1, past the last page) silently clamps so a stale
// bookmark renders the closest valid page instead of 500-ing.
func listPagination(rawPage string, total int64, pageSize int) (page, totalPages, offset int) {
	if pageSize <= 0 {
		pageSize = 1
	}
	page = 1
	if rawPage != "" {
		if v, err := strconv.Atoi(rawPage); err == nil && v > 0 {
			page = v
		}
	}
	totalPages = int((total + int64(pageSize) - 1) / int64(pageSize))
	if totalPages == 0 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
	}
	offset = (page - 1) * pageSize
	return page, totalPages, offset
}

// pagerNeighbours computes the pager's prev/next link targets. Zero
// means "no link in this direction" — the template renders the arrow
// as disabled.
func pagerNeighbours(page, totalPages int) (prev, next int) {
	if page > 1 {
		prev = page - 1
	}
	if page < totalPages {
		next = page + 1
	}
	return prev, next
}

// pagerView is the value the shared "pager" partial in layout.html
// renders. PrevHref / NextHref are empty when the corresponding arrow
// should render disabled; handlers precompute them so the template
// stays branch-free.
type pagerView struct {
	Page       int
	TotalPages int
	PrevHref   string
	NextHref   string
}

// sortLink is one column header's precomputed link state — the href to
// navigate to on click and the CSS class identifying whether this
// column is the active sort + which direction it currently shows. The
// arrow itself is drawn via CSS pseudo-element keyed off the class
// (see .sort-link.asc / .sort-link.desc in admin.css).
type sortLink struct {
	Href  string
	Class string // "" or "active asc" / "active desc"
}

// listURLState bundles the query params that travel together across an
// admin list page's links: the search needle, the active sort column,
// the sort direction, and the page number. Methods on this type
// generate the URL strings the template consumes — handlers don't
// touch query strings directly.
type listURLState struct {
	BasePath string // e.g. "/admin/entries"
	Search   string // ?q= value (un-escaped)
	SortKey  string // ?sort= value
	SortDir  string // ?dir= value ("asc" / "desc")
	Page     int    // ?page= value (1 = omit)
}

// hrefSort returns the URL to navigate to when the user clicks the
// header for `key`. The page resets to 1 because the page index is
// meaningless across a different ordering. If `key` is already the
// active sort column, the direction toggles; otherwise it uses the
// caller-supplied default direction for that column.
func (s listURLState) hrefSort(key, defaultDir string) string {
	dir := defaultDir
	if key == s.SortKey {
		if s.SortDir == "asc" {
			dir = "desc"
		} else {
			dir = "asc"
		}
	}
	return s.encode(listURLState{
		BasePath: s.BasePath,
		Search:   s.Search,
		SortKey:  key,
		SortDir:  dir,
		Page:     1,
	})
}

// hrefPage returns the URL for jumping to `page` while preserving the
// current search and sort state. Returns "" for page <= 0 so callers
// can render a disabled arrow.
func (s listURLState) hrefPage(page int) string {
	if page <= 0 {
		return ""
	}
	return s.encode(listURLState{
		BasePath: s.BasePath,
		Search:   s.Search,
		SortKey:  s.SortKey,
		SortDir:  s.SortDir,
		Page:     page,
	})
}

// classFor returns the CSS class for `key`'s header — "" when this
// column isn't the active sort, "active asc" / "active desc" when it
// is. The trailing direction is what the CSS arrow pseudo-element
// keys off of.
func (s listURLState) classFor(key string) string {
	if key != s.SortKey {
		return ""
	}
	if s.SortDir == "asc" {
		return "active asc"
	}
	return "active desc"
}

// encode renders a listURLState as a relative URL. Empty fields are
// omitted; page=1 is omitted so the canonical landing URL stays
// clean. Order is stable (q, sort, dir, page) so equivalent states
// produce identical URLs — easier to spot in browser history.
func (listURLState) encode(s listURLState) string {
	v := url.Values{}
	if s.Search != "" {
		v.Set("q", s.Search)
	}
	if s.SortKey != "" {
		v.Set("sort", s.SortKey)
	}
	if s.SortDir != "" {
		v.Set("dir", s.SortDir)
	}
	if s.Page > 1 {
		v.Set("page", strconv.Itoa(s.Page))
	}
	if len(v) == 0 {
		return s.BasePath
	}
	return s.BasePath + "?" + encodeStable(v)
}

// encodeStable is url.Values.Encode with a fixed key order so the
// output is reproducible across runs. url.Values.Encode itself sorts
// keys alphabetically, which is good for determinism but produces
// "?dir=…&page=…&q=…&sort=…" — readable enough, but we want q first
// because it's the most user-visible part of the URL.
func encodeStable(v url.Values) string {
	order := []string{"q", "sort", "dir", "page"}
	out := ""
	for _, k := range order {
		val := v.Get(k)
		if val == "" {
			continue
		}
		if out != "" {
			out += "&"
		}
		out += k + "=" + url.QueryEscape(val)
	}
	return out
}
