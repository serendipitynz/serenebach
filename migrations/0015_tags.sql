-- +goose Up
-- +goose StatementBegin
CREATE TABLE tags (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    wid        INTEGER NOT NULL DEFAULT 1,
    name       TEXT    NOT NULL,
    slug       TEXT    NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX idx_tags_wid_slug ON tags(wid, slug);
CREATE UNIQUE INDEX idx_tags_wid_name ON tags(wid, name);

CREATE TABLE entry_tags (
    entry_id INTEGER NOT NULL,
    tag_id   INTEGER NOT NULL,
    PRIMARY KEY (entry_id, tag_id)
);
CREATE INDEX idx_entry_tags_tag ON entry_tags(tag_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE entry_tags;
DROP TABLE tags;
-- +goose StatementEnd
