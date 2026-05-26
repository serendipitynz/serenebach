---
title: Template editing
slug: templates
order: 60
---

# Template editing

The look of the public site is controlled by templates. The Go port of Serene Bach uses the same template philosophy as SB3 — most existing SB3 templates can be imported as-is, though a few legacy features aren't supported.

## Template structure

A template set consists of:

- Base HTML template (full-page HTML)
- Individual-entry HTML template (used only in single-entry mode)
- CSS template

The individual-entry HTML template is optional; when absent, the base HTML template is used for single-entry pages too.

You can save multiple template sets and switch between them in **Design**. Archive / category listings and profile pages can each use a different template. Per-category templates are also supported.

Template assets such as images can be uploaded and referenced with `{site_parts}`:

```html
<img src="{site_parts}logo.png" alt="">
```

`.js` / `.woff` / `.woff2` / `.ttf` / `.otf` files can also be uploaded and referenced from `{site_parts}`.

## HTML template syntax

HTML templates are ordinary HTML with two special constructs: **blocks** and **tags**.

### Blocks

```html
<!-- BEGIN block_name -->
<p>{tag_name}</p>
<!-- END block_name -->
```

A block is the region between `<!-- BEGIN block_name -->` and `<!-- END block_name -->`. Its repetition count changes depending on the page state.

**Important:** `BEGIN` and `END` must each appear on a line of their own. The following will **NOT** work:

```html
<!-- Wrong: other tags on the same line -->
<div><!-- BEGIN entry --><p>{entry_title}</p><!-- END entry --></div>
```

### Tags

Anything inside `{...}` is replaced at render time with the actual value.

```html
<h1>{blog_name_only}</h1>
<article>
  <h2>{entry_title}</h2>
  {entry_description}
</article>
```

## Block reference

| Category | Block | Description |
|---|---|---|
| Title | `title` | Page header. Always rendered once. |
| Title | `toppage` | Shown only on the top (home) page. Hidden on category, archive, tag, single-entry, and profile pages. |
| Entry | `entry` | Entry loop. Repeats per entry on list pages; once on single-entry pages. |
| Entry | `pinned_entry` | **Single-entry pages only:** count = 1 for pinned entries, 0 otherwise. Always 0 on list pages (home, category, etc.) — use the `{entry_pinned}` tag for per-entry conditional styling on list pages instead. |
| Entry | `option` | Shown only on single-entry pages. |
| Entry | `sequel` | Shown only on single-entry pages. Contains prev/next entry navigation. |
| Comments | `comment_area` | Shown on single-entry pages when comments are accepted. Contains the comment form. |
| Comments | `comment` | Individual approved comments. Repeats per comment. |
| Pagination | `page` | Page navigation. Shown when total pages > 1. |
| Profile | `profile` | User directory list (sidebar). |
| Profile | `profile_area` | Profile detail block. Only on `/profile/{id}/` pages. |
| Category | `category_area` | Category heading block. Only on category-scoped list pages. |
| Lists | `archives` | Monthly archive list. |
| Lists | `category` | Category list. |
| Lists | `latest_entry` | Recent entries list. |
| Lists | `link` | Blogroll / link list. |
| Lists | `recent_comment` | Recent comments list. |
| Lists | `selected_entry` | Selected/recommended entries list. **Always 0 in the Go port.** |
| Flat pages | `dedicated_page` | Shown only on flat pages. Hidden on regular entry pages and listings. |

### Pinned entries (`{entry_pinned}` tag and `pinned_entry` block)

Setting the **pinned** flag on an entry floats it to the top of the home page and category archive page 1. Use the following template constructs to adjust the appearance based on pin state.

**On list pages (home / category)** use the `{entry_pinned}` tag. It yields `"pinned"` for pinned entries and `""` for regular ones, so you can inject it directly as a CSS class:

```html
<!-- BEGIN entry -->
<article class="entry {entry_pinned}">
  <h2><a href="{entry_permalink}">{entry_title}</a></h2>
</article>
<!-- END entry -->
```

```css
/* Style pinned entries differently */
.entry.pinned { border-left: 4px solid #f90; }
```

**On single-entry pages** the `pinned_entry` block can also be used (count = 1 when pinned, 0 otherwise):

```html
<!-- BEGIN entry -->
<!-- BEGIN pinned_entry -->
<span class="pin-badge">📌 Pinned</span>
<!-- END pinned_entry -->
<h2>{entry_title}</h2>
<!-- END entry -->
```

> **Note:** `pinned_entry` is always 0 (hidden) on list pages. Use the `{entry_pinned}` tag for per-entry branching on list pages.

## Tag reference

### Global tags (available anywhere)

| Tag | Value |
|---|---|
| `{site_encoding}` | Character encoding (`utf-8`) |
| `{site_lang}` | Weblog language code |
| `{site_title}` | Page title ( `"Blog \| Suffix"` when a suffix is set ) |
| `{site_top}` | Blog top-page URL |
| `{site_cgi}` | `/sb.cgi` (SB3 compatibility) |
| `{site_css}` | URL of the template's CSS |
| `{site_rss}` | `/rss.xml` |
| `{site_atom}` | `/atom.xml` |
| `{site_parts}` | URL prefix for template assets |
| `{site_mobile}` | Mobile access URL. **Always empty in the Go port.** |
| `{site_rsd}` | `/rsd.xml` (SB3 compatibility; XML-RPC is not implemented) |
| `{selected_archive}` | Current page suffix (archive label, category name, etc.) |
| `{script_name}` | `Serene Bach` |
| `{script_version}` | Version string |
| `{script_webpage}` | Official project URL |
| `{mode_name}` | Long mode name (`entry`, `category`, `archive`, `tag`, `profile`, `search`, `page`) |
| `{mode_id}` | Short mode identifier (`ent`, `cat`, `arc`, `tag`, `user`, `srch`, etc.) |
| `{blog_name_only}` | Blog title (plain text) |
| `{blog_name}` | Blog title wrapped in a link to the top page |
| `{blog_description}` | Blog description |
| `{csrf_token}` | CSRF token for public POST forms |
| `{custom_xxx}` | User-defined custom tags. The name and value registered in the admin panel are expanded here. |

### Pagination tags

Usable inside and outside the `page` block.

| Tag | Value |
|---|---|
| `{page_num}` | Total number of pages |
| `{page_now}` | Current page number (1-indexed) |
| `{prev_page_url}` | Previous page URL (empty on first page) |
| `{prev_page_link}` | Previous-page HTML anchor (`<<`) |
| `{next_page_url}` | Next page URL (empty on last page) |
| `{next_page_link}` | Next-page HTML anchor (`>>`) |

### Tags inside the `entry` block

| Tag | Value |
|---|---|
| `{entry_id}` | Entry ID |
| `{entry_permalink}` | Entry permalink URL |
| `{entry_title}` | Entry title |
| `{entry_date}` | Entry date. Formatted with "Design settings > Settings > Date/time formats > Entry date" (default: `%Year%-%Mon%-%Day% (%Week%)`) on both list and single-entry pages. |
| `{entry_time}` | Posting time wrapped in a permalink anchor |
| `{entry_disp_time}` | Posting time (plain string) |
| `{entry_description}` | Entry body (format-rendered HTML) |
| `{entry_sequel}` | "Read more" link on list pages; "More" body on single-entry pages |
| `{entry_mode}` | `list`, `entry`, or `page` (flat pages) |
| `{entry_likes_count}` | Number of likes |
| `{entry_like_url}` | Like POST URL |
| `{entry_stamps_count}` | Total stamp count |
| `{entry_stamp_url}` | Stamp POST URL |
| `{entry_keywords}` | Keywords (comma-separated) |
| `{entry_keyword}` | SB3 spelling alias for `{entry_keywords}` |
| `{entry_tags}` | Tag list HTML fragment |
| `{entry_pinned}` | `"pinned"` for pinned entries, `""` (empty) otherwise. Can be injected directly as a CSS class (e.g. `class="entry {entry_pinned}"`). Works correctly per-entry on both list and single-entry pages. |
| `{permalink}` | SB3 short alias for `{entry_permalink}` |
| `{comment_num}` | Comments anchor HTML (`-` when comments are closed) |
| `{comment_count}` | Raw comment count (empty when comments are closed) |
| `{sb_entry_marking}` | Scroll anchor on list pages; empty on single-entry pages |
| `{category_name}` | Category name link (`-` when uncategorised) |
| `{category_id}` | Category ID (empty when uncategorised) |
| `{category_slug}` | Category slug — matches the `/category/<slug>/` URL segment when set (empty otherwise) |
| `{category_disp_name}` | Category display name (`-` when uncategorised) |
| `{user_name}` | Author **login name** (SB3-compatible) |
| `{user_disp_name}` | Author display name |
| `{user_login}` | Alias for `{user_name}` (login name) |
| `{user_id}` | Author user ID |

On single-entry pages (`entry` block count = 1) the following extra tags are available:

| Tag | Value |
|---|---|
| `{entry_og_image}` | OG image URL |
| `{entry_og_image_width}` | `1200` |
| `{entry_og_image_height}` | `630` |

Per-kind stamp counts are available as `{entry_stamps_heart}`, `{entry_stamps_laugh}`, `{entry_stamps_wow}`, and `{entry_stamps_party}`.

### Tags inside the `sequel` block

| Tag | Value |
|---|---|
| `{prev_permalink}` | Previous entry permalink (empty at edge) |
| `{prev_title}` | Previous entry title |
| `{next_permalink}` | Next entry permalink (empty at edge) |
| `{next_title}` | Next entry title |
| `{prev_entry}` | Ready-made anchor `« Title` (empty at edge) |
| `{next_entry}` | Ready-made anchor `Title »` (empty at edge) |

> The "previous / next" chronological relationship depends on the weblog's configured entry sort order.

### Tags inside the `comment_area` block

| Tag | Value |
|---|---|
| `{comment_post_url}` | Comment POST URL |
| `{form_ts}` | Anti-spam Unix timestamp |
| `{comment_error}` | Form error message (HTML-escaped) |
| `{cookie_name}` | Prefilled commenter name from cookie |
| `{cookie_email}` | Prefilled commenter email from cookie |
| `{cookie_url}` | Prefilled commenter URL from cookie |
| `{turnstile_widget}` | Cloudflare Turnstile widget HTML (empty when not configured) |
| `{sb_comment_js}` | **Always empty in the Go port.** |

### Tags inside the `comment` block

| Tag | Value |
|---|---|
| `{comment_name}` | Comment author name (HTML-escaped) |
| `{comment_time}` | Comment timestamp |
| `{comment_description}` | Comment body (HTML-escaped, newlines → `<br>`) |
| `{comment_url}` | Comment author URL (scheme-allow-listed) |
| `{comment_icon}` | **Always empty in the Go port.** (reserved for a future avatar feature) |

### Tags inside the `profile` block (sidebar)

| Tag | Value |
|---|---|
| `{user_list}` | Pre-rendered `<ul><li><a href="...">Name</a></li>...</ul>` fragment of all list-visible users |

### Tags inside the `profile_area` block (profile page)

| Tag | Value |
|---|---|
| `{profile_id}` | User numeric ID |
| `{profile_name}` | User display name |
| `{profile_login}` | User login name |
| `{profile_description}` | Profile description (format-rendered HTML) |
| `{profile_email}` | **Always empty in the Go port.** (admin email is not public) |
| `{user_id}` | Alias for `{profile_id}` |
| `{user_name}` | Alias for `{profile_login}` (login name, SB3-compatible) |
| `{user_disp_name}` | Alias for `{profile_name}` |
| `{user_login}` | Alias for `{profile_login}` |

### Tags inside the `category_area` block (category page)

| Tag | Value |
|---|---|
| `{category_pagename}` | The category's own name |
| `{category_fullname}` | Full name with parent chain (`Parent > Child`) |
| `{category_slug}` | Category slug (empty when unset) |
| `{category_description}` | Category description (format-rendered HTML) |

### Tags inside the `archives` block

| Tag | Value |
|---|---|
| `{archives_list}` | Monthly archive `<ul><li><a href="...">YYYY-MM (N)</a></li>...</ul>` HTML fragment |

### Tags inside the `category` block

| Tag | Value |
|---|---|
| `{category_list}` | Top-level categories only, as a `<ul>` HTML fragment |
| `{subcategory_list}` | Nested categories including sub-categories, as a `<ul>` HTML fragment |

### Tags inside the `recent_comment` block

| Tag | Value |
|---|---|
| `{recent_comment_list}` | Recent comments `<ul><li>EntryTitle<br />=&gt; <a href="...">NameDate</a></li>...</ul>` HTML fragment (SB3 `_comment` compatible). `Date` follows "Design settings > Settings > Date/time formats > List" (default: ` (%Mon%/%Day%)`). |

### Tags inside the `latest_entry` block

| Tag | Value |
|---|---|
| `{latest_entry_list}` | Recent entries `<ul><li><a href="...">Title</a>Date</li>...</ul>` HTML fragment (SB3 `_latest` compatible). `Date` follows "Design settings > Settings > Date/time formats > List" (default: ` (%Mon%/%Day%)`). |

### Tags inside the `link` block

| Tag | Value |
|---|---|
| `{link_list}` | Blogroll HTML fragment (group nesting supported) |

## SB3-compatible aliases

The Go port provides the following aliases for compatibility with SB3 templates.

| Tag | Alias of | Note |
|---|---|---|
| `{permalink}` | `{entry_permalink}` | SB3 short form |
| `{entry_keyword}` | `{entry_keywords}` | SB3 singular spelling |
| `{user_login}` | `{user_name}` | Go-port alias for login name |

## Unsupported / behaviourally different tags and blocks

These tags and blocks are either not implemented or behave differently from SB3. When you import an SB3 template, the template editor shows warnings for any that are present.

### Unsupported tags (always empty)

| Tag | Reason |
|---|---|
| `{trackback_url}` | Trackback is out of scope (spam vector) |
| `{trackback_count}` | Same as above |
| `{recent_trackback_list}` | Same as above |
| `{comment_iconform}` | Comment icons are not supported |
| `{related_category}` | Secondary category assignment is not modelled |
| `{related_category_disp}` | Same as above |
| `{entry_excerpt}` | Summary / excerpt field is not modelled |
| `{calendar}` | Calendar sidebar widget is not implemented |
| `{calendar2}` | Same as above |
| `{calendar_horizontal}` | Same as above |
| `{calendar_vertical}` | Same as above |
| Any `{trackback_...}` | Trackback feature is out of scope |
| Any `{amazon_...}` | Amazon affiliate integration is out of scope |
| Any `{asin_...}` | Same as above |

### Unsupported blocks

| Block | Reason |
|---|---|
| `trackback_area` | Trackback is out of scope |
| `recent_trackback` | Same as above |
| `trackback` | Same as above |
| `amazon_area` | Amazon affiliate is out of scope |
| `amazon` | Same as above |
| `comment_iconform` | Comment icons are not supported |
| `calendar` | Calendar sidebar widget is not implemented |
| `mobile_top` | Mobile mode was dropped |
| `mobile_entry` | Same as above |
| `mobile_comment_area` | Same as above |
| `mobile_comment_form` | Same as above |
| `mobile_trackback_area` | Same as above |

### Tags with different semantics

| Tag | SB3 semantics | Go-port semantics |
|---|---|---|
| `{site_mobile}` | Mobile URL | Always empty |
| `{comment_icon}` | Icon image | Always empty |
| `{profile_email}` | User email address | Always empty (kept private) |
| `{sb_comment_js}` | SB3 comment script | Always empty |

### Blocks with different semantics

| Block | SB3 semantics | Go-port semantics |
|---|---|---|
| `selected_entry` | Shown when recommended-posts flag is set | Always 0 |

## CSS template

The CSS template can also use tags:

| Tag | Value |
|---|---|
| `{site_parts}` | URL prefix for template assets |
| `{site_encoding}` | Character encoding |

## Related pages

This page covers the editing of one template set. The decisions that live above the template — which template is active, site-wide rendering options, custom tags, import/export, and OG card defaults — are gathered in [Blog design and OG cards](design).

- [Blog design and OG cards](design)
- [Preview mode](preview)
- [Migrating from SB2 / SB3 and feature differences](sb3-migration)
