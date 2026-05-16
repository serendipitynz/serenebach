---
title: Webhooks
slug: webhooks
order: 130
---

# Webhooks (outbound notifications)

Serene Bach can POST a JSON payload to a URL you choose whenever certain events happen — an entry is published, a comment is received, an image is uploaded. Use it to wire your blog into Slack, Discord, Zapier, n8n, or any custom HTTP receiver.

Configure subscriptions at **`/admin/settings/webhooks`**. The page requires the "power user" role or higher.

## Available events

| Event ID            | Fires when                                                          |
| ------------------- | ------------------------------------------------------------------- |
| `entry.published`   | An entry transitions to published (draft / closed → published)      |
| `entry.updated`     | A published entry is re-saved                                       |
| `entry.deleted`     | An entry is deleted                                                 |
| `comment.received`  | A comment is stored (including ones waiting for moderation)         |
| `comment.approved`  | A comment is approved (auto-approval or admin approval)             |
| `image.uploaded`    | An image is uploaded                                                |

A single webhook can subscribe to any combination of events via the checkbox grid.

## Payload formats

Each subscription picks one of two wire shapes.

### Envelope (default)

The full nested JSON: `id` / `event` / `timestamp` / `weblog` / `data`. Best for self-hosted receivers and middleware (Zapier / n8n / Make) that can parse arbitrary JSON.

```json
{
  "id": "01J...",
  "event": "entry.published",
  "timestamp": "2026-05-16T12:34:56Z",
  "weblog": {
    "id": 1,
    "title": "My Blog",
    "url": "https://example.com/"
  },
  "data": {
    "id": 42,
    "slug": "hello",
    "title": "Hello, World!",
    "url": "https://example.com/entry/hello/",
    "status": "published",
    "author": { "id": 1, "name": "admin" },
    "published_at": "2026-05-16T12:34:56Z",
    "categories": ["Misc"],
    "tags": ["go", "serenebach"]
  }
}
```

### Flat (Slack / Discord / Slack Workflow Builder compatible)

Nested keys are joined with `_` into a single-level object. A human-readable one-line summary is added under `text` (Slack) and `content` (Discord), both at the top level.

```json
{
  "event": "entry.published",
  "id": "01J...",
  "timestamp": "2026-05-16T12:34:56Z",
  "weblog_id": 1,
  "weblog_title": "My Blog",
  "weblog_url": "https://example.com/",
  "data_id": 42,
  "data_title": "Hello, World!",
  "data_url": "https://example.com/entry/hello/",
  "data_status": "published",
  "data_author_id": 1,
  "data_author_name": "admin",
  "data_categories_0": "Misc",
  "data_tags_0": "go",
  "text":    "[My Blog] 📝 New entry: Hello, World! — https://example.com/entry/hello/",
  "content": "[My Blog] 📝 New entry: Hello, World! — https://example.com/entry/hello/"
}
```

Both Slack and Discord silently ignore unknown top-level keys, so the same single payload satisfies every common receiver:

- **Slack Incoming Webhook** — reads `text`, posts to the channel
- **Discord Incoming Webhook** — reads `content`, posts to the channel
- **Slack Workflow Builder webhook trigger** — declare any flat key (`data_title`, `weblog_title`, …) as a request variable
- **n8n / Zapier / self-hosted** — the same flat keys, or `text` / `content`, are all consumable

## Sending to Slack

The shortest path is an Incoming Webhook with the `flat` format.

1. In Slack, create an Incoming Webhook integration and copy the `https://hooks.slack.com/services/...` URL.
2. Open `/admin/settings/webhooks/new`, paste the URL.
3. Set **Payload format** to **Flat**.
4. Check the events you want and save.

Messages like `[My Blog] 📝 New entry: ...` will appear in the channel.

For richer flows, build a Slack Workflow Builder "From Webhook" workflow, declare variables matching the flat keys (`data_title`, `data_url`, ...), and compose your own message in a "Send a message" step. Paste the `https://hooks.slack.com/triggers/...` URL into the webhook form and keep the format on **Flat**.

## Sending to Discord

Same approach as Slack — a single `flat` subscription works.

1. In your Discord channel settings, create a Webhook integration and copy the URL.
2. In `/admin/settings/webhooks/new`, paste the URL and set **Payload format** to **Flat**.
3. Pick events and save.

Discord reads `content` and ignores the rest.

## Signature verification (HMAC-SHA256)

When you set a secret on the subscription, every request carries:

```
X-SB-Event: entry.published
X-SB-Delivery: 01J...
X-SB-Signature: sha256=<hex>
```

`X-SB-Signature` is HMAC-SHA256 of the **raw request body**, keyed with your secret. Verify on your side with a constant-time comparison:

```python
import hmac, hashlib

def verify(secret: bytes, body: bytes, header: str) -> bool:
    if not header.startswith("sha256="):
        return False
    want = hmac.new(secret, body, hashlib.sha256).hexdigest()
    return hmac.compare_digest(want, header[len("sha256="):])
```

Always set a secret when posting to public channels or third-party services, and verify on the receiver. With no secret, the signature header is omitted entirely.

## Delivery log and troubleshooting

The 🔍 (deliveries) link on each row opens `/admin/settings/webhooks/{id}/deliveries`. It shows the most recent 200 attempts.

- **200–299** — the receiver acknowledged success.
- **400–599** — the receiver rejected the request. The detail column captures up to 2 KiB of the receiver's response body so you can see *why*.
  - Example: `webhook: non-2xx response 400: invalid_payload` — Slack didn't understand the JSON shape; switching to **Flat** usually fixes it.
  - Example: `webhook: non-2xx response 400: missing_text_or_fallback_or_attachments` — Slack received no `text`; **Flat** adds it automatically.
- **error** — no HTTP response was received at all. The detail column shows the DNS / connect / timeout diagnostic.

The **Test** button on the listing sends a synthetic `event: "ping"` payload so you can verify connectivity without waiting for a real event.

## Enable / disable

Click the state column in the list to toggle a single subscription on or off. To stop *all* webhook dispatch process-wide (e.g. during an incident with a misbehaving receiver), set `SB_WEBHOOKS_DISABLED=1` and restart.

## CGI mode

Under CGI deployments the process exits after the response, so webhook dispatch runs **synchronously** with a 3 second timeout. Under the long-lived HTTP server it runs in a goroutine with a 10 second timeout. Be aware that a slow receiver in CGI mode can add up to 3 seconds to admin actions like saving an entry.

## URL restrictions (SSRF protection)

For safety the following destinations are refused at form validation time and again at connect time:

- Schemes other than `http://` / `https://`
- Loopback (`localhost`, `127.0.0.1`, `::1`)
- RFC1918 private networks (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`)
- Link-local (`169.254.0.0/16`, `fe80::/10`), multicast, the unspecified address

This stops webhook URLs from being used to probe your internal network. The check runs on the resolved IP as well, so a public-looking hostname (e.g. `internal.example.com`) that resolves to an internal address is also rejected.
