---
title: Migrating from SB2 / SB3 and feature differences
slug: sb3-migration
order: 150
---

# Migrating from SB2 / SB3 and feature differences

You can import data from a Perl-based Serene Bach 2 data directory or a Serene Bach 3 SQLite database into the Go port. Not every feature ports one-to-one, so plan to verify entries, categories, and templates after migration.

## Before you start

Have these ready:

- From SB3: the SQLite database, ideally with the surrounding `data/` directory
- From SB2: the entire `data/` directory (the one with `configure.cgi`, `entry/`, `category.cgi`, etc.)
- The Go port binary
- A fresh database to import into
- Whatever images and template files you used in the original Serene Bach

Always work from a copy. Never import directly against your production data.

## Basic flow

First, set up the destination database with an admin user and the default template. Skipping the sample entries makes verification easier:

```bash
SB_SEED_NO_SAMPLES=1 ./serenebach seed
```

### Importing from SB3 (SQLite)

```bash
./serenebach import /path/to/sb3.db
```

Omitting `--sb-version` (or passing `--sb-version 3` explicitly) selects the SB3 reader.

### Importing from SB2 (flat-file)

```bash
./serenebach import --sb-version 2 /path/to/sb2/data
```

The path argument is the SB2 `data/` directory itself — the one that directly contains `configure.cgi` and the `entry/` directory. The SB2 path also imports comments; the SB3 path currently does not.

When import completes, you'll see counts and warnings. Warnings flag template tags or other constructs that need attention.

### About `configure.cgi`

The URL conventions for both SB2 and SB3 (the `/blog/log/eid42.html`-style permalinks for instance) live in the flat-file `configure.cgi` (admin-edited settings) and `init.cgi` (install-time settings) inside the source `data/` directory — not in the SQLite database.

- For SB3, the importer automatically checks the parent of the SQLite path for these files. Copy the whole `data/` directory and point the importer at the `data.db` inside it.
- For SB2, the data directory you pass on the command line is read directly.

Without `configure.cgi`, the import falls back to defaults — fine for a root-mounted blog, but legacy URL redirects break for blogs hosted under a sub-path (e.g. `https://example.com/sb/`).

## What gets imported

| Data | Notes | SB2 | SB3 |
|---|---|---|---|
| Blog settings | Title, description, URL, language, etc. | ✓ | ✓ |
| Categories | Including parent/child relationships | ✓ | ✓ |
| Published entries | Body, "more", timestamps, category | ✓ | ✓ |
| Keywords / Tags | Per-entry keywords + tag creation | keywords only | ✓ (auto-tagged) |
| Comments | Author name, body, date, IP, etc. | ✓ | — |
| Templates | Main HTML, CSS, individual-entry HTML | ✓ | ✓ |

Imported templates are not activated automatically. Review them on the templates screen and activate when you're ready.

## What does not get imported

| Data | Reason / what to do |
|---|---|
| Users | Different password hash format. Re-create users in the Go port |
| Drafts and closed entries | The standard import covers published entries only (SB2: stat 1 / 2; SB3: stat 1) |
| Trackbacks | Not supported in the Go port |
| Images | Move the files manually and re-register them in the image library if needed |
| Plugins | Perl plugins do not run in the Go port |
| Amazon-related features | Not supported in the Go port |
| Links (SB2 link.cgi) | Re-create from the admin UI |

## Reviewing templates

The Go port aims for a high level of compatibility with SB2 / SB3 templates, but it doesn't cover every tag.

Supported, broadly:

- Basic entry, category, and archive layouts
- Profile pages
- Comment forms
- Recent entries, recent comments, category lists, monthly archives
- SB3-style date format tokens

Not supported:

- Trackback-related tags
- Amazon-affiliate tags
- Mobile-only output
- Some "recommended" / "selected entry" style tags
- Anything that depended on SB3 plugins

After import, the editor flags unsupported or behaviourally different tags so you can adjust them before publishing.

## URL compatibility

The Go port redirects these legacy SB2 / SB3 URL shapes to their canonical counterparts:

- Dynamic URLs like `sb.cgi?eid=N` → 301 to the entry page
- Static archive URLs like `log/eid42.html` → 301 to the entry page
- Category directory URLs → 301 to the category page

Static-archive redirects only work when `configure.cgi` was readable at import time (see "About `configure.cgi`" above). If your blog ran under a sub-path and you want those legacy URLs to keep resolving, copy the entire `data/` directory before importing.

Coverage isn't complete for every hand-written or external URL — open the important pages after migration to confirm.

## Migrating images

Image files are not imported automatically. Copy them onto a path the public site can serve.

For images you want to manage going forward through the image library, re-upload them from the **Images** screen. Existing entries that reference images by direct path will continue to render as long as those paths resolve.

## Character-encoding auto-detection

SB2 typically ran in EUC-JP and SB3 typically in UTF-8, but operators frequently mixed them. The importer treats encoding as content-detected, not version-derived: each record file (entry, comment, template, configure.cgi) goes through Content-Type hint → HTML/CSS charset declaration → ISO-2022-JP escape sniff → UTF-8 validity → Shift_JIS / EUC-JP byte-distribution score before the bytes land in the destination database. A SB2 deployment that ran in UTF-8 (or a SB3 deployment that ran in EUC-JP) is detected just as reliably.

## SB2 / SB3 vs the Go port — main differences

| Item | SB2 / SB3 | Go port |
|---|---|---|
| Runtime | Perl / CGI | Go single binary, server or CGI |
| Storage | SB2: flat-file, SB3: SQLite | SQLite |
| Templates | SB templates | Largely SB-compatible templates |
| Entry formats | HTML, SB lightweight syntax | HTML, Markdown, sbtext |
| Reader replies | Comments + trackbacks | Comments only — trackbacks unsupported |
| Image management | Upload management | Image library, thumbnails, OG card backgrounds |
| Static rebuild | Supported | Supported, also outputs images and template assets |
| XML-RPC | Supported | RSD endpoint exists; XML-RPC itself is not implemented |
| AI integration | None | MCP, llms.txt, in-admin writing assist |
| Plugins | Perl plugins | No Perl plugins |

## Post-migration checklist

1. Confirm entry and category counts.
2. Open the homepage, an entry page, a category page, and an archive page on the public site.
3. Review template warnings and fix unsupported tags.
4. Verify that image paths resolve.
5. Re-check comment-acceptance, blog URL, and OG card settings.
6. Re-create the users you need.

## Related pages

- [Template editing](templates)
- [Library](images)
- [Blog design and OG cards](design)
