---
title: 静的再構築と配信
slug: rebuild-publishing
order: 110
---

# 静的再構築と配信

Serene Bach は、アクセスごとにページを生成する動的配信と、あらかじめ HTML を書き出して配信する静的配信の両方に対応しています。

通常は動的配信のまま運用できます。アクセスが多いサイト、CDN に載せたいサイト、公開側をできるだけ軽くしたいサイトでは、静的再構築を使います。

## 管理画面から再構築する

「再構築」画面で「今すぐ再構築」を実行すると、公開サイト一式を書き出します。

出力先はサーバ設定の `SB_REBUILD_OUT` で指定できます。初期値は `./data/public` です。

再構築中にもう一度実行しようとした場合は、先に始まった処理が優先されます。

## コマンドから再構築する

コマンドラインからも静的サイトを生成できます。

```bash
./serenebach build --out=./public
```

一覧ページに表示する記事数を変える場合は `--limit` を指定します。

```bash
./serenebach build --out=./public --limit=20
```

## 出力されるもの

再構築では、次のようなファイルが作成されます。

- トップページ
- 記事ページ
- カテゴリー、タグ、アーカイブページ
- RSS / Atom フィード
- llms.txt と llms-full.txt
- テンプレートの CSS
- アップロード画像
- テンプレート用ファイル

管理画面、ログイン画面、MCP エンドポイントは静的ファイルには含まれません。管理機能を使うには、Serene Bach 本体の動的サーバも必要です。

## 静的配信の例

生成したディレクトリは、Nginx、Apache、Cloudflare Pages、S3 互換ストレージなど、通常の静的ファイル配信環境へ置けます。

Nginx の例:

```nginx
server {
    listen 80;
    root /var/www/html;
    try_files $uri $uri/ =404;
}
```

## 更新タイミング

静的配信では、記事を保存しただけでは出力済み HTML は更新されません。記事や設定を変更した後は、再構築を実行してください。

cron などで定期的に再構築することもできます。

```cron
0 * * * * cd /var/lib/serenebach && ./serenebach build --out=/var/www/html
```

## Sitemap / robots.txt

Serene Bach は検索エンジン向けに `sitemap.xml` と `robots.txt` を自動生成します。

### sitemap.xml

`/sitemap.xml` には以下の URL が含まれます。

- トップページ `/`
- 公開記事 `/entry/<slug>/`
- 非 hidden カテゴリー `/category/<slug>/`
- タグ `/tag/<slug>/`
- 公開 flat page

月別アーカイブ、年別アーカイブ、プロフィールページ、RSS/Atom、llms.txt 系は含まれません。

### robots.txt

`/robots.txt` は全クローラーに対して `Allow: /` を示し、`Sitemap:` 行に `sitemap.xml` の URL を含めます。

### 有効 / 無効の切り替え

管理画面「設定 > 基本設定」で `sitemap.xml` と `robots.txt` の配信を個別に ON/OFF できます。無効にすると該当ファイルは **404** を返します（空ファイルではありません）。

### 静的書き出し

`task build-site` または「記事公開時の自動再構築」が有効な場合、生成された `sitemap.xml` / `robots.txt` は静的出力先にも書き出されます。OFF に切り替えた後の再構築で、古いファイルは自動的に削除されます。

### Google Search Console への登録

1. [Google Search Console](https://search.google.com/search-console) でプロパティを追加します。
2. 「Sitemap」メニューから `https://<your-domain>/sitemap.xml` を提出します。

## 関連ページ

- [公開設定と画面設定](settings-publishing)
- [ライブラリ](images)
- [プレビュー機能](preview)
