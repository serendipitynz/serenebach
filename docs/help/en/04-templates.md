---
title: Template editing
slug: templates
order: 40
---

# Template editing

The look of the public site is controlled by templates. The Go port of Serene Bach uses the same template philosophy as SB3 — most existing SB3 templates can be imported as-is, though a few legacy features aren't supported.

## What you can edit

The template editor covers:

- The full page HTML
- The HTML for individual entry pages
- The stylesheet
- Template assets like logos and background images

Preview before saving so you can confirm a new look without immediately replacing what's published.

## Basic syntax

Insert dynamic values with tags:

```html
<h1>{blog_title}</h1>
<article>
  <h2>{entry_title}</h2>
  {entry_description}
</article>
```

Anything inside `{...}` is replaced at render time with the actual value.

For repeated content (entry lists, category lists, recent comments) use blocks:

```html
<!-- BEGIN entry -->
<article>
  <h2>{entry_title}</h2>
  {entry_description}
</article>
<!-- END entry -->
```

`BEGIN` and `END` must each appear on a line of their own.

## Common tags

| Tag | Value |
|---|---|
| `{blog_title}` | Blog title |
| `{blog_description}` | Blog description |
| `{blog_url}` | Blog URL |
| `{site_css}` | URL of the template's CSS |
| `{site_parts}` | URL prefix for template assets |
| `{entry_title}` | Entry title |
| `{entry_description}` | Entry body |
| `{entry_sequel}` | "More" content |
| `{entry_posted}` | Posted timestamp |
| `{entry_permalink}` | Entry URL |
| `{entry_category_name}` | Category name |
| `{entry_comments_count}` | Number of comments |
| `{entry_likes_count}` | Number of likes |
| `{entry_stamps_count}` | Number of stamps |

Some SB3-compatible aliases are also supported. After importing an older template, check the editor's status panel to see what was flagged.

## Template assets

Logos, background images, and supplementary CSS files can be uploaded as template assets. Reference them with `{site_parts}`:

```html
<img src="{site_parts}logo.png" alt="">
```

## Design settings

The **Design Settings** screen (the サイドバー entry that opens `/admin/templates`) covers site-wide rendering knobs:

- Switching the active template
- Templates used for archive / category listings
- Template used for the profile page
- Number of entries per listing page
- Sort order for entries and comments
- Date format

## Import and export

You can import an SB3-style `template.txt`. Older character encodings (Shift_JIS, EUC-JP, ISO-2022-JP) are auto-converted to UTF-8 during import.

Export uses the same `template.txt` format — useful for backups or moving a template to another instance.

## Unsupported legacy features

The Go port does not support trackbacks, Amazon affiliate tags, or mobile-only views. Imports succeed, but you'll need to remove or rewrite those bits in the template editor.

See [Migrating from SB2 / SB3 and feature differences](sb3-migration) for the full list.

## Related pages

- [Preview mode](preview)
- [Migrating from SB2 / SB3 and feature differences](sb3-migration)
