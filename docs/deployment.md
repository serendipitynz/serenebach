# Deployment

The same Serene Bach binary speaks two protocols, chosen
automatically. There's also a static-rebuild mode for hybrid
deployments where a CDN serves HTML and the binary backs the admin.

## HTTP server

The default. Deploy on a VPS, Fly.io, Pikapods, a Raspberry Pi, etc.
Supervise with systemd, `fly deploy`, or your favourite process
manager.

```bash
./serenebach --addr=:8080 --db=/var/lib/serenebach/blog.db
```

## CGI

If the `GATEWAY_INTERFACE` environment variable is set (every
CGI-compliant web server sets it), the binary serves one request
over stdin/stdout and exits. Useful for traditional shared hosting
like さくらのレンタルサーバ.

Example invocation from the shell — useful for smoke-testing CGI
mode without a web server:

```bash
GATEWAY_INTERFACE=CGI/1.1 SERVER_PROTOCOL=HTTP/1.1 \
  SERVER_NAME=localhost SERVER_PORT=80 \
  REQUEST_METHOD=GET PATH_INFO=/ \
  SCRIPT_NAME="" REMOTE_ADDR=127.0.0.1 HTTP_HOST=localhost \
  SB_DB=./data/dev.db ./bin/serenebach
```

Cross-compile a static CGI binary:

```bash
task build-cgi   # Linux x86_64
task build-pi    # Linux arm64 (Raspberry Pi / ARM VPS)
```

### First-run setup over the browser

A fresh deploy with no `users` row yet auto-redirects every request to **`/setup`**. Drop the binary + `.htaccess` onto the host, open the URL once, and the admin form lets you set a username, password, and site title without ever touching SSH or `task seed`. Once the admin row exists the gate flips off and `/setup` returns 404 for the rest of the install's life. The CLI `seed` subcommand still works and remains the recommended path for environments without browser access (FTP-only shared hosts, kiosk reinstalls, scripted provisioning).

## Static site generation

```bash
task build-site
```

Writes the full site (home, every entry permalink, every category,
every archive period, plus `style.css`) under `./data/public/`.
File layout mirrors the live URL structure:

```
data/public/
├── index.html
├── style.css
├── entry/<id>/index.html
├── category/<id>/index.html
└── archive/<year>/[month]/index.html
```

Any static host — nginx, Apache, GitHub Pages, Cloudflare Pages,
rclone mount, or `python3 -m http.server` for a quick preview —
will serve it.

The same rebuild is available from the admin UI at
**`/admin/rebuild`**: a single "今すぐ再構築" button triggers the same
`rebuild.Build` pipeline, shows a per-section result (home /
entries / categories / archive / css), and reports the last rebuild
time based on the `index.html` mtime. Concurrent clicks are
serialised by a mutex; the second caller sees a "busy" message
instead of racing the first.

`SB_IMAGE_DIR` (uploaded images, including OG cards) and
`SB_TEMPLATE_DIR` (per-template assets) are mirrored into the
output so a static deployment carries its media alongside the HTML.

## Hybrid (recommended for traffic)

Run the binary as a long-lived HTTP server behind a reverse
proxy / CDN, but periodically run `task build-site` to keep a
static snapshot ready. Public reads can hit the snapshot via
the CDN; the dynamic backend handles the admin UI, comment
submissions, MCP, and writes that need fresh DB state.

When deploying behind a reverse proxy, set `SB_TRUSTED_PROXIES`
to the proxy's IP range so `X-Forwarded-For` is honoured for
the first-party analytics dedup.
