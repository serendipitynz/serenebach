-- +goose Up
CREATE TABLE site_custom_tags (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    wid        INTEGER NOT NULL DEFAULT 1,
    name       TEXT    NOT NULL,  -- e.g. "custom_ga_code" (prefix included)
    value      TEXT    NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')),
    updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now'))
);

CREATE UNIQUE INDEX idx_site_custom_tags_wid_name ON site_custom_tags(wid, name);

-- +goose Down
DROP TABLE site_custom_tags;
