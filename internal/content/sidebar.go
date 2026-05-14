// sidebar.go — populators for the SB3 "parts" blocks that live on
// every rendered page alongside the main entry/list area.
//
// Each block corresponds to a callback in SB3's sb::Content::List:
// `archives`, `category`, `recent_comment`, `latest_entry`. The SB3
// convention is "a block with count 1 holding a
// single pre-rendered `<ul>…</ul>` tag"; an empty list collapses the
// block to count 0 so the markup strips away cleanly.
//
// These helpers are intentionally dumb — they render the list fragment
// in Go and emit it as a single tag. Per-item templating inside the
// block (à la the `entry` loop) is a separate feature SB3 doesn't
// bother with for sidebars either.
package content

import (
	"html"
	"strconv"
	"strings"
	"time"

	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/storage/repo"
	"github.com/serendipitynz/serenebach/internal/template/sbtemplate"
)

// firstOfMonth fabricates "first of the month" for archive labels. The
// instant must be built in the same timezone the formatter projects
// into, otherwise FormatArchiveDate's t.In(s.TZ) shift across UTC can
// roll the synthetic 1st-of-the-month back to the previous month for
// negative-offset zones (e.g. America/New_York: 2026-01-01 00:00 UTC
// → 2025-12-31 19:00 EST).
func firstOfMonth(year, month int, loc *time.Location) time.Time {
	if loc == nil {
		loc = time.Local
	}
	return time.Date(year, time.Month(month), 1, 0, 0, 0, 0, loc)
}

// noNameLabel returns the reader-facing placeholder for an anonymous
// commenter, picked off the blog's configured language. Engine chrome
// only — author-visible admin copy resolves through internal/i18n.
func noNameLabel(lang string) string {
	if lang == "en" {
		return "(no name)"
	}
	return "(名前なし)"
}

// SidebarData bundles the pre-fetched inputs every sidebar block needs.
// Callers populate what they can (typically from the same query set
// they're already running) and leave the rest nil — individual block
// helpers fall back to "empty list → count 0" when their input slice
// is empty.
type SidebarData struct {
	// Archives: (year, month) + count. Ordered newest-first.
	Archives []repo.ArchivePeriod
	// CategoryTree: top-level categories (ParentID == 0) with
	// per-row entry counts. SubCategories is keyed by parent id.
	CategoryTree []SidebarCategory
	// RecentComments: N most recent approved comments across the
	// weblog.
	RecentComments []repo.RecentApprovedMessage
	// LatestEntries: N most recent published entries.
	LatestEntries []domain.Entry
	// Links: the blogroll. Groups + their member links come through in
	// sort_order; the renderer groups children under their parent.
	Links []domain.Link
}

// SidebarCategory pairs a category with its post count for the
// `{category_list}` fragment.
type SidebarCategory struct {
	Category domain.Category
	Count    int64
}

// applySidebarBlocks emits every sidebar block a template references.
// Blocks the template doesn't mention are skipped cheaply via
// HasBlock; blocks with no data emit count 0 so raw `{-name}`
// placeholders don't leak through.
func applySidebarBlocks(s Site, c *sbtemplate.Context, tmpl *sbtemplate.Template, data SidebarData) {
	applyArchivesBlock(s, c, tmpl, data.Archives)
	applyCategorySidebarBlock(s, c, tmpl, data.CategoryTree)
	applyRecentCommentBlock(s, c, tmpl, data.RecentComments)
	applyLatestEntryBlock(s, c, tmpl, data.LatestEntries)
	applyLinkBlock(c, tmpl, data.Links)
	// selected_entry (SB3's "recommended posts") relies on a flag
	// we haven't modelled yet — strip to 0 so imported templates
	// don't trip on the raw marker.
	if tmpl.HasBlock("selected_entry") {
		c.Block("selected_entry", 0)
	}
}

// applyArchivesBlock mirrors sb::Content::List::_archives. Emits
// `{archives_list}` as a `<ul><li><a href="/archive/YYYY/MM/">YYYY年
// MM月 (N)</a></li>…</ul>` fragment.
func applyArchivesBlock(s Site, c *sbtemplate.Context, tmpl *sbtemplate.Template, periods []repo.ArchivePeriod) {
	if !tmpl.HasBlock("archives") {
		return
	}
	if len(periods) == 0 {
		c.Block("archives", 0)
		return
	}
	var b strings.Builder
	b.WriteString("<ul>")
	for _, p := range periods {
		url := s.ArchivePermalink(p.Year, p.Month)
		label := archiveLabelFor(s, p.Year, p.Month)
		b.WriteString(`<li><a href="`)
		b.WriteString(html.EscapeString(url))
		b.WriteString(`">`)
		b.WriteString(html.EscapeString(label))
		b.WriteString(`</a> (`)
		b.WriteString(strconv.FormatInt(p.Count, 10))
		b.WriteString(`)</li>`)
	}
	b.WriteString("</ul>")
	c.Num(0)
	c.TagHTML("archives_list", b.String())
	c.Block("archives", 1)
}

// archiveLabelFor picks a month label for the archive sidebar —
// uses the weblog's `DateFormatArchive` so the label matches whatever
// the author configured in デザイン設定 > 時刻表記設定.
func archiveLabelFor(s Site, year, month int) string {
	// Fabricate a "first of the month" time so dateformat tokens
	// render correctly. The specific day doesn't matter — archive
	// patterns use Year / Mon only.
	t := firstOfMonth(year, month, s.resolveTZ())
	return s.FormatArchiveDate(t)
}

// applyCategorySidebarBlock mirrors sb::Content::List::_category.
// Emits two fragments:
//
//	{category_list}     — top-level categories only (single `<ul>`)
//	{subcategory_list}  — every category with count > 0, nested
//	                      `<ul><li>parent<ul>child…</ul></li></ul>`
//
// Empty branches (count = 0) are pruned, matching SB3's own
// behaviour. Parent chains are capped at categoryMaxDepth so a
// malformed row can't hang rendering.
func applyCategorySidebarBlock(s Site, c *sbtemplate.Context, tmpl *sbtemplate.Template, cats []SidebarCategory) {
	if !tmpl.HasBlock("category") {
		return
	}
	byParent := map[int64][]SidebarCategory{}
	hasVisible := false
	for _, sc := range cats {
		// Hidden categories drop out of every public surface, so the
		// sidebar nav must skip them too — otherwise a hidden category
		// would still be clickable from the {category_list} fragment
		// that imported SB3 templates render.
		if sc.Category.Hidden {
			continue
		}
		if sc.Count <= 0 {
			continue
		}
		byParent[sc.Category.ParentID] = append(byParent[sc.Category.ParentID], sc)
		hasVisible = true
	}
	if !hasVisible {
		c.Block("category", 0)
		return
	}
	topList := renderCategoryTree(s, byParent, 0, 1)
	subList := renderCategoryTree(s, byParent, 0, categoryMaxDepth)
	if topList == "" {
		c.Block("category", 0)
		return
	}
	c.Num(0)
	c.TagHTML("category_list", topList)
	c.TagHTML("subcategory_list", subList)
	c.Block("category", 1)
}

// categoryMaxDepth caps the recursion when emitting nested category
// trees. Matches the depth cap already used in categoryFullname.
const categoryMaxDepth = 16

// renderCategoryTree returns the `<ul>` fragment for every category
// whose ParentID == parent, nesting into each child's sub-tree up to
// remainingDepth levels deep. remainingDepth == 1 yields a single-level
// list (no children), which is what {category_list} emits.
func renderCategoryTree(s Site, byParent map[int64][]SidebarCategory, parent int64, remainingDepth int) string {
	kids := byParent[parent]
	if len(kids) == 0 || remainingDepth <= 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<ul>")
	for _, sc := range kids {
		url := s.CategoryPermalink(sc.Category)
		b.WriteString(`<li><a href="`)
		b.WriteString(html.EscapeString(url))
		b.WriteString(`">`)
		b.WriteString(html.EscapeString(sc.Category.Name))
		b.WriteString(`</a> (`)
		b.WriteString(strconv.FormatInt(sc.Count, 10))
		b.WriteString(`)`)
		if remainingDepth > 1 {
			if sub := renderCategoryTree(s, byParent, sc.Category.ID, remainingDepth-1); sub != "" {
				b.WriteString(sub)
			}
		}
		b.WriteString(`</li>`)
	}
	b.WriteString("</ul>")
	return b.String()
}

// applyRecentCommentBlock mirrors sb::Content::List::_comment. Emits
// `{recent_comment_list}` as a `<ul><li>` fragment linking each
// comment back to its entry.
func applyRecentCommentBlock(s Site, c *sbtemplate.Context, tmpl *sbtemplate.Template, messages []repo.RecentApprovedMessage) {
	if !tmpl.HasBlock("recent_comment") {
		return
	}
	if len(messages) == 0 {
		c.Block("recent_comment", 0)
		return
	}
	var b strings.Builder
	b.WriteString("<ul>")
	for _, m := range messages {
		key := m.EntrySlug
		if key == "" {
			key = strconv.FormatInt(m.EntryID, 10)
		}
		url := s.TopURL() + "entry/" + key + "/"
		label := m.AuthorName
		if label == "" {
			label = noNameLabel(s.Weblog.Lang)
		}
		b.WriteString(`<li><a href="`)
		b.WriteString(html.EscapeString(url))
		b.WriteString(`">`)
		b.WriteString(html.EscapeString(m.EntryTitle))
		b.WriteString(`</a> — `)
		b.WriteString(html.EscapeString(label))
		b.WriteString(`</li>`)
	}
	b.WriteString("</ul>")
	c.Num(0)
	c.TagHTML("recent_comment_list", b.String())
	c.Block("recent_comment", 1)
}

// applyLinkBlock mirrors sb::Content::List::_link. Emits the
// `{link_list}` tag as a nested `<ul>` — root-level links render as
// `<li><a href=…>Name</a></li>` directly, while groups wrap their
// visible children in `<li><span>Group</span><ul>…</ul></li>`. Groups
// with no visible children are skipped entirely (SB3 behaviour), and
// links without a URL are skipped. The block count is 1 when at least
// one row survives the filter, 0 otherwise.
func applyLinkBlock(c *sbtemplate.Context, tmpl *sbtemplate.Template, links []domain.Link) {
	if !tmpl.HasBlock("link") {
		return
	}
	if len(links) == 0 {
		c.Block("link", 0)
		return
	}
	// Bucket link-rows (disp==0 handled by the caller) under their
	// parent group id. Ungrouped rows (ParentID == 0 + !IsGroup) render
	// as top-level links.
	children := map[int64][]domain.Link{}
	for _, l := range links {
		if l.IsGroup() {
			continue
		}
		if l.URL == "" {
			continue
		}
		if l.ParentID != 0 {
			children[l.ParentID] = append(children[l.ParentID], l)
		}
	}

	var b strings.Builder
	b.WriteString("<ul>")
	written := 0
	for _, l := range links {
		if l.IsGroup() {
			kids := children[l.ID]
			if len(kids) == 0 {
				continue
			}
			b.WriteString(`<li>`)
			b.WriteString(linkSpanOpen(l))
			b.WriteString(html.EscapeString(l.Name))
			b.WriteString(`</span><ul>`)
			for _, kid := range kids {
				b.WriteString(renderLinkItem(kid))
			}
			b.WriteString(`</ul></li>`)
			written++
			continue
		}
		if l.ParentID != 0 || l.URL == "" {
			continue
		}
		b.WriteString(renderLinkItem(l))
		written++
	}
	b.WriteString("</ul>")
	if written == 0 {
		c.Block("link", 0)
		return
	}
	c.Num(0)
	c.TagHTML("link_list", b.String())
	c.Block("link", 1)
}

// linkSpanOpen emits `<span>` with an optional title attribute so the
// group label can show tooltip text when Description is non-empty.
func linkSpanOpen(l domain.Link) string {
	if l.Description == "" {
		return `<span>`
	}
	return `<span title="` + html.EscapeString(l.Description) + `">`
}

// renderLinkItem emits one `<li><a …>Name</a></li>` row honouring the
// optional title (description) and target attributes.
func renderLinkItem(l domain.Link) string {
	var b strings.Builder
	b.WriteString(`<li><a href="`)
	b.WriteString(html.EscapeString(l.URL))
	b.WriteString(`"`)
	if l.Description != "" {
		b.WriteString(` title="`)
		b.WriteString(html.EscapeString(l.Description))
		b.WriteString(`"`)
	}
	if l.Target != "" {
		b.WriteString(` target="`)
		b.WriteString(html.EscapeString(l.Target))
		b.WriteString(`"`)
	}
	b.WriteString(`>`)
	b.WriteString(html.EscapeString(l.Name))
	b.WriteString(`</a></li>`)
	return b.String()
}

// applyLatestEntryBlock mirrors sb::Content::List::_latest. Emits
// `{latest_entry_list}` as a simple `<ul><li><a>Title</a></li>…`
// fragment.
func applyLatestEntryBlock(s Site, c *sbtemplate.Context, tmpl *sbtemplate.Template, entries []domain.Entry) {
	if !tmpl.HasBlock("latest_entry") {
		return
	}
	if len(entries) == 0 {
		c.Block("latest_entry", 0)
		return
	}
	var b strings.Builder
	b.WriteString("<ul>")
	for _, e := range entries {
		url := s.EntryPermalink(e)
		b.WriteString(`<li><a href="`)
		b.WriteString(html.EscapeString(url))
		b.WriteString(`">`)
		b.WriteString(html.EscapeString(e.Title))
		b.WriteString(`</a></li>`)
	}
	b.WriteString("</ul>")
	c.Num(0)
	c.TagHTML("latest_entry_list", b.String())
	c.Block("latest_entry", 1)
}
