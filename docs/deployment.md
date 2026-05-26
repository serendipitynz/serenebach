# Deployment

The same Serene Bach binary speaks two protocols, chosen automatically. There's also a static-rebuild mode for hybrid deployments where a CDN serves HTML and the binary backs the admin.

## HTTP server

The default. Deploy on a VPS, Fly.io, Pikapods, a Raspberry Pi, etc. Supervise with systemd, `fly deploy`, or your favourite process manager.

```bash
./serenebach --addr=:8080 --db=/var/lib/serenebach/blog.db
```

## Docker

A `Dockerfile` and `docker-compose.yml` are provided for containerized deployments.

```bash
# Build
docker build -t serenebach .

# First run: start the server and open the URL in a browser to complete /setup
docker run -d -p 8080:8080 -v serenebach-data:/home/nonroot/data --name serenebach serenebach serve
# Then open http://localhost:8080/setup in a browser

# Alternatively, use the CLI seed with an explicit password (not recommended for public deployments without changing the default)
docker run --rm -v serenebach-data:/home/nonroot/data -e SB_ADMIN_PASSWORD=<strong-secret> serenebach seed
```

The runtime image is based on `gcr.io/distroless/static-debian12:nonroot` (~24 MB). No shell, no root user, minimal attack surface. HTTPS traffic to AI providers works because the CA certificate bundle is included in the distroless base.

### Pre-built image

Pre-built multi-arch images are published to GitHub Container Registry:

```bash
docker pull ghcr.io/serendipitynz/serenebach:latest
```

If an anonymous pull returns `unauthorized`, the GHCR package exists but is not public. In GitHub, open the package settings for `serenebach` and change **Package visibility** to public before documenting the image as publicly installable.

Supported platforms are `linux/amd64` and `linux/arm64`, so the same image works on typical Intel/AMD VPSes, ARM VPSes, Raspberry Pi-style hosts, and Docker-capable QNAP models on those architectures.

For production deployments, prefer a pinned release tag (see the [GitHub Releases page](https://github.com/serendipitynz/serenebach/releases) for the current version) over `latest`. `latest` follows the most recent default-branch image and may change underneath a running deployment.

The container runs as the non-root user from the distroless base (UID/GID `65532`). Its writable state lives under `/home/nonroot/data` by default:

| Container path | Purpose |
|---|---|
| `/home/nonroot/data/serenebach.db` | Main SQLite database |
| `/home/nonroot/data/img` | Uploaded images and OG cards |
| `/home/nonroot/data/templates` | Per-template assets |
| `/home/nonroot/data/public` | Static rebuild output |

Back up the mounted volume or bind-mounted directory as a unit. SQLite, uploads, template assets, and static rebuild output are all intentionally colocated there.

### docker-compose

```bash
docker compose up -d
```

Edit `docker-compose.yml` to set environment variables. `SB_AI_SECRET` is commented out by default; uncomment and set it to a long random string only if you plan to use the AI writing assists. Leaving it undefined or empty disables AI features entirely.

Data is persisted in a Docker volume named `serenebach-data`.

For deployments that should use the published image instead of building from a local checkout, use this compose file shape. Replace `latest` with a release tag from the [Releases page](https://github.com/serendipitynz/serenebach/releases) for production:

```yaml
services:
  serenebach:
    image: ghcr.io/serendipitynz/serenebach:latest
    ports:
      - "127.0.0.1:8080:8080"
    volumes:
      - serenebach-data:/home/nonroot/data
    # environment:
    #   Set only when you use AI writing assists.
    #   SB_AI_SECRET: "replace-with-a-long-random-secret"
    #   Set when Serene Bach is behind a reverse proxy and you want
    #   analytics to honour X-Forwarded-For from that proxy.
    #   SB_TRUSTED_PROXIES: "127.0.0.1/32"
    restart: unless-stopped

volumes:
  serenebach-data:
```

On first boot, open `/setup` and create the administrator account. You can seed from the CLI instead, but the browser setup flow is usually safer for public deployments because it avoids temporary default credentials:

```bash
docker compose up -d
docker compose logs -f serenebach
# Then open http://<host>:8080/setup or https://<domain>/setup.
```

### QNAP Container Station

QNAP's exact labels vary between QTS / QuTS hero and Container Station versions, but the deployment model is the same: run the published image, persist `/home/nonroot/data`, and put a NAS reverse proxy or router in front of the exposed port only if the blog should be reachable from the internet.

Recommended layout:

- Image: `ghcr.io/serendipitynz/serenebach:latest` (or a pinned release tag from the [Releases page](https://github.com/serendipitynz/serenebach/releases) for production)
- Container port: `8080`
- Host port: `8080` for LAN-only testing, or another unused high port if QNAP already uses `8080`
- Persistent storage: named volume mapped to `/home/nonroot/data`, or a bind mount such as `/share/Container/serenebach/data:/home/nonroot/data`
- Restart policy: `unless-stopped` or QNAP's equivalent "always restart unless manually stopped"

If you use a bind mount, make sure the container's non-root user can write to it. Over SSH on the NAS, one common setup is:

```bash
mkdir -p /share/Container/serenebach/data
chown -R 65532:65532 /share/Container/serenebach/data
```

If your QNAP model or policy does not allow changing ownership on the shared folder, use a Docker named volume from Container Station instead.

Container Station's "Application" / compose mode can use (replace `latest` with a release tag for production):

```yaml
services:
  serenebach:
    image: ghcr.io/serendipitynz/serenebach:latest
    ports:
      - "8080:8080"
    volumes:
      - /share/Container/serenebach/data:/home/nonroot/data
    restart: unless-stopped
```

After the container starts:

1. Open `http://<qnap-lan-ip>:8080/setup`.
2. Create the administrator account and site title.
3. In **admin settings**, set the site base URL to the final public URL, for example `https://blog.example.com/`.
4. If the site is public, terminate HTTPS at QNAP's reverse proxy, a router / tunnel, or another front-end service, then forward to the container's HTTP port.
5. Back up `/share/Container/serenebach/data` or the named volume regularly.

Avoid exposing the QNAP management UI and the blog admin UI on the same public port. A reverse proxy with HTTPS and a dedicated hostname is the cleanest arrangement.

### VPS with Docker

On a VPS, run Serene Bach behind an existing reverse proxy or load balancer. Bind the container to localhost so the app itself is not directly exposed to the internet:

```bash
sudo mkdir -p /opt/serenebach
sudo chown "$USER":"$USER" /opt/serenebach
cd /opt/serenebach
```

Create `compose.yaml` (replace `latest` with a release tag from the [Releases page](https://github.com/serendipitynz/serenebach/releases) for production):

```yaml
services:
  serenebach:
    image: ghcr.io/serendipitynz/serenebach:latest
    ports:
      - "127.0.0.1:8080:8080"
    volumes:
      - ./data:/home/nonroot/data
    environment:
      # When running inside Docker, requests arrive through the bridge network.
      # Use the default Docker private IP range (172.16.0.0/12).
      # For non-container or host-network deployments, use 127.0.0.1/32 instead.
      SB_TRUSTED_PROXIES: "172.16.0.0/12"
      # SB_AI_SECRET: "replace-with-a-long-random-secret"
      # SB_UPLOAD_MAX_MB: "10"
    restart: unless-stopped
```

Prepare the bind mount and start the container:

```bash
mkdir -p data
sudo chown -R 65532:65532 data
docker compose pull
docker compose up -d
docker compose logs -f serenebach
```

Point your reverse proxy at `http://127.0.0.1:8080`, enable HTTPS, then open `https://<domain>/setup`.

Minimal Caddy example:

```caddyfile
blog.example.com {
	reverse_proxy 127.0.0.1:8080
}
```

Minimal nginx example:

```nginx
server {
    listen 443 ssl http2;
    server_name blog.example.com;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

Operational checklist:

- Pin the image tag in `compose.yaml`.
- Run `docker compose pull && docker compose up -d` when upgrading to a new tag.
- Back up `/opt/serenebach/data` before upgrades.
- Set the site base URL in the admin UI to the HTTPS URL.
- Keep `SB_TRUSTED_PROXIES` limited to the proxy addresses that actually sit in front of the container.
  - Inside Docker, this is usually the bridge network CIDR (e.g. `172.16.0.0/12`), not `127.0.0.1/32`.
  - Use `127.0.0.1/32` only for host-network or non-container deployments.
- Leave `SB_AI_SECRET` unset unless AI writing assists are needed; setting it later enables the AI UI without changing the image.

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

    This writes `admin.css`, `admin.js`, the ES module graph (`modules/*.js`), logos, favicon, and the Ace editor bundle (`ace/*.js`) to disk.

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
