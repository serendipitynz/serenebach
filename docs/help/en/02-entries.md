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

## Entry list

The **Entries** list supports title search and status filtering. Click a title to open the editor. The arrow opens the public page in a new tab for published entries.

## Related pages

- [Categories and tags](categories-tags)
- [Image uploads](images)
- [Preview mode](preview)
