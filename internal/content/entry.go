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

	tmpl, err := sbtemplate.Parse(v.Template.MainBody, sbtemplate.DefaultCallback)
	if err != nil {
		return "", fmt.Errorf("content.EntryView: parse: %w", err)
	}
	c := tmpl.New()

	v.Site.
		WithTemplate(v.Template).
		WithMode("ent", strconv.FormatInt(v.Entry.ID, 10)).
		WithPageSuffix(v.Entry.Title).
		Apply(c)
	c.Block("title", 1)
	// `option` block is SB3's "show this part only on entry pages"
	// gate — populated with count=1 on entry permalink pages so
	// entry-only markup (comment forms, stamp buttons in the layout
	// area) renders, and stripped on list views.
	if tmpl.HasBlock("option") {
		c.Block("option", 1)
	}

	c.Num(0)
	c.Tag("entry_id", strconv.FormatInt(v.Entry.ID, 10))
	c.Tag("entry_permalink", v.Site.EntryPermalink(v.Entry))
	c.Tag("entry_title", v.Entry.Title)
	c.Tag("entry_date", v.Site.FormatEntryDate(v.Entry.PostedAt))
	c.Tag("entry_time", v.Site.FormatEntryTime(v.Entry.PostedAt))
	c.Tag("entry_description", formatBody(v.Entry.Body, v.Entry.Format, "entry.body"))
	c.Tag("entry_sequel", formatBody(v.Entry.More, v.Entry.Format, "entry.more"))
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
	c.Tag("entry_og_image", v.Site.OGImageURL(v.Entry.ID))
	// Dimensions exposed so themes can emit the full
	// <meta property="og:image:width"> / <...:height> pair that
	// Facebook / Slack / Discord use to lay out the card instantly
	// instead of waiting for a HEAD fetch. Values match og.CardWidth
	// / CardHeight — kept as literal strings so the content package
	// doesn't take a dependency on the og renderer.
	c.Tag("entry_og_image_width", "1200")
	c.Tag("entry_og_image_height", "630")
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
	c.Tag("entry_tags", renderTagsFragment(v.Site, v.Tags))
	c.Tag("csrf_token", v.CSRFToken)

	if v.Category != nil {
		c.Tag("category_name", v.Category.Name)
		c.Tag("category_id", strconv.FormatInt(v.Category.ID, 10))
		c.Tag("category_disp_name", v.Category.Name)
	}
	if v.Author != nil {
		// SB3 semantics (since the 2026-04 compat pass):
		//   {user_name}      = login name (SB3's authlink text)
		//   {user_disp_name} = display name (SB3's authname)
		//   {user_login}     = Go-port alias for the login name
		//   {user_id}        = numeric id
		// Display name is self-edited by the author; escape on
		// emission so arbitrary role tiers can't inject HTML.
		disp := displayName(*v.Author)
		c.Tag("user_name", html.EscapeString(v.Author.Name))
		c.Tag("user_disp_name", html.EscapeString(disp))
		c.Tag("user_login", html.EscapeString(v.Author.Name))
		c.Tag("user_id", strconv.FormatInt(v.Author.ID, 10))
	}
	c.Block("entry", 1)

	// Permalink-only block: nav links between adjacent entries. SB v3's
	// sample uses `sequel` to wrap this kind of content so it doesn't show
	// on list pages.
	if tmpl.HasBlock("sequel") {
		c.Num(0)
		// SB3 _sequel emits per-direction triples so templates can
		// roll their own nav markup. prev_entry / next_entry keep
		// the ready-made anchor fragment for the existing shipped
		// default template.
		if v.Prev != nil {
			c.Tag("prev_permalink", v.Site.EntryPermalink(*v.Prev))
			c.Tag("prev_title", v.Prev.Title)
		} else {
			c.Tag("prev_permalink", "")
			c.Tag("prev_title", "")
		}
		if v.Next != nil {
			c.Tag("next_permalink", v.Site.EntryPermalink(*v.Next))
			c.Tag("next_title", v.Next.Title)
		} else {
			c.Tag("next_permalink", "")
			c.Tag("next_title", "")
		}
		c.Tag("prev_entry", v.navLink(v.Prev, "« "))
		c.Tag("next_entry", v.navLink(v.Next, " »"))
		c.Block("sequel", 1)
	}

	// ---- comments ---------------------------------------------------
	v.applyComments(c, tmpl)

	applyProfileBlock(v.Site, c, tmpl, v.ProfileUsers)
	applySidebarBlocks(v.Site, c, tmpl, v.Sidebar)

	// Blocks deliberately left empty on permalink. profile_area fires
	// only on /profile/{id}/; trackback_* are permanently unsupported.
	for _, blk := range []string{"trackback_area", "profile_area", "page", "recent_trackback"} {
		if tmpl.HasBlock(blk) {
			c.Block(blk, 0)
		}
	}

	return c.Render(), nil
}

// applyComments populates the `comment_area` and `comment` blocks with
// approved comment data and the per-page form fields (post URL, entry id,
// honeypot timestamp, optional error message). The `comment_area` block is
// hidden entirely when the weblog's CommentMode is "closed".
func (v EntryView) applyComments(c *sbtemplate.Context, tmpl *sbtemplate.Template) {
	if !tmpl.HasBlock("comment_area") {
		return
	}
	if v.CommentMode == domain.CommentClosed {
		c.Block("comment_area", 0)
		return
	}

	c.Num(0)
	c.Tag("comment_post_url", v.Site.EntryPermalink(v.Entry)+"comment")
	c.Tag("form_ts", strconv.FormatInt(v.FormTS, 10))
	// `err` is reflected from a URL query param — escape so an
	// attacker can't craft `?err=<script>…</script>` into a victim
	// link and execute in their browser.
	c.Tag("comment_error", html.EscapeString(v.FormError))
	// Prefill cookies are user-controllable but land inside
	// input `value="..."` attributes. Escape defensively so a
	// self-planted cookie with a quote can't break out of the
	// attribute.
	c.Tag("cookie_name", html.EscapeString(v.CookieName))
	c.Tag("cookie_email", html.EscapeString(v.CookieEmail))
	c.Tag("cookie_url", html.EscapeString(v.CookieURL))
	c.Tag("turnstile_widget", v.TurnstileHTML)

	for i, m := range v.Messages {
		c.Num(i)
		// Every comment field is untrusted — html-escape to neutralise
		// stored XSS. comment_url additionally passes through a scheme
		// allow-list so `javascript:` / `data:` / etc. can't leak past
		// the submit-time check (belt + braces).
		c.Tag("comment_name", html.EscapeString(m.AuthorName))
		c.Tag("comment_time", v.Site.FormatCommentTime(m.PostedAt))
		c.Tag("comment_description", formatCommentBody(m.Body))
		c.Tag("comment_url", html.EscapeString(safeExternalURL(m.AuthorURL)))
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
