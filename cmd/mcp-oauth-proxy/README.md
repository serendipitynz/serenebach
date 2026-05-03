# MCP OAuth Proxy for Serene Bach

A minimal OAuth 2.0 proxy that sits between ChatGPT (or any MCP client that **requires OAuth**) and a Serene Bach instance whose MCP endpoint only accepts a fixed **Bearer token**.

```
ChatGPT ──OAuth──► proxy ──Bearer──► Serene Bach /mcp
```

The proxy handles the OAuth authorization-code flow with PKCE on the front side, then attaches a static `Authorization: Bearer <token>` header when forwarding JSON-RPC requests to the upstream `/mcp` endpoint.

## Why this exists

Serene Bach’s MCP HTTP transport uses Bearer-token auth (`POST /mcp` with `Authorization: Bearer <token>`).  
ChatGPT’s MCP connector, however, only offers two choices:

1. **No authentication**
2. **OAuth 2.0**

If you already generated a read/write MCP token inside Serene Bach and want to use it from ChatGPT, this proxy bridges the gap without modifying the blog server.

## Build

```bash
task build-proxy
# or
go build -o bin/mcp-oauth-proxy ./cmd/mcp-oauth-proxy
```

The proxy is a single statically-linked Go binary (`CGO_ENABLED=0` is fine).

## Required environment variables

| Variable | Description |
|----------|-------------|
| `UPSTREAM_URL` | Base URL of the Serene Bach instance, e.g. `https://blog.example.com` |
| `MCP_BEARER_TOKEN` | The static token minted in `/admin/settings/ai` → **MCPトークン管理** |
| `OAUTH_CLIENT_ID` | Client ID you will enter in ChatGPT’s MCP settings (choose any string, e.g. `chatgpt_mcp`) |

## Optional environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PROXY_LISTEN_ADDR` | `:8080` | Address to listen on |
| `BASE_URL` | `http://localhost:8080` | Public URL of this proxy (used in OAuth metadata) |
| `AUTH_PIN` | *(empty)* | If set, the `/authorize` page requires this PIN before issuing a code |
| `OAUTH_REDIRECT_URIS` | *(empty)* | Comma-separated allowlist of `redirect_uri` values. **Strongly recommended for public deployments.** When empty, any URI is accepted (development only). |
| `TOKEN_TTL` | `24h` | Access-token lifetime |

## Run

### Quick start (development / localhost)

```bash
export UPSTREAM_URL=https://blog.example.com
export MCP_BEARER_TOKEN=sb_tok_xxxxxxxxxxxxxxxx
export OAUTH_CLIENT_ID=chatgpt_mcp
export BASE_URL=http://localhost:8080

./bin/mcp-oauth-proxy
```

### Production (PIN + redirect URI allowlist)

```bash
export UPSTREAM_URL=https://blog.example.com
export MCP_BEARER_TOKEN=sb_tok_xxxxxxxxxxxxxxxx
export OAUTH_CLIENT_ID=chatgpt_mcp
export BASE_URL=https://mcp-proxy.example.com
export AUTH_PIN=$(openssl rand -hex 4)               # e.g. 1a2b3c4d
export OAUTH_REDIRECT_URIS="https://chatgpt.com/..." # ChatGPT's redirect URI

./bin/mcp-oauth-proxy
```

When you first connect ChatGPT, the proxy shows a simple HTML form asking for the PIN.  This prevents anyone who knows the proxy URL from obtaining an access token.  The `OAUTH_REDIRECT_URIS` allowlist ensures that even if the `client_id` is leaked, an attacker cannot redirect the authorization code to their own endpoint.

## ChatGPT configuration

In ChatGPT’s MCP settings, choose **OAuth** and enter:

| Field | Value |
|-------|-------|
| Client ID | Same as `OAUTH_CLIENT_ID` env var |
| Authorization URL | `https://<proxy-host>/authorize` |
| Token URL | `https://<proxy-host>/token` |
| Scope | *(leave blank)* |

ChatGPT will discover the rest from `/.well-known/oauth-authorization-server` automatically.

The first time ChatGPT connects, open the authorization URL in your browser, enter the PIN (if configured), and approve the connection.  ChatGPT then receives an access token and starts calling `POST /mcp`.

## Security notes

1. **Always run this behind HTTPS** in production.  Bearer tokens and authorization codes must never travel over plain HTTP.
2. **Set `AUTH_PIN` and `OAUTH_REDIRECT_URIS`** for any public-facing deployment.  Without a PIN, anyone who discovers the proxy URL can complete the OAuth flow.  Without a redirect URI allowlist, anyone who knows the `client_id` can receive the authorization code on their own endpoint.
3. **Token storage is in-memory only**.  Restarting the proxy invalidates all outstanding access tokens.  ChatGPT will simply re-run the OAuth flow.
4. The proxy strips the upstream `WWW-Authenticate` header so Serene Bach’s internal Bearer realm is never exposed to the client.
5. Request bodies are capped at **1 MiB** and upstream requests time out after **30 seconds** to prevent resource exhaustion.

## Endpoints

| Path | Purpose |
|------|---------|
| `GET /.well-known/oauth-authorization-server` | OAuth metadata (JSON) |
| `GET /authorize` | Authorization endpoint (redirects with `?code=…`) |
| `POST /token` | Token endpoint (authorization-code → access-token) |
| `POST /mcp` | MCP JSON-RPC proxy (forwards to upstream `/mcp` with static Bearer token) |
| `OPTIONS /mcp` | CORS preflight |

## Limitations

- Only **authorization-code** grant with **PKCE S256** is supported (this is what ChatGPT uses).
- Token and code storage is **in-memory**; a restart clears all sessions.
- No refresh-token support.  When the access token expires, ChatGPT re-authorizes automatically.

## License

Same as Serene Bach — MIT.
