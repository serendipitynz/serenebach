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
task seed   # creates the dev DB, applies migrations, seeds admin user + samples
task dev    # serves on :8080
```

Then open <http://localhost:8080/> for the public site or <http://localhost:8080/admin/login> for the admin UI. Default credentials from `task seed` are `admin` / `changeme` — override via `SB_ADMIN_NAME` / `SB_ADMIN_PASSWORD` before seeding.

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
