-- +goose Up
-- +goose StatementBegin
CREATE TABLE pages (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    wid         INTEGER NOT NULL DEFAULT 1,
    title       TEXT    NOT NULL DEFAULT '',
    body        TEXT    NOT NULL DEFAULT '',
    format      TEXT    NOT NULL DEFAULT '',
    slug        TEXT    NOT NULL DEFAULT '',
    template_id INTEGER NOT NULL DEFAULT 0,
    sort_order  INTEGER NOT NULL DEFAULT 0,
    status      INTEGER NOT NULL DEFAULT 0,
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);

CREATE UNIQUE INDEX idx_pages_wid_slug ON pages(wid, slug);
CREATE INDEX idx_pages_wid_status ON pages(wid, status);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE pages;
-- +goose StatementEnd
