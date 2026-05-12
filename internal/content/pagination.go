// pagination.go — `page` block population for list routes.
//
// Mirrors sb::Content::Common::_page: each list page (home / category
// / tag / archive) can expose a paginator via `<!-- BEGIN page -->` +
// the {page_num} / {page_now} / {prev_page_*} / {next_page_*} tag
// family. Block count is 1 when there's more than one page of entries,
// 0 when the whole set fits on a single page (so the markup strips
// cleanly) — same truthy-gate SB3 uses.
package content

import (
	"html"
	"strconv"

	"github.com/serendipitynz/serenebach/internal/template/sbtemplate"
)

// Pagination bundles the inputs applyPageBlock needs. Zero value
// (TotalEntries == 0 or BasePath empty) is safe — the helper
// collapses the block to 0 and leaves the `{page_*}` tags at their
// default empty-string values set by Site.Apply.
type Pagination struct {
	// CurrentPage is 1-indexed. Handlers clamp to >= 1 before the
	// repo query and reject `?page=N` values past the last page
	// with a 404, but the helper is defensive — values <= 0
	// collapse the block rather than render nonsense.
	CurrentPage int
	// PageSize mirrors the author's entries_per_page setting.
	PageSize int
	// TotalEntries is the full unpaginated count across the route's
	// filter (home / category / tag / archive). Handlers fetch it
	// via the matching CountPublishedEntries* repo method.
	TotalEntries int64
	// BasePath is the URL path the paginator links against — "/",
	// "/category/1/", "/tag/foo/", "/archive/2026/04/". The
	// helper appends "?page=N" for prev/next links.
	BasePath string
}

// PageCount returns how many pages the entry list fills. 0 when
// there's nothing to page through; 1 on a single-page list.
func (p Pagination) PageCount() int {
	if p.PageSize <= 0 || p.TotalEntries <= 0 {
		return 0
	}
	n := int(p.TotalEntries) / p.PageSize
	if int(p.TotalEntries)%p.PageSize != 0 {
		n++
	}
	return n
}

// applyPageBlock populates the `page` block + its tag family. Always
// sets the tags (overriding the empty defaults Site.Apply seeded) so
// templates that reference {page_now} outside the block still show
// the right number on page >= 2.
func applyPageBlock(c *sbtemplate.Context, tmpl *sbtemplate.Template, pg Pagination) {
	pageCount := pg.PageCount()

	// Tags go onto num=0 (global scope) so templates can use them
	// both inside and outside the `page` block — matches SB3's
	// `_page` which emits with $cms->num(0). Explicit Num(0) reset
	// is required here because earlier block loops (the `entry`
	// iteration, most of all) leave the cursor at a later index.
	c.Num(0)
	c.Tag("page_num", strconv.Itoa(pageCount))
	c.Tag("page_now", strconv.Itoa(max(pg.CurrentPage, 1)))

	prevURL := ""
	nextURL := ""
	prevLink := ""
	nextLink := ""
	if pg.CurrentPage > 1 {
		prevURL = pg.BasePath + "?page=" + strconv.Itoa(pg.CurrentPage-1)
		prevLink = `<a href="` + html.EscapeString(prevURL) + `" rel="prev">&lt;&lt;</a>`
	}
	if pg.CurrentPage > 0 && pg.CurrentPage < pageCount {
		nextURL = pg.BasePath + "?page=" + strconv.Itoa(pg.CurrentPage+1)
		nextLink = `<a href="` + html.EscapeString(nextURL) + `" rel="next">&gt;&gt;</a>`
	}
	c.Tag("prev_page_url", prevURL)
	c.TagHTML("prev_page_link", prevLink)
	c.Tag("next_page_url", nextURL)
	c.TagHTML("next_page_link", nextLink)

	if !tmpl.HasBlock("page") {
		return
	}
	if pageCount <= 1 {
		c.Block("page", 0)
		return
	}
	c.Block("page", 1)
}
