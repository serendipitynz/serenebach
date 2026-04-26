-- +goose Up
-- +goose StatementBegin
ALTER TABLE users DROP COLUMN auto_linebreak;
ALTER TABLE users ADD COLUMN description_format TEXT NOT NULL DEFAULT 'html';
ALTER TABLE categories ADD COLUMN description_format TEXT NOT NULL DEFAULT 'html';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE categories DROP COLUMN description_format;
ALTER TABLE users DROP COLUMN description_format;
ALTER TABLE users ADD COLUMN auto_linebreak INTEGER NOT NULL DEFAULT 1;
-- +goose StatementEnd
