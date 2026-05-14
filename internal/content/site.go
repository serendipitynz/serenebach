// Package content composes domain data into sbtemplate-ready output, the way
// the original sb::Content::* modules did. The public-facing rendering path
// lives here; admin UI is elsewhere.
package content

import (
	"html"
	"strconv"
	"strings"
	"time"

	"github.com/serendipitynz/serenebach/internal/dateformat"
	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/format"
	"github.com/serendipitynz/serenebach/internal/template/sbtemplate"
	"github.com/serendipitynz/serenebach/internal/version"
)

// applyProfileBlock populates SB3's `profile` block — the sidebar /
// parts block that carries the *user directory* link list. Inside the
// block the single tag `{user_list}` is a pre-rendered <ul><li>
// fragment of every list_visible user, matching
// sb::Content::List::_profile:
//
//	<ul>
//	  <li><a href="/profile/<id>/">Display Name</a></li>
//	  ...
//	</ul>
//
// Block count is 1 when at least one user is visible, 0 otherwise.
// This is the *sidebar* profile block — the distinct per-user detail
// block `profile_area` is populated by applyProfileAreaBlock and only
// fires on the /profile/{id}/ route.
func applyProfileBlock(s Site, c *sbtemplate.Context, tmpl *sbtemplate.Template, users []domain.User) {
	if !tmpl.HasBlock("profile") {
		return
	}
	if len(users) == 0 {
		c.Block("profile", 0)
		return
	}
	var b strings.Builder
	b.WriteString("<ul>")
	for _, u := range users {
		name := u.DisplayName
		if name == "" {
			name = u.Name
		}
		b.WriteString(`<li><a href="`)
		b.WriteString(html.EscapeString(s.TopURL()))
		b.WriteString(`profile/`)
		b.WriteString(strconv.FormatInt(u.ID, 10))
		b.WriteString(`/">`)
		b.WriteString(html.EscapeString(name))
		b.WriteString(`</a></li>`)
	}
	b.WriteString("</ul>")
	c.Num(0)
	c.TagHTML("user_list", b.String())
	c.Block("profile", 1)
}

// applyProfileAreaBlock populates SB3's `profile_area` block (the
// per-user detail block that fires on the profile permalink page
// `/profile/{id}/`). SB3 emits `{profile_name}` (display name) and
// `{profile_description}` inside; we add the `{user_*}` aliases that
// entry / list views already use so templates can reuse the same tag
// names on the profile page.
//
// `profile_email` is emitted as the empty string: the signed-in email
// is an admin-login credential and isn't intended for public display.
// Templates that want to surface a contact path should use a dedicated
// description field instead.
func applyProfileAreaBlock(c *sbtemplate.Context, tmpl *sbtemplate.Template, u domain.User) {
	if !tmpl.HasBlock("profile_area") {
		return
	}
	disp := displayName(u)
	c.Num(0)
	c.Tag("profile_id", strconv.FormatInt(u.ID, 10))
	c.Tag("profile_name", disp)
	c.Tag("profile_login", u.Name)
	c.TagHTML("profile_description", renderDescription(u.Description, u.DescriptionFormat))
	c.Tag("profile_email", "")
	c.Tag("user_id", strconv.FormatInt(u.ID, 10))
	c.Tag("user_name", u.Name)
	c.Tag("user_disp_name", disp)
	c.Tag("user_login", u.Name)
	c.Block("profile_area", 1)
}

// renderDescription runs a free-form description through format.Render
// with an html fallback. Shared by profile + category description
// rendering so both fields honour the same format catalogue.
func renderDescription(body, kind string) string {
	if body == "" {
		return ""
	}
	out, err := format.Render(body, kind)
	if err != nil {
		return body
	}
	return out
}

// applyCategoryAreaBlock mirrors SB3's `category_area` block
// (sb::Content::_parse_category + sb::Content::Category::_content):
// gated 1 when the list page is scoped to a specific category, 0
// otherwise. Emits {category_pagename} (own name), {category_fullname}
// (parent-chain "Parent > Child"), and {category_description}
// (format-rendered) inside the block.
//
// cat is the category the page is scoped to — nil on home / tag /
// archive pages. all is the id→Category lookup ListView already
// builds; we use it to walk up the parent chain.
func applyCategoryAreaBlock(c *sbtemplate.Context, tmpl *sbtemplate.Template, cat *domain.Category, all map[int64]domain.Category) {
	if !tmpl.HasBlock("category_area") {
		return
	}
	if cat == nil {
		c.Block("category_area", 0)
		return
	}
	c.Num(0)
	c.Tag("category_pagename", cat.Name)
	c.Tag("category_fullname", categoryFullname(*cat, all))
	c.Tag("category_slug", cat.Slug)
	c.TagHTML("category_description", renderDescription(cat.Description, cat.DescriptionFormat))
	c.Block("category_area", 1)
}

// categoryFullname walks parent_id up the tree and joins names with
// " > " (SB3's `fullname` shape). Cycles — which categoryUpdate
// already forbids — are silently broken after a depth cap so a
// malformed row can't hang rendering.
func categoryFullname(cat domain.Category, all map[int64]domain.Category) string {
	const maxDepth = 16
	names := []string{cat.Name}
	seen := map[int64]struct{}{cat.ID: {}}
	parentID := cat.ParentID
	for depth := 0; depth < maxDepth && parentID != 0; depth++ {
		parent, ok := all[parentID]
		if !ok {
			break
		}
		if _, cycle := seen[parent.ID]; cycle {
			break
		}
		seen[parent.ID] = struct{}{}
		names = append([]string{parent.Name}, names...)
		parentID = parent.ParentID
	}
	return strings.Join(names, " > ")
}

// renderTagsFragment turns a tag slice into the HTML fragment emitted
// as {entry_tags}. Each tag becomes `<a class="tag" href="...">#name</a>`
// separated by single spaces. Returns an empty string when there are no
// tags — templates then typically wrap the tag with a conditional block.
// Kept intentionally minimal: styling (chip look, background, spacing)
// is up to the template CSS, not hardcoded markup.
func renderTagsFragment(s Site, tags []domain.Tag) string {
	if len(tags) == 0 {
		return ""
	}
	parts := make([]string, 0, len(tags))
	for _, t := range tags {
		parts = append(parts,
			`<a class="tag" href="`+html.EscapeString(s.TagPermalink(t))+`">#`+html.EscapeString(t.Name)+`</a>`)
	}
	return strings.Join(parts, " ")
}

// Site bundles the per-weblog values that every page shares — URLs, language,
// encoding. It knows how to write itself into an sbtemplate Context as
// {site_*} and {blog_*} tags.
type Site struct {
	Weblog   domain.Weblog
	Encoding string // always "utf-8" for now; SB v3 let this be user-configurable.
	// BasePath is the deployment sub-path (e.g. "/sb4"). Used as the URL
	// prefix for all site links when Weblog.BaseURL is not configured.
	// When BaseURL is set it takes precedence, so the two can be set
	// independently for split static/dynamic deployments.
	BasePath string
	// TemplateID lets `{site_parts}` resolve to the current template's
	// asset folder. 0 means "not known yet" and the tag collapses to "".
	TemplateID int64
	// Mode is the SB3 "mode" identifier — "ent" (entry), "cat"
	// (category), "arc" (archive), "page" (home / list), "tag",
	// "user" (profile). Surfaces as {mode_id} / {mode_name} on the
	// rendered page. Default "page" when unset.
	Mode string
	// ModeContext is the per-mode discriminator: entry id, category
	// id, archive YYYYMM[DD] condition, tag slug, user id.
	ModeContext string
	// PageSuffix is appended to {site_title} after " | " — SB3
	// concatenates the entry subject / archive label here so the
	// <title> of a given page reads "Blog | Specific page".
	PageSuffix string
	// CommentNumLabel is the localised word for "comments" used in
	// the {comment_num} anchor label (e.g. "Comments" or "コメント").
	// Resolved from the public i18n bundle via the "comment.numLabel"
	// key. Empty falls back to "Comments".
	CommentNumLabel string
	// ReadMoreLabel is the localised text for the {entry_sequel}
	// "read more" anchor on list pages (e.g. "read more ..." or
	// "続きを読む …"). Resolved from the public i18n bundle via the
	// "entry.readMore" key. Empty falls back to "read more ...".
	ReadMoreLabel string
	// CustomTags are user-defined {custom_*} sbtemplate placeholders.
	// Injected by Apply so every page type receives them automatically.
	CustomTags []domain.CustomTag
	// TZ is the timezone every {*_date} / {*_time} tag is rendered
	// in. Nil falls back to time.Local so the field can be left
	// unset by call sites that haven't been updated yet (and by
	// tests). Handlers and the rebuilder always seed this from
	// config.Config.TZ via WithTZ so the deployed binary renders
	// the same archive dates regardless of the host TZ.
	TZ *time.Location
}

func NewSite(w domain.Weblog) Site {
	s := Site{Weblog: w, Encoding: "utf-8"}
	s.CommentNumLabel = commentLabelForLang(w.Lang)
	s.ReadMoreLabel = readMoreLabelForLang(w.Lang)
	return s
}

// commentLabelForLang returns the "comments" label for the {comment_num}
// anchor. The handler layer can override Site.CommentNumLabel with a
// value from the public i18n bundle for full catalogue coverage; this
// fallback covers the static-rebuild path where the bundle isn't loaded.
func commentLabelForLang(lang string) string {
	switch lang {
	case "en":
		return "Comments"
	default:
		return "コメント"
	}
}

// readMoreLabelForLang returns the "read more ..." label for the
// {entry_sequel} anchor on list pages. The handler layer can override
// Site.ReadMoreLabel with a value from the public i18n bundle.
func readMoreLabelForLang(lang string) string {
	switch lang {
	case "en":
		return "read more ..."
	default:
		return "続きを読む …"
	}
}

// WithBasePath returns a copy of the site bound to a deployment sub-path.
// Use this on every dynamic render so {site_*} URLs stay correct under
// sub-path deployments even when Weblog.BaseURL hasn't been configured.
func (s Site) WithBasePath(p string) Site {
	s.BasePath = p
	return s
}

// WithTemplate returns a copy of the site bound to the active template id
// so {site_parts} points at the right asset URL. Uses a copy so the
// callers that pre-compute Site once don't mutate each other.
func (s Site) WithTemplate(tmpl *domain.Template) Site {
	if tmpl != nil {
		s.TemplateID = tmpl.ID
	}
	return s
}

// WithMode returns a copy tagged with the SB3 mode + context, driving
// {mode_name} / {mode_id}. Use short SB3 codes: "ent", "cat", "arc",
// "tag", "user", "page".
func (s Site) WithMode(mode, ctx string) Site {
	s.Mode = mode
	s.ModeContext = ctx
	return s
}

// WithPageSuffix seeds the " | <suffix>" tail appended to {site_title}
// on sub-pages. Used by entry / category / archive handlers so the
// rendered HTML <title> matches what the browser tab shows in SB3.
func (s Site) WithPageSuffix(suffix string) Site {
	s.PageSuffix = suffix
	return s
}

// WithCustomTags returns a copy of the site bound to the given custom
// tags so {custom_*} placeholders resolve on the rendered page.
func (s Site) WithCustomTags(tags []domain.CustomTag) Site {
	s.CustomTags = tags
	return s
}

// WithTZ returns a copy bound to the given timezone so date/time tags
// render against the same zone the archive boundaries are bucketed in.
// nil is accepted and resets to "use time.Local on resolve".
func (s Site) WithTZ(loc *time.Location) Site {
	s.TZ = loc
	return s
}

// resolveTZ centralises the "fall back to time.Local" rule so every
// formatter goes through one path. Always returns a non-nil location.
func (s Site) resolveTZ() *time.Location {
	if s.TZ != nil {
		return s.TZ
	}
	return time.Local
}

func (s Site) baseURL() string {
	if s.Weblog.BaseURL != "" {
		return strings.TrimRight(s.Weblog.BaseURL, "/") + "/"
	}
	if s.BasePath != "" {
		return s.BasePath + "/"
	}
	return "/"
}

func (s Site) TopURL() string  { return s.baseURL() }
func (s Site) CGIURL() string  { return s.baseURL() + "sb.cgi" }
func (s Site) RSSURL() string  { return s.baseURL() + "rss.xml" }
func (s Site) AtomURL() string { return s.baseURL() + "atom.xml" }

// CSSURL is the URL emitted as {site_css}. When a rendering template
// is pinned (WithTemplate has been called), each template gets its
// own stylesheet at /template/<id>/style.css so category / archive /
// profile pages load *their* CSS, not the active template's. The
// bare /style.css remains as a shortcut to the active template for
// backward compatibility with any external link using it.
func (s Site) CSSURL() string {
	if s.TemplateID != 0 {
		return s.baseURL() + "template/" + strconv.FormatInt(s.TemplateID, 10) + "/style.css"
	}
	return s.baseURL() + "style.css"
}

// PartsURL returns the public URL prefix (with trailing slash) where the
// active template's assets live. Empty when no template id has been
// pinned — in that case `{site_parts}` expands to the empty string.
func (s Site) PartsURL() string {
	if s.TemplateID == 0 {
		return ""
	}
	return s.baseURL() + "template/" + strconv.FormatInt(s.TemplateID, 10) + "/"
}

// OGImageURL returns the absolute URL of the Open Graph card image
// for an entry. Matches the file layout the admin OG renderer writes to
// <SB_IMAGE_DIR>/og/<entry_id>.png.
func (s Site) OGImageURL(entryID int64) string {
	return s.baseURL() + "img/og/" + strconv.FormatInt(entryID, 10) + ".png"
}

// PageOGImageURL returns the absolute URL of the Open Graph card image
// for a flat page. Matches the file layout the admin OG renderer writes to
// <SB_IMAGE_DIR>/og/page_<page_id>.png.
func (s Site) PageOGImageURL(pageID int64) string {
	return s.baseURL() + "img/og/page_" + strconv.FormatInt(pageID, 10) + ".png"
}

// formatWith expands an SB3 format string against t using the weblog's
// lang, substituting pkgDefault when the weblog has nothing configured
// for that context. Centralised so every {*_date}/{*_time} tag goes
// through the same "fall back to default" path. The instant is
// projected into s.TZ first so the rendered hour/day matches the
// timezone the archive boundaries are also bucketed in — without this
// the same UTC posted_at would format to host-local on the dynamic
// path and to the configured TZ on archive ranges, which is the
// inconsistency SB_TZ exists to remove.
func (s Site) formatWith(pattern, pkgDefault string, t time.Time) string {
	if pattern == "" {
		pattern = pkgDefault
	}
	return dateformat.Expand(pattern, t.In(s.resolveTZ()), s.Weblog.Lang)
}

// FormatEntryDate renders t as the author's configured entry-date
// pattern (falls through to dateformat.DefaultEntryDate).
func (s Site) FormatEntryDate(t time.Time) string {
	return s.formatWith(s.Weblog.DateFormatEntry, dateformat.DefaultEntryDate, t)
}

// FormatEntryTime renders t as the entry-time pattern.
func (s Site) FormatEntryTime(t time.Time) string {
	return s.formatWith(s.Weblog.TimeFormatEntry, dateformat.DefaultEntryTime, t)
}

// FormatCommentTime renders t as the comment-timestamp pattern.
func (s Site) FormatCommentTime(t time.Time) string {
	return s.formatWith(s.Weblog.DateFormatComment, dateformat.DefaultCommentDate, t)
}

// FormatListDate renders t as the list-view entry-date pattern, used
// on home / category / tag / archive pages where the shorter form is
// conventional.
func (s Site) FormatListDate(t time.Time) string {
	return s.formatWith(s.Weblog.DateFormatList, dateformat.DefaultListDate, t)
}

// FormatArchiveDate renders t as the archive-heading pattern.
func (s Site) FormatArchiveDate(t time.Time) string {
	return s.formatWith(s.Weblog.DateFormatArchive, dateformat.DefaultArchiveDate, t)
}

// EntriesPerPageOrDefault returns the author-configured list page size,
// falling back to 10 when the weblog row pre-dates the column or the
// field was cleared.
func (s Site) EntriesPerPageOrDefault() int {
	if s.Weblog.EntriesPerPage > 0 {
		return s.Weblog.EntriesPerPage
	}
	return 10
}

// EntrySortAsc reports whether list pages should render entries
// oldest-first (the "日付の古いものを上に" setting). The fetch still
// grabs newest-first from the DB with the configured LIMIT so
// pagination semantics stay natural; the caller reverses the slice
// before rendering when this returns true.
func (s Site) EntrySortAsc() bool {
	return s.Weblog.EntrySortOrder == "asc"
}

// CommentSortDesc reports whether comments should render
// newest-first. SB3's default and ours is oldest-first (asc).
func (s Site) CommentSortDesc() bool {
	return s.Weblog.CommentSortOrder == "desc"
}

// Apply populates the {site_*} and {blog_*} tags on the current iteration
// cursor. Call once before rendering. Tags mirror SB3's
// sb::Content::Common::_main + _title — aliases and SB3-only tags are
// emitted alongside our native names for imported-template compat.
func (s Site) Apply(c *sbtemplate.Context) {
	title := s.Weblog.Title
	if s.PageSuffix != "" {
		title = title + " | " + s.PageSuffix
	}
	c.Tag("site_encoding", s.Encoding)
	c.Tag("site_lang", s.Weblog.Lang)
	c.Tag("site_title", title)
	c.Tag("site_top", s.TopURL())
	c.Tag("site_cgi", s.CGIURL())
	c.Tag("site_css", s.CSSURL())
	c.Tag("site_rss", s.RSSURL())
	c.Tag("site_atom", s.AtomURL())
	c.Tag("site_parts", s.PartsURL())
	// {site_mobile} has no backing feature (mobile mode was dropped
	// during the rewrite). {site_rsd} points at the /rsd.xml route,
	// which exists purely to satisfy imported templates — the
	// advertised XML-RPC API isn't implemented.
	c.Tag("site_mobile", "")
	c.Tag("site_rsd", s.baseURL()+"rsd.xml")
	c.Tag("selected_archive", s.PageSuffix)
	// Script metadata — SB3 reads from $sb::PRODUCT / $sb::VERSION /
	// $sb::WEBPAGE. Kept as literals here; bump alongside
	// internal/version when the product name or public URL change.
	c.Tag("script_name", "Serene Bach")
	c.Tag("script_version", version.Public)
	c.Tag("script_webpage", "https://github.com/serendipitynz/serenebach")
	// Mode metadata. SB3 uses short ("ent"/"cat"/...) for mode_id
	// and long ("entry"/"category"/...) for mode_name; we emit the
	// long form for the name and mirror ModeContext for the id so
	// a template can branch on `{mode_id}` per route.
	c.Tag("mode_name", modeNameFor(s.Mode))
	c.Tag("mode_id", s.ModeContext)
	// Title block tags — SB3 emits {blog_name} as a <a href="top/">
	// anchor; templates that want the plain form use {blog_name_only}.
	c.Tag("blog_name_only", s.Weblog.Title)
	c.TagHTML("blog_name",
		`<a href="`+html.EscapeString(s.TopURL())+`">`+html.EscapeString(s.Weblog.Title)+`</a>`)
	c.Tag("blog_description", s.Weblog.Description)
	// Pagination tags — populated by a future handler that wires real
	// pagination; empty for now so the raw `{page_*}` markers don't
	// leak into imported templates.
	c.Tag("page_num", "")
	c.Tag("page_now", "")
	c.Tag("prev_page_url", "")
	c.Tag("prev_page_link", "")
	c.Tag("next_page_url", "")
	c.Tag("next_page_link", "")
	// User-defined custom tags — raw HTML, injected after every built-in
	// tag so they can reference (or shadow) standard names if desired.
	for _, ct := range s.CustomTags {
		c.TagHTML(ct.Name, ct.Value)
	}
}

// modeNameFor returns the SB3 long-form mode label for the short mode
// code — "entry" for "ent", "category" for "cat", etc. Unknown /
// empty collapses to "page" (home / generic list), which SB3 uses as
// its default when no mode matches.
func modeNameFor(short string) string {
	switch short {
	case "ent":
		return "entry"
	case "cat":
		return "category"
	case "arc":
		return "archive"
	case "tag":
		return "tag"
	case "user":
		return "profile"
	case "srch":
		return "search"
	}
	return "page"
}
