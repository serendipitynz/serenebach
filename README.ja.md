<a href="https://go.serenebach.net/"><img src="https://raw.githubusercontent.com/serendipitynz/serenebach/main/web/templates/admin/assets/sb_logo_dark.svg?sanitize=true" alt="Serene Bach" width="400"></a>

[![CI result](https://github.com/serendipitynz/serenebach/workflows/CI/badge.svg)](https://github.com/serendipitynz/serenebach/actions?query=workflow%3ACI)
[![Go Report Card](https://goreportcard.com/badge/github.com/serendipitynz/serenebach)](https://goreportcard.com/report/github.com/serendipitynz/serenebach)

---

セルフホスト型 Go 製ウェブログエンジン。WordPress と Hugo の中間にある軽量な選択肢として位置づけています。

[![管理画面ツアー (17秒、音声なし) — クリックで HD 版 mp4](docs/assets/admin-tour.webp)](https://go.serenebach.net/screenshots/admin-tour.mp4)

🌐 **[go.serenebach.net](https://go.serenebach.net)** — 機能紹介・スクリーンショット
📄 English: see [README.md](README.md)

## 概要

- 単一の静的 Go バイナリ (CGO 不要)
- SQLite (Pure Go: [`modernc.org/sqlite`](https://modernc.org/sqlite)) — DB サーバ別建て不要
- 常駐 HTTP サーバ・もしくは従来型レンタルサーバの CGI として動作
- 管理画面 UI / MCP サーバ / 管理画面ヘルプ もすべてバイナリに同梱
- ハイブリッド配信向けの静的サイト生成 (CDN 前段 + 動的管理画面)
- テンプレートから JS / Web フォントファイルも参照可能（画像・CSS と同様にアップロードして利用）
- 旧 Serene Bach v2 (テキストファイル) / v3 (SQLite) (Perl 版) からのインポートに対応。YAML front-matter 付き markdown ファイル群からの取り込みもサポート
- Outbound Webhooks: 記事公開 / コメント受信 / 画像アップロード で Slack / Discord / Zapier / n8n に通知
- 画像だけでなく音声・文書・動画もライブラリで管理・アップロード可能（Markdown/HTML 記事への挿入に対応）
- `sitemap.xml` と `robots.txt` の自動生成（サイト設定で ON/OFF 切替可能）
- 記事・固定ページ単位の SEO メタ情報: 要約（`{entry_excerpt}`、SB3 `sum` 互換。`<meta name="description">` / OG に反映）、canonical URL、`noindex` トグル（`sitemap.xml` からも除外）
- `/search?q=…` の全文検索（SQLite FTS5 + trigram トークナイザ）。任意言語の部分一致（日本語含む）に対応し、管理画面の記事一覧検索と MCP の `search_entries` ともインデックスを共有

## クイックスタート

[Go](https://go.dev/doc/install) と [Task](https://taskfile.dev/installation/) が必要です。

```bash
task dev    # :8080 でサーバ起動 (DB は最初のリクエストで自動作成)
```

ブラウザで <http://localhost:8080/> を開くと、管理者がまだ存在しない初回起動時は **`/setup`** に自動でリダイレクトされます。フォームから管理者ユーザ名・パスワード・サイトのタイトルを設定し、サンプル記事を投入するか選んだら完了です。以降は公開サイトが `/`、管理画面が `/admin/login` で動きます。

`task dev` は `SB_DEV=1` を自動で設定するため、`web/templates/admin/*.html` を編集するとサーバ再起動なしで次のリクエストから変更が反映されます。

CLI 派の方は `task seed` も従来通り使えます。dev DB を作って既定値 (`admin` / `changeme`、`SB_ADMIN_NAME` / `SB_ADMIN_PASSWORD` で上書き可) で管理者を作成し、ブラウザを介さずにセットアップを終えられます。

`.env` のテンプレートは `.env.example` にあります。コピーして編集してください — AI 執筆補助を使う場合は `SB_AI_SECRET` の設定が必要です。

サーバモードの HTTP タイムアウトとグレースフルシャットダウンは適切な既定値が設定されており、`SB_READ_HEADER_TIMEOUT` / `SB_WRITE_TIMEOUT` / `SB_IDLE_TIMEOUT` / `SB_MAX_HEADER_BYTES` / `SB_SHUTDOWN_TIMEOUT` で調整できます。`SB_TZ`（例: `Asia/Tokyo`）を設定するとアーカイブの月／年境界や記事の日付描画に使うタイムゾーンを固定でき、ホストの時計が違ってもバイナリが同じ出力を生成します。`SB_CSRF_MULTIPART_MAX_BYTES`（バイト単位、既定 `1048576`）は no-JS フォールバックフォームに対して CSRF ミドルウェアが認証前に読み取る multipart ボディの上限を制御します。JS 経由のアップロードは `X-CSRF-Token` ヘッダでトークンを送るためこの上限は適用されません。全環境変数の一覧は [docs/configuration.md](docs/configuration.md) を参照してください。

手元のバイナリのバージョンを確認したいときは `serenebach --version` を実行してください。データベースや環境変数の設定が壊れている／まだ無い状態でも動くので、新しく展開したバイナリの識別にそのまま使えます。

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
docker pull ghcr.io/serendipitynz/serenebach:latest

docker run -d -p 8080:8080 -v serenebach-data:/home/nonroot/data ghcr.io/serendipitynz/serenebach:latest
```

タグの種類:
- `latest` — デフォルトブランチの最新ビルド
- リリースタグ (`4.0.0-beta.N`, `4.0.0`, …) — 現行バージョンは [GitHub Releases](https://github.com/serendipitynz/serenebach/releases) を参照
- `main` — `main` ブランチの先端

本番運用では `latest` よりもリリースタグ固定を推奨します。QNAP Container Station / VPS での構成例は [docs/deployment.md](docs/deployment.md) を参照してください。

## 品質チェック

CI でも push / PR ごとに同じコマンドが走ります:

- `task lint` — `.golangci.yml` を使って `golangci-lint` を実行 (`staticcheck` に加えて gocyclo (しきい値 15、goreportcard と同値) などのプロジェクト lint セットを含む)
- `task test` — `go test ./...` を実行

## 付属ツール

| ツール | 用途 |
|---|---|
| `./bin/serenebach mcp serve` | stdio 経由で MCP サーバを起動 (Claude Code / Cursor / Zed 向け) |
| `./bin/serenebach backup` | DB・画像・テンプレートの整合 ZIP スナップショットを作成。`--include-analytics` / `--include-public` オプション付き |
| `task build-proxy` | MCP OAuth プロキシ (`bin/mcp-oauth-proxy`) をビルド。ChatGPT の OAuth 専用 MCP クライアントと、Serene Bach の Bearer トークン認証 `/mcp` エンドポイントを中継します。環境変数や ChatGPT 設定方法は `cmd/mcp-oauth-proxy/README.md` を参照してください。 |

## ドキュメント

ドキュメント本体は英語で書いています。管理画面 (`/admin/help`) のヘルプは日本語と英語の両方を用意していて、ブラウザの言語設定に追従します。

| 内容 | リンク |
|---|---|
| 公開・管理画面の URL リファレンス | [docs/url-map.md](docs/url-map.md) |
| 環境変数 / フラグ / `task` ショートカット | [docs/configuration.md](docs/configuration.md) |
| 動作モード (HTTP サーバ / CGI / 静的生成 / Docker) | [docs/deployment.md](docs/deployment.md) |
| Serene Bach v2 / v3 (Perl 版) からの移行 | [docs/importing-legacy-sb.md](docs/importing-legacy-sb.md) |
| Markdown ファイル群からの取り込み | [docs/importing-markdown.md](docs/importing-markdown.md) |
| アーキテクチャ + 設計ノート (CSRF, anti-spam, OG カード, アクセス解析 …) | [docs/architecture.md](docs/architecture.md) |
| エンドユーザー向けヘルプ (管理画面 `/admin/help` で閲覧可) | [docs/help/ja/](docs/help/ja/) |

## ライセンス

[MIT](LICENSE)
