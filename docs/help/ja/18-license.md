---
title: ライセンス
slug: license
order: 210
---

# ライセンスと third-party 表記

Go 版 Serene Bach 本体は **MIT ライセンス** で公開されています。全文はリポジトリの `LICENSE` を参照してください。

このページでは、Serene Bach のバイナリに同梱して配布しているサードパーティ資材と、ビルドに使用している主な Go ライブラリのライセンスをまとめています。

## 同梱しているサードパーティ資材

Serene Bach のバイナリに埋め込まれて配布される資材です。

### Noto Sans JP (Medium)

OG カード画像描画に使用しているフォントです。

- ライセンス: SIL Open Font License, Version 1.1 (OFL 1.1)
- 著作権表示: `Copyright 2014, 2015 Adobe Systems Incorporated`
- Reserved Font Name: `Source`（由来元 Source Han Sans に基づく）
- ライセンス全文: リリースアーカイブのルートに同梱される `NotoSansJP-LICENSE.txt`、およびソースツリーの `internal/og/assets/NotoSansJP-LICENSE.txt`

OFL 1.1 の要件に従い、フォントの著作権表示とライセンス全文を各配布コピーに同梱しています。"Noto" は Google LLC の商標です。

### Ace エディター

テンプレートと記事本文の編集に使用しているコードエディターです（`ajaxorg/ace`）。

- ライセンス: BSD 3-Clause License
- 配置: `web/templates/admin/assets/ace/`（バイナリに埋め込み）

## ビルドに使用している主な Go ライブラリ

`go.mod` の直接依存です。各ライセンスの全文は、`go mod download` で取得されるモジュールキャッシュ内の各モジュールに含まれます。

| ライブラリ | 用途 | ライセンス |
|---|---|---|
| `github.com/go-chi/chi/v5` | HTTP ルーター | MIT |
| `github.com/pressly/goose/v3` | DB マイグレーション | MIT |
| `github.com/yuin/goldmark` | Markdown レンダリング | MIT |
| `modernc.org/sqlite` | Pure Go SQLite | BSD 3-Clause |
| `golang.org/x/crypto` | パスワードハッシュなど | BSD 3-Clause |
| `golang.org/x/image` | OG カード画像処理 | BSD 3-Clause |
| `golang.org/x/text` | 文字コード処理 | BSD 3-Clause |
| `gopkg.in/yaml.v3` | YAML パース | MIT / Apache-2.0 |

間接依存を含む完全な依存一覧は `go.mod` / `go.sum` を参照してください。

## 関連ページ

- [付録](appendix)
- [はじめに](getting-started)
