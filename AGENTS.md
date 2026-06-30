# AGENTS.md

Instructions for AI agents (Claude Code / Codex / Cursor / etc.) working in this repository. For human-oriented overviews, see [README.md](README.md) / [README.ja.md](README.ja.md). A Japanese mirror of this file lives at [AGENTS.ja.md](AGENTS.ja.md).

## About this project

Serene Bach is a Go rebuild of "Serene Bach", a Perl CGI weblog that Takuya Otani (SerendipityNZ Ltd) shipped between 2005 and 2017. The positioning:

> A modern revival of the casual, FTP-it-and-forget feel of the CGI era —
> not ActivityPub, not SaaS, not static-only.

The axis is **ownership and portability**: a single static binary, SQLite, FTP-able onto cheap shared hosting.

Site: <https://go.serenebach.net> / License: MIT.

## Language policy (project-level)

- **Code comments in English.**
- **Commit messages in English** (English is the default; quoted Japanese strings or proper nouns are fine).

The conversational language an agent uses with the user is a per-user preference, not a project rule — configure that at your tool's user/global level, not here.

## Hard constraints (do not violate)

- **No CGO** — `CGO_ENABLED=0` is required. Pure Go SQLite (`modernc.org/sqlite`) is chosen specifically to keep cross-compilation and CGI execution on shared hosting (e.g. Sakura Internet) working.
- **Single Linux binary + SQLite + FTP-able** — the silhouette is core to the positioning. Do not introduce dependencies that break it.
- **No Tailwind CSS** — hard ban (owner's personal policy). Not even "just for some quick styling". Use SCSS or vanilla CSS. `web/templates/admin/admin.css` follows "one stylesheet, no build step".
- **No `.pm`-style dynamic plugins** — extension is handled through other mechanisms (outbound webhooks / sbtemplate tags).
- **Trackback is permanently out of scope** — keep a "0-stripe" placeholder for SB3 template compatibility, but never implement the feature. It is a spam vector with no upside.
- **Always confirm with the user before adding a new production dependency.** Build-time tools (`go-task`, `goose`) are a separate bucket but should still not grow casually.

## Stack

| Area              | Choice                                                              |
| ----------------- | ------------------------------------------------------------------- |
| Language          | Go (single statically-linked binary, `CGO_ENABLED=0`)               |
| Router            | `github.com/go-chi/chi/v5`                                          |
| Database          | `modernc.org/sqlite` (pure Go SQLite)                               |
| Migrations        | `github.com/pressly/goose/v3` with embedded `migrations/*.sql`      |
| HTML templates    | `html/template` (admin) + custom sbtemplate-compat engine (public)  |
| Markdown          | `github.com/yuin/goldmark`                                          |
| Front-end         | htmx, plus Alpine.js / Preact only on heavy screens                 |
| CSS               | Vanilla CSS with custom properties                                  |
| AI                | Provider abstraction (OpenAI-compatible / Claude / LM Studio / Ollama) |
| Editor            | Ace (templates and post body, lazy-loaded, Solarized)               |
| Task runner       | `go-task` (`Taskfile.yml`)                                          |

## Common commands

```bash
task dev                  # Run locally on :8080 (./data/dev.db)
task seed                 # Seed an admin user (admin / changeme)
task migrate              # Auto-applied at startup, but can be run manually
task import -- <path>     # Import from SB2 / SB3 sources
task import-md -- <path>  # Import from a directory of markdown files
task build-site           # Static rebuild → ./data/public
task test                 # go test ./...
task build                # Native build to bin/serenebach
task build-all            # Cross-compile to 8 targets
task release              # Create a GitHub draft release
```

## Conventions

### Commit messages

Strict [Conventional Commits](https://www.conventionalcommits.org/).

- Summary **≤ 50 characters**, total body **≤ 2048 characters**.
- Type: `feat` / `fix` / `docs` / `style` / `refactor` / `perf` / `test` / `build` / `ci` / `chore` / `revert`.
- When adding a co-author, use `Co-Authored-By: Claude <noreply@anthropic.com>`. **Do not include the model name or version.**
- Do not pass `--no-verify` or `--no-gpg-sign` unless the user explicitly asks for it.

### PR comments

When posting via `gh pr comment` and similar, write **English first, then a `===` separator, then the Japanese translation** (this differs from commit messages, which stay English-only).

```
English message

===

日本語訳
```

### README sync

Any change visible to operators (env vars / CLI flags / Task targets / URL routes / DB columns admin users touch by hand / deploy modes / license) **must update [README.md](README.md) and [README.ja.md](README.ja.md) in the same commit**. Skip this for purely internal refactors.

### Git

- Do not commit or push unless the user explicitly asks.
- Do not rewrite history (`git rebase` etc.) unless explicitly asked.
- **Do not amend or force-push to respond to PR review comments.** Add a new normal commit per round of review feedback so reviewers can diff before/after, and so the PR Conversation tab keeps the "this commit addresses that comment" linkage. Use `git commit --amend` only when the user explicitly asks ("混ぜて" / "amend で"). Never run `git push --force` or `git push --force-with-lease` without explicit approval.
- Before a large change, summarize the intent and get agreement first.

### Verification

If the work touches lint or tests, run them before finishing. If you cannot run them, state why explicitly.

## SB3 template compatibility — read before touching sbtemplate

The owner cares strongly that **existing SB3 templates keep working unmodified**. When adding or modifying sbtemplate tags or blocks:

1. The canonical list of unsupported / divergent tags lives in [`internal/template/lint/lint.go`](internal/template/lint/lint.go) under `SevUnsupported` / `SevDiffers`. **Do not re-derive it from SB3 docs.**
2. **If you have a local copy of the SB3 Perl source tree** (kept as `_base/` in the project owner's working copy; SB3 itself is not yet open-sourced), grep `_base/lib/sb/Content*.pm` to confirm semantics. Relevant files: `Content.pm`, `Content/Common.pm`, `Entry.pm`, `List.pm`, `Message.pm`, `Category.pm`, `Profile.pm`, `Feed.pm`.
3. If semantics differ from SB3, expose **both via aliases** (e.g. `{permalink}` ↔ `{entry_permalink}`).
4. Unused blocks must be **0-striped** (`c.Block(name, 0)`). Leaving them undefined leaks `<!-- BEGIN foo -->...<!-- END foo -->` markers into public output.
5. **`{user_name}` returns the login name** for SB3 compatibility (changed 2026-04-25). The display name is `{user_disp_name}`, and `{user_login}` is an alias. **Do not revert this.**

### sbtemplate parser is line-based

`internal/template/sbtemplate/sbtemplate.go` scans line by line. **Place `<!-- BEGIN foo -->` and `<!-- END foo -->` on their own lines.** Inlining as `<!-- BEGIN link --><nav>{link_list}</nav><!-- END link -->` causes the block body to register as empty.

## Design choices that look surprising but are intentional

### Public POST endpoints sit **outside** the CSRF middleware

`/entry/{key}/{comment,like,stamp}` are mounted outside the `csrf.Middleware` group; `public.SameOriginGuard` runs in its place. Reasoning:

- Statically rebuilt HTML cannot embed a per-session CSRF token (no session, HttpOnly cookies).
- SB3 also did not require CSRF on comments (UX compatibility).

New reader-facing POSTs go under `public.MountMutations`. **Admin POSTs stay inside the CSRF middleware** — different threat model.

The same-origin allow-list is the union of `SB_PUBLIC_ALLOWED_ORIGINS` (CSV) and the `weblogs.base_url` value loaded at startup. Empty means fail-closed.

### `/admin/templates` is labelled "デザイン" in the UI

The URL/label mismatch is **deliberate**. The page started as a simple template list, then accumulated design settings (archive template, profile template, date formats, OG defaults, …). The URL was kept for compatibility while the UI label evolved to reflect the broader scope. Code symbols stay `templates*`; human-facing docs use "デザイン" (previously "デザイン設定").

The sibling menu "テンプレート編集" (`/admin/templates/active/edit`) is a separate shortcut that edits the currently active template directly.

### Importer charset detection is never hardcoded by version

SB2 typically ships EUC-JP and SB3 typically ships UTF-8, but real installations mix and match depending on server environment. **Always run content through `internal/jacharset.DecodeToUTF8`.** Detection order: Content-Type hint → `<meta charset>` / CSS `@charset` → ISO-2022-JP escape sniff → UTF-8 validity → Shift_JIS/EUC-JP byte-frequency score.

## Documentation

Read [docs/architecture.md](docs/architecture.md) before starting any new phase of work.

| Topic                                                  | Path                                             |
| ------------------------------------------------------ | ------------------------------------------------ |
| Architecture (CSRF / anti-spam / OG / MCP / AI / …)    | [docs/architecture.md](docs/architecture.md) — **must read** |
| Public + admin URL reference                           | [docs/url-map.md](docs/url-map.md)               |
| Environment variables, flags, `task` commands          | [docs/configuration.md](docs/configuration.md)   |
| Deploy modes (HTTP / CGI / static rebuild / Docker)    | [docs/deployment.md](docs/deployment.md)         |
| SB2 / SB3 migration                                    | [docs/importing-legacy-sb.md](docs/importing-legacy-sb.md) |
| Markdown directory import                              | [docs/importing-markdown.md](docs/importing-markdown.md) |
| End-user help (also served at `/admin/help`)           | [docs/help/](docs/help/)                         |

## Project status

The fastest way to see "what is shipped right now" is `git log --oneline -50`.
