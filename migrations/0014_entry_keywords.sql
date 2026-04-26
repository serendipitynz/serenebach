-- +goose Up
-- +goose StatementBegin
ALTER TABLE entries ADD COLUMN keywords TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE entries DROP COLUMN keywords;
-- +goose StatementEnd
