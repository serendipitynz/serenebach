# Importing from a directory of markdown files

The markdown importer takes a directory of `*.md` files, each with a YAML front-matter envelope, and upserts the contents into the destination SereneBach database as entries. It is one of the three source modes the `import` subcommand supports (`md`, `sb-version=2`, `sb-version=3`).

The typical use case is a static-site-style content workflow on top of SereneBach: write release notes / news posts as markdown, run the importer, then run `task build-site` to produce static HTML for upload to a CDN. Comments, likes, stamps, and the admin UI keep working for the parts of the deployment that stay dynamic.

## Quick start

```bash
mkdir -p ./content/news
cat > ./content/news/hello-world.md <<'EOF'
---
title: "Hello, World"
status: published
posted_at: 2026-05-18T12:00:00+09:00
---

# Hello

Body markdown goes here.
EOF

task import-md -- ./content/news
task build-site
```

The dev database at `./data/dev.db` will now contain the entry. The static rebuild writes the HTML and OG card into `./data/public/`.

## Front-matter schema

YAML, fenced by `---` lines at the top of the file:

```yaml
---
slug: hello-world
title: "Hello, World"
status: published
posted_at: 2026-05-18T12:00:00+09:00
category: en
keywords: "intro,hello"
pinned: false
more: ""
---
```

| Field      | Required | Type             | Notes                                                                                                              |
|------------|----------|------------------|--------------------------------------------------------------------------------------------------------------------|
| `slug`     | No\*     | string           | Upsert key + URL slug. Format: `^[a-z0-9]+(-[a-z0-9]+)*$`. **Defaults to the filename basename when omitted**.    |
| `title`    | **Yes**  | string           | Entry title                                                                                                        |
| `status`   | No       | enum             | `published` (default) / `draft` / `closed`                                                                         |
| `posted_at`| No       | RFC3339 string   | Defaults to the file mtime                                                                                          |
| `category` | No       | string           | Existing `categories.slug`. Unknown values warn and fall through to uncategorised — **categories are not auto-created** |
| `keywords` | No       | string           | Comma-separated SEO meta keywords                                                                                  |
| `pinned`   | No       | bool             | Float to top of home and category list page 1                                                                      |
| `more`     | No       | string           | Sequel body (rarely needed)                                                                                        |

\* `slug` is technically optional but a slug *must* come from somewhere — either the front-matter field, or a filename that already matches the slug format. If both are absent the file is skipped with a warning.

## Filename = implicit slug

Files are sorted by name, then read top-level only (subdirectories are **not** traversed). For each file:

1. If `slug` is set in front-matter and valid, that wins.
2. Otherwise the filename (without `.md`) is used, provided it matches the slug format.
3. Otherwise the file is skipped and the importer prints a hint asking you to either fix the filename or set `slug:` explicitly.

This makes most release-note-style content work with **just `title`** in the front-matter, because a file named `v4-0-0-beta-11-en.md` already carries its own slug. The front-matter `slug` field is the escape hatch for cases where the filename can't follow slug rules (Japanese filenames, etc.).

## Upsert behaviour

The importer reads `entries` by `(wid, slug)`. If a row exists, the title / body / status / category / keywords / pinned fields are refreshed and `updated_at` bumped; `posted_at` and `created_at` are preserved so re-importing a file doesn't shuffle archive boundaries.

If no row exists, a new entry is inserted with `format = 'markdown'`.

**Entries the importer doesn't see are left alone.** Deleting a markdown file from your content directory does *not* delete the corresponding entry. If you want to retire a post, set `status: closed` in the markdown and re-import, or delete it via the admin UI.

## OG card generation

When the binary has `SB_IMAGE_DIR` (or the underlying `Config.ImageDir`) set, the importer writes one OG card PNG per imported entry into `<ImageDir>/og/<entry_id>.png`. This matches the location the admin's `regenerateOGCard` writes to and the location `task build-site` picks files up from.

A render failure for one entry logs a warning and continues with the next one — the DB row is never rolled back over an OG glitch.

When `SB_IMAGE_DIR` is unset (typical for the dev workflow), the markdown import still succeeds and the OG step is silently skipped. The site can still be served; the `<meta property="og:image">` references will 404 until you either set `SB_IMAGE_DIR` and re-import, or trigger the admin "Regenerate OG card" action.

## Operational notes

- **Categories are not auto-created.** Create the categories you want to use via the admin UI (or the seed step) before importing. Unknown category slugs warn and fall through to "uncategorised" so the import still completes.
- **Single weblog assumed.** The importer targets `weblog_id = 1` unless you reach in and pass a different `Options.TargetWID` programmatically. The multiblog feature is not in scope.
- **Author defaults to user 1.** Same caveat as the SB importer.
- **`format` is set to `markdown` on both insert and update.** The body is stored as the raw markdown source; goldmark renders it on every public-page request and during the static rebuild.

## Errors and warnings

The importer's philosophy is *"one file's mistake doesn't stop the run"*: per-file problems print to stderr as `import: warning: …` lines, but the rest of the directory is processed normally. The only hard-stop is a DB write failure, which rolls back the entire transaction.

Common warnings:

| Warning                                              | What to do                                                          |
|------------------------------------------------------|---------------------------------------------------------------------|
| `front-matter \`title\` missing or empty`            | Add `title: "..."` to the YAML                                      |
| `filename is not a valid slug…`                      | Either rename the file to match `[a-z0-9-]` or add `slug:` to YAML  |
| `front-matter \`slug\` "..." is not a valid slug`    | Fix the slug to match `[a-z0-9-]`, 1-100 chars                       |
| `duplicate slug …`                                   | Two files map to the same slug; rename one or override with `slug:` |
| `category "..." not found in destination`            | Create the category in the admin UI, or remove the field            |
| `front-matter \`posted_at\` … is not RFC3339`        | Use a value like `2026-05-18T12:00:00+09:00`; falls back to mtime   |
