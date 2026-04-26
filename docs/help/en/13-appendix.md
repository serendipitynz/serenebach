---
title: Appendix
slug: appendix
order: 200
---

# Appendix

Operational notes you'll consult periodically.

## Backups

The bulk of Serene Bach's state lives in three places: the SQLite database, the image directory, and the template asset directory.

At minimum, back up these three:

- The SQLite database
- The contents of `SB_IMAGE_DIR`
- The contents of `SB_TEMPLATE_DIR`

A naive copy of a live SQLite file can be inconsistent. Use SQLite's `.backup` instead:

```bash
sqlite3 /var/lib/serenebach/blog.db ".backup /backup/blog.db"
```

The image and template directories can be backed up with a regular file copy or `rsync`.

## Common environment variables

| Variable | Description |
|---|---|
| `SB_DB` | Path to the SQLite database |
| `SB_ADMIN_NAME` | Admin user name created during seed |
| `SB_ADMIN_PASSWORD` | Admin password created during seed |
| `SB_ADMIN_EMAIL` | Admin email created during seed |
| `SB_SEED_NO_SAMPLES` | Set to `1` to skip sample entries during seed |
| `SB_IMAGE_DIR` | Where uploaded images are stored |
| `SB_TEMPLATE_DIR` | Where template assets are stored |
| `SB_REBUILD_OUT` | Where the static rebuild writes output |
| `SB_UPLOAD_MAX_MB` | Per-file image upload size cap |
| `SB_TURNSTILE_SITEKEY` | Cloudflare Turnstile site key |
| `SB_TURNSTILE_SECRET` | Cloudflare Turnstile secret |
| `SB_ANALYTICS_DISABLED` | Set to `1` to stop recording analytics |
| `SB_ANALYTICS_DB` | Path to a separate analytics SQLite database |
| `SB_ANALYTICS_RETENTION_DAYS` | How many days of analytics to keep |
| `SB_AI_SECRET` | Master secret for encrypting AI configuration API keys |
| `SB_MCP_AUDIT_DB` | Path to a separate SQLite file for the MCP write-tool audit log |
| `SB_TRUSTED_PROXIES` | CIDRs whose `X-Forwarded-For` headers are honoured (comma-separated) |
| `SB_PUBLIC_ALLOWED_ORIGINS` | Additional origins permitted on reader-facing POSTs (comments, likes, stamps) |

The admin Settings screen surfaces some of these values for reference.

## How HTML format is treated

Choosing HTML as the entry format ships your body verbatim onto the public page. This is consistent with SB3's behaviour.

It's convenient when only trusted users can author entries, but it's not appropriate for sites that accept entries from arbitrary contributors. In a multi-author setup, decide who gets entry-creation rights and which formats they're allowed to use.

## Logs

Serene Bach logs to standard error. Under a long-running supervisor, view logs through that supervisor:

```bash
journalctl -u serenebach
```

Under CGI, check the web server's error log instead.

## When you can't log in

If you've lost the admin password, take a backup first, then inspect the user data in the database.

If the deployment is fresh, you can rerun `seed` to recreate the initial admin user. Make sure rerunning won't disturb existing users or data first.

## When the OG card looks stale

Saving an entry usually refreshes its OG card image. If a social network is still showing an old card, it's likely the social network's own cache.

Changing the blog-wide OG background or text colour kicks off a regeneration that takes a moment. For static deployments, run a rebuild as well.

## Checking the version

The footer of the admin UI shows the Serene Bach version. Include it in bug reports.

## License

The Go port of Serene Bach is published under the MIT licence. See `LICENSE` in the repository for details.

## Related pages

- [Getting started](getting-started)
- [Migrating from SB3 and feature differences](sb3-migration)
- [Static rebuild and publishing](rebuild-publishing)
