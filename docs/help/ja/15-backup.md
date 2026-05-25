---
title: バックアップ
slug: backup
order: 15
---

# バックアップ

Serene Bach のすべてのデータを 1 つの ZIP アーカイブにまとめて書き出せます。

## CLI から実行

```bash
./serenebach backup --out ./backup-2026-05-23.zip
```

## オプション

| フラグ | 既定値 | 内容 |
|---|---|---|
| `--out <path>` | `backup-YYYY-MM-DD-HHMMSS.zip` | 出力先パス（`-` で stdout） |
| `--include-analytics` | off | Analytics DB / MCP audit DB を同梱（別ファイル設定時のみ） |
| `--include-public` | off | 静的書き出し成果物も同梱 |
| `--exclude <names>` | (なし) | `images` / `templates` を除外 |
| `--quiet` | off | 進捗を表示しない |

## アーカイブの中身

```
backup-2026-05-23-093045.zip
├── manifest.json
├── db/
│   ├── serenebach.db        ← VACUUM INTO した整合スナップショット
│   ├── analytics.db         ← --include-analytics かつ別ファイル時のみ
│   └── mcp_audit.db         ← 同上
├── img/                     ← アップロード画像
├── templates/               ← テンプレートアセット
└── public/                  ← --include-public 時のみ
```

## 復元方法

restore サブコマンドは現在ありません。手動で以下の手順を踏んでください：

1. ZIP を展開
2. `db/serenebach.db` を所定の場所に配置
3. 必要に応じて `img/` / `templates/` / `public/` も配置
4. `./serenebach migrate` を実行

## セキュリティ

- ZIP ファイルは `0o600` 権限で作成されます
- 出力先が CGI 環境の場合、`--out` の明示が必要です
