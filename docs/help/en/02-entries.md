---
title: Writing and managing entries
slug: entries
order: 20
---

# Writing and managing entries

Create entries from **New Entry** and edit them from **Entries**. You can leave older HTML entries as-is and write new ones in Markdown — formats are chosen per entry.

## Body formats

| Format | Description | When to use |
|---|---|---|
| HTML | Renders the HTML you typed verbatim | Migrating older entries, fine-grained markup |
| Markdown | Headings, links, tables, etc. via Markdown syntax | Day-to-day writing |
| sbtext | Limited subset of the SB3 lightweight syntax | Displaying entries imported from SB3 |

HTML is the most flexible but the markup you type ships straight to the public page. If you let untrusted contributors author entries, prefer Markdown.

## Body and "more"

The body is the main content shown on the entry page and (in part) on listings.

The "more" section holds a longer continuation that's revealed on the individual entry page. It corresponds to SB3's "entry continuation". Templates with a sequel block render this only for entries that have a "more" payload.

## Main fields

| Field | Description |
|---|---|
| Title | Used in listings, the entry page, feeds, and the OG card |
| Category | The entry's primary classification. One per entry |
| Tags | Cross-cutting labels for browsing. Multiple per entry |
| Status | Draft / Published / Closed |
| Posted at | The timestamp shown on the entry. Future dates are allowed |
| Slug | A short URL component used in place of the numeric ID |
| Keywords | Used for `<meta name="keywords">` in supported templates |
| OG card background | Override the social-share card background per entry. After saving, use the **Generate OG card** button to rebuild it manually |

## Entry status

| Status | Behaviour on the public site |
|---|---|
| Draft | Hidden from the public site. Previewable from the admin UI |
| Published | Visible on the public site, in feeds, and (when enabled) in llms.txt |
| Closed | Hidden from the public site. Use this to retract a previously published entry |

If you set a future posted date on a published entry, the entry uses that timestamp. When publishing statically, run a rebuild at the time you want the entry to surface.

## Slug

A slug lets the entry URL use a short readable name instead of the numeric ID.

Example: `my-first-post`

Allowed characters are lowercase letters, digits, and hyphens. Non-ASCII slugs are not supported. When the slug is empty, the URL falls back to the numeric ID.

## Inserting images

Use **Insert image** in the editor toolbar to drop in an image you've already uploaded. Dragging an image file directly onto the body area uploads and inserts it in one step.

HTML entries get an `<img>` tag; Markdown entries get the Markdown image syntax.

## Flat pages

**Flat pages** are standalone content pages independent of entries. Use them for `/about`, `/privacy`, or any page that should not appear in listings, archives, or feeds.

Flat page characteristics:

- Not shown on the home page, category pages, archives, or feeds
- No categories, tags, comments, or "more" section
- Custom URL path (e.g. `/about`, `/service/pricing`)
- Per-page template selection (falls back to the active template)
- OG cards can be generated just like entries

Paths may contain lowercase letters, digits, hyphens, and slashes. System-reserved paths such as `/entry` and `/admin` are rejected.

## Pinned entries

Tick **Pinned** in the entry editor to float the entry to the top of the home page and category archives. Use it for announcements, featured posts, or anything you want to keep visible.

Where pinning takes effect:

- Home page and category archive: pinned entries float to the top
- Monthly and tag archives, feeds (RSS / Atom): **no effect** (normal date order)
- Individual entry page, prev / next navigation: no effect

When several entries are pinned, they appear at the top in the usual newest-first order, followed by the rest of the listing in the usual date order.

Pinned entries always group at the very front of the listing. If you pin more entries than fit on a single page, the overflow continues onto page 2 and beyond. In practice, keep the number of pinned entries within one page worth of entries.

For templates, pinned entries expose the `{entry_pinned}` tag (value `pinned` or empty) and the `pinned_entry` block. See [Templates](templates) for details.

## Entry list

The **Entries** list supports title search and status filtering. Click a title to open the editor. The arrow opens the public page in a new tab for published entries.

## Related pages

- [Categories and tags](categories-tags)
- [Library](images)
- [Preview mode](preview)
