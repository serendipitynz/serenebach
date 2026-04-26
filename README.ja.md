# Serene Bach

セルフホスト型 Go 製ウェブログエンジン。WordPress と Hugo の中間にある軽量な選択肢として位置づけています。

🌐 **[go.serenebach.net](https://go.serenebach.net)** — 機能紹介・スクリーンショット
📄 English: see [README.md](README.md)

## 概要

- 単一の静的 Go バイナリ (CGO 不要)
- SQLite (Pure Go: [`modernc.org/sqlite`](https://modernc.org/sqlite)) — DB サーバ別建て不要
- 常駐 HTTP サーバ・もしくは従来型レンタルサーバの CGI として動作
- 管理画面 UI / MCP サーバ / 管理画面ヘルプ もすべてバイナリに同梱
- ハイブリッド配信向けの静的サイト生成 (CDN 前段 + 動的管理画面)
- 旧 Serene Bach v3 (Perl 版) からの SQLite インポートに対応

## クイックスタート

```bash
go mod tidy
task seed   # dev DB を作成、マイグレーション適用、管理ユーザとサンプル投入
task dev    # :8080 でサーバ起動
```

公開サイト: <http://localhost:8080/>
管理画面: <http://localhost:8080/admin/login>

`task seed` が作る初期認証情報は `admin` / `changeme`。シード前に `SB_ADMIN_NAME` / `SB_ADMIN_PASSWORD` で上書き可能です。

`.env` のテンプレートは `.env.example` にあります。コピーして編集してください — AI 執筆補助を使う場合は `SB_AI_SECRET` の設定が必要です。

## ドキュメント

ドキュメント本体は英語で書いています。管理画面 (`/admin/help`) のヘルプは日本語と英語の両方を用意していて、ブラウザの言語設定に追従します。

| 内容 | リンク |
|---|---|
| 公開・管理画面の URL リファレンス | [docs/url-map.md](docs/url-map.md) |
| 環境変数 / フラグ / `task` ショートカット | [docs/configuration.md](docs/configuration.md) |
| 動作モード (HTTP サーバ / CGI / 静的生成) | [docs/deployment.md](docs/deployment.md) |
| Serene Bach v3 (Perl 版) からの移行 | [docs/importing-sb3.md](docs/importing-sb3.md) |
| アーキテクチャ + 設計ノート (CSRF, anti-spam, OG カード, アクセス解析 …) | [docs/architecture.md](docs/architecture.md) |
| エンドユーザー向けヘルプ (管理画面 `/admin/help` で閲覧可) | [docs/help/ja/](docs/help/ja/) |

## ライセンス

[MIT](LICENSE)
