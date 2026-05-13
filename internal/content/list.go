package content

import (
	"fmt"
	"html"
	"log"
	"strconv"

	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/format"
	"github.com/serendipitynz/serenebach/internal/template/sbtemplate"
)

// ListView renders a list page (home, category, archive — they all share the
// entry-loop shape). Feed it pre-fetched entries plus the lookup maps for
// categories and users so no SQL happens inside the render call.
type ListView struct {
	Site       Site
	Template   *domain.Template
	Entries    []domain.Entry
	Categories map[int64]domain.Category
	Users      map[int64]domain.User
	// Tags is the per-entry tag slice, keyed by entry id. Optional —
	// nil / missing ids render as an empty {entry_tags} fragment.
	Tags map[int64][]domain.Tag
	// Category, when set, is the row the list page is scoped to —
	// /category/<id>/ pages fill this in so {category_description} and
	// {category_name} expose the category heading text. nil on home /
	// tag / archive pages (those use PageTitle instead).
	Category *domain.Category
	// ProfileUsers feeds the {profile_area} block — filtered to
	// list_visible=true users. See EntryView.ProfileUsers.
	ProfileUsers []domain.User
	// Sidebar carries the pre-fetched inputs for the SB3 sidebar
	// "parts" blocks. See EntryView.Sidebar.
	Sidebar SidebarData
	// Pagination populates the `page` block + {page_num} / {page_now}
	// / {prev_page_*} / {next_page_*} tags. Zero value collapses the
	// block (home / category / tag / archive pages all populate it;
	// feed-only contexts leave it empty).
	Pagination Pagination

	// PageTitle, when non-empty, overrides {site_title} for this render.
	// Category and archive handlers use it to add section context like
	// "Category: News - Serene Bach".
	PageTitle string
	// Mode is the SB3 mode code for the rendering route — "page"
	// (home / default list), "cat", "arc", "tag". Drives
	// {mode_name}/{mode_id}. Empty collapses to "page".
	Mode string
	// ModeContext is the per-mode discriminator (category id, archive
	// YYYYMM, tag slug…) exposed as {mode_id}.
	ModeContext string
	// CSRFToken is embedded into the per-entry like form's hidden input so
	// the global CSRF middleware accepts POSTs from the list pages too.
	CSRFToken string
}

// Render produces the final HTML string. Falls back to sane defaults when a
// lookup map is missing an entry's category or author rather than failing —
// public pages should tolerate stale references.
func (v ListView) Render() (string, error) {
	if v.Template == nil || v.Template.MainBody == "" {
		return "", fmt.Errorf("content.ListView: no template main body")
	}

	tmpl, err := cachedParse(v.Template, "main", v.Template.MainBody)
	if err != nil {
		return "", fmt.Errorf("content.ListView: parse: %w", err)
	}
	c := tmpl.New()

	v.applyHeader(c, tmpl)
	v.applyCategoryHeader(c, tmpl)
	v.applyToppage(c)

	for i, e := range v.Entries {
		v.applyEntry(c, i, e)
	}
	c.Block("entry", len(v.Entries))

	applyProfileBlock(v.Site, c, tmpl, v.ProfileUsers)
	applySidebarBlocks(v.Site, c, tmpl, v.Sidebar)
	applyPageBlock(c, tmpl, v.Pagination)
	stripUnusedListBlocks(c, tmpl)

	return c.Render(), nil
}

// applyHeader sets up the site-level tags and strips the entry-only
// `option` block so list pages share the same chrome regardless of
// mode.
func (v ListView) applyHeader(c *sbtemplate.Context, tmpl *sbtemplate.Template) {
	v.Site.
		WithTemplate(v.Template).
		WithMode(v.Mode, v.ModeContext).
		WithPageSuffix(v.PageTitle).
		Apply(c)
	if v.PageTitle != "" {
		c.Tag("site_title", v.PageTitle)
	}
	c.Tag("csrf_token", v.CSRFToken)
	// `option` block is entry-only; strip on every list view.
	if tmpl.HasBlock("option") {
		c.Block("option", 0)
	}
	// Show the header "title" block once for list pages.
	c.Block("title", 1)
}

// applyCategoryHeader exposes category metadata both as page-level tags
// (legacy Go-port templates) and via the SB3 `category_area` block.
// Home / tag / archive pages clear the tags and strip the block.
func (v ListView) applyCategoryHeader(c *sbtemplate.Context, tmpl *sbtemplate.Template) {
	if v.Category != nil {
		catLink := html.EscapeString(v.Site.CategoryPermalink(*v.Category))
		catName := html.EscapeString(v.Category.Name)
		c.TagHTML("category_name", `<a href="`+catLink+`">`+catName+`</a>`)
		c.TagHTML("category_description", renderDescription(v.Category.Description, v.Category.DescriptionFormat))
	}
	applyCategoryAreaBlock(c, tmpl, v.Category, v.Categories)
}

// applyToppage emits the SB3 `toppage` gate: 1 on the home / default
// list, 0 on every other list kind (category / archive / tag).
func (v ListView) applyToppage(c *sbtemplate.Context) {
	if v.Mode == "" || v.Mode == "page" {
		c.Block("toppage", 1)
	} else {
		c.Block("toppage", 0)
	}
}

// applyEntry populates one iteration of the `entry` loop.
func (v ListView) applyEntry(c *sbtemplate.Context, i int, e domain.Entry) {
	c.Num(i)
	c.Tag("entry_id", strconv.FormatInt(e.ID, 10))
	c.Tag("entry_permalink", v.Site.EntryPermalink(e))
	c.Tag("entry_title", e.Title)
	c.Tag("entry_date", v.Site.FormatListDate(e.PostedAt))
	permalink := html.EscapeString(v.Site.EntryPermalink(e))
	timeStr := v.Site.FormatEntryTime(e.PostedAt)
	c.TagHTML("entry_time", `<a href="`+permalink+`">`+html.EscapeString(timeStr)+`</a>`)
	c.Tag("entry_disp_time", timeStr)
	c.TagHTML("entry_description", formatBody(e.Body, e.Format, "list.body"))
	v.applyEntrySequel(c, e, permalink)
	c.Tag("entry_mode", "list")
	c.Tag("entry_likes_count", strconv.FormatInt(e.LikesCount, 10))
	c.Tag("entry_like_url", v.Site.EntryPermalink(e)+"like")
	c.Tag("entry_stamps_count", strconv.FormatInt(e.StampsCount, 10))
	c.Tag("entry_stamp_url", v.Site.EntryPermalink(e)+"stamp")
	c.Tag("entry_keywords", e.Keywords)
	c.Tag("entry_keyword", e.Keywords)
	c.Tag("permalink", v.Site.EntryPermalink(e))
	c.TagHTML("entry_tags", renderTagsFragment(v.Site, v.Tags[e.ID]))
	// {entry_pinned} yields "pinned" or "" per iteration — usable as a
	// CSS class. pinned_entry sub-block is 0-striped on list pages
	// because sbtemplate block counts are global (not per-iteration);
	// use {entry_pinned} tag for per-entry conditional styling instead.
	if e.Pinned {
		c.Tag("entry_pinned", "pinned")
	} else {
		c.Tag("entry_pinned", "")
	}
	// {comment_num} / {comment_count}: list pages always show the
	// link (comments are "accepted" on list regardless of mode).
	// The count comes from the denormalised CommentsCount column.
	label := commentNumLabel(v.Site.CommentNumLabel, e.CommentsCount)
	href := html.EscapeString(v.Site.EntryPermalink(e) + "#comments")
	c.TagHTML("comment_num", `<a href="`+href+`">`+html.EscapeString(label)+`</a>`)
	c.Tag("comment_count", strconv.FormatInt(e.CommentsCount, 10))
	// SB3 emits a scroll anchor on list pages so permalinks can
	// jump straight to the entry. The DefaultCallback already
	// injected {sb_entry_marking} at the top of the entry block.
	c.TagHTML("sb_entry_marking", `<a id="mark`+strconv.FormatInt(e.ID, 10)+`"></a>`)

	v.applyEntryCategoryTags(c, e)
	v.applyEntryAuthorTags(c, e)
}

func (v ListView) applyEntrySequel(c *sbtemplate.Context, e domain.Entry, permalink string) {
	if e.More == "" {
		c.Tag("entry_sequel", "")
		return
	}
	label := v.Site.ReadMoreLabel
	if label == "" {
		label = "read more ..."
	}
	c.TagHTML("entry_sequel", `<a href="`+permalink+`#sequel">`+html.EscapeString(label)+`</a>`)
}

func (v ListView) applyEntryCategoryTags(c *sbtemplate.Context, e domain.Entry) {
	cat, ok := v.Categories[e.CategoryID]
	if !ok {
		c.Tag("category_name", "-")
		c.Tag("category_id", "")
		c.Tag("category_disp_name", "-")
		return
	}
	catLink := html.EscapeString(v.Site.CategoryPermalink(cat))
	catName := html.EscapeString(cat.Name)
	c.TagHTML("category_name", `<a href="`+catLink+`">`+catName+`</a>`)
	c.Tag("category_id", strconv.FormatInt(cat.ID, 10))
	c.Tag("category_disp_name", cat.Name)
}

func (v ListView) applyEntryAuthorTags(c *sbtemplate.Context, e domain.Entry) {
	u, ok := v.Users[e.AuthorID]
	if !ok {
		c.Tag("user_name", "")
		c.Tag("user_disp_name", "")
		c.Tag("user_login", "")
		c.Tag("user_id", "")
		return
	}
	// See entry.go for the SB3 semantics — user_name is the login
	// name, user_disp_name is the display name. Both are
	// self-edited; Tag auto-escapes on emission.
	c.Tag("user_name", u.Name)
	c.Tag("user_disp_name", displayName(u))
	c.Tag("user_login", u.Name)
	c.Tag("user_id", strconv.FormatInt(u.ID, 10))
}

// stripUnusedListBlocks zeros out the entry-mode / unsupported blocks
// so they render as empty instead of leaking `{-name}` placeholders.
// profile_area / sequel / comment_area belong to entry pages; trackback
// blocks wait on the (permanently out-of-scope) trackback feature.
func stripUnusedListBlocks(c *sbtemplate.Context, tmpl *sbtemplate.Template) {
	for _, blk := range []string{"pinned_entry", "sequel", "comment_area", "trackback_area", "profile_area", "recent_trackback", "dedicated_page"} {
		if tmpl.HasBlock(blk) {
			c.Block(blk, 0)
		}
	}
}

// formatDate / formatTime use a neutral modern format. SB3 supported its own
// %Year%/%Mon%/%Day% DSL via configure.cgi; porting that is a later concern.

// formatBody runs the entry body (or 追記 block) through the configured
// formatter. On error it logs and falls back to the raw input so a render
// failure only costs formatting, never the page itself. `tag` is a short
// label that makes the log line easy to find when debugging.
func formatBody(body, kind, tag string) string {
	out, err := format.Render(body, kind)
	if err != nil {
		log.Printf("content.formatBody: %s: %v", tag, err)
		return body
	}
	return out
}

func displayName(u domain.User) string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.Name
}
