# Architecture and design notes

The "how it works under the hood" reference. Useful for contributors, operators auditing security/perf, and anyone curious about the trade-offs the Go port made.

## Stack

- **Go** — single statically-linked binary, no CGO
- **SQLite** via [`modernc.org/sqlite`](https://modernc.org/sqlite) (pure Go) — no external DB
- [`chi`](https://github.com/go-chi/chi) router + `html/template` for the admin UI
- [`goose`](https://github.com/pressly/goose) migrations, embedded into the binary and applied on startup
- A faithful Go port of the original `sb::Template` engine so existing Serene Bach templates render unchanged
- [`go-task`](https://taskfile.dev/) for dev workflows (optional but handy)

## Requirements

- Go 1.22 or newer (1.26 is what is currently tested)
- SQLite CLI (`sqlite3`) — only for peeking at the database; not a runtime dependency
- Optional: `task` (go-task) if you want the shorthand commands
- Optional: Python 3 — only if you want to serve the static build locally

---

## Sections

- [CSRF protection](#csrf-protection)
- [Comments and anti-spam](#comments-and-anti-spam)
- [Templates and design settings](#templates-and-design-settings)
- [SB3 template compatibility](#sb3-template-compatibility)
- [Reactions: likes + stamps](#reactions-likes--stamps)
- [Open Graph cards](#open-graph-cards)
- [Entry body formats](#entry-body-formats)
- [Image uploads](#image-uploads)
- [First-party analytics](#first-party-analytics)
- [AI integration](#ai-integration)
- [MCP server](#mcp-server)

---

## CSRF protection

Every non-GET request carries a double-submit CSRF token. On every response the server ensures an `sb_csrf` cookie is set; every form (both admin and public) embeds the same value as a hidden `csrf_token` input. Mismatches are rejected with `403` before the handler runs.

This is belt-and-suspenders with `SameSite=Lax` on every cookie we set: the cookie covers the common cases, the token catches the corners (cross-subdomain cookie leaks, older-browser quirks, iframe embedding). There's no per-site setup — the cookie is minted on the first GET.

For reader-facing POSTs (comments, likes, stamps), an additional **same-origin guard** rejects requests whose `Origin` / `Referer` header doesn't match the configured base URL or `SB_PUBLIC_ALLOWED_ORIGINS`. This makes embed-from-anywhere XSRF attempts fail even if a token leaks.

Reverse-proxy deployments should set `SB_TRUSTED_PROXIES` so the analytics layer accepts forwarded headers from the proxy and not from arbitrary clients.

## Comments and anti-spam

Comment behaviour is driven by the per-weblog `comment_mode` column:

- `open` — submissions are published immediately after passing spam checks.
- `moderated` (default) — submissions sit in a queue; the admin approves them in `/admin/comments?status=waiting`. Returning commenters whose email has already been approved once are auto-approved ("trust memory").
- `closed` — the form is not rendered and POSTs are rejected.

Each entry additionally carries `entries.accept_comments` (default `1`). When the weblog mode is `open` or `moderated`, the entry form exposes a "Accept comments" checkbox; turning it off hides the form and existing comments on that entry's public page and rejects any POSTs to it. The weblog-level `closed` mode wins over the per-entry flag, so the checkbox is hidden in that case and every entry's comments stay off.

A bundled pipeline drops bot-driven noise with no per-site setup:

1. A hidden honeypot field and a minimum form-lifetime check reject the most obvious drive-by submissions silently.
2. A per-IP sliding-window rate limit caps submissions at 3 per minute.
3. Per-weblog banned-words list (`weblogs.spam_words`, newline-separated; lines starting with `#` are treated as comments). Matches trigger a silent reject.
4. Per-weblog IP blacklist (`weblogs.ip_blacklist`) accepts both exact addresses and CIDR ranges (v4 + v6). Blocked IPs are silently dropped before the comment ever reaches the DB.

**Cloudflare Turnstile (optional)**: set both `SB_TURNSTILE_SITEKEY` and `SB_TURNSTILE_SECRET`. When configured, the comment form renders the CF challenge widget and the POST handler verifies tokens against `challenges.cloudflare.com/turnstile/v0/siteverify`. Leaving either variable empty disables the feature entirely — no widget, no verify call, no remote dependency.

Commenters can opt into a prefill cookie via the `次回のために…記憶する` checkbox on the form; that stores their name, email, and url in `sb_name` / `sb_email` / `sb_url` cookies so the next visit shows the fields already filled in.

## Templates and design settings

Templates are edited in the browser at `/admin/templates`. The sidebar entry is labelled **デザイン設定** (the URL stayed `templates` for compatibility — it's now an umbrella for template-related design settings rather than just a template list). The page has multiple tabs:

- **List** — every template row with activate / delete / export / reorder (drag the ≡ handle). Deleting the currently-active template is refused so the public site always has something to render.
- **Settings** — pin a template per route family. Archive (year / month / category / tag) pages use the archive pin when set; profile pages use the profile pin when set; everything else stays on the active template.
- **OG card** — the blog-wide default OG card background and text colour. Per-entry overrides on the entry editor take precedence.
- **Import** — upload an SB3 `template.txt` (multipart/mixed bundle with base.html / style.css / optional entry.html / binary assets). Each imported asset lands under `SB_TEMPLATE_DIR/<new_id>/`.

Per-template assets (logos, background images, webfonts, etc.) upload to the same editor via drag-and-drop. They are served read-only at `/template/<template_id>/<filename>`, and the sbtemplate tag `{site_parts}` resolves to that prefix for the currently-rendered template so HTML / CSS can reference them as `{site_parts}logo.png`.

Export reproduces the bundle as a downloadable `template.txt` — exported files round-trip through the parser and through the legacy SB3 template importer.

## SB3 template compatibility

The engine carries a compatibility layer so imported SB3 templates render without hand-editing the markup:

- **`/profile/{id}/`** populates SB3's `profile_area` block with `{profile_name}` / `{profile_description}` / `{user_*}`. Users with `list_visible=0` return 404.
- **`/sb.cgi?mode=…`** is a legacy shim that redirects every SB3 URL shape to the native route. `mode=comment` POSTs 307-forward (body preserved) to `/entry/{eid}/comment` so imported comment forms keep working.
- **`{subcategory_list}`** emits the nested `<ul>` tree it did in SB3 (the early Go port left it empty).
- **`{site_rsd}`** resolves to `/rsd.xml` — the discovery XML is served, though the advertised XML-RPC API is not implemented.
- **sbtext formatter** — format values `sbtext` / `1` / `2` expand the minimal SB3 subset (paragraphs, URL autolink, `''strong''` / `'''italic'''`, `[label|URL]` bracket links).
- **Template lint** — imports surface warnings for unsupported tags / blocks (`trackback_*`, `amazon_*`, `comment_iconform`, `selected_entry`, …) in `Report.Warnings` and on the editor's status panel.
- **`{user_name}` breaking change** — it now returns the **login name** (SB3 semantics). Use `{user_disp_name}` for the display name. `{user_login}` is an alias of `{user_name}`.

Intentionally unsupported (flagged by lint, not implemented): `{site_mobile}`, `{comment_icon}` / `comment_iconform`, `selected_entry` / `selected_entry_list`, the trackback family, amazon-affiliate tags.

## Reactions: likes + stamps

Every entry carries two per-visitor reaction counters:

- **Like** — single-kind thumbs-up, `POST /entry/{id}/like`. Dedup via `(entry, fingerprint)` unique index + per-entry cookie. Exposed in templates as `{entry_likes_count}` / `{entry_like_url}`.
- **Stamps** — four emoji reactions (heart ❤ / laugh 😂 / wow 😮 / party 🎉) via `POST /entry/{id}/stamp` with `kind=<name>`. Dedup via `(entry, stamp_kind, fingerprint)` so one visitor can attach multiple kinds to the same entry but not stack the same kind. Denormalised total exposed as `{entry_stamps_count}` + per-kind breakdowns as `{entry_stamps_heart}`, `{entry_stamps_laugh}` etc. `{entry_stamp_url}` is the POST target.

Admin analytics (`/admin/analytics`) shows PV / いいね / スタンプ as three sortable columns on the Top 10 listing — click a header to re-rank. The sort mode persists via `?sort=views|likes|stamps`.

## Open Graph cards

Every published entry gets a 1200×630 PNG card at `<SB_IMAGE_DIR>/og/<entry_id>.png`. The card is regenerated on entry create / update (server mode) and removed on delete. The default template emits the standard meta tags in `<head>`:

```html
<meta property="og:title" content="{entry_title}">
<meta property="og:image" content="{entry_og_image}">
<meta property="og:image:width" content="{entry_og_image_width}">
<meta property="og:image:height" content="{entry_og_image_height}">
<meta property="og:type" content="article">
<meta name="twitter:card" content="summary_large_image">
```

The renderer is pure Go — Noto Sans JP (Medium) and the SB default background image are embedded into the binary, so cards work on a fresh install without any runtime asset path config.

**Customisation:**

- Blog-wide background and text colour at `/admin/templates/og`. Off-aspect backgrounds are centre-cropped (CSS `object-fit: cover` semantics); text colour applies to both the entry title and the site name.
- Per-entry override on the entry editor (`OG カード背景` field).
- Resolution priority at render time: entry → weblog → embedded default. Decode failures silently fall through.

**CGI mode note:** `cgi.Serve` buffers the entire response before writing to stdout, so the memory spike during PNG encoding can OOM-kill shared-hosting processes. To avoid this, auto-regeneration on save is disabled in CGI mode. Operators can still generate cards explicitly via the **OG カードを生成** button on the entry editor, which posts to `POST /admin/entries/{id}/og` and returns a small JSON payload so the buffer stays tiny.

Static rebuild mirrors `<SB_IMAGE_DIR>` (including `og/`) into `<OutDir>/img/`, so a statically-deployed site carries the cards alongside the HTML.

## Entry body formats

Every entry carries a `format` field that decides how the stored body and 追記 get turned into HTML. The admin entry form exposes the choice via a dropdown alongside Status / Category / 投稿日時.

- **HTML** (default): the stored bytes are emitted verbatim. This preserves every entry from before the format choice was introduced — nothing is reformatted.
- **Markdown**: rendered by [goldmark](https://github.com/yuin/goldmark) with GFM (tables / task lists / strikethrough) and bare-URL autolinking. Raw `<script>` etc. inside a Markdown body is escaped — if you need literal HTML, store the entry as HTML format instead.
- **sbtext**: a minimal subset of the SB3 Hatena-style notation — paragraph splitting on blank lines, URL autolink, `[text|URL]` shortcut, `''emph''` / `'''italic'''` inline markers. Imported SB3 entries authored as `1` / `2` / `sbtext` map here so they keep their original look; richer SB3 features (headings, tables, footnotes) intentionally remain out of scope.

The format choice is per-entry, so a blog can mix HTML legacy posts with new Markdown drafts without a migration.

## Image uploads

The admin UI includes a drop-zone gallery at `/admin/images`. Drag one or more files onto the zone (or click to pick from the OS dialog) and they land in `SB_IMAGE_DIR` under a `YYYY/MM/<slug>-<shortid>.<ext>` layout, with a same-tree `.thumb.jpg` sibling for the gallery preview. The upload is served read-only at `/img/<stored_path>`.

- **Accepted formats**: JPEG, PNG, GIF, WebP. Everything else is rejected via `http.DetectContentType` on the first 512 bytes — the browser's `Content-Type` header is *not* trusted.
- **Size cap**: `SB_UPLOAD_MAX_MB`, default 10 MB. Both the pre-flight `Content-Length` check and `http.MaxBytesReader` enforce the ceiling.
- **Thumbnails**: generated in pure Go (`image/jpeg`, `image/png`, `image/gif`, `golang.org/x/image/webp` + `golang.org/x/image/draw`). No cgo, no vips — the single-binary story stays intact. Longest edge is capped at 240 px.
- **Editor integration**: the entry form has a *画像を挿入* button that opens a picker showing every uploaded image. Clicking one inserts an `<img>` (or Markdown image) tag at the textarea cursor. Dragging a file straight onto the textarea uploads *and* inserts in one step.
- **Static rebuild**: `SB_IMAGE_DIR` is mirrored into `<SB_REBUILD_OUT>/img/` during rebuild so a static deployment carries its media alongside the HTML.
- **CSRF**: uploads go through the global middleware. The drop-zone JS sends `X-CSRF-Token`; the no-JS fallback `<form enctype="multipart/form-data">` submits a hidden `csrf_token` field.

Admin can scope per-author delete: regular-tier users may only remove images they uploaded themselves; power and admin can remove any.

## First-party analytics

Every public GET is logged to a `page_views` table so the admin dashboard (`/admin/analytics`) can show a pageview total, unique visitors, returning visitors, top entries, and a daily breakdown. The design is intentionally first-party and privacy-leaning:

- No IP addresses, no User-Agent strings, no referers are stored.
- Each visitor gets an opaque random cookie (`sb_visitor_id`, HttpOnly, 1-year TTL) for dedup. That's the only identifier on the row.
- Obvious bot User-Agents (googlebot, bingbot, crawler, spider, …) are filtered out at the middleware before they ever reach the DB.
- `/admin/*`, static assets, and POST requests are never counted.

Defaults:

- **Retention**: 30 days. Override with `SB_ANALYTICS_RETENTION_DAYS` (any positive integer; `0` keeps rows forever). Old rows are deleted via a probabilistic in-request sweep — no cron required.
- **Storage**: the main application DB. Point `SB_ANALYTICS_DB` at a separate SQLite file when you want to keep analytics away from the weblog content (useful for fast backups of just the blog).
- **Enable/disable**: on by default. Set `SB_ANALYTICS_DISABLED=1` to turn off recording and the dashboard together.

## AI integration

Optional. The admin UI ships a writing assist that hooks into the entry editor (rewrite / continue / summarise / suggest title / suggest tags) and into image upload (auto alt-text generation). Disabled until the operator sets `SB_AI_SECRET` on the server — that secret is hashed with SHA-256 to derive an AES-256-GCM key, which encrypts each user's saved provider API key. Per-user provider config lives under `/admin/profile` (model + API key) and `/admin/settings/ai` (operator-side config).

Provider model is pluggable. Current implementations live under `internal/ai/` — adding a new one is a single-file drop-in.

## MCP server

Serene Bach exposes its content via the [Model Context Protocol](https://modelcontextprotocol.io/) so AI agents (Claude Desktop, Cursor, Zed, etc.) can browse and write entries.

- **stdio transport** — `./serenebach mcp serve`. Runs on the same machine as the agent, no token required.
- **HTTP transport** — `POST /mcp` with `Authorization: Bearer <token>`. Tokens are minted from `/admin/settings/ai` (admin only); each token has a scope (`read` / `write`) and a bound "acts-as" user id used for write attribution.

Tools:

- **Read**: `list_entries`, `get_entry`, `search_entries`, `list_categories`, `list_tags`, `get_analytics`, `list_images`.
- **Write** (write-scope tokens only): `create_entry`, `update_entry`, `publish_entry`, `upload_image`.

Every write operation lands in an audit log (`mcp_audit_log` in the main DB; redirect to a separate file via `SB_MCP_AUDIT_DB`). The admin AI 設定 tab surfaces recent rows with the calling token, acting user, tool name, target, and timestamp.

## Outbound webhooks

Serene Bach can POST a JSON payload to operator-specified URLs when domain events fire — entry publish / update / delete, comment received / approved, image uploaded. Designed as the cheapest integration surface: a single binary still, no queue, no daemon.

- **Configuration** — `/admin/settings/webhooks` (requires `power_user` role). Each subscription has a URL, an optional HMAC-SHA256 secret, a per-event checkbox grid, and an active toggle. Up to 50 subscriptions per weblog.
- **Dispatch mode** — server mode fans out per delivery on a goroutine (10 s `http.Client` timeout). CGI mode dispatches synchronously inside the request with a 3 s timeout, because goroutines die with the process. `SB_WEBHOOKS_DISABLED=1` cuts every dispatch to a no-op.
- **Signing** — when `secret` is set, payloads ship with `X-SB-Signature: sha256=<hex>`, the HMAC-SHA256 of the request body. Helpers `webhook.Sign` / `webhook.Verify` use constant-time comparison.
- **SSRF guard** — `webhook.ValidateURL` rejects non-http(s) schemes, loopback (`127.x`, `::1`, `localhost`), link-local, multicast, and RFC1918 private ranges before the HTTP client opens a connection. Redirects are not followed.
- **Persistence** — `webhooks` and `webhook_deliveries` (migration `0046_webhooks.sql`). Each attempt logs one delivery row with status code, transport error, and timestamps; the dispatcher prunes anything past the most recent 200 entries per subscription.
- **Privacy** — comment payloads carry `commenter` name and a 240-rune `body_excerpt` only. Email and IP are intentionally omitted.

Static rebuild does not fire webhooks — the design treats `entry.published` as "an admin pressed save with status=published", which is the dynamic path. A subsequent `task build-site` re-emits HTML but is not itself a publication event.
