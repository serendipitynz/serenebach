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

## Legacy URL redirects

After import, the Go binary translates inbound links from the previous SB2 / SB3 deployment to the current canonical paths. Two URL families are covered — dynamic `/sb.cgi?…` and static archive URLs — both driven by per-weblog settings recorded at import time.

If no SB import has run, or the importer could not read `configure.cgi` / `init.cgi`, `weblogs.legacy_*` stays empty and the redirect layer is off: requests pass through to the next handler. The shim never hijacks `/sb.cgi` on a fresh install.

### Dynamic: `/sb.cgi?…`

Mounted by `Handler.MountLegacy` (GET + POST). The shim translates the query string into the canonical URL and lets chi route from there — the legacy Perl dispatcher is not run.

| Request | Response | Destination |
| --- | --- | --- |
| `?eid=N` (no `mode`) | 301 | `/entry/{key}/` — `legacy_id` lookup against `entries.legacy_id` |
| `?cid=N` (no `mode`) | 301 | `/category/{key}/` — `legacy_id` lookup against `categories.legacy_id` |
| `?month=YYYYMM` (no `mode`) | 301 | `/archive/YYYY/MM/` (no DB lookup; format validation only) |
| `mode=entry&eid=N` | 301 | `/entry/{key}/` — same lookup as `?eid=` |
| `mode=category&cid=N` | 301 | `/category/{key}/` — same lookup as `?cid=` |
| `mode=archive&cond=YYYY` | 301 | `/archive/YYYY/` |
| `mode=archive&cond=YYYYMM` | 301 | `/archive/YYYY/MM/` |
| `mode=user&pid=N` | 301 | `/profile/N/` — id passthrough (user import is out of scope; see *Known limitations*) |
| `mode=comment&eid=N` — POST | **307** | `/entry/N/comment` — method + body preserved so the canonical handler still owns CSRF / Turnstile / spam checks. `eid` is passthrough here, not a `legacy_id` lookup, because the URL only appears in imported templates and goes away once the template is replaced |
| `mode=comment&eid=N` — GET | 301 | `/entry/N/#comment-form` |
| `mode=search&search=<term>` (or `mode=search&q=<term>`) | 301 | `/search?q=<term>` — term is URL-encoded; an empty term still 301s to bare `/search` |
| `?search=<term>` (no `mode`) | 301 | `/search?q=<term>` — SB3 native shape. `sb::App::Main` infers `srch` mode purely from the `search` query parameter (last-assignment-wins, so it overrides `?eid=` / `?cid=` / `?month=` when both are present) |
| `mode=page` | 301 | `/` |
| Unknown / empty `mode` | 301 | `/` (so a misconfigured imported template doesn't drop the reader off a cliff) |

The mount lives **outside** the CSRF middleware group on purpose — imported SB3 comment forms POST without a modern session token, and the destination handler validates from scratch. See `docs/architecture.md` for the broader public-POST design.

### Static: archive HTML and category directories

Implemented as a chi middleware (`Handler.LegacyStaticMiddleware`) that runs **before** `StripSlashes`. Off-pattern requests fall through to the next handler.

Three patterns are recognised, driven by `weblogs.legacy_*`:

| Pattern | Destination | Lookup |
| --- | --- | --- |
| `{base_path}{log_path}{id_prefix}{N}{suffix}` | `/entry/{key}/` | `entries.legacy_id = N` |
| `{base_path}{log_path}{name}{suffix}` | `/entry/{key}/` | `entries.legacy_file = name` (fallback when the id-prefix branch missed) |
| `{base_path}{category_dir}/` | `/category/{key}/` | `categories.legacy_dir = category_dir` |

Example: with `base_path=/blog/`, `log_path=log/`, `id_prefix=eid`, `suffix=.html`:

- `/blog/log/eid42.html` → `/entry/{key}/` (id 42)
- `/blog/log/launch.html` → `/entry/{key}/` (entry whose SB3 `entry_file` was `launch`)
- `/blog/photos/` → `/category/{key}/` (category whose SB3 `category_dir` was `photos/`)

A category dir equal to the global `log_path` is skipped so the bare archive root isn't claimed by a category match.

### Configuration sources

The importer assembles each weblog's URL-shape settings by overlaying four layers in increasing priority order, mirroring SB's own `sb::Config` load semantics. Each layer overlays only its non-empty values, so a later layer never blanks an earlier non-empty setting:

1. Built-in defaults — `archive_type=Individual`, `log_path=` (empty), `base_path=/`, `cgi_name=sb.cgi`, `id_prefix=eid`, `suffix=.html`.
2. `sb_config` rows from the source SQLite DB (SB3 only; SB2 has no equivalent table, so this layer is skipped).
3. `<data-dir>/init.cgi` — installation-time overrides.
4. `<data-dir>/configure.cgi` — admin-edited settings, the source of truth for most live blogs.

For SB3, `<data-dir>` is auto-detected by checking the parent and grandparent of the SQLite path for `configure.cgi` / `init.cgi`. For SB2, `<data-dir>` is the import path argument itself. If neither file is found, only layers 1 and 2 apply — the redirect layer still works for `/sb.cgi?...` dynamic shapes (defaults are good enough), but **static URL redirects effectively require `configure.cgi` to be present** because they depend on the operator's chosen `base_path` / `log_path` / `suffix`. The recommended import procedure is therefore to copy the whole legacy `data/` directory and point the importer at the `data.db` inside it (SB3) or at the directory itself (SB2).

Mapping from legacy keys to `weblogs.legacy_*` columns:

| Legacy key | Column | Notes |
| --- | --- | --- |
| `conf_entry_archive` | `legacy_archive_type` | `Individual` / `Monthly` / empty (dynamic only). Monthly disables static-HTML redirects — see *Known limitations* |
| `conf_dir_log` | `legacy_log_path` | Normalised to no leading slash + trailing slash (`log/`) |
| `conf_srv_base` (or `conf_srv_cgi` fallback) | `legacy_base_path` | Scheme/host stripped, normalised to leading + trailing slash (`/blog/`). The `conf_srv_cgi` fallback matches `sb::Config::verify_values` since SB2 deployments often only set the CGI URL |
| `basic_sb` | `legacy_cgi_name` | e.g. `sb.cgi` |
| `basic_preid` | `legacy_id_prefix` | Default `eid` |
| `basic_suffix` | `legacy_suffix` | Default `.html` |

### Per-entry / per-category lookup keys

Recorded at import time so the redirect layer can find the destination row without re-loading the full domain model:

| Column | Source | Notes |
| --- | --- | --- |
| `entries.legacy_id` | SB `entry_id` | SB ids are 0-based, so `0` is a valid value — never use it as a sentinel. NULL means "no legacy id recorded" |
| `entries.legacy_file` | SB `entry_file` | Custom save name; empty for default `eid{id}` runs |
| `categories.legacy_id` | SB `cat_id` | |
| `categories.legacy_dir` | SB `cat_dir` | Trailing slash included; empty for "no custom dir" |

All four columns are indexed (partial indexes excluding the empty / NULL sentinels) per `migrations/0033_legacy_url_compat.sql`.

### Known limitations

- **Monthly archive type is out of scope.** When `legacy_archive_type=Monthly`, the static-HTML branches are disabled. The legacy URL was `YYYYMM.html` with the per-entry anchor fragment living inside the page, and a 301 cannot recover that fragment.
- **Static URL redirects need `configure.cgi` (or `init.cgi`).** Without those, `base_path` / `log_path` / `suffix` fall back to built-in defaults that almost certainly don't match a real deployment's static layout. Dynamic `/sb.cgi?…` redirects keep working regardless.
- **`mode=user&pid=N` is id passthrough, not a lookup.** SB stored `crypt()` password hashes that are not bcrypt-compatible, so user accounts are intentionally not imported and there is no guarantee that an SB `user_id` equals a Go `user.id`. Imported templates that emit `{site_cgi}?mode=user&pid=N` still land on `/profile/N/`, which may or may not be the right person.
- **Comment-form `eid` is passthrough.** `mode=comment&eid=N` 307-forwards to `/entry/N/comment` directly. This means the rare external POST landing on a post-import legacy URL targets the *new* numeric id, not the SB legacy id. Accepted collateral — these URLs only realistically appear in not-yet-replaced imported templates.

### Related compatibility (not redirects)

- `/profile/{id}/` populates SB3's `profile_area` block.
- `{site_rsd}` / `/rsd.xml` is served (the discovery XML works, even though the underlying XML-RPC API is not implemented).

### Implementation pointers

| Layer | File |
| --- | --- |
| Dynamic redirect handler | `internal/handler/public/legacy_cgi.go` |
| Static redirect middleware | `internal/handler/public/legacy_static.go` |
| Mount + cached config | `internal/handler/public/public.go` (`MountLegacy`, `Handler.LegacyURL`) |
| Storage schema | `migrations/0033_legacy_url_compat.sql` |
| Storage accessors | `internal/storage/repo/legacy.go` |
| Importer config load (SB3) | `internal/importer/importer.go` (`loadLegacyConfig`, `applyLegacyConfig`) |
| Importer config load (SB2) | `internal/importer/sb2.go` (`applySB2WeblogAndConfig`) |
| Tests | `internal/app/profile_public_test.go` (`TestLegacyCGIRedirectsPerMode`, `TestLegacyStaticRedirects`, `TestLegacyCGICommentPostUses307`), `internal/app/public_route_precedence_test.go` (`TestPublicRouteLegacyCGIRedirects`) |

See `docs/architecture.md` for the broader template-tag compatibility list — the import phase surfaces lint warnings for unsupported tags, but the actual rendering policy lives there.

## Post-import checklist

1. Confirm entry and category counts in `/admin/entries` and `/admin/categories` match the source DB.
2. Open the homepage, an entry page, a category page, and an archive page on the public site.
3. Visit each imported template's editor to review the lint warnings, then activate the one you want.
4. Verify image paths resolve — copy `<sb3_root>/upload` next to `SB_IMAGE_DIR` if you want broken legacy `<img>` tags to start rendering again.
5. Re-check comment-acceptance, blog URL, and OG card defaults.
6. Re-create users.
