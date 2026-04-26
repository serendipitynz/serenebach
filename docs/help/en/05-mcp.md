---
title: AI integration and MCP
slug: mcp
order: 50
---

# AI integration and MCP

Serene Bach supports MCP. From an MCP-aware editor or AI agent you can search entries, draft new ones, upload images, and more.

MCP is convenient, but a write-scoped token effectively lets the connected tool create or update entries on your behalf. Issue tokens carefully, with the minimum scope each consumer actually needs.

## What you can do

With **read** scope, the consumer can browse published entries, categories, tags, images, and a slice of analytics.

With **write** scope, the consumer can additionally create drafts, update entries, publish them, and upload images. The recommended workflow is to have the AI write to a draft, then review and publish it from the admin UI rather than letting the AI publish directly.

## HTTP connection

For remote AI tools, issue an MCP access token from **Settings > AI Settings**.

When issuing, set:

| Field | Description |
|---|---|
| Name | A label that helps you identify the token later |
| Scope | Read or Write |
| Acts as | The user attributed as the author of entries created or updated through this token |

The raw token is shown **once** at creation. There's no way to recover it later, so copy it into the consumer's configuration immediately.

The endpoint URL is:

```text
https://example.com/mcp
```

Authenticate with a Bearer token:

```bash
curl -X POST https://example.com/mcp \
  -H "Authorization: Bearer sb_mcp_..." \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize"}'
```

## stdio connection

For editors and local tools running on the same host, use the stdio transport:

```bash
./serenebach --db=/path/to/blog.db mcp serve
```

In Claude Desktop and similar tools, register the binary plus `mcp serve` as the server command:

```json
{
  "mcpServers": {
    "serenebach": {
      "command": "/path/to/serenebach",
      "args": ["--db=/path/to/blog.db", "mcp", "serve"]
    }
  }
}
```

stdio assumes that any process able to spawn the binary is trusted. On a shared host or a multi-user machine, be careful with binary permissions and the database file.

## In-admin AI writing assist

Separate from MCP, the admin UI ships an in-line AI writing assist. It requires `SB_AI_SECRET` on the server. Each user then registers their provider and API key under **Settings > AI Settings**.

In the entry editor, the assist can rewrite, continue, summarise, and suggest titles or tags. Image uploads can also generate alt text via the assist.

## Revoking tokens

Unused tokens can be revoked from the **AI Settings** screen. After revocation, MCP connections using that token are rejected.

## Related pages

- [Users and roles](users-roles)
- [Publishing settings and OG cards](settings-publishing)
- [Image uploads](images)
