-- +goose Up
-- +goose StatementBegin
CREATE TABLE links (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    wid         INTEGER NOT NULL DEFAULT 1,
    name        TEXT    NOT NULL,
    url         TEXT    NOT NULL DEFAULT '',
    description TEXT    NOT NULL DEFAULT '',
    target      TEXT    NOT NULL DEFAULT '',
    kind        TEXT    NOT NULL DEFAULT 'link',
    parent_id   INTEGER NOT NULL DEFAULT 0,
    sort_order  INTEGER NOT NULL DEFAULT 0,
    disp        INTEGER NOT NULL DEFAULT 0,
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);
CREATE INDEX idx_links_wid_order ON links(wid, sort_order);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_links_wid_order;
DROP TABLE IF EXISTS links;
-- +goose StatementEnd
