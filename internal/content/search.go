package content

import (
	"fmt"
	"html"
	"log"
	"strconv"
	"strings"

	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/format"
	"github.com/serendipitynz/serenebach/internal/template/sbtemplate"
)

// SearchView renders the /search results page. It piggybacks on the
// active template's `main_body` so search results share chrome (header,
// sidebar, footer) with the home / list pages. Custom Serene Bach
// blocks `search_results` (>0 hits) and `search_empty` (0 hits) gate
// per-state markup. The `entry` block iterates over Results — but only
// when there are hits.
//
// When Query is empty (the user landed on /search with no q), the
// renderer treats it as "guidance mode" and emits 0 hits with
// search_results=0, search_empty=0 so the template can show its
// default search-form prompt instead of a results header.
type SearchView struct {
	Site         Site
	Template     *domain.Template
	Query        string
	Results      []domain.Entry
	Page         int
	PageSize     int
	TotalCount   int
	Categories   map[int64]domain.Category
	Users        map[int64]domain.User
	Tags         map[int64][]domain.Tag
	ProfileUsers []domain.User
	Sidebar      SidebarData
	Pagination   Pagination
	// HasQuery distinguishes "no query entered" (false → guidance
	// page, neither search_results nor search_empty fires) from
	// "query entered but matched nothing" (true with 0 results →
	// search_empty=1). Set by the handler from repo.HasSearchTerms.
	HasQuery bool
}

// Render produces the final HTML string for the /search page.
func (v SearchView) Render() (string, error) {
	if v.Template == nil || v.Template.MainBody == "" {
		return "", fmt.Errorf("content.SearchView: no template main body")
	}

	tmpl, err := cachedParse(v.Template, "main", v.Template.MainBody)
	if err != nil {
		return "", fmt.Errorf("content.SearchView: parse: %w", err)
	}
	c := tmpl.New()

	pageSuffix := "Search"
	if v.Query != "" {
		pageSuffix = "Search: " + v.Query
	}
	v.Site.
		WithTemplate(v.Template).
		WithMode("srch", v.Query).
		WithPageSuffix(pageSuffix).
		WithSearchContext(v.Query, v.TotalCount).
		Apply(c)

	c.Block("title", 1)
	if tmpl.HasBlock("option") {
		c.Block("option", 0)
	}
	c.Block("toppage", 0)

	for i, e := range v.Results {
		v.applyEntry(c, i, e)
	}
	c.Block("entry", len(v.Results))

	if tmpl.HasBlock("search_results") {
		if v.HasQuery && len(v.Results) > 0 {
			c.Block("search_results", 1)
		} else {
			c.Block("search_results", 0)
		}
	}
	if tmpl.HasBlock("search_empty") {
		if v.HasQuery && len(v.Results) == 0 {
			c.Block("search_empty", 1)
		} else {
			c.Block("search_empty", 0)
		}
	}

	applyProfileBlock(v.Site, c, tmpl, v.ProfileUsers)
	// {selected_entry_list} on the search page lists the current hit
	// set — SB3 _selected's headline use case. Guidance mode (no
	// query) leaves Results empty, collapsing the block to 0.
	v.Sidebar.SelectedEntries = v.Results
	applySidebarBlocks(v.Site, c, tmpl, v.Sidebar)
	applyPageBlock(c, tmpl, v.Pagination)

	// Blocks that don't apply to a search results page — strip them
	// just like the list view does.
	for _, blk := range []string{"pinned_entry", "sequel", "comment_area", "trackback_area", "profile_area", "recent_trackback", "dedicated_page", "category_area"} {
		if tmpl.HasBlock(blk) {
			c.Block(blk, 0)
		}
	}

	return c.Render(), nil
}

// applyEntry mirrors ListView.applyEntry, simplified for the search
// surface (no pinned_entry sub-block, no comment-form tags since
// search results aren't a comment surface).
func (v SearchView) applyEntry(c *sbtemplate.Context, i int, e domain.Entry) {
	c.Num(i)
	c.Tag("entry_id", strconv.FormatInt(e.ID, 10))
	c.Tag("entry_permalink", v.Site.EntryPermalink(e))
	c.Tag("entry_title", e.Title)
	c.Tag("entry_date", v.Site.FormatEntryDate(e.PostedAt))
	permalink := html.EscapeString(v.Site.EntryPermalink(e))
	timeStr := v.Site.FormatEntryTime(e.PostedAt)
	c.TagHTML("entry_time", `<a href="`+permalink+`">`+html.EscapeString(timeStr)+`</a>`)
	c.Tag("entry_disp_time", timeStr)
	c.TagHTML("entry_description", formatBodyForSearch(e.Body, e.Format, "search.body"))
	c.Tag("entry_sequel", "")
	c.Tag("entry_mode", "list")
	c.Tag("entry_likes_count", strconv.FormatInt(e.LikesCount, 10))
	c.Tag("entry_like_url", v.Site.EntryPermalink(e)+"like")
	c.Tag("entry_stamps_count", strconv.FormatInt(e.StampsCount, 10))
	c.Tag("entry_stamp_url", v.Site.EntryPermalink(e)+"stamp")
	c.Tag("entry_keywords", e.Keywords)
	c.Tag("entry_keyword", e.Keywords)
	c.Tag("entry_excerpt", entryExcerpt(e))
	c.Tag("permalink", v.Site.EntryPermalink(e))
	c.TagHTML("entry_tags", renderTagsFragment(v.Site, v.Tags[e.ID]))
	if e.Pinned {
		c.Tag("entry_pinned", "pinned")
	} else {
		c.Tag("entry_pinned", "")
	}
	label := commentNumLabel(v.Site.CommentNumLabel, e.CommentsCount)
	href := html.EscapeString(v.Site.EntryPermalink(e) + "#comments")
	c.TagHTML("comment_num", `<a href="`+href+`">`+html.EscapeString(label)+`</a>`)
	c.Tag("comment_count", strconv.FormatInt(e.CommentsCount, 10))
	c.TagHTML("sb_entry_marking", `<a id="mark`+strconv.FormatInt(e.ID, 10)+`"></a>`)
	v.applyEntryCategoryTags(c, e)
	v.applyEntryAuthorTags(c, e)
}

func (v SearchView) applyEntryCategoryTags(c *sbtemplate.Context, e domain.Entry) {
	cat, ok := v.Categories[e.CategoryID]
	if !ok {
		c.Tag("category_name", "-")
		c.Tag("category_id", "")
		c.Tag("category_slug", "")
		c.Tag("category_disp_name", "-")
		return
	}
	catLink := html.EscapeString(v.Site.CategoryPermalink(cat))
	catName := html.EscapeString(cat.Name)
	c.TagHTML("category_name", `<a href="`+catLink+`">`+catName+`</a>`)
	c.Tag("category_id", strconv.FormatInt(cat.ID, 10))
	c.Tag("category_slug", cat.Slug)
	c.Tag("category_disp_name", cat.Name)
}

func (v SearchView) applyEntryAuthorTags(c *sbtemplate.Context, e domain.Entry) {
	u, ok := v.Users[e.AuthorID]
	if !ok {
		c.Tag("user_name", "")
		c.Tag("user_disp_name", "")
		c.Tag("user_login", "")
		c.Tag("user_id", "")
		return
	}
	c.Tag("user_name", u.Name)
	c.Tag("user_disp_name", displayName(u))
	c.Tag("user_login", u.Name)
	c.Tag("user_id", strconv.FormatInt(u.ID, 10))
}

// formatBodyForSearch renders the entry body with the configured
// formatter; failures fall back to the raw text so a misformatted
// imported entry doesn't break the search page.
func formatBodyForSearch(body, kind, tag string) string {
	out, err := format.Render(body, kind)
	if err != nil {
		log.Printf("content.formatBodyForSearch: %s: %v", tag, err)
		return body
	}
	return out
}

// SearchQueryMaxLen caps how long a user-supplied ?q= can be. Mirrors
// the documented limit in the design and keeps a single source of truth.
const SearchQueryMaxLen = 200

// TruncateSearchQuery enforces SearchQueryMaxLen on a normalized query
// string, cutting at rune boundaries so multi-byte input (Japanese)
// never lands in the middle of a code point. Use this from the public
// handler before passing the string to either repo or view.
func TruncateSearchQuery(s string) string {
	if len(s) <= SearchQueryMaxLen {
		return s
	}
	// SearchQueryMaxLen is in characters, not bytes — count runes.
	if n := runeLen(s); n <= SearchQueryMaxLen {
		return s
	}
	runes := []rune(s)
	if len(runes) <= SearchQueryMaxLen {
		return s
	}
	return strings.TrimSpace(string(runes[:SearchQueryMaxLen]))
}

func runeLen(s string) int { return len([]rune(s)) }
