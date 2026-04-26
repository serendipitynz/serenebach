---
title: Static rebuild and publishing
slug: rebuild-publishing
order: 120
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

## Related pages

- [Publishing settings and OG cards](settings-publishing)
- [Image uploads](images)
- [Preview mode](preview)
