# Importing from legacy Serene Bach (SB2 / SB3)

If you have a production Serene Bach 2 or 3 (Perl) installation on hand, you can migrate the content directly. The importer dispatches on `--sb-version`; SB3 (default) reads the SQLite database, SB2 reads the flat-file `data/` directory.

## Procedure — SB3 (SQLite)

```bash
# prepare a destination that has an admin user but no demo entries
task clean
SB_SEED_NO_SAMPLES=1 task seed

# import weblog / categories / templates / entries
task import -- /path/to/your/sb3.db
```

The importer also auto-detects your SB3 data directory by looking at the parent (and grandparent) of the SQLite path for `configure.cgi` and `init.cgi`. Those files carry the URL-shaping settings the legacy redirect layer needs — you'll get the best result if you copy the whole SB3 `data/` directory and point the importer at the `data.db` inside it.

## Procedure — SB2 (flat-file)

```bash
SB_SEED_NO_SAMPLES=1 task seed
task import -- --sb-version 2 /path/to/your/sb2/data
```

The path argument is the SB2 `data/` directory itself — the one that contains `configure.cgi`, `entry.cgi`, `entry/`, `category.cgi`, `message.cgi`, `message/`, `template.cgi`, `template/`, and so on. The importer walks each per-id detail file (e.g. `entry/123.cgi`) rather than relying on the abbreviated index files, so every field the destination schema cares about is preserved.

The SB2 path also imports comments (Message records) — the SB3 path currently does not. Drafts and future-scheduled entries are dropped unless you pass an explicit override.

## What gets imported

- Weblog title + description, base URL, language
- Every category, parent–child structure preserved
- Every template (added as inactive so your current default stays active until you flip it explicitly)
- Every published entry (body, "more", timestamps, category, keywords)
- Tags created from SB3 keyword data

## What does *not* get imported

- **Users** — crypt-hashed passwords are not bcrypt-compatible. The seeded admin user takes over authorship attribution. Re-create any other accounts you need.
- **Drafts and closed entries** — the standard import covers published entries only.
- **Comments** — SB2 import: yes. SB3 import: not currently supported.
- **Trackbacks** — not supported in the Go port at all.
- **Images** — copy the files manually; re-register through the image library if you want them to appear in the admin picker.
- **Plugins** — Perl plugins do not run in the Go port.
- **Amazon-related features** — not supported in the Go port.
- **Links / blogroll** — SB2's `link.cgi` is not imported; re-create via the admin UI. (SB3's blogroll structure differs and isn't imported either.)

## Character-encoding handling

SB2 typically stored content in EUC-JP. SB3 templates are sometimes Shift_JIS, EUC-JP, or ISO-2022-JP. The importer auto-detects in five passes (Content-Type charset hint → HTML `<meta charset>` / CSS `@charset` probe → ISO-2022-JP escape sniff → UTF-8 validity → Shift_JIS/EUC-JP byte-distribution score) and re-encodes everything to UTF-8 before storing. The same path is used for both SB2 record files and SB3 template bundles, so a SB2 deployment that ran in UTF-8 (or a SB3 deployment that ran in EUC-JP) is detected just as reliably as the version's "standard" encoding.

## URL-compatibility shims

After import, the Go binary accepts a few legacy URL shapes so old inbound links keep resolving:

- `/sb.cgi?mode=entry/category/archive/user` → 301 to the canonical path
- `/sb.cgi?mode=comment` → 307 forwards the POST body to `/entry/{id}/comment` so imported comment forms keep posting
- Static archive URLs — `/<base>/<log>/<prefix><N><suffix>` (the classic `/blog/log/eid42.html` shape) → 301 to the canonical entry URL. Per-blog values (`<base>`, `<log>`, `<prefix>`, `<suffix>`) come from `configure.cgi`, so blogs hosted under a sub-path (e.g. `https://example.com/sb/`) keep their inbound links only when that file is present alongside the imported `data.db`.
- `/profile/{id}/` populates SB3's `profile_area` block
- `{site_rsd}` / `/rsd.xml` is served (the discovery XML works, even though the underlying XML-RPC API is not implemented)

See `docs/architecture.md` for the broader template-tag compatibility list — the import phase surfaces lint warnings for unsupported tags, but the actual rendering policy lives there.

## Post-import checklist

1. Confirm entry and category counts in `/admin/entries` and `/admin/categories` match the source DB.
2. Open the homepage, an entry page, a category page, and an archive page on the public site.
3. Visit each imported template's editor to review the lint warnings, then activate the one you want.
4. Verify image paths resolve — copy `<sb3_root>/upload` next to `SB_IMAGE_DIR` if you want broken legacy `<img>` tags to start rendering again.
5. Re-check comment-acceptance, blog URL, and OG card defaults.
6. Re-create users.
