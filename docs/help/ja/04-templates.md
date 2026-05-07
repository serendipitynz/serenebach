---
title: テンプレート編集
slug: templates
order: 40
---

# テンプレート編集

公開サイトの見た目は、テンプレートで変更できます。Go 版 Serene Bach でも、SB3 と同じ考え方のテンプレートを使います。既存の SB3 テンプレートは多くの場合そのまま取り込めますが、一部の古い機能には対応していません。

## テンプレート構成

Serene Bach では、1 セットのテンプレートとして次のものを使います。

- ベース HTML テンプレート（ページ全体の HTML）
- 個別記事用 HTML テンプレート（個別記事表示モードのときのみ使用）
- CSS テンプレート

個別記事用 HTML テンプレートを設定しなくても構いません。その場合、個別記事表示でもベース HTML テンプレートが利用されます。

テンプレートのセットは複数保存でき、「デザイン設定」で利用するテンプレートを切り替えられます。また、アーカイブ表示やプロフィール表示に適用するテンプレートを個別に設定することもできます。カテゴリーごとにテンプレートを変えることも可能です。

テンプレート用の画像などは、アセットとしてアップロードできます。アップロードしたファイルは `{site_parts}` を使って参照します。

```html
<img src="{site_parts}logo.png" alt="">
```

## HTML テンプレート構造

HTML テンプレートは基本的に HTML の要素で構成されます。内部に**独自ブロック**と**独自タグ**を記述することで、状態に応じて表示を変化させられます。

### 独自ブロック

```html
<!-- BEGIN block_name -->
<p>{tag_name}</p>
<!-- END block_name -->
```

`<!-- BEGIN block_name -->` と `<!-- END block_name -->` で囲まれた領域がブロックです。ブロック名が有効であれば、状態に応じて表示数が変化します。

**重要:** `BEGIN` と `END` はそれぞれ**独立した行**に置く必要があります。以下のような記述は正しく**動作しません**。

```html
<!-- 誤り: 同じ行に他のタグがある -->
<div><!-- BEGIN entry --><p>{entry_title}</p><!-- END entry --></div>
```

### 独自タグ

`{tag_name}` と記述された部分が、実際の内容に置き換わります。

```html
<h1>{blog_name_only}</h1>
<article>
  <h2>{entry_title}</h2>
  {entry_description}
</article>
```

## 独自ブロック一覧

| 分類 | ブロック名 | 説明 |
|---|---|---|
| タイトル関連 | `title` | タイトル表示に利用します。常に 1 回表示されます。 |
| タイトル関連 | `toppage` | トップページ（ホーム）でのみ表示されるブロックです。カテゴリー、アーカイブ、タグ、個別記事、プロフィールなどでは表示されません。 |
| 記事関連 | `entry` | 記事表示に利用します。ページ表示やアーカイブ表示では該当記事数分繰り返されます。個別記事表示では 1 回です。 |
| 記事関連 | `option` | 個別記事表示モードのときにのみ表示されます。 |
| 記事関連 | `sequel` | 個別記事表示モードのときにのみ表示されます。前後の記事へのナビゲーションを含みます。 |
| コメント関連 | `comment_area` | 個別記事表示モードのときに表示されます。コメントフォームなどが含まれます。コメント受付停止時は表示されません。 |
| コメント関連 | `comment` | 記事に寄せられたコメント表示に利用します。承認済みコメントの数だけ繰り返して表示されます。 |
| ページナビゲーション | `page` | ページ表示モードのページナビゲーション表示に利用します。総ページ数が 2 以上のときに表示されます。 |
| プロフィール関連 | `profile` | ユーザーリスト表示に利用します（サイドバーなど）。 |
| プロフィール関連 | `profile_area` | プロフィール表示モードのプロフィール詳細表示に利用します。 |
| カテゴリー関連 | `category_area` | カテゴリーページで表示されるブロックです。カテゴリー名、フルネーム、説明を含みます。 |
| リスト関連 | `archives` | 月別アーカイブリスト表示に利用します。 |
| リスト関連 | `category` | カテゴリーリスト表示に利用します。 |
| リスト関連 | `latest_entry` | 最新記事リスト表示に利用します。 |
| リスト関連 | `link` | リンクリスト（ブログロール）表示に利用します。 |
| リスト関連 | `recent_comment` | 最新コメントリスト表示に利用します。 |
| リスト関連 | `selected_entry` | 選択記事リスト表示に利用します。**Go 版では常に 0（非表示）です。** |
| 固定ページ | `dedicated_page` | 固定ページでのみ表示されるブロックです。通常の記事ページや一覧ページでは表示されません。 |

## 独自タグ一覧

### グローバルタグ（ブロック非依存）

これらはどのブロックの外側でも利用できます。

| タグ | 内容 |
|---|---|
| `{site_encoding}` | 文字コード（`utf-8`） |
| `{site_lang}` | ウェブログの言語コード |
| `{site_title}` | ページタイトル（ページサフィックスがある場合は `Blog \| Suffix` の形） |
| `{site_top}` | ブログのトップページ URL |
| `{site_cgi}` | `/sb.cgi`（SB3 互換） |
| `{site_css}` | テンプレートの CSS URL |
| `{site_rss}` | `/rss.xml` |
| `{site_atom}` | `/atom.xml` |
| `{site_parts}` | テンプレートアセットのベース URL |
| `{site_mobile}` | 携帯電話用アクセス URL。**Go 版では常に空文字です。** |
| `{site_rsd}` | `/rsd.xml`（SB3 互換。XML-RPC 本体は未実装） |
| `{selected_archive}` | 現在選択されているアーカイブ種類やカテゴリー名などのページサフィックス |
| `{script_name}` | `Serene Bach` |
| `{script_version}` | バージョン文字列 |
| `{script_webpage}` | 公式ウェブページの URL |
| `{mode_name}` | モードの長い名前（`entry`, `category`, `archive`, `tag`, `profile`, `search`, `page`） |
| `{mode_id}` | モードの短い識別子（`ent`, `cat`, `arc`, `tag`, `user`, `srch` など） |
| `{blog_name_only}` | ブログタイトル（プレーン文字列） |
| `{blog_name}` | ブログタイトル（トップページへのリンク付き HTML） |
| `{blog_description}` | ブログの説明文 |
| `{csrf_token}` | CSRF トークン（公開 POST フォーム用） |

### ページネーション関連タグ

`page` ブロックの内外で利用可能です。

| タグ | 内容 |
|---|---|
| `{page_num}` | 総ページ数 |
| `{page_now}` | 現在表示しているページ番号（1 から開始） |
| `{prev_page_url}` | 前のページ URL（先頭ページでは空） |
| `{prev_page_link}` | 前のページへの HTML アンカー（`<<`） |
| `{next_page_url}` | 次のページ URL（最終ページでは空） |
| `{next_page_link}` | 次のページへの HTML アンカー（`>>`） |

### `entry` ブロック内のタグ

| タグ | 内容 |
|---|---|
| `{entry_id}` | 記事 ID |
| `{entry_permalink}` | 記事のパーマリンク URL |
| `{entry_title}` | 記事タイトル |
| `{entry_date}` | 記事の日付（リスト用 / 個別記事用で異なる書式） |
| `{entry_time}` | 投稿時刻（パーマリンクで囲まれた HTML） |
| `{entry_disp_time}` | 投稿時刻（プレーン文字列） |
| `{entry_description}` | 記事本文（フォーマット適用済み HTML） |
| `{entry_sequel}` | リストページでは「続きを読む」リンク、個別記事ページでは追記本文 |
| `{entry_mode}` | `list`、`entry`、または `page`（固定ページ） |
| `{entry_likes_count}` | いいね数 |
| `{entry_like_url}` | いいね POST 先 URL |
| `{entry_stamps_count}` | スタンプ総数 |
| `{entry_stamp_url}` | スタンプ POST 先 URL |
| `{entry_keywords}` | キーワード（カンマ区切り） |
| `{entry_keyword}` | `{entry_keywords}` の SB3 スペル互換エイリアス |
| `{entry_tags}` | タグ一覧の HTML フラグメント |
| `{permalink}` | `{entry_permalink}` の SB3 短縮エイリアス |
| `{comment_num}` | コメント数を表示するアンカー HTML（コメント受付停止時は `-`） |
| `{comment_count}` | コメント数の生の数字（コメント受付停止時は空） |
| `{sb_entry_marking}` | リストページではスクロール用アンカー、個別記事ページでは空 |
| `{category_name}` | カテゴリー名へのリンク（未分類時は `-`） |
| `{category_id}` | カテゴリー ID（未分類時は空） |
| `{category_disp_name}` | カテゴリー表示名（未分類時は `-`） |
| `{user_name}` | 記事の著者の**ログイン名**（SB3 互換） |
| `{user_disp_name}` | 記事の著者の表示名 |
| `{user_login}` | `{user_name}` のエイリアス（ログイン名） |
| `{user_id}` | 著者のユーザー ID |

個別記事ページ（`entry` ブロック count = 1）では、次の追加タグも利用できます。

| タグ | 内容 |
|---|---|
| `{entry_og_image}` | OG 画像 URL |
| `{entry_og_image_width}` | `1200` |
| `{entry_og_image_height}` | `630` |

また、スタンプの種類ごとのカウントは `{entry_stamps_heart}`、`{entry_stamps_laugh}`、`{entry_stamps_wow}`、`{entry_stamps_party}` という形で取得できます。

### `sequel` ブロック内のタグ

| タグ | 内容 |
|---|---|
| `{prev_permalink}` | ひとつ前の記事のパーマリンク（ない場合は空） |
| `{prev_title}` | ひとつ前の記事のタイトル |
| `{next_permalink}` | ひとつ後の記事のパーマリンク（ない場合は空） |
| `{next_title}` | ひとつ後の記事のタイトル |
| `{prev_entry}` | 前の記事への完成アンカー `« Title`（ない場合は空） |
| `{next_entry}` | 次の記事への完成アンカー `Title »`（ない場合は空） |

> ナビゲーションの「前」「後（次）」の時系列関係は、環境設定の記事の並び順に依存します。

### `comment_area` ブロック内のタグ

| タグ | 内容 |
|---|---|
| `{comment_post_url}` | コメント POST 先 URL |
| `{form_ts}` | スパム対策用 Unix タイムスタンプ |
| `{comment_error}` | フォームエラーメッセージ（HTML エスケープ済み） |
| `{cookie_name}` | コメント投稿者名の Cookie 値 |
| `{cookie_email}` | コメント投稿者メールの Cookie 値 |
| `{cookie_url}` | コメント投稿者 URL の Cookie 値 |
| `{turnstile_widget}` | Cloudflare Turnstile ウィジェット HTML（未設定時は空） |
| `{sb_comment_js}` | **Go 版では常に空文字です。** |

### `comment` ブロック内のタグ

| タグ | 内容 |
|---|---|
| `{comment_name}` | コメント投稿者名（HTML エスケープ済み） |
| `{comment_time}` | コメント投稿時刻 |
| `{comment_description}` | コメント本文（HTML エスケープ + 改行を `<br>` に変換） |
| `{comment_url}` | コメント投稿者 URL（スキーム許可リスト通過済み） |
| `{comment_icon}` | **Go 版では常に空文字です。**（将来のアバター機能用に予約） |

### `profile` ブロック（サイドバー）内のタグ

| タグ | 内容 |
|---|---|
| `{user_list}` | 全表示ユーザーの `<ul><li><a href="...">Name</a></li>...</ul>` HTML フラグメント |

### `profile_area` ブロック（プロフィールページ）内のタグ

| タグ | 内容 |
|---|---|
| `{profile_id}` | ユーザーの数値 ID |
| `{profile_name}` | ユーザーの表示名 |
| `{profile_login}` | ユーザーのログイン名 |
| `{profile_description}` | プロフィール内容（フォーマット適用済み HTML） |
| `{profile_email}` | **Go 版では常に空文字です。**（管理者メールは非公開） |
| `{user_id}` | `{profile_id}` と同値（エイリアス） |
| `{user_name}` | `{profile_login}` と同値（ログイン名、SB3 互換） |
| `{user_disp_name}` | `{profile_name}` と同値（エイリアス） |
| `{user_login}` | `{profile_login}` と同値（エイリアス） |

### `category_area` ブロック（カテゴリーページ）内のタグ

| タグ | 内容 |
|---|---|
| `{category_pagename}` | カテゴリー自身の名前 |
| `{category_fullname}` | 親チェーン付きフルネーム（`Parent > Child`） |
| `{category_description}` | カテゴリー説明（フォーマット適用済み HTML） |

### `archives` ブロック内のタグ

| タグ | 内容 |
|---|---|
| `{archives_list}` | 月別アーカイブリストの `<ul><li><a href="...">YYYY年MM月 (N)</a></li>...</ul>` HTML フラグメント |

### `category` ブロック内のタグ

| タグ | 内容 |
|---|---|
| `{category_list}` | トップレベルカテゴリーのみの `<ul>` HTML フラグメント |
| `{subcategory_list}` | サブカテゴリー含む入れ子 `<ul>` HTML フラグメント |

### `recent_comment` ブロック内のタグ

| タグ | 内容 |
|---|---|
| `{recent_comment_list}` | 最近のコメントリストの `<ul><li><a href="...">EntryTitle</a> — Name</li>...</ul>` HTML フラグメント |

### `latest_entry` ブロック内のタグ

| タグ | 内容 |
|---|---|
| `{latest_entry_list}` | 最新記事リストの `<ul><li><a href="...">Title</a></li>...</ul>` HTML フラグメント |

### `link` ブロック内のタグ

| タグ | 内容 |
|---|---|
| `{link_list}` | リンクリスト（グループ入れ子対応）の `<ul>` HTML フラグメント |

## SB3 互換エイリアス

Go 版 Serene Bach では、SB3 のテンプレートとの互換性のため、以下のエイリアスを提供しています。

| タグ名 | エイリアス先 | 備考 |
|---|---|---|
| `{permalink}` | `{entry_permalink}` | SB3 の短縮形 |
| `{entry_keyword}` | `{entry_keywords}` | SB3 の単数形スペル |
| `{user_login}` | `{user_name}` | Go 版追加エイリアス |

## 未対応・挙動が異なるタグ・ブロック

Go 版 Serene Bach で対応していない、または SB3 と異なる挙動を示すタグ・ブロックです。SB3 テンプレートを取り込んだ場合、テンプレート編集画面で警告が表示されます。

### 未対応タグ（常に空または置き換わらない）

| タグ | 理由 |
|---|---|
| `{trackback_url}` | トラックバック機能は非対象（スパム対策） |
| `{trackback_count}` | 同上 |
| `{recent_trackback_list}` | 同上 |
| `{comment_iconform}` | コメントアイコン未実装 |
| `{related_category}` | 複数カテゴリー紐付け（サブカテゴリー）未実装 |
| `{related_category_disp}` | 同上 |
| `{entry_excerpt}` | 要約フィールド未実装 |
| `{calendar}` | カレンダーサイドバー未実装 |
| `{calendar2}` | 同上 |
| `{calendar_horizontal}` | 同上 |
| `{calendar_vertical}` | 同上 |
| `{trackback_...}` | `trackback_` で始まるタグはすべて非対象 |
| `{amazon_...}` | Amazon アフィリエイト統合非対象 |
| `{asin_...}` | 同上 |

### 未対応ブロック

| ブロック | 理由 |
|---|---|
| `trackback_area` | トラックバック機能非対象 |
| `recent_trackback` | 同上 |
| `trackback` | 同上 |
| `amazon_area` | Amazon アフィリエイト非対象 |
| `amazon` | 同上 |
| `comment_iconform` | コメントアイコン未実装 |
| `calendar` | カレンダーサイドバー未実装 |
| `mobile_top` | モバイルモード廃止 |
| `mobile_entry` | 同上 |
| `mobile_comment_area` | 同上 |
| `mobile_comment_form` | 同上 |
| `mobile_trackback_area` | 同上 |

### 挙動が異なるタグ

| タグ | SB3 の挙動 | Go 版の挙動 |
|---|---|---|
| `{site_mobile}` | 携帯電話用 URL | 常に空文字 |
| `{comment_icon}` | アイコン画像 | 常に空文字 |
| `{profile_email}` | ユーザーのメールアドレス | 常に空文字（非公開） |
| `{sb_comment_js}` | SB3 のコメント用 JS | 常に空文字 |

### 挙動が異なるブロック

| ブロック | SB3 の挙動 | Go 版の挙動 |
|---|---|---|
| `selected_entry` | おすすめ記事フラグに応じて表示 | 常に 0 |

## CSS テンプレート

CSS テンプレートにも独自タグを利用できます。

| タグ | 内容 |
|---|---|
| `{site_parts}` | テンプレート用の画像が置かれるディレクトリアドレス |
| `{site_encoding}` | ウェブページの文字コード |

## テンプレート設定

「デザイン設定」では、公開サイト全体に関わる表示設定を変更できます。

- 利用中テンプレートの切り替え
- アーカイブやカテゴリー一覧で使うテンプレート
- プロフィールページで使うテンプレート
- 一覧に表示する記事数
- 記事やコメントの並び順
- 日付の表示形式

日付の表示形式は SB3 と同じ `%Year%` / `%Mon%` / `%Day%` などの書き方に対応しています。詳しくは [公開設定と OG カード](settings-publishing) を参照してください。

## インポートとエクスポート

SB3 形式の `template.txt` をインポートできます。古いテンプレートで使われている Shift_JIS、EUC-JP、ISO-2022-JP などの文字コードは、取り込み時に UTF-8 へ変換されます。

エクスポートも `template.txt` 形式で行います。バックアップや別環境への移動に利用してください。

## 関連ページ

- [プレビュー機能](preview)
- [SB2 / SB3 からの移行と機能差異](sb3-migration)
- [公開設定と OG カード](settings-publishing)
