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
- Imports content from legacy Serene Bach v3 SQLite databases

## Quick start

```bash
go mod tidy
task dev    # serves on :8080 (auto-creates the dev DB on first request)
```

Open <http://localhost:8080/> in a browser. The first request to a database without an admin user redirects to **`/setup`**, where you create the administrator account and choose whether to insert a couple of sample entries. After that, the public site lives at `/` and the admin UI at `/admin/login`.

Prefer the CLI? `task seed` still works — it creates the dev DB and seeds an admin (`admin` / `changeme` by default; override via `SB_ADMIN_NAME` / `SB_ADMIN_PASSWORD`) without going through the browser flow.

A `.env` template ships at `.env.example`. Copy it to `.env` and fill in `SB_AI_SECRET` if you plan to enable the AI writing assists.

## Documentation

| Topic | Link |
|---|---|
| Public + admin URL reference | [docs/url-map.md](docs/url-map.md) |
| Environment variables, flags, `task` shortcuts | [docs/configuration.md](docs/configuration.md) |
| Deployment modes (HTTP server / CGI / static rebuild) | [docs/deployment.md](docs/deployment.md) |
| Migrating from Serene Bach v3 | [docs/importing-sb3.md](docs/importing-sb3.md) |
| Stack overview + design notes (CSRF, anti-spam, OG cards, analytics, …) | [docs/architecture.md](docs/architecture.md) |
| End-user help (also served at `/admin/help` from the running binary) | [docs/help/](docs/help/) |

## License

[MIT](LICENSE). See the file for the full text.
