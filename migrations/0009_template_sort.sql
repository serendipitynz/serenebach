-- +goose Up
-- +goose StatementBegin
ALTER TABLE templates ADD COLUMN sort_order INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE templates DROP COLUMN sort_order;
-- +goose StatementEnd
