---
title: License
slug: license
order: 210
---

# License and third-party notices

The Go port of Serene Bach itself is published under the **MIT License**. See `LICENSE` in the repository for the full text.

This page summarises the third-party assets distributed inside the Serene Bach binary, along with the main Go libraries used to build it.

## Bundled third-party assets

Assets embedded into the Serene Bach binary and distributed with it.

### Noto Sans JP (Medium)

The font used to render text on OG card images.

- License: SIL Open Font License, Version 1.1 (OFL 1.1)
- Copyright notice: `Copyright 2014, 2015 Adobe Systems Incorporated`
- Reserved Font Name: `Source` (inherited from the upstream Source Han Sans)
- Full text: `NotoSansJP-LICENSE.txt`, bundled at the root of each release archive and present in the source tree at `internal/og/assets/NotoSansJP-LICENSE.txt`

In line with OFL 1.1, the font's copyright notice and full licence text are shipped with every distributed copy. "Noto" is a trademark of Google LLC.

### Ace editor

The code editor used for templates and entry bodies (`ajaxorg/ace`).

- License: BSD 3-Clause License
- Location: `web/templates/admin/assets/ace/` (embedded in the binary)

## Main Go libraries used in the build

The direct dependencies from `go.mod`. The full text of each licence is included with the respective module in the module cache fetched by `go mod download`.

| Library | Purpose | License |
|---|---|---|
| `github.com/go-chi/chi/v5` | HTTP router | MIT |
| `github.com/pressly/goose/v3` | DB migrations | MIT |
| `github.com/yuin/goldmark` | Markdown rendering | MIT |
| `modernc.org/sqlite` | Pure Go SQLite | BSD 3-Clause |
| `golang.org/x/crypto` | Password hashing, etc. | BSD 3-Clause |
| `golang.org/x/image` | OG card image processing | BSD 3-Clause |
| `golang.org/x/text` | Character-encoding handling | BSD 3-Clause |
| `gopkg.in/yaml.v3` | YAML parsing | MIT / Apache-2.0 |

For the complete dependency list, including indirect dependencies, see `go.mod` / `go.sum`.

## Related pages

- [Appendix](appendix)
- [Getting started](getting-started)
