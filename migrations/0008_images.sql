-- +goose Up
-- +goose StatementBegin
CREATE TABLE images (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    wid         INTEGER NOT NULL DEFAULT 1,
    uploaded_by INTEGER NOT NULL,
    filename    TEXT    NOT NULL,
    stored_path TEXT    NOT NULL,
    thumb_path  TEXT    NOT NULL DEFAULT '',
    mime_type   TEXT    NOT NULL,
    size_bytes  INTEGER NOT NULL,
    width       INTEGER NOT NULL DEFAULT 0,
    height      INTEGER NOT NULL DEFAULT 0,
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);
CREATE INDEX idx_images_wid_created ON images(wid, created_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE images;
-- +goose StatementEnd
