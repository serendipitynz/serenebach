---
title: Static rebuild and publishing
slug: rebuild-publishing
order: 110
---

# Static rebuild and publishing

Serene Bach supports both modes: dynamic rendering on each request, and static publishing where HTML is written ahead of time and served from disk.

The default dynamic mode is fine for most blogs. Static rebuild is useful for high-traffic sites, CDN-fronted deployments, or anywhere you want the public side as light as possible.

## Rebuilding from the admin UI

The **Rebuild** screen has a **Rebuild now** button that writes the full public site.

The output directory is set by `SB_REBUILD_OUT`. The default is `./data/public`.

If a rebuild is already running and you click again, the in-flight rebuild wins.

## Rebuilding from the command line

You can also rebuild from the command line:

```bash
./serenebach build --out=./public
```

To change the listing page size, pass `--limit`:

```bash
./serenebach build --out=./public --limit=20
```

## What gets written

A rebuild produces:

- Homepage
- Entry pages
- Category, tag, and archive pages
- RSS / Atom feeds
- llms.txt and llms-full.txt
- The active template's CSS
- Uploaded images
- Template assets

The admin UI, login screen, and MCP endpoint are **not** included in the static output. The dynamic Serene Bach process is still required for those.

## Example: serving statically

The output directory plugs into Nginx, Apache, Cloudflare Pages, S3-compatible storage, etc.

Nginx example:

```nginx
server {
    listen 80;
    root /var/www/html;
    try_files $uri $uri/ =404;
}
```

## When to rebuild

Saving an entry doesn't update already-written HTML. After editing entries or settings, rerun the rebuild.

A cron job is a common solution:

```cron
0 * * * * cd /var/lib/serenebach && ./serenebach build --out=/var/www/html
```

## Sitemap / robots.txt

Serene Bach automatically generates `sitemap.xml` and `robots.txt` for search engines.

### sitemap.xml

`/sitemap.xml` includes the following URLs:

- Home `/`
- Published entries `/entry/<slug>/`
- Non-hidden categories `/category/<slug>/`
- Tags `/tag/<slug>/`
- Published flat pages

Monthly / yearly archives, profile pages, RSS/Atom feeds, and llms.txt files are **not** included.

### robots.txt

`/robots.txt` allows all crawlers (`Allow: /`) and includes a `Sitemap:` line pointing to `sitemap.xml`.

### Enable / Disable

You can toggle each file independently in Admin "Settings > Basic settings". When disabled, the route returns **404** (not an empty file).

### Static rebuild

When you run `task build-site` or have "auto-rebuild on publish" enabled, `sitemap.xml` and `robots.txt` are written to the static output directory. If you turn either toggle off and rebuild again, the stale file is automatically removed.

### Google Search Console

1. Add your property at [Google Search Console](https://search.google.com/search-console).
2. Use the "Sitemaps" menu to submit `https://<your-domain>/sitemap.xml`.

## Related pages

- [Publishing and screen settings](settings-publishing)
- [Library](images)
- [Preview mode](preview)
