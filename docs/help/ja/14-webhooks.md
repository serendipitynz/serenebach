---
title: Webhooks
slug: webhooks
order: 140
---

# Webhooks (外部通知)

記事を公開した、コメントが届いた、画像をアップロードした、といったタイミングで、指定した URL に JSON を POST して外部サービスに通知できます。Slack / Discord / Zapier / n8n などとの連携に使えます。

設定画面は **`/admin/settings/webhooks`** にあります。利用には「上級ユーザー」以上の権限が必要です。

## 通知できるイベント

| イベント ID         | いつ送信されるか                                        |
| ------------------- | ------------------------------------------------------ |
| `entry.published`   | 記事を公開したとき (下書き / 非公開 → 公開)              |
| `entry.updated`     | 公開済み記事を更新したとき                              |
| `entry.deleted`     | 記事を削除したとき                                      |
| `comment.received`  | コメントを受信したとき (承認前を含む)                    |
| `comment.approved`  | コメントが承認されたとき (自動承認・手動承認の両方)      |
| `image.uploaded`    | 画像（image kind）をアップロードしたとき               |

Webhook 1 つにつき複数のイベントを購読できます。チェックボックスで選択してください。

## 送信形式 (Payload format)

各 Webhook ごとに送信する JSON の形を選べます。

### Envelope (既定)

`id` / `event` / `timestamp` / `weblog` / `data` をネストしたままの JSON を送ります。自前受信や Zapier / n8n / Make など、フォーマット自由度の高い連携先向けです。

```json
{
  "id": "01J...",
  "event": "entry.published",
  "timestamp": "2026-05-16T12:34:56Z",
  "weblog": {
    "id": 1,
    "title": "My Blog",
    "url": "https://example.com/"
  },
  "data": {
    "id": 42,
    "slug": "hello",
    "title": "Hello, World!",
    "url": "https://example.com/entry/hello/",
    "status": "published",
    "author": { "id": 1, "name": "admin" },
    "published_at": "2026-05-16T12:34:56Z",
    "categories": ["雑記"],
    "tags": ["go", "serenebach"]
  }
}
```

### Flat (Slack / Discord / Slack Workflow Builder 互換)

ネストキーを `_` で連結して 1 階層にしたうえで、人間可読のサマリーを `text` (Slack 用) と `content` (Discord 用) として top-level に同梱します。

```json
{
  "event": "entry.published",
  "id": "01J...",
  "timestamp": "2026-05-16T12:34:56Z",
  "weblog_id": 1,
  "weblog_title": "My Blog",
  "weblog_url": "https://example.com/",
  "data_id": 42,
  "data_title": "Hello, World!",
  "data_url": "https://example.com/entry/hello/",
  "data_status": "published",
  "data_author_id": 1,
  "data_author_name": "admin",
  "data_categories_0": "雑記",
  "data_tags_0": "go",
  "text":    "[My Blog] 📝 New entry: Hello, World! — https://example.com/entry/hello/",
  "content": "[My Blog] 📝 New entry: Hello, World! — https://example.com/entry/hello/"
}
```

Slack と Discord はどちらも知らないキーを silently ignore するので、`flat` 形式 1 本で次の連携先すべてに直結できます。

- **Slack Incoming Webhook** — `text` を読んで投稿
- **Discord Incoming Webhook** — `content` を読んで投稿
- **Slack Workflow Builder の Webhook トリガー** — 任意のフラットキー (`data_title` / `weblog_title` …) を変数として参照
- **n8n / Zapier / 自前受信** — 既存のフラットキー、または `text` / `content` をそのまま利用

## Slack に通知する手順

最短ルートは Incoming Webhook + `flat` 形式の組み合わせです。

1. Slack ワークスペースで Incoming Webhook を作成し、`https://hooks.slack.com/services/...` の URL を控える
2. `/admin/settings/webhooks/new` を開き、URL 欄にそのまま貼り付け
3. **送信形式** で **Flat** を選択
4. 通知したいイベントにチェック → 保存

これだけでチャンネルに `[My Blog] 📝 New entry: ...` のようなメッセージが届きます。

Slack Workflow Builder のリッチなワークフローを組みたい場合は、Slack 側で Webhook トリガーのワークフローを作り、リクエスト変数として `data_title` / `data_url` 等を宣言したうえで「Send a message」ステップで自由に組み立てられます。

## Discord に通知する手順

Slack と同じく `flat` 形式 1 本でいけます。

1. Discord チャンネル設定 → 連携サービス → Webhook を作成し、URL を控える
2. `/admin/settings/webhooks/new` で URL を貼り付け、**送信形式** に **Flat** を選択
3. イベントを選んで保存

`content` キーが読み取られ、チャンネルにメッセージが投稿されます。

## 署名検証 (HMAC-SHA256)

「シークレット」を設定すると、リクエストに以下のヘッダが付きます。

```
X-SB-Event: entry.published
X-SB-Delivery: 01J...
X-SB-Signature: sha256=<hex>
```

`X-SB-Signature` は **リクエスト Body の生バイト列** を、設定したシークレットを鍵として HMAC-SHA256 で計算した値です。受信側で同じ計算を行い、定数時間比較で一致を確認してください。

受信側の擬似コード:

```python
import hmac, hashlib

def verify(secret: bytes, body: bytes, header: str) -> bool:
    if not header.startswith("sha256="):
        return False
    want = hmac.new(secret, body, hashlib.sha256).hexdigest()
    return hmac.compare_digest(want, header[len("sha256="):])
```

公開チャンネルや SaaS へ流す場合は必ずシークレットを設定し、受信側で検証してください。シークレットが未設定の場合は署名ヘッダ自体が付きません。

## 配信履歴とトラブルシュート

各 Webhook の行から **🔍 (詳細)** リンクで `/admin/settings/webhooks/{id}/deliveries` を開けます。直近 200 件の配信結果が表示されます。

- **200〜299** — 受信側が成功を返したケース。緑バッジ
- **400〜599** — 受信側がエラーを返したケース。赤バッジ + 「詳細」欄に **受信側のレスポンス本文** (最大 2 KiB) が記録されるので、それを見て原因を判断します
  - 例: `webhook: non-2xx response 400: invalid_payload` → Slack が JSON 形式を受け付けなかった (送信形式を `Flat` にすると解決することが多い)
  - 例: `webhook: non-2xx response 400: missing_text_or_fallback_or_attachments` → Slack に `text` キーが届いていない (`Flat` 形式なら自動付与されます)
- **error** — そもそも HTTP 応答がもらえなかったケース。詳細欄に DNS / 接続 / タイムアウト等の理由が出ます

「テスト送信」ボタン (一覧の **テスト** ボタン) で、設定済み Webhook に `event: "ping"` の synthetic payload を 1 件投げて疎通確認できます。

## 「有効 / 無効」の切替

一覧ページで状態列をクリックすると有効/無効をすぐに切り替えられます。配信を完全に止めたい場合は無効に。

サーバ全体で全 Webhook を緊急停止したい場合は `SB_WEBHOOKS_DISABLED=1` 環境変数を立ててプロセスを再起動してください。

## CGI モードでの挙動

CGI 配信モードではプロセスが応答後に終了するため、Webhook 配信は **同期 (タイムアウト 3 秒)** で送られます。サーバモード (常駐 HTTP) では goroutine 経由の非同期 (タイムアウト 10 秒) です。CGI 環境で重い受信側を購読すると、記事保存などの管理操作に最大 3 秒の遅延が乗ることに注意してください。

## 受信先 URL の制約 (SSRF 対策)

セキュリティ上、以下の宛先は登録できません:

- `http://` / `https://` 以外のスキーマ
- `localhost`, `127.0.0.1`, `::1` などの loopback
- `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16` などの RFC1918 プライベートネットワーク
- リンクローカル (`169.254.0.0/16`, `fe80::/10`)、マルチキャスト、未指定アドレス

これらは Webhook を内部ネットワーク探索に悪用されることを防ぐためです。URL バリデーションだけでなく、配送時の名前解決結果に対しても同じチェックが走るので、`example.internal` のような公開風ホスト名が内部 IP に解決される場合も拒否されます。
