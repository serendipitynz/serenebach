---
title: License
slug: license
order: 210
---

# License and third-party notices

The Go port of Serene Bach itself is published under the **MIT License**. See `LICENSE` in the repository for the full text.

This page summarises the third-party assets distributed inside the Serene Bach binary, along with the main Go libraries used to build it. The copyright notices and full license texts (conditions and disclaimers included) are collected in `THIRD-PARTY-NOTICES.txt`, bundled at the root of each release archive. Only the bundled font's OFL text ships separately, as `NotoSansJP-LICENSE.txt`.

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

- License: BSD 3-Clause License (`Copyright (c) 2010, Ajax.org B.V.`)
- Full text: `web/templates/admin/assets/ace/LICENSE` (next to the asset, embedded in the binary) and `THIRD-PARTY-NOTICES.txt` in the distribution
- Location: `web/templates/admin/assets/ace/` (embedded in the binary)

BSD 3-Clause requires the copyright notice, conditions, and disclaimer to accompany binary distributions, so the full license text is shipped with the distribution as above.

## Main Go libraries used in the build

The table below is a highlight of the main libraries (the direct `go.mod` dependencies). The copyright notices and full license texts for these and **every module compiled into the binary, indirect dependencies included**, are collected in `THIRD-PARTY-NOTICES.txt`, shipped with the distribution. That file is generated from the modules in the `go list -deps ./cmd/serenebach` import graph.

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

For the full license texts of every module in the binary see `THIRD-PARTY-NOTICES.txt`; for dependency versions see `go.mod` / `go.sum`.

## Related pages

- [Appendix](appendix)
- [Getting started](getting-started)
