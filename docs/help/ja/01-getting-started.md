---
title: はじめに
slug: getting-started
order: 10
---

# はじめに

Serene Bach は、1 つの実行ファイルと SQLite データベースで動作します。VPS などで常駐サーバとして動かすことも、CGI に対応したレンタルサーバで動かすこともできます。

## 動作に必要なもの

- Linux / macOS / Windows のいずれか
- データベースや画像を書き込めるディレクトリ
- CGI として使う場合は、CGI 実行に対応した Web サーバ

配布済みバイナリを使う場合、Go や Python などのランタイムを別途入れる必要はありません。

## 最初の起動準備

はじめて使うときは、まずデータベースに管理ユーザーと標準テンプレートを作成します。

```bash
./serenebach seed
```

初期ユーザーは `admin`、初期パスワードは `changeme` です。公開前に必ずパスワードを変更してください。

サンプル記事を入れずに空のブログとして始めたい場合は、次のように実行します。

```bash
SB_SEED_NO_SAMPLES=1 ./serenebach seed
```

## 常駐サーバとして使う

```bash
./serenebach --addr=:8080 serve
```

ブラウザで `http://localhost:8080/admin/login` を開くと管理画面に入れます。公開サイトは `http://localhost:8080/` です。

データベースの場所を指定したい場合は `--db` を使います。

```bash
./serenebach --db=/var/lib/serenebach/blog.db --addr=:8080 serve
```

## CGI として使う

CGI 環境では、同じバイナリを CGI スクリプトとして配置できます。配布バイナリ（または `task build-linux-amd64` などでクロスコンパイルしたバイナリ）をサーバの種類に合わせて選び、CGI として動かしたい名前（慣例的には `serenebach.cgi`）にリネームしてアップロードします。Web サーバ側で CGI 実行を有効にし、実行権限を付けてください。

```bash
mv serenebach-linux-amd64 serenebach.cgi
chmod +x serenebach.cgi
```

CGI として呼び出された場合、Serene Bach は自動的に CGI モードで 1 リクエストずつ処理します。

## 最初の記事を書く

1. 管理画面にログインします。
2. 左メニューの「新規記事」を開きます。
3. タイトルと本文を入力します。
4. 必要に応じてカテゴリー、タグ、公開日時を設定します。
5. 状態を「公開」にして保存します。

保存した記事は、左メニュー上部の「公開サイト」から確認できます。

## 次に読むページ

- [記事の作成と管理](entries)
- [テンプレート編集](templates)
- [公開設定と OG カード](settings-publishing)
