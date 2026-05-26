---
title: Publishing and screen settings
slug: settings-publishing
order: 120
---

# Publishing and screen settings

The **Settings** screen is where you change the blog name, description, URL, in-admin appearance, and AI configuration. The visible tabs depend on your role.

OG card defaults, template switching, and other design-related options are gathered on the [Blog design and OG cards](design) page.

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
| sitemap.xml / robots.txt | Whether to expose `sitemap.xml` and `robots.txt` for search engines |

For a public blog, set the base URL. Without it, feed and social-share URLs may not resolve correctly.

See [Static rebuild and publishing](rebuild-publishing) for the details of `sitemap.xml` / `robots.txt`.

## llms.txt

When enabled, llms.txt publishes a Markdown view of the public entries that AI agents can ingest:

- `/llms.txt`: index of entries
- `/llms-full.txt`: full text of published entries

Leave it disabled if you don't want this. Both URLs return 404 in that case.

## AI settings

The AI Settings tab covers the in-admin AI writing assist plus MCP access tokens.

The writing assist requires `SB_AI_SECRET` on the server. Each user can then register a provider and API key for themselves.

Admin users can also issue and revoke MCP access tokens from this screen. See [AI integration and MCP](mcp) for details.

## Related pages

- [Blog design and OG cards](design)
- [AI integration and MCP](mcp)
- [Library](images)
