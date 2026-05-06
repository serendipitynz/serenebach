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
- 旧 Serene Bach v2 (テキストファイル) / v3 (SQLite) (Perl 版) からのインポートに対応

## クイックスタート

[Go](https://go.dev/doc/install) と [Task](https://taskfile.dev/installation/) が必要です。

```bash
task dev    # :8080 でサーバ起動 (DB は最初のリクエストで自動作成)
```

ブラウザで <http://localhost:8080/> を開くと、管理者がまだ存在しない初回起動時は **`/setup`** に自動でリダイレクトされます。フォームから管理者ユーザ名・パスワード・サイトのタイトルを設定し、サンプル記事を投入するか選んだら完了です。以降は公開サイトが `/`、管理画面が `/admin/login` で動きます。

`task dev` は `SB_DEV=1` を自動で設定するため、`web/templates/admin/*.html` を編集するとサーバ再起動なしで次のリクエストから変更が反映されます。

CLI 派の方は `task seed` も従来通り使えます。dev DB を作って既定値 (`admin` / `changeme`、`SB_ADMIN_NAME` / `SB_ADMIN_PASSWORD` で上書き可) で管理者を作成し、ブラウザを介さずにセットアップを終えられます。

`.env` のテンプレートは `.env.example` にあります。コピーして編集してください — AI 執筆補助を使う場合は `SB_AI_SECRET` の設定が必要です。

## Docker

```bash
# ビルド
docker build -t serenebach .

# 実行: サーバを起動して http://localhost:8080/setup で管理者を作成
docker run -d -p 8080:8080 -v serenebach-data:/home/nonroot/data serenebach

# または CLI で seed する場合はパスワードを明示的に指定
docker run --rm -v serenebach-data:/home/nonroot/data -e SB_ADMIN_PASSWORD=<secret> serenebach seed
```

同梱の `docker-compose.yml` を使う場合:

```bash
docker compose up -d
```

### 公式イメージ (GHCR)

GitHub Container Registry (`ghcr.io/serendipitynz/serenebach`) に公式コンテナイメージを公開しています。

```bash
docker pull ghcr.io/serendipitynz/serenebach:4.0.0-beta.3

docker run -d -p 8080:8080 -v serenebach-data:/home/nonroot/data ghcr.io/serendipitynz/serenebach:4.0.0-beta.3
```

タグの種類:
- `latest` — デフォルトブランチの最新ビルド
- `4.0.0-beta.3`, `4.0.0`, … — リリースバージョンに対応するセマンティックバージョンタグ
- `main` — `main` ブランチの先端

本番運用では `latest` よりもリリースタグ固定を推奨します。QNAP Container Station / VPS での構成例は [docs/deployment.md](docs/deployment.md) を参照してください。

## 付属ツール

| ツール | 用途 |
|---|---|
| `./bin/serenebach mcp serve` | stdio 経由で MCP サーバを起動 (Claude Code / Cursor / Zed 向け) |
| `task build-proxy` | MCP OAuth プロキシ (`bin/mcp-oauth-proxy`) をビルド。ChatGPT の OAuth 専用 MCP クライアントと、Serene Bach の Bearer トークン認証 `/mcp` エンドポイントを中継します。環境変数や ChatGPT 設定方法は `cmd/mcp-oauth-proxy/README.md` を参照してください。 |

## ドキュメント

ドキュメント本体は英語で書いています。管理画面 (`/admin/help`) のヘルプは日本語と英語の両方を用意していて、ブラウザの言語設定に追従します。

| 内容 | リンク |
|---|---|
| 公開・管理画面の URL リファレンス | [docs/url-map.md](docs/url-map.md) |
| 環境変数 / フラグ / `task` ショートカット | [docs/configuration.md](docs/configuration.md) |
| 動作モード (HTTP サーバ / CGI / 静的生成 / Docker) | [docs/deployment.md](docs/deployment.md) |
| Serene Bach v2 / v3 (Perl 版) からの移行 | [docs/importing-sb3.md](docs/importing-sb3.md) |
| アーキテクチャ + 設計ノート (CSRF, anti-spam, OG カード, アクセス解析 …) | [docs/architecture.md](docs/architecture.md) |
| エンドユーザー向けヘルプ (管理画面 `/admin/help` で閲覧可) | [docs/help/ja/](docs/help/ja/) |

## ライセンス

[MIT](LICENSE)
