-- +goose Up
-- +goose StatementBegin
ALTER TABLE entries ADD COLUMN summary       TEXT    NOT NULL DEFAULT '';  -- SB3 'sum' (= {entry_excerpt})
ALTER TABLE entries ADD COLUMN canonical_url TEXT    NOT NULL DEFAULT '';
ALTER TABLE entries ADD COLUMN noindex       INTEGER NOT NULL DEFAULT 0;  -- 0=index, 1=noindex
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE entries DROP COLUMN noindex;
ALTER TABLE entries DROP COLUMN canonical_url;
ALTER TABLE entries DROP COLUMN summary;
-- +goose StatementEnd
