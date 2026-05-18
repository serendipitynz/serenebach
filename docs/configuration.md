# Configuration reference

Every knob the running binary respects, plus the `task` shortcuts the dev workflow leans on.

## Environment variables

| Variable | Purpose |
|---|---|
| `SB_DB` | Path to the SQLite database (default `./data/dev.db`) |
| `SB_BASE_PATH` | Deployment sub-path used as the URL prefix for all site links when the weblog Base URL is not configured (e.g. `/sb/`). Auto-detected from `SCRIPT_NAME` in CGI mode when not set |
| `SB_ADMIN_NAME` | Seed / default admin user name |
| `SB_ADMIN_PASSWORD` | Seed / default admin password |
| `SB_ADMIN_EMAIL` | Seed / default admin email |
| `SB_SEED_NO_SAMPLES` | Set to `1` to skip demo entries during `task seed` |
| `SB_IMAGE_DIR` | Directory uploaded images are written to and served from at `/img/*` (default `./data/img`) |
| `SB_TEMPLATE_DIR` | Directory per-template assets are written to and served from at `/template/<id>/*` (default `./data/templates`) |
| `SB_REBUILD_OUT` | Directory the admin-triggered static rebuild writes to (default `./data/public`) |
| `SB_UPLOAD_MAX_MB` | Maximum size (megabytes) for a single image upload (default `10`) |
| `SB_TURNSTILE_SITEKEY` | Cloudflare Turnstile public key (leave unset to disable the challenge) |
| `SB_TURNSTILE_SECRET` | Cloudflare Turnstile secret key (leave unset to disable the challenge) |
| `SB_ANALYTICS_DISABLED` | Set to `1` to turn off pageview recording and the admin dashboard |
| `SB_ANALYTICS_DB` | Path to a separate SQLite file for analytics (default: use the main DB) |
| `SB_ANALYTICS_RETENTION_DAYS` | Days of `page_views` to keep (default `30`, `0` = forever) |
| `SB_AI_SECRET` | Master secret for encrypting per-user AI provider API keys. AI features are hidden until this is set |
| `SB_MCP_AUDIT_DB` | Path to a separate SQLite file for the MCP write-tool audit log (default: use the main DB) |
| `SB_TRUSTED_PROXIES` | CIDRs whose `X-Forwarded-For` headers are honoured (comma-separated). Leave empty for direct-to-internet deployments |
| `SB_PUBLIC_ALLOWED_ORIGINS` | Additional origins permitted on reader-facing POSTs (comments, likes, stamps). Comma-separated, full `scheme://host[:port]` |
| `SB_DEV` | Set to `1` to disable template and i18n caching so admin UI edits are reflected on the next request without restarting. `task dev` sets this automatically |
| `SB_READ_HEADER_TIMEOUT` | `time.ParseDuration` value capping how long the server waits for the request line + headers (default `10s`, server mode only) |
| `SB_READ_TIMEOUT` | `time.ParseDuration` value capping the total time to read the request including body. Default is `0` (no whole-request deadline) so multi-megabyte image uploads on slow links are not cut off; Slowloris defence is provided by `SB_READ_HEADER_TIMEOUT` and body size is bounded by `SB_UPLOAD_MAX_MB`. Server mode only |
| `SB_WRITE_TIMEOUT` | `time.ParseDuration` value capping how long the server has to write the response (default `60s`; sized to absorb on-demand OG-image generation, server mode only) |
| `SB_IDLE_TIMEOUT` | `time.ParseDuration` value for keep-alive idle timeout (default `120s`, server mode only) |
| `SB_MAX_HEADER_BYTES` | Maximum size, in bytes, of the request line + headers (default `1048576`, i.e. 1 MiB; server mode only) |
| `SB_SHUTDOWN_TIMEOUT` | `time.ParseDuration` value bounding how long graceful shutdown waits for in-flight requests after `SIGINT`/`SIGTERM` before exiting (default `15s`, server mode only) |
| `SB_TZ` | IANA timezone name (e.g. `Asia/Tokyo`, `UTC`) used for archive year/month boundaries, the admin `posted_at` form input, and rendered entry/list/archive dates. Default is the host clock (`time.Local`). Set this when the binary moves between hosts whose local clocks differ (Docker UTC vs Sakura JST) so the static rebuild and the dynamic site agree on which entries fall in which month |
| `SB_WEBHOOKS_DISABLED` | Set to `1` to cut every outbound-webhook dispatch to a no-op. Operators reach for this when a misbehaving subscriber needs to be silenced faster than the admin UI's per-row toggle can be visited. Per-row enable/disable lives on `/admin/settings/webhooks` |

### MCP OAuth proxy env vars

These are read by `cmd/mcp-oauth-proxy`, not the main binary. Set them when running the proxy standalone.

| Variable | Purpose |
|---|---|
| `UPSTREAM_URL` | Base URL of the Serene Bach instance the proxy forwards to (e.g. `https://blog.example.com`) |
| `MCP_BEARER_TOKEN` | Static Bearer token minted in `/admin/settings/ai` → **MCPトークン管理** |
| `OAUTH_CLIENT_ID` | Client ID registered in ChatGPT's MCP settings (e.g. `chatgpt_mcp`) |
| `OAUTH_REDIRECT_URIS` | **Recommended for public deployments.** Comma-separated allowlist of `redirect_uri` values. When empty, any URI is accepted (development only). |
| `AUTH_PIN` | **Recommended for public deployments.** If set, the `/authorize` page requires this PIN before issuing a code |
| `BASE_URL` | Public URL of the proxy itself, used in OAuth metadata (default `http://localhost:8080`) |
| `PROXY_LISTEN_ADDR` | Listen address for the proxy (default `:8080`) |
| `TOKEN_TTL` | Access-token lifetime (default `24h`) |

## Top-level flags

| Flag | Purpose |
|---|---|
| `--addr` | HTTP listen address (default `:8080`) |
| `--db` | SQLite path (overrides `SB_DB`) |
| `--mode` | `server` or `cgi` (auto-detected if empty) |
| `--version` | Print `vX.Y.Z (build abcdef1)` and exit |

## `.env` loading

`Taskfile.yml` loads a project-root `.env` into every task's environment. Copy `.env.example` to `.env` and fill in values — most notably `SB_AI_SECRET` (required to enable the AI writing assists). Shell-level `VAR=x task dev` always wins over the file, so the two paths compose without surprises.

## Task shortcuts

Everything below is a `go run` or `go build` under the hood, so the Taskfile is optional — use it if you like typing less.

| Command | What it does |
|---|---|
| `task dev` | Run the server on `:8080` against `./data/dev.db`. Automatically sets `SB_DEV=1` so admin template edits are hot-reloaded |
| `task build` | Build a native binary at `./bin/serenebach` |
| `task build-{os}-{arch}` | Cross-compile for a specific target. `{os}` ∈ `linux` / `freebsd` / `windows` / `darwin`, `{arch}` ∈ `amd64` / `arm64`. Output: `bin/serenebach-{os}-{arch}` (`.exe` on Windows). Run `task --list` for the full set. |
| `task build-all` | Cross-compile all eight targets into `bin/` in parallel. |
| `task release` | Cross-compile all 8 targets, package as `tar.gz` / `zip` with README + LICENSE, generate `SHA256SUMS`, and create a **draft** GitHub release via `gh` for `v{version}` (read from `internal/version/version.go`). The tag is created server-side only when the draft is published, so a failed run leaves no half-state. Refuses to run on a dirty tree, with unpushed commits, over an existing tag, or when a release already exists for the tag. |
| `task seed` | Create / update the admin user, bundled template, and sample entries |
| `task migrate` | Apply pending migrations (also runs on every startup) |
| `task build-site` | Render the whole site to static HTML under `./data/public` |
| `task extract-assets` | Write embedded admin assets to `./admin-static` for Apache direct serving in CGI mode |
| `task import -- <path>` | Import from a legacy SereneBach v3 SQLite database |
| `task import-md -- <dir>` | Import entries from a directory of markdown files with YAML front-matter. See [docs/importing-markdown.md](importing-markdown.md) |
| `task build-proxy` | Build the MCP OAuth proxy at `./bin/mcp-oauth-proxy`. Bridges ChatGPT's OAuth-only MCP client to Serene Bach's Bearer-token `/mcp` endpoint. See `cmd/mcp-oauth-proxy/README.md` for env vars and ChatGPT configuration. |
| `./bin/serenebach mcp serve` | Start the MCP server over stdio — exposes the read tools to Claude Code / Cursor / Zed |
| `./bin/serenebach extract-assets` | Write embedded admin assets (`admin.css`, `admin.js`, logos, favicon) to disk so Apache can serve them directly in CGI mode. See [docs/deployment.md](docs/deployment.md) |
| `task lint` | Run `golangci-lint` against `.golangci.yml` (covers `staticcheck` plus the project lint set, including `gocyclo` at the goreportcard threshold of 15) |
| `task test` | `go test ./...` |
| `task tidy` | `go mod tidy` |
| `task clean` | Remove `./bin` and `./data` |
| `docker build -t serenebach .` | Build a container image (see [docs/deployment.md](docs/deployment.md)) |
| `docker compose up -d` | Run the server via Docker Compose with persistent volume |

## What lives in the UI vs the env

The admin Settings page edits per-weblog content values (title, description, base URL, language, comment mode, spam words). Operational configuration — anything secret-bearing or path-bearing — stays in environment variables by design. The settings page surfaces the read-only env snapshot in 基本設定 so you can confirm what's in effect without SSHing in.

| Edited via UI | Stays in env |
|---|---|
| Blog title / description | `SB_TURNSTILE_SITEKEY` / `SB_TURNSTILE_SECRET` (secrets) |
| Base URL / `lang` | `SB_UPLOAD_MAX_MB`, `SB_IMAGE_DIR` |
| Comment mode | `SB_REBUILD_OUT` |
| Spam-words list / IP blacklist | `SB_ANALYTICS_*` |

Changes take effect immediately for dynamic rendering. After editing content settings, run a static rebuild (`/admin/rebuild`) to regenerate the on-disk HTML with the new values.
