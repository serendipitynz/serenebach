# Importing from legacy Serene Bach v3

If you have a production Serene Bach 3 (Perl) SQLite database on
hand, you can migrate the content directly.

## Procedure

```bash
# prepare a destination that has an admin user but no demo entries
task clean
SB_SEED_NO_SAMPLES=1 task seed

# import weblog / categories / templates / entries
task import -- /path/to/your/sb3.db
```

## What gets imported

- Weblog title + description, base URL, language
- Every category, parent–child structure preserved
- Every template (added as inactive so your current default stays
  active until you flip it explicitly)
- Every published entry (body, "more", timestamps, category,
  keywords)
- Tags created from SB3 keyword data

## What does *not* get imported

- **Users** — crypt-hashed passwords are not bcrypt-compatible.
  The seeded admin user takes over authorship attribution. Re-create
  any other accounts you need.
- **Drafts and closed entries** — the standard import covers
  published entries only.
- **Comments** — not currently supported.
- **Trackbacks** — not supported in the Go port at all.
- **Images** — copy the files manually; re-register through the
  image library if you want them to appear in the admin picker.
- **Plugins** — Perl plugins do not run in the Go port.
- **Amazon-related features** — not supported in the Go port.

## Character-encoding handling

SB3 templates often live in Shift_JIS, EUC-JP, or ISO-2022-JP. The
importer auto-detects these in three passes (Content-Type charset
hint → HTML `<meta charset>` / CSS `@charset` probe → byte-
distribution scoring) and re-encodes everything to UTF-8 before
storing. Failures fall back to UTF-8 with a warning rather than
panicking.

## URL-compatibility shims

After import, the Go binary accepts a few legacy URL shapes so old
inbound links keep resolving:

- `/sb.cgi?mode=entry/category/archive/user` → 301 to the canonical
  path
- `/sb.cgi?mode=comment` → 307 forwards the POST body to
  `/entry/{id}/comment` so imported comment forms keep posting
- `/profile/{id}/` populates SB3's `profile_area` block
- `{site_rsd}` / `/rsd.xml` is served (the discovery XML works,
  even though the underlying XML-RPC API is not implemented)

See `docs/architecture.md` for the broader template-tag
compatibility list — the import phase surfaces lint warnings for
unsupported tags, but the actual rendering policy lives there.

## Post-import checklist

1. Confirm entry and category counts in `/admin/entries` and
   `/admin/categories` match the source DB.
2. Open the homepage, an entry page, a category page, and an
   archive page on the public site.
3. Visit each imported template's editor to review the lint
   warnings, then activate the one you want.
4. Verify image paths resolve — copy `<sb3_root>/upload` next to
   `SB_IMAGE_DIR` if you want broken legacy `<img>` tags to start
   rendering again.
5. Re-check comment-acceptance, blog URL, and OG card defaults.
6. Re-create users.
