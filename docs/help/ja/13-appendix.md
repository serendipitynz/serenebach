---
title: 付録
slug: appendix
order: 200
---

# 付録

運用時に確認することが多い内容をまとめています。

## バックアップ

Serene Bach の主なデータは SQLite データベースと画像ディレクトリ、テンプレートディレクトリに保存されます。

最低限、次の 3 つをバックアップしてください。

- SQLite データベース
- `SB_IMAGE_DIR` の内容
- `SB_TEMPLATE_DIR` の内容

SQLite は稼働中に単純コピーすると壊れる場合があります。可能であれば SQLite の `.backup` を使ってください。

```bash
sqlite3 /var/lib/serenebach/blog.db ".backup /backup/blog.db"
```

画像とテンプレート用ファイルは、通常のファイルコピーや rsync でバックアップできます。

## よく使う環境変数

| 変数 | 内容 |
|---|---|
| `SB_DB` | SQLite データベースのパス |
| `SB_BASE_PATH` | デプロイ時のサブパス（例: `/sb/`） |
| `SB_DEV` | `1` にすると開発モード（テンプレートのキャッシュ無効化など） |
| `SB_ADMIN_NAME` | seed 時に作成する管理ユーザー名 |
| `SB_ADMIN_PASSWORD` | seed 時に作成する管理ユーザーのパスワード |
| `SB_ADMIN_EMAIL` | seed 時に作成する管理ユーザーのメールアドレス |
| `SB_SEED_NO_SAMPLES` | `1` にするとサンプル記事を作成しません |
| `SB_IMAGE_DIR` | アップロードファイルの保存先 |
| `SB_TEMPLATE_DIR` | テンプレート用ファイルの保存先 |
| `SB_REBUILD_OUT` | 静的再構築の出力先 |
| `SB_UPLOAD_MAX_MB` | ファイル 1 つあたりのアップロード上限 |
| `SB_TURNSTILE_SITEKEY` | Cloudflare Turnstile のサイトキー |
| `SB_TURNSTILE_SECRET` | Cloudflare Turnstile のシークレット |
| `SB_ANALYTICS_DISABLED` | `1` でアクセス解析を停止します |
| `SB_ANALYTICS_DB` | アクセス解析用 SQLite データベースのパス |
| `SB_ANALYTICS_RETENTION_DAYS` | アクセス解析データの保持日数 |
| `SB_AI_SECRET` | AI 設定の API キー暗号化に使う秘密値 |
| `SB_MCP_AUDIT_DB` | MCP の書き込み監査ログを別 SQLite ファイルに分けたい場合のパス |
| `SB_TRUSTED_PROXIES` | リバースプロキシ配下で `X-Forwarded-For` を信頼する CIDR 一覧 (カンマ区切り) |
| `SB_PUBLIC_ALLOWED_ORIGINS` | 公開側 POST (コメント、いいね、スタンプ) で受け付ける追加オリジン |

現在の設定値の一部は、管理画面の設定ページでも確認できます。

## HTML フォーマットの扱い

記事フォーマットで HTML を選ぶと、本文はそのまま公開ページへ出力されます。これは SB3 から続く挙動です。

信頼できるユーザーだけが記事を書けるブログでは便利ですが、不特定多数に執筆権限を渡す用途には向きません。共同運用では、誰に記事作成権限を渡すか、どのフォーマットを使うかを決めておいてください。

## ログ

Serene Bach のログは標準エラーに出力されます。常駐サーバとして動かしている場合は、systemd や利用しているプロセスマネージャのログで確認します。

```bash
journalctl -u serenebach
```

CGI として動かしている場合は、Web サーバのエラーログを確認してください。

## ログインできない場合

管理ユーザーのパスワードが分からなくなった場合は、バックアップを取った上で、データベースのユーザー情報を確認してください。

初期化直後であれば、`seed` を再実行して初期管理ユーザーを作り直せます。ただし、既存ユーザーや運用中データに影響がないか確認してから実行してください。

## OG カードが古い場合

記事を保存すると、通常は OG カード画像も更新されます。SNS 側に古い画像が残っている場合は、SNS のキャッシュが原因のことがあります。

記事編集画面の「OG カードを生成」ボタンから、手動で再生成することもできます。CGI 運用では、保存時の自動生成が無効になっているため、このボタンが OG カードを作る唯一の方法です。

ブログ共通の OG 背景や文字色を変更した場合は、反映まで少し時間がかかることがあります。静的配信の場合は、再構築も実行してください。

## バージョン確認

管理画面のフッターに Serene Bach のバージョンが表示されます。不具合報告の際は、このバージョンも添えてください。

## ライセンス

Go 版 Serene Bach は MIT ライセンスで公開されています。詳しくはリポジトリの `LICENSE` を参照してください。

## 関連ページ

- [はじめに](getting-started)
- [SB2 / SB3 からの移行と機能差異](sb3-migration)
- [静的再構築と配信](rebuild-publishing)
