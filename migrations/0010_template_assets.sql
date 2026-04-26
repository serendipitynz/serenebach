-- +goose Up
-- +goose StatementBegin
CREATE TABLE template_assets (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    template_id INTEGER NOT NULL,
    filename    TEXT    NOT NULL,
    mime_type   TEXT    NOT NULL,
    size_bytes  INTEGER NOT NULL,
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);
-- One asset filename per template — filesystem path is deterministic
-- off this pair, so the unique index doubles as our dedup rule on upload.
CREATE UNIQUE INDEX uq_template_assets_tpl_file ON template_assets(template_id, filename);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE template_assets;
-- +goose StatementEnd
