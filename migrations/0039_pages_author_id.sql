-- +goose Up
-- +goose StatementBegin
ALTER TABLE pages ADD COLUMN author_id INTEGER NOT NULL DEFAULT 1;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE pages DROP COLUMN author_id;
-- +goose StatementEnd
