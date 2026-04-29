-- +goose Up
-- +goose StatementBegin
ALTER TABLE weblogs ADD COLUMN auto_rebuild_on_publish INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE weblogs DROP COLUMN auto_rebuild_on_publish;
-- +goose StatementEnd
