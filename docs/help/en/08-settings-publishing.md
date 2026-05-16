---
title: Publishing settings and OG cards
slug: settings-publishing
order: 80
---

# Publishing settings and OG cards

The **Settings** screen is where you change the blog name, description, URL, in-admin appearance, and AI configuration. The visible tabs depend on your role.

## Screen settings

These are per-user admin UI preferences:

- Appearance: Auto / Light / Dark
- Display language: Japanese / English

They affect how the admin UI looks for you, not the public site.

## Basic settings

Basic settings cover blog-wide values that affect the public site. Available to admin and power users.

| Field | Description |
|---|---|
| Blog title | Used as the site name |
| Description | Used in site descriptions, feeds, and OG description |
| Base URL | Used to build absolute URLs in feeds and OG cards |
| Language | Drives the public-page language and reader-facing messages |
| llms.txt | Whether to expose the AI-agent-friendly text endpoint |

For a public blog, set the base URL. Without it, feed and social-share URLs may not resolve correctly.

The OG card defaults (background image and text colour) live under **Design > OG Card**. See [OG cards](#og-cards) below.

## llms.txt

When enabled, llms.txt publishes a Markdown view of the public entries that AI agents can ingest:

- `/llms.txt`: index of entries
- `/llms-full.txt`: full text of published entries

Leave it disabled if you don't want this. Both URLs return 404 in that case.

## OG cards

OG cards are the images social platforms show when someone shares an entry URL. Serene Bach generates a 1200 x 630 image per entry.

The blog-wide defaults (background image and text colour) are edited under **Design > OG Card** in the left sidebar. Per-entry overrides are set on the entry editor.

The background is resolved in this order:

1. Entry-level OG background (set in the entry editor)
2. Blog-wide OG background (Design > OG Card)
3. The bundled default

Backgrounds are picked from the image library. Off-aspect images are centre-cropped.

The text colour applies to both the entry title and the blog title. When unset, the standard two-tone defaults apply. Tick "Hide text" if your background image already includes its own typography.

## AI settings

The AI Settings tab covers the in-admin AI writing assist plus MCP access tokens.

The writing assist requires `SB_AI_SECRET` on the server. Each user can then register a provider and API key for themselves.

Admin users can also issue and revoke MCP access tokens from this screen.

## Design settings

The **Design** screen carries cross-template rendering knobs:

- Templates used for archive / category / tag listings
- Template used for the profile page
- Number of entries per listing page
- Sort order for entries
- Sort order for comments
- Date format

Date formats use SB3-style tokens such as `%Year%`, `%Mon%`, `%Day%`, `%Week%`, `%Hour%`, `%Min%`. Example: `%Year%-%Mon%-%Day% (%Week%)`

## Related pages

- [Template editing](templates)
- [AI integration and MCP](mcp)
- [Image uploads](images)
