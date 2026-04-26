-- +goose Up
-- +goose StatementBegin
ALTER TABLE categories ADD COLUMN description TEXT    NOT NULL DEFAULT '';
ALTER TABLE categories ADD COLUMN template_id INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE categories DROP COLUMN description;
ALTER TABLE categories DROP COLUMN template_id;
-- +goose StatementEnd
