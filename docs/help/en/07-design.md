---
title: Blog design and OG cards
slug: design
order: 70
---

# Blog design and OG cards

The **Design** screen handles which template is active, site-wide rendering options, the OG card defaults, custom tags, and template import/export.

For editing a single template set itself, see [Template editing](templates). This page covers the cross-template decisions: which template to use, the site-wide rendering knobs, and the OG card image shown when an entry URL is shared on social platforms.

## Design settings

The **Design** screen carries site-wide rendering knobs:

- Switching the active template
- Templates used for archive / category / tag listings
- Template used for the profile page
- Number of entries per listing page
- Sort order for entries
- Sort order for comments
- Date format

Date formats use the same SB3-style tokens such as `%Year%`, `%Mon%`, `%Day%`, `%Week%`, `%Hour%`, `%Min%`. Example: `%Year%-%Mon%-%Day% (%Week%)`

To assign a per-category template, set it from each category's edit form.

## OG cards

OG cards are the images social platforms show when someone shares an entry URL. Serene Bach generates a 1200 x 630 image per entry.

The blog-wide defaults (background image and text colour) are edited under **Design > OG Card**. Per-entry overrides are set on the entry editor.

The background is resolved in this order:

1. Entry-level OG background (set in the entry editor)
2. Blog-wide OG background (Design > OG Card)
3. The bundled default

Backgrounds are picked from the image library. Off-aspect images are centre-cropped.

The text colour applies to both the entry title and the blog title. When unset, the standard two-tone defaults apply. Tick "Hide text" if your background image already includes its own typography.

## Custom tags

The **Custom Tags** tab lets you register your own `{custom_xxx}` placeholders for use in templates. Values are inserted as raw HTML / text — they are **not escaped** when rendered.

```html
<!-- In a template -->
<div class="analytics">{custom_google_analytics}</div>
```

Rules for registered tags:

| Field | Rule |
|---|---|
| Tag name | `custom_` prefix followed by lowercase letters, digits, and underscores only (max 50 chars after the prefix) |
| Value | HTML or plain text (max 64 KB) |
| Limit | Up to 50 tags per weblog |

Registered tags are injected automatically on **every page type** — entry lists, single entries, categories, archives, and profiles. Changes take effect immediately on the dynamic site; a static rebuild is required if you use `task build-site`.

> **Security note:** values are emitted as raw HTML. Because only admin-level operators can edit them, the XSS surface is limited to trusted users.

## Template import and export

You can import an SB3-style `template.txt`. Older character encodings (Shift_JIS, EUC-JP, ISO-2022-JP) are auto-converted to UTF-8 during import.

Export uses the same `template.txt` format — useful for backups or moving a template to another instance.

## Related pages

- [Template editing](templates)
- [Publishing and screen settings](settings-publishing)
- [Library](images)
