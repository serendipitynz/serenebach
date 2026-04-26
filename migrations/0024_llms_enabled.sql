-- +goose Up
-- +goose StatementBegin
ALTER TABLE weblogs ADD COLUMN llms_enabled INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE weblogs DROP COLUMN llms_enabled;
-- +goose StatementEnd
