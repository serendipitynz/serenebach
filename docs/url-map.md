# URL map

Every route the running server exposes, split between the public surface and the admin UI. Trailing slashes are accepted with or without (chi `middleware.StripSlashes`); the canonical form carries the slash so static rebuilds (`<path>/index.html`) and dynamic routes line up.

## Public (server-rendered or statically built)

| Path | Purpose |
|---|---|
| `/` | Home: most recent published entries |
| `/entry/{key}/` | Single entry permalink with prev / next navigation. `{key}` is the entry's slug when one is set; otherwise the numeric id. A hit on the id-form 301s to the slug surface |
| `/entry/{key}/comment` | POST target for the comment form |
| `/entry/{key}/like` | POST target for the like button (monotonic counter, cookie + fingerprint dedup) |
| `/entry/{key}/stamp` | POST target for stamp reactions (`kind=heart|laugh|wow|party`) |
| `/category/{key}/` | Entries in one category. `{key}` is the category's slug when one is set; otherwise the numeric id. A hit on the id-form 301s to the slug surface, mirroring the entry route |
| `/tag/{slug}/` | Entries carrying one tag (tags are author-assigned; see admin tag management) |
| `/archive/{year}/` | Year archive |
| `/archive/{year}/{month}/` | Month archive |
| `/search?q=<query>&page=<n>` | Full-text entry search (FTS5 trigram). Pagination uses the same `?page=N` shape; out-of-range pages 404. Dynamic-only — static-only deployments do not expose this route. Hidden-category entries are excluded |
| `/profile/{id}/` | Author profile page (SB3 `?pid=N` equivalent). Users with `list_visible=0` 404 |
| `?page=N` | Pagination query (1-indexed) valid on home / category / tag / archive routes; out-of-range values 404 |
| `/rss.xml` | RSS 2.0 feed of the latest 20 entries (served dynamically or from the static snapshot) |
| `/atom.xml` | Atom 1.0 feed of the latest 20 entries |
| `/rsd.xml` | RSD 1.0 discovery XML. The `{site_rsd}` tag points here for imported templates; the advertised XML-RPC API itself is not implemented |
| `/sb.cgi?mode=…` | SB3 legacy shim. `mode=entry/category/archive/user` returns 301, `mode=comment` 307-forwards the POST body to `/entry/{id}/comment` |
| `/sitemap.xml` | Sitemap protocol 0.9 URL set. 404 when disabled in site settings |
| `/robots.txt` | Crawler directives + `Sitemap:` line. 404 when disabled in site settings |
| `/llms.txt` | Markdown index for AI agents. 404 unless the weblog opts in via 基本設定 |
| `/llms-full.txt` | Full Markdown dump (up to 500 entries) for agents that want the knowledge base in one request. Same opt-in toggle as /llms.txt |
| `/style.css` | Active template's stylesheet (alias kept for backward compat) |
| `/template/{id}/style.css` | Per-template stylesheet — pages rendered through a pinned template (`{site_css}`) load their own CSS here |
| `/img/*` | Uploaded files (images, audio, documents, movies) served from `SB_IMAGE_DIR` (default `./data/img`) |

## Admin (requires login)

| Path | Purpose |
|---|---|
| `/admin/login` | Login form + POST target |
| `/admin/static/admin.css` | Embedded admin stylesheet |
| `/admin/static/admin.js` | Embedded admin script entry (ES module; drawer toggle, picker, AI compose) |
| `/admin/static/modules/*` | Admin ES module graph (i18n, storage, toast, …) |
| `/admin/` | Dashboard (counters + recent entries + quick links) |
| `/admin/entries` | Entry list (all statuses) |
| `/admin/entries/new` | New entry form (GET + POST) |
| `/admin/entries/{id}/edit` | Edit form (GET + POST) |
| `/admin/entries/{id}/delete` | Delete (POST) |
| `/admin/images` | File library (images / audio / documents / movies) + drop-zone upload (GET = gallery, POST = upload, `?format=json` for the editor picker) |
| `/admin/images/{id}/delete` | Delete a file + its files on disk (POST) |
| `/admin/images/{id}/alt` | POST — vision-generated alt text. Fires automatically after upload when the uploader has enabled auto-alt (image kind only) |
| `/admin/images/{id}/rename` | POST — rename the display filename (stored_path is untouched so past links stay valid) |
| `/admin/categories` | Category list (name, slug, parent, sort order, entry count) |
| `/admin/categories/new` | New category form (GET + POST) |
| `/admin/categories/{id}/edit` | Edit form (GET + POST) |
| `/admin/categories/{id}/delete` | Delete a category; attached entries are reassigned to 未分類 (category_id = -1) in the same transaction (POST) |
| `/admin/categories/reorder` | Drag-and-drop reorder target; accepts JSON `{"ids":[...]}` (POST, CSRF via `X-CSRF-Token`) |
| `/admin/tags` | Tag list + inline rename + delete (tags are created implicitly from entry form input) |
| `/admin/tags/{id}/update` | Rename / re-slug a tag (POST) |
| `/admin/tags/{id}/delete` | Delete a tag and every entry→tag association (POST) |
| `/admin/links` | Blogroll list (groups + links in one sortable table; drag / ID / site name / URL / status / delete) |
| `/admin/links/new` | New link or group (GET + POST). `?parent=<id>` pre-scopes a new link under an existing group and hides the type picker |
| `/admin/links/{id}/edit` | Edit form (GET + POST). Kind is frozen; group edit pages embed a member list with a "新規リンク→" header that reuses `?parent=<id>` |
| `/admin/links/{id}/delete` | Delete a link; deleting a group detaches its members to ungrouped root-level rows (POST) |
| `/admin/links/reorder` | Drag-drop reorder target; accepts JSON `{"ids":[...]}` (POST, CSRF via `X-CSRF-Token`) |
| `/admin/users` | User management — list + inline create (admin role only) |
| `/admin/users/{id}/edit` | Edit a user: name, display name, email, role, profile, password (admin only) |
| `/admin/users/{id}/delete` | Delete a user; last-admin / self-delete blocked (admin only) |
| `/admin/users/reorder` | Drag-and-drop reorder target; JSON `{"ids":[...]}` (admin only) |
| `/admin/profile` | Self-profile editor — every logged-in user can edit their own name / display name / email / password / description / list-visible (GET + POST) |
| `/admin/comments` | Comment moderation queue (filter via `?status=waiting|approved|hidden`) |
| `/admin/comments/settings` | Comment-reception settings — mode (open / moderated / closed), spam-word list, IP blacklist (exact IP + CIDR) (GET + POST) |
| `/admin/comments/{id}/approve` | Approve (POST) |
| `/admin/comments/{id}/hide` | Soft-hide (POST) |
| `/admin/comments/{id}/delete` | Hard delete (POST) |
| `/admin/analytics` | First-party analytics dashboard (`?days=7|30|90`) |
| `/admin/templates` | Design — list (drag-drop reorder, activate, delete, export) |
| `/admin/templates/settings` | Design — pin archive / profile template (GET + POST) |
| `/admin/templates/og` | Design — OG card defaults (background image + text colour) (GET + POST) |
| `/admin/templates/import` | Design — import an SB3-format `template.txt` bundle. Legacy Shift_JIS / EUC-JP / ISO-2022-JP sources are auto-converted to UTF-8 (GET + POST) |
| `/admin/templates/active/edit` | Shortcut — redirects to the edit form for the currently-active template |
| `/admin/templates/{id}/edit` | Template editor — base HTML / CSS / optional entry HTML / assets (GET + POST + `/save-as`) |
| `/admin/templates/{id}/activate` | Flip this template to "in use" (POST) |
| `/admin/templates/{id}/delete` | Delete a template (refused for the active one) (POST) |
| `/admin/templates/{id}/export` | Download `template.txt` — SB3-compatible multipart/mixed bundle with assets |
| `/admin/templates/{id}/preview` | Preview a template against a live request without activating it |
| `/admin/templates/reorder` | Drag-drop reorder target; JSON `{"ids":[...]}`, CSRF via `X-CSRF-Token` |
| `/admin/templates/{id}/assets` | Upload a template asset (multipart POST) |
| `/admin/templates/{id}/assets/{assetID}/delete` | Remove a template asset + file (POST) |
| `/admin/rebuild` | Static site rebuild trigger (GET status + POST to run) |
| `/admin/help` | Embedded help. Pages live at `/admin/help/{slug}` |
| `/admin/settings` | Settings root — redirects to 基本設定 for users who can manage design, otherwise to 画面設定 |
| `/admin/settings/screen` | 画面設定 tab — per-user appearance + display-language preferences (browser localStorage). Visible to every logged-in user |
| `/admin/settings/basic` | 基本設定 tab — weblog info (title / description / base URL / lang / llms.txt opt-in) + env-var snapshot. CanManageDesign only |
| `/admin/settings/ai` | AI 設定 tab — per-user AI writing-assist config + (admin only) MCP bearer-token management + audit log. AI config panel hidden when `SB_AI_SECRET` is unset; the tab link itself still renders |
| `/admin/settings/ai/test` | POST — smoke-test the saved AI provider with a canned prompt. Used by the 疎通テスト button |
| `/admin/settings/mcp/new` | POST — mint a new MCP bearer token (admin only) |
| `/admin/settings/mcp/{id}/revoke` | POST — revoke an MCP bearer token (admin only) |
| `/admin/settings/webhooks` | Outbound webhook list (`power_user` only). GET shows registered subscriptions + last-delivery status; POST creates one |
| `/admin/settings/webhooks/new` | New-webhook form |
| `/admin/settings/webhooks/{id}/edit` | Edit-webhook form |
| `/admin/settings/webhooks/{id}` | POST — update one webhook |
| `/admin/settings/webhooks/{id}/delete` | POST — delete one webhook + its delivery rows |
| `/admin/settings/webhooks/{id}/toggle` | POST — flip the active flag |
| `/admin/settings/webhooks/{id}/test` | POST — fire a synthetic `ping` payload at the subscription so the operator can verify connectivity |
| `/admin/settings/webhooks/{id}/deliveries` | Per-subscription delivery log (last 50 attempts) |
| `/admin/logout` | Logout (POST) |
| `/admin/ai/compose` | POST (JSON) — AI writing assists: rewrite / continue / summarise (Ace toolbar) + title / tags / keywords (entry-form ✨ buttons) |
| `/mcp` | MCP HTTP transport. Accepts JSON-RPC 2.0 under `Authorization: Bearer <token>`; no CSRF, no session — tokens are the only gate. Read tools (`list_entries`, `get_entry`, `search_entries`, `list_categories`, `list_tags`, `get_analytics`, `list_images`) plus write tools (`create_entry`, `update_entry`, `publish_entry`, `upload_image`) gated behind write-scope tokens |
