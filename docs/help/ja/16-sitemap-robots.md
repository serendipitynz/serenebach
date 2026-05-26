---
title: Sitemap / robots.txt
slug: sitemap-robots
order: 16
---

# Sitemap / robots.txt

Serene Bach は検索エンジン向けに `sitemap.xml` と `robots.txt` を自動生成します。

## sitemap.xml

`/sitemap.xml` には以下の URL が含まれます。

- トップページ `/`
- 公開記事 `/entry/<slug>/`
- 非 hidden カテゴリー `/category/<slug>/`
- タグ `/tag/<slug>/`
- 公開 flat page

月別アーカイブ、年別アーカイブ、プロフィールページ、RSS/Atom、llms.txt 系は含まれません。

## robots.txt

`/robots.txt` は全クローラーに対して `Allow: /` を示し、`Sitemap:` 行に `sitemap.xml` の URL を含めます。

## 有効 / 無効の切り替え

管理画面「設定 > サイト設定」で `sitemap.xml` と `robots.txt` の配信を個別に ON/OFF できます。無効にすると該当ファイルは **404** を返します（空ファイルではありません）。

## 静的書き出し

`task build-site` または「記事公開時の自動再構築」が有効な場合、生成された `sitemap.xml` / `robots.txt` は静的出力先にも書き出されます。OFF に切り替えた後の再構築で、古いファイルは自動的に削除されます。

## Google Search Console への登録

1. [Google Search Console](https://search.google.com/search-console) でプロパティを追加します。
2. 「Sitemap」メニューから `https://<your-domain>/sitemap.xml` を提出します。
