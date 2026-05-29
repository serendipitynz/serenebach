---
title: Full-text search
slug: search
order: 105
---

# Full-text search

Serene Bach exposes a public full-text search at `/search?q=<query>`. It indexes every published entry's title, body, More section, and keywords. The same index also powers the admin entry list's search box and the MCP `search_entries` tool, so the three surfaces always agree on what matches.

## Adding a search form to your template

The `/search` route is a dynamic feature: only servers running the binary respond to it. Search is not embedded into static-only deployments by default. To add a search form to any template, paste the following snippet anywhere in the sidebar or header markup:

```html
<!-- BEGIN search_form -->
<form action="{search_url}" method="get" role="search">
  <input type="search" name="q" value="{search_query}" placeholder="Search...">
  <button type="submit">Search</button>
</form>
<!-- END search_form -->
```

- `{search_url}` expands to the absolute URL of `/search`; empty when search is disabled (see "Static rebuild" below).
- `{search_query}` carries the visitor's previous query on the results page, so the input still shows what they typed. On every other page it expands to an empty string.
- `{search_total}` reflects the total hit count on the results page and is empty elsewhere.

The `search_form` block is gated by the engine: when the search form is disabled (see below), the block is stripped to 0 and the form does not render. You can leave the markup in your template unconditionally — the block will appear or disappear automatically.

## How the search works

Search uses SQLite FTS5 with the trigram tokenizer. In practice:

- **Three-or-more-character tokens** match by substring through the FTS5 index. Both English and Japanese substrings are supported (`"東京タワー"` matches every entry that contains the four characters in that order).
- **One- and two-character tokens** (typical of Japanese 2-character compounds like 東京, 大阪, 会社) cannot be expressed in the trigram index, so they fall back to a per-row LIKE scan. They are still AND-combined with any longer tokens in the same query.
- **Separate words with spaces**: `機械学習 画像認識` searches for entries containing both terms. A long unsegmented phrase such as `機械学習による画像認識の最新手法` will only match if that exact substring appears in an entry — break the words apart for broader recall.
- **Operators and symbols pass through literally**: `node:fs`, `foo(bar)`, `A*B`, `C++`, and `max-age` are all valid search terms — FTS5 reserved words like `AND`/`OR`/`NOT` and operators like `-` are not interpreted.
- Hidden categories are excluded from the public `/search`. The admin entry list and the MCP `search_entries` tool both leave hidden categories in their results (each operates in its own administrative context).
- Drafts and closed entries never appear in the public `/search`.
- Queries are capped at 200 characters.

## Search results page

Search results render through your active template's main body, so the page picks up your sidebar, footer, header, and styling automatically. Two custom blocks are available for results-specific markup:

```html
<!-- BEGIN search_results -->
<h2>Results for "{search_query}"</h2>
<p>Found {search_total} entries.</p>
<!-- END search_results -->

<!-- BEGIN search_empty -->
<p>No entries matched "{search_query}". Try different keywords.</p>
<!-- END search_empty -->
```

- `search_results` renders only when the query produces at least one hit.
- `search_empty` renders only when the query was non-empty but matched no entries.
- Both blocks 0-strip on every other page so you can leave them in a shared layout.

The matched entries iterate through the normal `entry` block, so the standard `{entry_title}`, `{entry_permalink}`, `{entry_date}`, `{entry_description}`, and `{entry_tags}` tags all work as on the home and category lists.

## Static rebuild

The `/search` route is dynamic; static builds skip it by default and the `search_form` block 0-strips so visitors don't see a form that points at a missing route. If you serve a static snapshot **alongside** the dynamic backend (and the same-origin policy allows the GET to hit it), you can enable the form in static output by setting `weblogs.static_search_form_enabled = 1` directly in SQLite. The default is off as a safety against publishing a broken form.

## Repair (`reindex`)

INSERT / UPDATE / DELETE on entries automatically keep the FTS index in sync via SQLite triggers. If you ever suspect the index is out of step with the base entries table (manual DB edits, a dropped trigger, or after changing the tokenizer in a future migration), rebuild it:

```bash
./serenebach reindex
```

`reindex` is safe to run repeatedly; it deletes every row in `entries_fts` and reinserts every entry. There is no need to run it after an import — the importer goes through the same INSERT path, which fires the triggers.

## Limitations

- No bm25 ranking yet — results are ordered newest-first.
- Snippet highlighting is not implemented.
- Comments and flat pages are not in the search index.
- The static-snapshot search form has no working backend unless a dynamic instance is reachable.
