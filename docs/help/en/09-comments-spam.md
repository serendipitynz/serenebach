---
title: Comments and spam controls
slug: comments-spam
order: 90
---

# Comments and spam controls

Serene Bach can accept reader comments per entry. You review, approve, hide, or delete them from the admin UI.

## Acceptance modes

Configure the blog-wide policy under **Comments > Settings**:

| Setting | Behaviour |
|---|---|
| Accept | Comments that pass the spam checks are published immediately |
| Moderated | Comments stay hidden until an admin approves them |
| Don't accept | New comments are rejected |

The default is moderated. New blogs and anything spam-prone should stay moderated.

## Managing comments

The **Comments** screen filters by status:

- Pending
- Approved
- Hidden
- All

Click a comment body to see the author name, email, URL, IP address, and the full body in a modal.

## Spam protection

Some defenses are always on:

- A hidden field that catches automated submissions
- Rejection of submissions that arrive too quickly
- Per-IP throttling against rapid repeated posts
- Validation of URL fields and required inputs

You can layer additional defences via the admin UI:

## IP blacklist

Add one IP address or range per line:

```text
198.51.100.5
198.51.100.0/24
2001:db8::1
```

Matching submissions are silently dropped without persisting.

## Spam words

Banned words to check against the author name, email, URL, and body. One word per line:

```text
casino
viagra
```

A match silently drops the submission.

## Cloudflare Turnstile

To use Turnstile, set the following on the server:

```bash
SB_TURNSTILE_SITEKEY=0x4AAAAA...
SB_TURNSTILE_SECRET=0x4AAAAA...
```

When both are set, the comment form shows the Turnstile challenge.

## Likes and stamps

Apart from comments, entries can carry "likes" and "stamps". The four stamp kinds are `heart`, `laugh`, `wow`, and `party`.

If your template emits the matching tags, the buttons appear on the public page. Aggregate counts surface on the analytics screen.

## Related pages

- [Template editing](templates)
- [Publishing settings and OG cards](settings-publishing)
