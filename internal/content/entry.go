package content

import (
	"fmt"
	"html"
	"strconv"

	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/template/sbtemplate"
)

// EntryView renders a single-entry permalink page. The SB template's `entry`
// block still drives the main body layout; the `sequel` block (when present)
// activates prev/next navigation markup. `comment_area` + `comment` blocks
// display existing comments and the submission form.
type EntryView struct {
	Site        Site
	Template    *domain.Template
	Entry       domain.Entry
	Category    *domain.Category // optional
	Author      *domain.User     // optional
	Prev        *domain.Entry    // nil at the edge
	Next        *domain.Entry    // nil at the edge
	Messages    []domain.Message // approved comments, oldest first
	CommentMode domain.CommentMode
	// StampCounts is the per-kind reaction tally for this entry. Caller
	// pre-fetches it so the renderer never hits the DB; nil is fine and
	// collapses to zero for every kind.
	StampCounts map[domain.StampKind]int64
	// Tags is the ordered tag list attached to this entry. Caller
	// pre-fetches it (TagsByEntry) so the renderer never hits the DB.
	// Empty / nil both render as an empty `{entry_tags}` fragment.
	Tags []domain.Tag
	// ProfileUsers is the list of users feeding the template's
	// {profile_area} block — only entries with list_visible=true.
	// Caller pre-fetches via repo.VisibleProfileUsers so the
	// renderer stays DB-free. nil / empty collapses the block.
	ProfileUsers []domain.User
	// Sidebar carries the pre-fetched inputs for the SB3 sidebar
	// "parts" blocks (archives / category / recent_comment /
	// latest_entry). Zero value is fine — each block gates on
	// "HasBlock" and collapses to 0 when its slice is empty.
	Sidebar   SidebarData
	FormError string // shown above the form when a POST failed validation
	FormTS    int64  // unix ts embedded in the form for the spam time check
	// Cookie prefill values lifted from the request. Already HTML-escaped
	// so they can be dropped straight into `value="..."`.
	CookieName  string
	CookieEmail string
	CookieURL   string
	// TurnstileHTML is the <div>+<script> snippet that renders the CF
	// challenge widget, or empty when Turnstile isn't configured.
	TurnstileHTML string
	// CSRFToken is the value embedded in the comment / like form's hidden
	// csrf_token input. Required by the global CSRF middleware for every
	// non-GET request.
	CSRFToken string
}

func (v EntryView) Render() (string, error) {
	if v.Template == nil || v.Template.MainBody == "" {
		return "", fmt.Errorf("content.EntryView: no template main body")
	}

	tmpl, err := cachedParse(v.Template, "main", v.Template.MainBody)
	if err != nil {
		return "", fmt.Errorf("content.EntryView: parse: %w", err)
	}
	c := tmpl.New()

	v.applyHeader(c, tmpl)
	v.applyEntryBody(c)
	v.applyCategoryTags(c)
	v.applyAuthorTags(c)
	v.applyCommentNum(c)
	c.Block("entry", 1)

	// sb_entry_marking is a scroll anchor emitted by SB3 on list pages
	// so permalinks can jump straight to the entry. On the permalink
	// page the anchor is pointless so we emit a no-op anchor (the
	// DefaultCallback already injected the placeholder). The list
	// renderer (ListView) emits the real anchor per iteration.
	c.Tag("sb_entry_marking", "")

	v.applySequelNav(c, tmpl)
	v.applyComments(c, tmpl)

	applyProfileBlock(v.Site, c, tmpl, v.ProfileUsers)
	applySidebarBlocks(v.Site, c, tmpl, v.Sidebar)
	stripUnusedEntryBlocks(c, tmpl)

	return c.Render(), nil
}

// applyHeader sets up the site-level tags + the permalink-page block
// gates (`title` shown, `toppage` stripped, `option` enabled).
func (v EntryView) applyHeader(c *sbtemplate.Context, tmpl *sbtemplate.Template) {
	v.Site.
		WithTemplate(v.Template).
		WithMode("ent", strconv.FormatInt(v.Entry.ID, 10)).
		WithPageSuffix(v.Entry.Title).
		Apply(c)
	c.Block("title", 1)
	// toppage block is stripped on entry pages (top-page-only gate).
	c.Block("toppage", 0)
	// `option` block is SB3's "show this part only on entry pages"
	// gate — populated with count=1 on entry permalink pages so
	// entry-only markup (comment forms, stamp buttons in the layout
	// area) renders, and stripped on list views.
	if tmpl.HasBlock("option") {
		c.Block("option", 1)
	}
}

// applyEntryBody populates the per-entry tags that live inside the
// `entry` block — title / permalink / time / body / stamps / OG /
// keywords / tags / pinned.
func (v EntryView) applyEntryBody(c *sbtemplate.Context) {
	c.Num(0)
	c.Tag("entry_id", strconv.FormatInt(v.Entry.ID, 10))
	c.Tag("entry_permalink", v.Site.EntryPermalink(v.Entry))
	c.Tag("entry_title", v.Entry.Title)
	c.Tag("entry_date", v.Site.FormatEntryDate(v.Entry.PostedAt))
	// SB3 wraps {entry_time} in a permalink anchor; {entry_disp_time}
	// remains the bare formatted time.
	permalink := html.EscapeString(v.Site.EntryPermalink(v.Entry))
	timeStr := v.Site.FormatEntryTime(v.Entry.PostedAt)
	c.TagHTML("entry_time", `<a href="`+permalink+`">`+html.EscapeString(timeStr)+`</a>`)
	c.Tag("entry_disp_time", timeStr)
	body := formatBody(v.Entry.Body, v.Entry.Format, "entry.body")
	if v.Entry.More != "" {
		body += `<a id="sequel"></a>`
	}
	c.TagHTML("entry_description", body)
	c.TagHTML("entry_sequel", formatBody(v.Entry.More, v.Entry.Format, "entry.more"))
	c.Tag("entry_mode", "entry")
	c.Tag("entry_likes_count", strconv.FormatInt(v.Entry.LikesCount, 10))
	c.Tag("entry_like_url", v.Site.EntryPermalink(v.Entry)+"like")
	c.Tag("entry_stamps_count", strconv.FormatInt(v.Entry.StampsCount, 10))
	c.Tag("entry_stamp_url", v.Site.EntryPermalink(v.Entry)+"stamp")
	// Per-kind stamp counts (e.g. {entry_stamps_heart}). View receives
	// the map pre-populated so templates can render zero counts too.
	for k, n := range v.StampCounts {
		c.Tag("entry_stamps_"+string(k), strconv.FormatInt(n, 10))
	}
	v.applyOG(c)
	// SEO keywords land in the template as a comma-separated string so
	// authors can drop `<meta name="keywords" content="{entry_keywords}">`
	// into the <head>. Empty when the author left the field blank.
	// entry_keyword (singular) is the SB3 spelling; keep both so
	// existing templates and the Go-native naming both resolve.
	c.Tag("entry_keywords", v.Entry.Keywords)
	c.Tag("entry_keyword", v.Entry.Keywords)
	// permalink is SB3's short alias for entry_permalink — both spellings
	// appear in the shipped templates.
	c.Tag("permalink", v.Site.EntryPermalink(v.Entry))
	c.TagHTML("entry_tags", renderTagsFragment(v.Site, v.Tags))
	c.Tag("csrf_token", v.CSRFToken)
	v.applyPinned(c)
}

// applyOG emits the OG image URL + literal 1200×630 dimensions so
// themes can render the full <meta property="og:image:*"> set without
// the content package depending on the og renderer for the constants.
func (v EntryView) applyOG(c *sbtemplate.Context) {
	c.Tag("entry_og_image", v.Site.OGImageURL(v.Entry.ID))
	c.Tag("entry_og_image_width", "1200")
	c.Tag("entry_og_image_height", "630")
}

// applyPinned exposes the pin state both as {entry_pinned} tag and as
// the pinned_entry sub-block, so templates can style permalink pages
// consistently with list-page CSS classes.
func (v EntryView) applyPinned(c *sbtemplate.Context) {
	if v.Entry.Pinned {
		c.Tag("entry_pinned", "pinned")
		c.Block("pinned_entry", 1)
		return
	}
	c.Tag("entry_pinned", "")
	c.Block("pinned_entry", 0)
}

func (v EntryView) applyCategoryTags(c *sbtemplate.Context) {
	if v.Category == nil {
		return
	}
	catLink := html.EscapeString(v.Site.CategoryPermalink(*v.Category))
	catName := html.EscapeString(v.Category.Name)
	c.TagHTML("category_name", `<a href="`+catLink+`">`+catName+`</a>`)
	c.Tag("category_id", strconv.FormatInt(v.Category.ID, 10))
	c.Tag("category_slug", v.Category.Slug)
	c.Tag("category_disp_name", v.Category.Name)
}

// applyAuthorTags fills the SB3 author surface (since the 2026-04
// compat pass):
//
//	{user_name}      = login name (SB3's authlink text)
//	{user_disp_name} = display name (SB3's authname)
//	{user_login}     = Go-port alias for the login name
//	{user_id}        = numeric id
//
// Tag auto-escapes both fields on emission so a self-edited display
// name can't inject HTML into the rendered page.
func (v EntryView) applyAuthorTags(c *sbtemplate.Context) {
	if v.Author == nil {
		return
	}
	c.Tag("user_name", v.Author.Name)
	c.Tag("user_disp_name", displayName(*v.Author))
	c.Tag("user_login", v.Author.Name)
	c.Tag("user_id", strconv.FormatInt(v.Author.ID, 10))
}

// applyCommentNum mirrors SB3's {comment_num}: a link like
// <a href="…#comments">Comments(N)</a> when comments are open, and a
// plain "-" when closed. {comment_count} is the raw number (empty when
// closed). AcceptComments=false collapses to the closed branch even
// when the weblog as a whole still accepts comments.
func (v EntryView) applyCommentNum(c *sbtemplate.Context) {
	if !v.commentsActive() {
		c.Tag("comment_num", "-")
		c.Tag("comment_count", "")
		return
	}
	label := commentNumLabel(v.Site.CommentNumLabel, v.Entry.CommentsCount)
	href := html.EscapeString(v.Site.EntryPermalink(v.Entry) + "#comments")
	c.TagHTML("comment_num", `<a href="`+href+`">`+html.EscapeString(label)+`</a>`)
	c.Tag("comment_count", strconv.FormatInt(v.Entry.CommentsCount, 10))
}

// applySequelNav fills the permalink-only `sequel` block with prev/next
// nav links. SB v3 wraps adjacent-entry markup in this block so list
// pages don't render it. The block is left untouched when the template
// has no `sequel` definition.
func (v EntryView) applySequelNav(c *sbtemplate.Context, tmpl *sbtemplate.Template) {
	if !tmpl.HasBlock("sequel") {
		return
	}
	c.Num(0)
	// SB3 _sequel emits per-direction triples so templates can roll
	// their own nav markup. prev_entry / next_entry keep the
	// ready-made anchor fragment for the existing shipped default
	// template.
	applyAdjacentEntryTags(c, "prev", v.Site, v.Prev)
	applyAdjacentEntryTags(c, "next", v.Site, v.Next)
	c.TagHTML("prev_entry", v.navLink(v.Prev, "« "))
	c.TagHTML("next_entry", v.navLink(v.Next, " »"))
	c.Block("sequel", 1)
}

func applyAdjacentEntryTags(c *sbtemplate.Context, side string, site Site, target *domain.Entry) {
	if target == nil {
		c.Tag(side+"_permalink", "")
		c.Tag(side+"_title", "")
		return
	}
	c.Tag(side+"_permalink", site.EntryPermalink(*target))
	c.Tag(side+"_title", target.Title)
}

// stripUnusedEntryBlocks zeros out the blocks that never fire on a
// permalink: profile_area is /profile/{id}/-only and trackback_* are
// permanently unsupported. Leaves them untouched when missing so the
// template parser doesn't error on a never-defined block.
func stripUnusedEntryBlocks(c *sbtemplate.Context, tmpl *sbtemplate.Template) {
	for _, blk := range []string{"trackback_area", "profile_area", "page", "recent_trackback", "dedicated_page"} {
		if tmpl.HasBlock(blk) {
			c.Block(blk, 0)
		}
	}
}

// commentsActive reports whether comments should appear for this entry.
// True only when the weblog's CommentMode is not closed AND the author
// has not opted this individual entry out via AcceptComments=false.
func (v EntryView) commentsActive() bool {
	return v.CommentMode != domain.CommentClosed && v.Entry.AcceptComments
}

// applyComments populates the `comment_area` and `comment` blocks with
// approved comment data and the per-page form fields (post URL, entry id,
// honeypot timestamp, optional error message). The `comment_area` block is
// hidden entirely when the weblog's CommentMode is "closed" or the entry
// has comments turned off via AcceptComments=false.
func (v EntryView) applyComments(c *sbtemplate.Context, tmpl *sbtemplate.Template) {
	if !tmpl.HasBlock("comment_area") {
		return
	}
	if !v.commentsActive() {
		c.Block("comment_area", 0)
		return
	}

	c.Num(0)
	c.Tag("comment_post_url", v.Site.EntryPermalink(v.Entry)+"comment")
	c.Tag("form_ts", strconv.FormatInt(v.FormTS, 10))
	// `err` is reflected from a URL query param; Tag auto-escapes so
	// an attacker can't craft `?err=<script>…</script>` into a victim
	// link and execute in their browser.
	c.Tag("comment_error", v.FormError)
	// Prefill cookies are user-controllable but land inside
	// input `value="..."` attributes. Tag auto-escapes so a
	// self-planted cookie with a quote can't break out of the
	// attribute.
	c.Tag("cookie_name", v.CookieName)
	c.Tag("cookie_email", v.CookieEmail)
	c.Tag("cookie_url", v.CookieURL)
	c.TagHTML("turnstile_widget", v.TurnstileHTML)
	// SB3 injected a <script> tag for cook.js here. The Go port has no
	// reader-facing comment JS, so {sb_comment_js} is always empty.
	// Setting it explicitly keeps the lint honest and mirrors
	// DefaultCallback's placeholder injection.
	c.Tag("sb_comment_js", "")

	for i, m := range v.Messages {
		c.Num(i)
		// Every comment field is untrusted — Tag auto-escapes to
		// neutralise stored XSS. comment_url additionally passes
		// through a scheme allow-list so `javascript:` / `data:` /
		// etc. can't leak past the submit-time check (belt + braces).
		c.Tag("comment_name", m.AuthorName)
		c.Tag("comment_time", v.Site.FormatCommentTime(m.PostedAt))
		c.TagHTML("comment_description", formatCommentBody(m.Body))
		c.Tag("comment_url", safeExternalURL(m.AuthorURL))
		c.Tag("comment_icon", "") // reserved for a future avatar/icon feature
	}
	if tmpl.HasBlock("comment") {
		c.Block("comment", len(v.Messages))
	}
	c.Block("comment_area", 1)
}

// navLink renders a short prev/next anchor or an empty string at the edge.
// affix is prepended or appended to the title depending on direction.
func (v EntryView) navLink(target *domain.Entry, affix string) string {
	if target == nil {
		return ""
	}
	href := html.EscapeString(v.Site.EntryPermalink(*target))
	title := html.EscapeString(target.Title)
	// caller passes "« " (prepend) or " »" (append); distinguish by whether
	// affix starts with an ASCII space.
	if len(affix) > 0 && affix[0] == ' ' {
		return `<a href="` + href + `">` + title + affix + `</a>`
	}
	return `<a href="` + href + `">` + affix + title + `</a>`
}

// commentNumLabel formats the reader-facing label inside the {comment_num}
// anchor. numLabel is the localised word for "comments" (e.g. "Comments" or
// "コメント"), already resolved by the caller from the public i18n bundle
// keyed as "comment.numLabel". Empty numLabel falls back to "Comments".
func commentNumLabel(numLabel string, count int64) string {
	if numLabel == "" {
		numLabel = "Comments"
	}
	return numLabel + "(" + strconv.FormatInt(count, 10) + ")"
}
