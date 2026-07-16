#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Regenerate the PII-free sample Serene Bach 2 data directory used by the
migration guide.

Why this exists: the project owner's real SB2 excerpt (_sandbox/sb2/data) is
a serenebach.net production sample and carries personal data (commenter
names / emails / IPs, ban lists, crypt password hashes). It must never appear
in a published guide. This script synthesises an equivalent *fictional* SB2
blog straight from the flat-file format, so the guide's example figures and
counts are reproducible from committed, privacy-safe data.

Goal: a directory that is structurally faithful to a real SB2 install, so a
non-technical reader sees the SAME set of files in the sample as in their own
`data/` and never gets stuck on "there's a file here that the sample doesn't
have." That means we write the per-record DETAIL files *and* the index files
(entry.cgi / message.cgi / trackback.cgi / template.cgi / user.cgi), the
list-only tables (category / link / image / plugin / amazon / weblog /
session) and the id counter file. Only the content is fictional. (The real
excerpt's ping.cgi / sbforum_rss.cgi / sb_*forum_rss.cgi are NOT SB2's own
data -- they come from unrelated ping-community / third-party plugins -- so
they are omitted.)

Format authority (do not re-derive from the encoded real data):
  - Field orders come from _sandbox/sb2/lib/sb/Data/*.pm elements().
  - Which classes have a detail/ dir, and the *subset* of columns written to
    each index file, come from %ContentOfList in
    _sandbox/sb2/lib/sb/Driver/Text.pm.
  - Records are TAB-separated with `\t` / `\n` / `\\` backslash escapes and a
    trailing empty field; files are EUC-JP (SB2's usual encoding, which also
    exercises the importer's charset auto-detection).

Output: ./data/ next to this script (docs/guide/samples/sb2-migration/data/).
All IPs are RFC 5737 documentation ranges (192.0.2.0/24, 198.51.100.0/24,
203.0.113.0/24); emails/URLs use example.com / example.net.

Expected import result (guide step 5-3):
    import: weblog updated=true, templates=2, categories=5, entries=10, skipped=2
    import: warning: template "標準テンプレート": block {trackback} not supported ...
Only published entries import; the draft and the private (closed) entry are
the 2 skipped. Trackbacks are never read by the importer (out of scope).
"""

import os
from datetime import datetime, timezone, timedelta

HERE = os.path.dirname(os.path.abspath(__file__))
DATA = os.path.join(HERE, "data")
JST = timezone(timedelta(hours=9))


def epoch(y, mo, d, h, mi):
    return int(datetime(y, mo, d, h, mi, tzinfo=JST).timestamp())


# ---------------------------------------------------------------------------
# Field orders (from Data/*.pm elements()) and index subsets (from Text.pm).
# ---------------------------------------------------------------------------
FULL = {
    "entry":     "id wid subj cat date auth stat com tb file tz add edit acm atb form ping body more sum key ext tmp".split(),
    "message":   "id wid eid stat date auth host tz mail url agnt body icon ext admn out".split(),
    "trackback": "id wid eid stat date subj name url tz body host admn icon out".split(),
    "template":  "id wid use name gen mod info main css entry files".split(),
    "user":      "id wid name pass real disp mail notice stat order prof aws edit ext info img friend cat form ad_css".split(),
    "category":  "id wid name text url main order temp dir disp sub num idx".split(),
    "link":      "id wid name url text user order disp type target".split(),
    "image":     "id wid auth date name file thumb stat icon_c icon_t dir eid tz".split(),
    "plugin":    "id wid name data text setting date url mail extra".split(),
    "amazon":    "id wid pid order stat name cat cre days make ism imd ilg ava lpr opr msg url date tz".split(),
    "weblog":    "id title text pacc psrv psubj pfrom pcat pthum pform pping pcron ptime ppass papop ppath smtp stype ext plugin".split(),
    "session":   "id key data".split(),
}
INDEX = {
    "entry":     "id wid subj cat date auth stat com tb file tz add".split(),
    "message":   "id wid eid stat date auth host tz".split(),
    "trackback": "id wid eid stat date subj name url tz".split(),
    "template":  "id wid use name gen mod info".split(),
    "user":      "id wid name pass real disp mail notice stat order".split(),
}
# Classes that store per-record detail files under data/{class}/{id}.cgi.
DETAIL_CLASSES = set(INDEX.keys())


def enc_field(s):
    # Reverse of Driver::Text._encode: backslash first, then tab / newline.
    return str(s).replace("\\", "\\\\").replace("\t", "\\t").replace("\n", "\\n")


def row(order, d):
    # SB2 appends an empty trailing field before the newline.
    return "\t".join(enc_field(d.get(k, "")) for k in order) + "\t"


def write_text(relpath, text):
    path = os.path.join(DATA, relpath)
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "wb") as f:
        f.write(text.encode("euc-jp"))


def write_class(name, records):
    """Write a data class: index file {name}.cgi plus, for detail classes,
    one {name}/{id}.cgi per record."""
    order = FULL[name]
    idx_order = INDEX.get(name, order)  # list-only classes: index == full
    write_text(name + ".cgi", "\n".join(row(idx_order, r) for r in records) + "\n")
    if name in DETAIL_CLASSES:
        for r in records:
            write_text("%s/%s.cgi" % (name, r["id"]), row(order, r) + "\n")


def write_kv(relpath, pairs):
    # configure.cgi / init.cgi / id.cgi are key<TAB>value lines.
    write_text(relpath, "".join("%s\t%s\n" % (k, v) for k, v in pairs))


# ===========================================================================
# Fictional blog: 山あるき日記 (a weekend low-mountain hiking diary)
# ===========================================================================

# --- categories (ids 1..5) --------------------------------------------------
# main = parent category id. It MUST be "" (empty) for a top-level category,
# NOT "0": sb::Admin::Category lists roots with `main eq ''`, and "0" would
# mean "child of category id 0". (This is exactly why our first attempt showed
# zero categories in SB2.) disp is a colon-joined option string
# (top:list:line:sum) -- sb::Data::Category::DEFAULT_OPTION is "0:0:0:1:".
# idx=0 selects the dynamic (?cid=) category URL.
CAT_DISP = "0:0:0:1:"
categories = [
    dict(id=1, wid=0, name="日記",     text="日々のできごと",     main="", order=1, temp=-1, dir="diary/",  disp=CAT_DISP, idx=0, num=2),
    dict(id=2, wid=0, name="山あるき", text="登った山の記録",     main="", order=2, temp=-1, dir="hiking/", disp=CAT_DISP, idx=0, sub="3,4,", num=3),
    dict(id=3, wid=0, name="道具",     text="登山道具のレビュー", main=2,  order=3, temp=-1, dir="gear/",   disp=CAT_DISP, idx=0, num=3),
    dict(id=4, wid=0, name="低山",     text="気軽に登れる低山",   main=2,  order=4, temp=-1, dir="teizan/", disp=CAT_DISP, idx=0, num=2),
    dict(id=5, wid=0, name="お知らせ", text="このブログについて", main="", order=5, temp=-1, dir="news/",   disp=CAT_DISP, idx=0, num=2),
]

# --- entries (ids 0..11; 10 published + 2 drafts) --------------------------
# SB2 entry stat is only ever 0=draft, 1=published, or 2=published+shown-on-top
# (see sb::Admin::Entry: open_save -> 1, and ->2 when the category's `top`
# option is set). There is NO stat=3; anything else is invalid. com/tb hold
# the denormalised comment / trackback counts SB2 keeps on each entry.
def entry(id, subj, cat, when, stat, body, more="", summary="", keywords="",
          file="", com=0, tb=0, form=""):
    return dict(id=id, wid=0, subj=subj, cat=cat, date=when, auth=0, stat=stat,
                com=com, tb=tb, file=file, tz="+0900", edit=0, acm=1, atb=1,
                form=form, body=body, more=more, sum=summary, key=keywords)

entries = [
    entry(0, "ブログ開設のごあいさつ", 5, epoch(2015, 5, 20, 10, 0), 1,
          "<p>「山あるき日記」を始めました。週末に無理のないペースで登った低山を中心に、道具のことも書いていきます。</p>",
          summary="ブログを始めたごあいさつです。", keywords="ごあいさつ,開設"),
    entry(1, "はじめての低山、近所の裏山へ", 4, epoch(2015, 5, 31, 17, 30), 1,
          "<p>まずは足慣らしにと、家から歩いて行ける裏山へ。標高は低いですが、山頂からは町が一望できました。</p>",
          summary="足慣らしに近所の裏山へ登りました。", keywords="低山,足慣らし", com=1),
    entry(2, "高尾山に登りました", 2, epoch(2015, 6, 20, 18, 30), 1,
          "<p>梅雨の晴れ間に高尾山へ。稲荷山コースからゆっくり登りました。</p>\n"
          "<p><img src=\"../img/takao.jpg\" alt=\"高尾山の山頂\"></p>",
          more="<p>下山後はふもとの茶屋でところてん。次はもう少し早い時間に出発したいところです。</p>",
          summary="稲荷山コースから高尾山へ登った記録。", keywords="高尾山,登山,日帰り",
          file="takao", com=3, tb=2),
    entry(3, "登山靴を新調した話", 3, epoch(2015, 7, 4, 21, 0), 1,
          "<p>ソールがすり減ってきたので登山靴を買い替えました。ミッドカットで足首がしっかり守られるタイプです。</p>",
          summary="ミッドカットの登山靴に買い替えました。", keywords="道具,登山靴,レビュー", com=1),
    entry(4, "ザックの中身を見直す", 3, epoch(2015, 7, 18, 20, 15), 1,
          "<p>日帰りでも荷物がつい増えがち。レイン、行動食、救急セットを軸に中身を整理しました。</p>",
          summary="日帰り装備のパッキングを見直しました。", keywords="道具,パッキング"),
    entry(5, "陣馬山から高尾山へ縦走", 2, epoch(2015, 8, 10, 19, 0), 1,
          "<p>思い切って陣馬山から高尾山への縦走に挑戦。長かったですが、稜線歩きが気持ちよかったです。</p>",
          more="<p>コースタイムは休憩込みで6時間ほど。水は多めに持って正解でした。</p>",
          summary="陣馬山〜高尾山を縦走した記録。", keywords="陣馬山,高尾山,縦走", com=2, tb=1),
    entry(6, "雨の日の過ごし方", 1, epoch(2015, 9, 5, 14, 0), 1,
          "<p>週末が雨だと山はお休み。こういう日は道具の手入れをしたり、次に行く山の地図を眺めたりしています。</p>",
          summary="山に行けない雨の日の過ごし方。", keywords="日記,雨"),
    entry(7, "御岳山でロックガーデン", 2, epoch(2015, 9, 22, 18, 45), 1,
          "<p>御岳山からロックガーデンを歩いてきました。苔むした沢沿いは涼しくて別世界でした。</p>\n"
          "<p><img src=\"../img/mitake.jpg\" alt=\"ロックガーデンの沢\"></p>",
          summary="御岳山のロックガーデンを歩きました。", keywords="御岳山,ロックガーデン"),
    entry(8, "レインウェアの選び方メモ", 3, epoch(2015, 10, 3, 22, 30), 1,
          "<p>透湿性と価格のバランスで悩みますが、まずは上下セパレートで動きやすいものを選ぶのがよさそうです。</p>",
          summary="レインウェア選びで考えたことのメモ。", keywords="道具,レインウェア", com=1),
    entry(9, "筑波山、ケーブルカーと徒歩と", 4, epoch(2015, 10, 25, 17, 0), 1,
          "<p>筑波山へ。行きはケーブルカー、帰りは徒歩で下りました。二つの峰を楽しめてお得な気分です。</p>",
          summary="筑波山を歩いた記録。", keywords="筑波山,低山,日帰り", com=2),
    # --- not imported: draft (stat=0) ---
    entry(10, "書きかけ：次に登りたい山リスト", 1, epoch(2015, 11, 1, 12, 0), 0,
          "<p>まだ書きかけです。公開していません。</p>",
          summary="", keywords=""),
    # --- not imported: a second draft (stat=0) ---
    entry(11, "書きかけ：紅葉の山はどこにしよう", 2, epoch(2015, 11, 8, 9, 0), 0,
          "<p>まだ書きかけです。紅葉の時期に登る山を検討中。</p>",
          summary="", keywords=""),
]

# --- comments (messages) : all attached to PUBLISHED entries ---------------
# stat: 0=waiting approval, 1=approved. Hosts are RFC5737 doc ranges.
def message(id, eid, stat, when, auth, host, body, mail="", url=""):
    return dict(id=id, wid=0, eid=eid, stat=stat, date=when, auth=auth, host=host,
                tz="+0900", mail=mail, url=url, agnt="Mozilla/5.0", body=body)

messages = [
    message(0, 1, 1, epoch(2015, 6, 1, 8, 20), "さとう", "203.0.113.10",
            "近所にいい裏山があるの、うらやましいです。"),
    message(1, 2, 1, epoch(2015, 6, 21, 8, 15), "たかはし", "203.0.113.22",
            "私も先週登りました。稲荷山コースは眺めがいいですよね。"),
    message(2, 2, 1, epoch(2015, 6, 21, 12, 40), "やまね", "198.51.100.5",
            "ところてん、下山後にぴったりですね。", url="http://example.net/yamane/"),
    message(3, 2, 0, epoch(2015, 6, 22, 22, 30), "みどり", "198.51.100.30",
            "写真がきれいです。次回の記事も楽しみにしています。"),
    message(4, 3, 1, epoch(2015, 7, 5, 9, 5), "くつや", "192.0.2.40",
            "ミッドカット、足首の安定感が違いますよね。"),
    message(5, 5, 1, epoch(2015, 8, 11, 7, 50), "たかはし", "203.0.113.22",
            "縦走おつかれさまでした。稜線、気持ちよさそう。"),
    message(6, 5, 0, epoch(2015, 8, 11, 23, 10), "しんじ", "198.51.100.77",
            "コースタイム参考になります。水の量、大事ですね。"),
    message(7, 8, 1, epoch(2015, 10, 4, 10, 0), "あめふり", "192.0.2.88",
            "セパレート、私も同意見です。上下別だと蒸れにくい。"),
    message(8, 9, 1, epoch(2015, 10, 26, 8, 30), "さとう", "203.0.113.10",
            "筑波山、ケーブルカーと徒歩の組み合わせいいですね。"),
    message(9, 9, 0, epoch(2015, 10, 26, 21, 15), "のぼる", "198.51.100.120",
            "二つの峰、どちらも眺めがよさそう。行ってみます。"),
]

# --- trackbacks : NOT imported (out of scope). Present for SB2 fidelity. ----
# stat: 1=approved. Attached to entries 2 and 5.
def trackback(id, eid, stat, when, subj, name, url, body, host="203.0.113.200"):
    return dict(id=id, wid=0, eid=eid, stat=stat, date=when, subj=subj, name=name,
                url=url, tz="+0900", body=body, host=host)

trackbacks = [
    trackback(0, 2, 1, epoch(2015, 6, 23, 11, 0), "高尾山の登山記録",
              "山ノート", "http://example.net/yamanote/takao.html",
              "こちらでも高尾山を紹介しています。"),
    trackback(1, 2, 1, epoch(2015, 6, 24, 15, 30), "稲荷山コースを歩く",
              "低山さんぽ", "http://example.net/teizan/inariyama.html",
              "稲荷山コースの詳しいレポートです。"),
    trackback(2, 5, 1, epoch(2015, 8, 13, 9, 45), "陣馬〜高尾 縦走メモ",
              "尾根歩きの記録", "http://example.net/one/jinba-takao.html",
              "同じ区間を逆から歩いた記録です。"),
]

# --- templates : imported as inactive. #0 keeps a {trackback} block so the
#     import report emits an "unsupported tag" warning. -----------------------
BASE_TPL = "\n".join([
    "<!DOCTYPE html>",
    "<html lang=\"ja\">",
    "<head>",
    "<meta charset=\"UTF-8\">",
    "<title>{weblog_title}</title>",
    "</head>",
    "<body>",
    "<h1>{weblog_title}</h1>",
    "<p>{weblog_description}</p>",
    "<!-- BEGIN entry -->",
    "<article>",
    "<h2>{entry_title}</h2>",
    "<div class=\"date\">{entry_date}</div>",
    "{entry_body}",
    "<!-- BEGIN trackback -->",
    "<p><a href=\"{tb_url}\">この記事にトラックバックする</a></p>",
    "<!-- END trackback -->",
    "</article>",
    "<!-- END entry -->",
    "</body>",
    "</html>",
])
SIMPLE_TPL = "\n".join([
    "<!DOCTYPE html>",
    "<html lang=\"ja\">",
    "<head><meta charset=\"UTF-8\"><title>{weblog_title}</title></head>",
    "<body>",
    "<header><a href=\"{site_url}\">{weblog_title}</a></header>",
    "<!-- BEGIN entry -->",
    "<section><h2>{entry_title}</h2>{entry_body}</section>",
    "<!-- END entry -->",
    "</body>",
    "</html>",
])
ENTRY_TPL = "\n".join([
    "<article>",
    "<h2>{entry_title}</h2>",
    "{entry_body}",
    "{more_link}",
    "</article>",
])
CSS = "\n".join([
    "body { font-family: sans-serif; margin: 2em auto; max-width: 640px; }",
    "h1 { border-bottom: 2px solid #333; }",
    ".date { color: #888; font-size: 0.85em; }",
])
templates = [
    dict(id=0, wid=0, use=1, name="標準テンプレート", gen=epoch(2015, 5, 20, 10, 0),
         mod=epoch(2015, 5, 20, 10, 0), info="移行サンプル用の標準テンプレート",
         main=BASE_TPL, css=CSS, entry=ENTRY_TPL),
    dict(id=1, wid=0, use=0, name="シンプルテンプレート", gen=epoch(2015, 5, 20, 10, 0),
         mod=epoch(2015, 6, 30, 12, 0), info="装飾を抑えたテンプレート",
         main=SIMPLE_TPL, css=CSS, entry=ENTRY_TPL),
]

# --- users : NOT imported (crypt passwords aren't bcrypt). Fake hashes. -----
# NB: "pass" is a Python keyword, so these use dict literals rather than dict().
users = [
    {"id": 0, "wid": 0, "name": "sanpo", "pass": "zzFAKEhashAAAA", "real": "散歩 太郎",
     "disp": "散歩 太郎", "mail": "sanpo@example.com", "notice": 1, "stat": 0, "order": 0},
    {"id": 1, "wid": 0, "name": "yamane", "pass": "zzFAKEhashBBBB", "real": "山根 花子",
     "disp": "山根 花子", "mail": "yamane@example.com", "notice": 0, "stat": 2, "order": 1},
]

# --- links (blogroll) : NOT imported ----------------------------------------
links = [
    dict(id=0, wid=0, name="低山さんぽ", url="http://example.net/teizan/",
         text="近隣の低山を紹介するブログ", user=0, order=1, disp=1),
    dict(id=1, wid=0, name="山ノート", url="http://example.net/yamanote/",
         text="山行記録のノート", user=0, order=2, disp=1),
    dict(id=2, wid=0, name="尾根歩きの記録", url="http://example.net/one/",
         text="縦走が中心の記録", user=0, order=3, disp=1),
]

# --- images (registry) : files NOT auto-migrated; body <img> paths stay -----
# NB: Image.elements() puts icon_c / icon_t between `stat` and `dir` (13 cols).
# Omitting them shifts `dir` two columns and SB2 shows the timezone (+0900) as
# the save directory -- which is exactly what our first attempt did.
images = [
    dict(id=0, wid=0, auth=0, date=epoch(2015, 6, 20, 18, 0), name="高尾山の山頂",
         file="takao.jpg", stat=1, icon_c=0, icon_t=0, dir="img/", eid=2, tz="+0900"),
    dict(id=1, wid=0, auth=0, date=epoch(2015, 9, 22, 18, 0), name="ロックガーデンの沢",
         file="mitake.jpg", stat=1, icon_c=0, icon_t=0, dir="img/", eid=7, tz="+0900"),
    dict(id=2, wid=0, auth=0, date=epoch(2015, 8, 10, 18, 0), name="陣馬山の稜線",
         file="jinba.jpg", stat=1, icon_c=0, icon_t=0, dir="img/", eid=5, tz="+0900"),
]

# --- plugins (config list) : mirrors the bundled plugin set -----------------
plugins = [
    dict(id=0, wid=0, name="AccessLog.pm", setting="on"),
    dict(id=1, wid=0, name="Memo.pm",
         data="<p>これはサンプル用のメモです。プラグインの設定はここに保存されます。</p>",
         setting="on"),
    dict(id=2, wid=0, name="Convert.pm", setting="on"),
    dict(id=3, wid=0, name="sbTextFormat.pm", setting="on"),
]

# --- amazon (associate cache) : NOT imported. Fake ASIN, no real assoc tag --
amazon = [
    dict(id=0, wid=0, pid=0, order=1, stat=1, name="はじめての低山ハイキング",
         cat="Book", cre="架空 著", make="サンプル出版",
         url="http://example.com/dp/ASINSAMPLE01", days="2014/03/01",
         date=epoch(2015, 6, 20, 18, 0), tz="+0900"),
]

# --- weblog (single blog row) -----------------------------------------------
weblog = [dict(id=0, title="山あるき日記",
               text="週末の低山歩きと道具の記録。移行ガイド用のサンプルブログです。")]

# --- sessions : none active (kept as an empty valid file) -------------------
sessions = []

# ===========================================================================
# Write everything
# ===========================================================================
write_class("category", categories)
write_class("weblog", weblog)
write_class("entry", entries)
write_class("message", messages)
write_class("trackback", trackbacks)
write_class("template", templates)
write_class("user", users)
write_class("link", links)
write_class("image", images)
write_class("plugin", plugins)
write_class("amazon", amazon)
write_class("session", sessions)

# --- configure.cgi : admin-edited settings (legacy URL shape) --------------
# Sub-path (/blog/) deployment so the guide can demonstrate the static-URL
# redirect, which only works when configure.cgi is present.
# conf_srv_base / conf_srv_cgi / conf_srv_admin are all *directory* URLs (they
# end in "/"), NOT paths to sb.cgi: SB2 builds links as conf_srv_cgi + basic_sb
# + "?cid=..." (see sb::Data::Category::cat_url), so appending sb.cgi here would
# double it. https, since a modern re-host would serve over TLS.
write_kv("configure.cgi", [
    ("conf_timezone", "+0900"),
    ("conf_lang", "ja"),
    ("conf_srv_base", "https://example.com/blog/"),
    ("conf_srv_cgi", "https://example.com/blog/"),
    ("conf_srv_admin", "https://example.com/blog/"),
    ("conf_dir_log", "log/"),
    ("conf_dir_img", "img/"),
    ("conf_entry_archive", "Individual"),
    ("conf_newent_disp", "8"),
    ("conf_entry_disp", "5"),
    ("conf_com_disp", "1"),
    ("conf_tb_disp", "1"),
])

# NB: no init.cgi is written. In a real SB2 install init.cgi lives in the CGI
# root (next to sb.cgi), NOT in data/, so a downloaded data/ folder does not
# contain it -- we match that. Its basic_preid / basic_suffix / basic_sb
# settings default to eid / .html / sb.cgi, which is exactly what the legacy
# URL redirect demo needs, so configure.cgi alone is enough here.

# --- id.cgi : next-id counter per class (max used id + 1) ------------------
def next_id(records):
    return (max(r["id"] for r in records) + 1) if records else 0

write_kv("id.cgi", [
    ("link", next_id(links)),
    ("category", next_id(categories)),
    ("image", next_id(images)),
    ("template", next_id(templates)),
    ("message", next_id(messages)),
    ("amazon", next_id(amazon)),
    ("plugin", next_id(plugins)),
    ("weblog", next_id(weblog)),
    ("trackback", next_id(trackbacks)),
    ("session", 0),
    ("user", next_id(users)),
    ("entry", next_id(entries)),
])

# --- log.cgi : simple access counter (count <TAB> last-ip <TAB> last-time) -
write_text("log.cgi", "1287\t203.0.113.10\t%d\n" % epoch(2015, 10, 26, 21, 30))

# --- log/ : SB2 keeps the access-log detail dir (empty here) ---------------
os.makedirs(os.path.join(DATA, "log"), exist_ok=True)

# NB: the real serenebach.net excerpt also has ping.cgi, sbforum_rss.cgi,
# sb_sforum_rss.cgi and sb_pforum_rss.cgi. None of these are Serene Bach 2's
# own data -- they are produced by unrelated ping-community / third-party
# plugins (there is no Ping/Forum Data class, and no "ping" counter in
# id.cgi) -- so they are intentionally left out of this sample.

# Sanity summary for the person running this script.
published = [e for e in entries if e["stat"] in (1, 2)]
print("wrote sample SB2 data to", DATA)
print("  entries: %d (%d published, %d not) | categories: %d | comments: %d | trackbacks: %d | templates: %d"
      % (len(entries), len(published), len(entries) - len(published),
         len(categories), len(messages), len(trackbacks), len(templates)))
