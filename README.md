# Serene Bach

A self-hostable Go weblog engine — a lighter path between WordPress and Hugo. Small to place, familiar to publish.

🌐 **[go.serenebach.net](https://go.serenebach.net)** — features, screenshots, positioning
📄 Japanese: see [README.ja.md](README.ja.md)

## At a glance

- Single statically-linked Go binary, no CGO
- SQLite via [`modernc.org/sqlite`](https://modernc.org/sqlite) (pure Go) — no separate database server
- Runs as a long-lived HTTP server, **or** as a CGI program on traditional shared hosting
- Embedded admin UI, MCP server, and end-user help — nothing extra to deploy
- Static rebuild for hybrid hosting (CDN / static front, dynamic admin behind)
- Imports content from legacy Serene Bach v2 (flat-file) and v3 (SQLite) installations

## Quick start

Requires [Go](https://go.dev/doc/install) and [Task](https://taskfile.dev/installation/).

```bash
task dev    # serves on :8080 (auto-creates the dev DB on first request)
```

Open <http://localhost:8080/> in a browser. The first request to a database without an admin user redirects to **`/setup`**, where you create the administrator account and choose whether to insert a couple of sample entries. After that, the public site lives at `/` and the admin UI at `/admin/login`.

`task dev` also sets `SB_DEV=1`, which disables template and i18n caching so edits to `web/templates/admin/*.html` are reflected on the next request without restarting the server.

Prefer the CLI? `task seed` still works — it creates the dev DB and seeds an admin (`admin` / `changeme` by default; override via `SB_ADMIN_NAME` / `SB_ADMIN_PASSWORD`) without going through the browser flow.

A `.env` template ships at `.env.example`. Copy it to `.env` and fill in `SB_AI_SECRET` if you plan to enable the AI writing assists.

## Docker

```bash
# Build
docker build -t serenebach .

# Run: start the server and open http://localhost:8080/setup to create the admin user
docker run -d -p 8080:8080 -v serenebach-data:/home/nonroot/data serenebach

# Or seed via CLI with an explicit password
docker run --rm -v serenebach-data:/home/nonroot/data -e SB_ADMIN_PASSWORD=<secret> serenebach seed
```

Or use the bundled `docker-compose.yml`:

```bash
docker compose up -d
```

### Pre-built images (GHCR)

Official container images are published to GitHub Container Registry (`ghcr.io/serendipitynz/serenebach`).

```bash
docker pull ghcr.io/serendipitynz/serenebach:4.0.0-beta.5

docker run -d -p 8080:8080 -v serenebach-data:/home/nonroot/data ghcr.io/serendipitynz/serenebach:4.0.0-beta.5
```

Available tags:
- `latest` — most recent build on the default branch
- `4.0.0-beta.5`, `4.0.0`, … — semver tags matching released versions
- `main` — tip of the `main` branch

For production, prefer a pinned release tag over `latest`. See [docs/deployment.md](docs/deployment.md) for QNAP Container Station and VPS deployment examples.

## Companion tools

| Tool | What it does |
|---|---|
| `./bin/serenebach mcp serve` | Start the MCP server over stdio for Claude Code / Cursor / Zed |
| `task build-proxy` | Build the MCP OAuth proxy (`bin/mcp-oauth-proxy`) — bridges ChatGPT's OAuth-only MCP client to Serene Bach's Bearer-token `/mcp` endpoint. See `cmd/mcp-oauth-proxy/README.md` for env vars and ChatGPT configuration. |

## Documentation

| Topic | Link |
|---|---|
| Public + admin URL reference | [docs/url-map.md](docs/url-map.md) |
| Environment variables, flags, `task` shortcuts | [docs/configuration.md](docs/configuration.md) |
| Deployment modes (HTTP server / CGI / static rebuild) | [docs/deployment.md](docs/deployment.md) |
| Migrating from Serene Bach v2 / v3 | [docs/importing-sb3.md](docs/importing-sb3.md) |
| Stack overview + design notes (CSRF, anti-spam, OG cards, analytics, …) | [docs/architecture.md](docs/architecture.md) |
| End-user help (also served at `/admin/help` from the running binary) | [docs/help/](docs/help/) |

## License

[MIT](LICENSE). See the file for the full text.
