-- +goose Up
-- +goose StatementBegin
ALTER TABLE pages ADD COLUMN summary       TEXT    NOT NULL DEFAULT '';  -- {entry_excerpt}, same as entries.summary
ALTER TABLE pages ADD COLUMN canonical_url TEXT    NOT NULL DEFAULT '';
ALTER TABLE pages ADD COLUMN noindex       INTEGER NOT NULL DEFAULT 0;  -- 0=index, 1=noindex
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE pages DROP COLUMN noindex;
ALTER TABLE pages DROP COLUMN canonical_url;
ALTER TABLE pages DROP COLUMN summary;
-- +goose StatementEnd
