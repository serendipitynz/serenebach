-- +goose Up
-- +goose StatementBegin
CREATE TABLE categories (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    wid        INTEGER NOT NULL DEFAULT 1,
    parent_id  INTEGER NOT NULL DEFAULT 0,  -- 0 = top-level; SB used cid=-1 for uncategorised
    name       TEXT    NOT NULL,
    slug       TEXT    NOT NULL DEFAULT '',
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX idx_categories_wid ON categories(wid);

CREATE TABLE entries (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    wid         INTEGER NOT NULL DEFAULT 1,
    author_id   INTEGER NOT NULL,
    category_id INTEGER NOT NULL DEFAULT -1,  -- -1 = uncategorised (SB convention)
    title       TEXT    NOT NULL DEFAULT '',
    body        TEXT    NOT NULL DEFAULT '',  -- 本文
    more        TEXT    NOT NULL DEFAULT '',  -- 追記 (sequel)
    format      TEXT    NOT NULL DEFAULT '',  -- text format identifier
    status      INTEGER NOT NULL DEFAULT 0,   -- 0=draft, 1=published, -1=closed
    posted_at   INTEGER NOT NULL,
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);
CREATE INDEX idx_entries_wid_posted ON entries(wid, posted_at DESC);
CREATE INDEX idx_entries_author ON entries(author_id);
CREATE INDEX idx_entries_category ON entries(category_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE entries;
DROP TABLE categories;
-- +goose StatementEnd
