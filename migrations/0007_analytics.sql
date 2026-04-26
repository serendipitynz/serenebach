-- +goose Up
-- +goose StatementBegin
CREATE TABLE page_views (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    visitor_id TEXT    NOT NULL,
    path       TEXT    NOT NULL,
    entry_id   INTEGER NOT NULL DEFAULT 0,   -- 0 when the path isn't a single entry
    created_at INTEGER NOT NULL
);
CREATE INDEX idx_page_views_created ON page_views(created_at);
CREATE INDEX idx_page_views_entry ON page_views(entry_id, created_at);
CREATE INDEX idx_page_views_visitor ON page_views(visitor_id, created_at);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE page_views;
-- +goose StatementEnd
