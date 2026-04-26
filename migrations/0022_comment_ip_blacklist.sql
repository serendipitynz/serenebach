-- +goose Up
-- +goose StatementBegin
ALTER TABLE weblogs ADD COLUMN ip_blacklist TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE weblogs DROP COLUMN ip_blacklist;
-- +goose StatementEnd
