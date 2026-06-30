# AGENTS.ja.md

このリポジトリで作業する AI エージェント (Claude Code / Codex / Cursor 等) 向けの指示書。人間向け概要は [README.md](README.md) / [README.ja.md](README.ja.md) を参照。英語の正本は [AGENTS.md](AGENTS.md)。食い違いがある場合は [AGENTS.md](AGENTS.md) を優先する。

## このプロジェクトについて

Serene Bach は、2005–2017 年に大谷拓也 (Takuya Otani / SerendipityNZ Ltd) が開発した Perl CGI 製ブログ "Serene Bach" の **Go 版リビルド**。ポジショニング:

> ActivityPub でも SaaS でも static-only でもない、
> 「FTP で置けば動く」CGI 時代の気軽さを現代のランタイムで復刻する。

軸は **ownership and portability** — single static binary + SQLite + FTP-able。

公式サイト: <https://go.serenebach.net> / ライセンス: MIT。

## 言語ポリシー (プロジェクトレベル)

- **コードコメントは英語**
- **コミットメッセージは英語** (英語が既定。引用される文字列・固有名詞のみ日本語混在可)

エージェントとユーザの会話言語は個人の好みであり、プロジェクトのルールではない。各ツールのユーザ/グローバル設定で各自設定する。

## ハードな制約 (絶対に崩さない)

- **CGO は使わない** — `CGO_ENABLED=0` 前提。pure Go SQLite (`modernc.org/sqlite`) を選んでいる理由は、クロスコンパイルとさくらレンタルサーバ等での CGI 動作維持。
- **Single Linux binary + SQLite + FTP-able** — このシルエットは positioning の核。壊す依存は入れない。
- **Tailwind CSS は禁止** — オーナーの個人ポリシー。「軽くスタイル当てるだけ」も禁止。SCSS / バニラ CSS を使う。`web/templates/admin/admin.css` は "One stylesheet, no build step"。
- **`.pm` 動的プラグインは導入しない** — 拡張は別系統 (outbound webhooks / sbtemplate タグ) で考える。
- **Trackback は完全に out of scope** — テンプレ互換のため "0-stripe" だけ残し、機能としては絶対に実装しない。スパムベクタなだけ。
- **新しい本番依存を入れる前に必ずユーザに確認** — `go-task` / `goose` のような開発・ビルド枠でも気軽には増やさない。

## 技術スタック

| 項目             | 決定                                                                  |
| ---------------- | --------------------------------------------------------------------- |
| 言語             | Go (single statically-linked binary, `CGO_ENABLED=0`)                 |
| ルータ           | `github.com/go-chi/chi/v5`                                            |
| DB               | `modernc.org/sqlite` (pure Go SQLite)                                 |
| マイグレーション | `github.com/pressly/goose/v3` + `migrations/*.sql` の embed           |
| HTML テンプレ    | `html/template` (admin) + 自作 sbtemplate 互換エンジン (公開側)       |
| Markdown         | `github.com/yuin/goldmark`                                            |
| Front-end        | htmx + 必要に応じて Alpine.js / Preact (重い画面のみ)                 |
| CSS              | バニラ CSS + custom properties                                        |
| AI               | provider 抽象 (OpenAI 互換 / Claude / LM Studio / Ollama)             |
| Editor           | Ace (テンプレ・本文編集、lazy-loaded、Solarized)                      |
| タスクランナー   | `go-task` (`Taskfile.yml`)                                            |

## よく使うコマンド

```bash
task dev                  # :8080 でローカル起動 (./data/dev.db)
task seed                 # 管理ユーザを seed (admin / changeme)
task migrate              # 起動時自動だが手動でも可
task import -- <path>     # SB2/SB3 から取り込み
task import-md -- <path>  # markdown ファイル群から取り込み
task build-site           # 静的書き出し → ./data/public
task test                 # go test ./...
task build                # bin/serenebach をネイティブビルド
task build-all            # 8 ターゲットへクロスコンパイル
task release              # gh draft release を作成
```

## 作法

### コミットメッセージ

[Conventional Commits](https://www.conventionalcommits.org/) に厳密に従う。

- summary は **50 文字以下**、body 含めて 2048 文字以下
- type は `feat` / `fix` / `docs` / `style` / `refactor` / `perf` / `test` / `build` / `ci` / `chore` / `revert`
- co-author は `Co-Authored-By: Claude <noreply@anthropic.com>` の形式。**モデル名/バージョンは含めない**
- `--no-verify` / `--no-gpg-sign` はユーザの明示指示なき限り禁止

### PR コメント

`gh pr comment` 等は **英語先 → `===` 区切り → 日本語訳** の順で書く (コミットメッセージとは別ルール)。

```
English message

===

日本語訳
```

### README 同期

operator から見える変更 (env / CLI フラグ / Task ターゲット / URL ルート / admin が手で触る DB カラム / デプロイモード / ライセンス) は、**[README.md](README.md) と [README.ja.md](README.ja.md) を同じコミットで更新**する。内部リファクタや operator から見えない変更ではスキップ可。

### Git

- ユーザに明示的に頼まれない限り **commit / push しない**
- `git rebase` 等の履歴書き換えも明示要求のみ
- **PR レビューコメントへの対応で `--amend` + force push を使わない。** レビュー対応は毎回 **新しい通常コミット** として積み増す。理由: レビュワが指摘前後を diff で追えるようにする / PR の Conversation タブで「このコミットでこのコメントに対応した」という対応関係を残す / force-push 事故を避ける。`git commit --amend` は **ユーザが明示的に「混ぜて」「amend で」と指示したときだけ** 使う。`git push --force` / `git push --force-with-lease` も **明示承認なしには絶対に走らせない**。
- 大きな変更の前は意図を要約して合意してから動く

### 検証

lint / test に関連するタスクは終了前に走らせる。走らせられない場合は理由を明示する。

## SB3 テンプレート互換 — 触る前に必ず読む

オーナーは **既存の SB3 テンプレートが手直しなく動くこと** を強く重視している。sbtemplate のタグ・ブロックを追加・変更するときは:

1. 未対応 / 振る舞いが異なるタグの正本リストは [`internal/template/lint/lint.go`](internal/template/lint/lint.go) の `SevUnsupported` / `SevDiffers`。**SB3 ドキュメントから再導出してはいけない**
2. **SB3 の Perl ソースツリーがローカルにある場合** (プロジェクトオーナーのワーキングコピーに `_base/` として保持。SB3 本体はまだ OSS 化されていない) は `_base/lib/sb/Content*.pm` を grep して挙動を確認する。主要ファイル: `Content.pm`, `Content/Common.pm`, `Entry.pm`, `List.pm`, `Message.pm`, `Category.pm`, `Profile.pm`, `Feed.pm`
3. SB3 と意味が違う場合は **alias で両対応** にする (例: `{permalink}` ↔ `{entry_permalink}`)
4. 使わないブロックは **0-stripe で消す** (`c.Block(name, 0)`)。未定義のままだと `<!-- BEGIN foo -->...<!-- END foo -->` が漏れる
5. **`{user_name}` は SB3 互換でログイン名を返す** (2026-04-25 切替)。表示名は `{user_disp_name}`、`{user_login}` は alias。**戻さない**

### sbtemplate パーサは行ベース

`internal/template/sbtemplate/sbtemplate.go` は1行ずつ走査する。**`<!-- BEGIN foo -->` と `<!-- END foo -->` は単独行に置く**。`<!-- BEGIN link --><nav>{link_list}</nav><!-- END link -->` のように 1行にまとめるとブロック本体が空として記録される。

## 一見奇妙だが意図的な設計

### 公開側 POST は CSRF middleware の **外**

`/entry/{key}/{comment,like,stamp}` は `csrf.Middleware` グループの外にマウントし、代わりに `public.SameOriginGuard` で守る。理由:

- 静的書き出した HTML には per-session CSRF token を埋められない (セッション無し、HttpOnly cookie)
- SB3 もコメントに CSRF を要求していなかった (UX 互換)

新しい reader-facing POST は `public.MountMutations` 配下に置く。**管理画面 POST は CSRF middleware 内**を維持する (脅威モデルが違う)。

allow-list = `SB_PUBLIC_ALLOWED_ORIGINS` (CSV) ∪ 起動時の `weblogs.base_url`。空なら fail-closed。

### `/admin/templates` の UI ラベルは「デザイン」

URL とラベルの食い違いは **意図的**。最初は単純なテンプレ一覧だったが、後からデザイン関連設定 (アーカイブテンプレ、プロフィールテンプレ、日付フォーマット、OG 既定値…) を集約したため。URL 互換のため URL は変えず、UI ラベルだけ広い概念に進化。コード内シンボルは `templates*` のままで、人間向けドキュメントは「デザイン」(以前は「デザイン設定」)を使う。

兄弟メニュー「テンプレート編集」(`/admin/templates/active/edit`) は別物 (現在利用中のテンプレートを直接編集するショートカット)。

### Importer の文字コードはバージョンで決め打ちしない

SB2 標準は EUC-JP、SB3 標準は UTF-8 だが、実運用ではどちらの組み合わせもあり得る (サーバ環境次第で揺れる)。**`internal/jacharset.DecodeToUTF8` を必ず通す**。判定順は Content-Type ヒント → `<meta charset>` / CSS `@charset` → ISO-2022-JP escape sniff → UTF-8 妥当性 → Shift_JIS/EUC-JP byte score。

## ドキュメント

新フェーズに着手する前に最低限 [docs/architecture.md](docs/architecture.md) を一読する。

| 内容                                                 | パス                                             |
| ---------------------------------------------------- | ------------------------------------------------ |
| アーキテクチャ (CSRF / 反スパム / OG / MCP / AI…)    | [docs/architecture.md](docs/architecture.md) — **必読** |
| 公開 + 管理 URL リファレンス                         | [docs/url-map.md](docs/url-map.md)               |
| 環境変数 / フラグ / `task` コマンド                  | [docs/configuration.md](docs/configuration.md)   |
| デプロイモード (HTTP / CGI / static rebuild / Docker) | [docs/deployment.md](docs/deployment.md)         |
| SB2 / SB3 からの移行                                 | [docs/importing-legacy-sb.md](docs/importing-legacy-sb.md) |
| Markdown ファイル群からの取り込み                    | [docs/importing-markdown.md](docs/importing-markdown.md) |
| エンドユーザ向けヘルプ (`/admin/help` でも配信)      | [docs/help/](docs/help/)                         |

## プロジェクトの状況

「今どこまで shipped か」を知りたいときは `git log --oneline -50` が最も早い。
