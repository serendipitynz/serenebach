package content

import (
	"fmt"
	"html"
	"strconv"

	"github.com/serendipitynz/serenebach/internal/domain"
)

// PageView renders a single flat page (not an entry). It reuses the
// entry_body / main_body template resolution chain so existing templates
// work without modification.
type PageView struct {
	Site     Site
	Template *domain.Template
	Page     domain.Page
	// ProfileUsers / Sidebar are shared with EntryView so the same
	// sidebar blocks render uniformly across every public page.
	ProfileUsers []domain.User
	Sidebar      SidebarData
}

func (v PageView) Render() (string, error) {
	if v.Template == nil || v.Template.MainBody == "" {
		return "", fmt.Errorf("content.PageView: no template main body")
	}

	// Prefer entry_body; fall back to main_body when empty.
	bodyTmpl := v.Template.EntryBody
	bodyField := "entry"
	if bodyTmpl == "" {
		bodyTmpl = v.Template.MainBody
		bodyField = "main"
	}

	tmpl, err := cachedParse(v.Template, bodyField, bodyTmpl)
	if err != nil {
		return "", fmt.Errorf("content.PageView: parse: %w", err)
	}
	c := tmpl.New()

	v.Site.
		WithTemplate(v.Template).
		WithMode("page", "").
		WithPageSuffix(v.Page.Title).
		Apply(c)
	c.Block("title", 1)
	c.Block("toppage", 0)
	if tmpl.HasBlock("option") {
		c.Block("option", 0)
	}

	c.Num(0)
	c.Tag("entry_id", strconv.FormatInt(v.Page.ID, 10))
	c.Tag("entry_permalink", v.Site.TopURL()+v.Page.Slug[1:])
	c.Tag("entry_title", v.Page.Title)
	c.Tag("entry_date", v.Site.FormatEntryDate(v.Page.CreatedAt))
	permalink := html.EscapeString(v.Site.TopURL() + v.Page.Slug[1:])
	timeStr := v.Site.FormatEntryTime(v.Page.CreatedAt)
	c.TagHTML("entry_time", `<a href="`+permalink+`">`+html.EscapeString(timeStr)+`</a>`)
	c.Tag("entry_disp_time", timeStr)
	c.TagHTML("entry_description", formatBody(v.Page.Body, v.Page.Format, "page.body"))
	c.Tag("entry_sequel", "")
	c.Tag("entry_mode", "page")
	c.Tag("entry_likes_count", "0")
	c.Tag("entry_like_url", "")
	c.Tag("entry_stamps_count", "0")
	c.Tag("entry_stamp_url", "")
	c.Tag("entry_og_image", v.Site.PageOGImageURL(v.Page.ID))
	c.Tag("entry_og_image_width", "1200")
	c.Tag("entry_og_image_height", "630")
	c.Tag("entry_keywords", "")
	c.Tag("entry_keyword", "")
	v.applySEO(c)
	c.Tag("permalink", v.Site.TopURL()+v.Page.Slug[1:])
	c.Tag("entry_tags", "")
	c.Tag("csrf_token", "")
	c.Tag("entry_category", "")
	c.Tag("category_name", "")
	c.Tag("category_id", "")
	c.Tag("category_disp_name", "")
	c.Tag("user_name", "")
	c.Tag("user_disp_name", "")
	c.Tag("user_login", "")
	c.Tag("user_id", "")
	c.Tag("comment_num", "-")
	c.Tag("comment_count", "")
	c.Block("entry", 1)
	c.Tag("sb_entry_marking", "")

	// Blocks that only make sense on real entry pages — strip them.
	if tmpl.HasBlock("sequel") {
		c.Block("sequel", 0)
	}
	if tmpl.HasBlock("comment_area") {
		c.Block("comment_area", 0)
	}
	if tmpl.HasBlock("trackback_area") {
		c.Block("trackback_area", 0)
	}
	if tmpl.HasBlock("recent_trackback") {
		c.Block("recent_trackback", 0)
	}

	// New block: dedicated_page — 1 only on flat pages.
	if tmpl.HasBlock("dedicated_page") {
		c.Block("dedicated_page", 1)
	}

	applyProfileBlock(v.Site, c, tmpl, v.ProfileUsers)
	applySidebarBlocks(v.Site, c, tmpl, v.Sidebar)

	// profile_area is entry-only / profile-only; strip here.
	if tmpl.HasBlock("profile_area") {
		c.Block("profile_area", 0)
	}

	return c.Render(), nil
}
