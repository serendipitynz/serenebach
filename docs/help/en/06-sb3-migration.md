---
title: Migrating from SB3 and feature differences
slug: sb3-migration
order: 60
---

# Migrating from SB3 and feature differences

You can import data from a Perl-based Serene Bach 3 SQLite database into the Go port. Not every feature ports one-to-one, so plan to verify entries, categories, and templates after migration.

## Before you start

Have these ready:

- The SB3 SQLite database
- The Go port binary
- A fresh database to import into
- Whatever images and template files you used in SB3

Always work from a copy. Never import directly against your production data.

## Basic flow

First, set up the destination database with an admin user and the default template. Skipping the sample entries makes verification easier:

```bash
SB_SEED_NO_SAMPLES=1 ./serenebach seed
```

Then import from the SB3 database:

```bash
./serenebach import /path/to/sb3.db
```

When import completes, you'll see counts and warnings. Warnings flag template tags or other constructs that need attention.

## What gets imported

| Data | Notes |
|---|---|
| Blog settings | Title, description, URL, language, etc. |
| Categories | Including parent/child relationships |
| Published entries | Body, "more", timestamps, category, keywords, etc. |
| Tags | Created from SB3 keywords |
| Templates | Main HTML, CSS, individual-entry HTML |

Imported templates are not activated automatically. Review them on the templates screen and activate when you're ready.

## What does not get imported

| Data | Reason / what to do |
|---|---|
| Users | Different password hash format. Re-create users in the Go port |
| Drafts and closed entries | The standard import covers published entries only |
| Comments | Not currently supported |
| Trackbacks | Not supported in the Go port |
| Images | Move the files manually and re-register them in the image library if needed |
| Plugins | Perl plugins do not run in the Go port |
| Amazon-related features | Not supported in the Go port |

## Reviewing templates

The Go port aims for a high level of compatibility with SB3 templates, but it doesn't cover every tag.

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

Some legacy SB3 URLs are redirected to their Go-port equivalents to keep old links working. Coverage isn't complete for every hand-written or external URL — open the important pages after migration to confirm.

## Migrating images

Image files are not imported automatically. Copy them onto a path the public site can serve.

For images you want to manage going forward through the image library, re-upload them from the **Images** screen. Existing entries that reference images by direct path will continue to render as long as those paths resolve.

## SB3 vs the Go port — main differences

| Item | SB3 | Go port |
|---|---|---|
| Runtime | Perl / CGI | Go single binary, server or CGI |
| Database | SQLite | SQLite |
| Templates | SB3 templates | Largely SB3-compatible templates |
| Entry formats | HTML, SB3 lightweight syntax | HTML, Markdown, sbtext |
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
- [Image uploads](images)
- [Publishing settings and OG cards](settings-publishing)
