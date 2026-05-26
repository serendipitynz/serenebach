---
title: Sitemap / robots.txt
slug: sitemap-robots
order: 16
---

# Sitemap / robots.txt

Serene Bach automatically generates `sitemap.xml` and `robots.txt` for search engines.

## sitemap.xml

`/sitemap.xml` includes the following URLs:

- Home `/`
- Published entries `/entry/<slug>/`
- Non-hidden categories `/category/<slug>/`
- Tags `/tag/<slug>/`
- Published flat pages

Monthly / yearly archives, profile pages, RSS/Atom feeds, and llms.txt files are **not** included.

## robots.txt

`/robots.txt` allows all crawlers (`Allow: /`) and includes a `Sitemap:` line pointing to `sitemap.xml`.

## Enable / Disable

You can toggle each file independently in Admin "Settings > Site Settings". When disabled, the route returns **404** (not an empty file).

## Static Rebuild

When you run `task build-site` or have "auto-rebuild on publish" enabled, `sitemap.xml` and `robots.txt` are written to the static output directory. If you turn either toggle off and rebuild again, the stale file is automatically removed.

## Google Search Console

1. Add your property at [Google Search Console](https://search.google.com/search-console).
2. Use the "Sitemaps" menu to submit `https://<your-domain>/sitemap.xml`.
