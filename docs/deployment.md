# Deployment

The same Serene Bach binary speaks two protocols, chosen automatically. There's also a static-rebuild mode for hybrid deployments where a CDN serves HTML and the binary backs the admin.

## HTTP server

The default. Deploy on a VPS, Fly.io, Pikapods, a Raspberry Pi, etc. Supervise with systemd, `fly deploy`, or your favourite process manager.

```bash
./serenebach --addr=:8080 --db=/var/lib/serenebach/blog.db
```

## CGI

If the `GATEWAY_INTERFACE` environment variable is set (every CGI-compliant web server sets it), the binary serves one request over stdin/stdout and exits. Useful for traditional shared hosting like さくらのレンタルサーバ.

Example invocation from the shell — useful for smoke-testing CGI mode without a web server:

```bash
GATEWAY_INTERFACE=CGI/1.1 SERVER_PROTOCOL=HTTP/1.1 \
  SERVER_NAME=localhost SERVER_PORT=80 \
  REQUEST_METHOD=GET PATH_INFO=/ \
  SCRIPT_NAME="" REMOTE_ADDR=127.0.0.1 HTTP_HOST=localhost \
  SB_DB=./data/dev.db ./bin/serenebach
```

Cross-compile a static binary for the host. Build tasks are named by configuration (`GOOS-GOARCH`), not by use case — pick whichever matches the target host:

```bash
task build-linux-amd64   # → bin/serenebach-linux-amd64
task build-linux-arm64   # → bin/serenebach-linux-arm64 (Raspberry Pi / ARM VPS)
```

Other targets are available too: `build-freebsd-amd64`, `build-freebsd-arm64`, `build-windows-amd64`, `build-windows-arm64`, `build-darwin-amd64`, `build-darwin-arm64`. Run `task --list` to see the full set.

For CGI hosting, copy the appropriate binary to the host and rename it (typically to `serenebach.cgi`) to match the web server's CGI configuration.

### Speeding up admin static assets in CGI mode

In CGI mode every request starts a new process. Admin assets (`admin.css`, `admin.js`, logos) therefore pay the binary-load + goose-check overhead on every page load. Two mitigations are built in:

1. **ETag / 304** — the binary already returns `ETag` headers and answers `If-None-Match` with `304 Not Modified`. After the first load, refreshing the admin page reuses the browser cache and the CGI process exits almost immediately (~80–100 ms TTFB instead of ~500 ms).

2. **`extract-assets` subcommand** — for hosts where you can place extra files alongside the CGI binary, run:
   ```bash
   ./serenebach extract-assets --out=./admin-static
   ```

   This writes `admin.css`, `admin.js`, logos, favicon, and the Ace editor bundle (`ace/*.js`) to disk.

   Then add an `.htaccess` rule (or equivalent) so Apache serves extracted files directly without invoking the CGI handler, while falling back to the CGI handler for anything that wasn't extracted:

   ```apache
   RewriteEngine On
   RewriteCond %{DOCUMENT_ROOT}/admin-static/$1 -f
   RewriteRule ^admin/static/(.+)$  admin-static/$1  [L]
   ```

   This drops TTFB for assets to ~5–10 ms. The binary keeps working as a fallback for any asset you didn't extract. Re-run `extract-assets` after every binary upgrade because the ETag changes with the build.

### First-run setup over the browser

A fresh deploy with no `users` row yet auto-redirects every request to **`/setup`**. Drop the binary + `.htaccess` onto the host, open the URL once, and the admin form lets you set a username, password, and site title without ever touching SSH or `task seed`. Once the admin row exists the gate flips off and `/setup` returns 404 for the rest of the install's life. The CLI `seed` subcommand still works and remains the recommended path for environments without browser access (FTP-only shared hosts, kiosk reinstalls, scripted provisioning).

## Static site generation

```bash
task build-site
```

Writes the full site (home, every entry permalink, every category, every archive period, plus `style.css`) under `./data/public/`. File layout mirrors the live URL structure:

```
data/public/
├── index.html
├── style.css
├── entry/<id>/index.html
├── category/<id>/index.html
└── archive/<year>/[month]/index.html
```

Any static host — nginx, Apache, GitHub Pages, Cloudflare Pages, rclone mount, or `python3 -m http.server` for a quick preview — will serve it.

The same rebuild is available from the admin UI at **`/admin/rebuild`**: a single "今すぐ再構築" button triggers the same `rebuild.Build` pipeline, shows a per-section result (home / entries / categories / archive / css), and reports the last rebuild time based on the `index.html` mtime. Concurrent clicks are serialised by a mutex; the second caller sees a "busy" message instead of racing the first.

`SB_IMAGE_DIR` (uploaded images, including OG cards) and `SB_TEMPLATE_DIR` (per-template assets) are mirrored into the output so a static deployment carries its media alongside the HTML.

## Hybrid (recommended for traffic)

Run the binary as a long-lived HTTP server behind a reverse proxy / CDN, but periodically run `task build-site` to keep a static snapshot ready. Public reads can hit the snapshot via the CDN; the dynamic backend handles the admin UI, comment submissions, MCP, and writes that need fresh DB state.

When deploying behind a reverse proxy, set `SB_TRUSTED_PROXIES` to the proxy's IP range so `X-Forwarded-For` is honoured for the first-party analytics dedup.
