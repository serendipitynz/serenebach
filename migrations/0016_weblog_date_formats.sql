-- +goose Up
-- +goose StatementBegin
ALTER TABLE weblogs ADD COLUMN date_format_entry   TEXT NOT NULL DEFAULT '';
ALTER TABLE weblogs ADD COLUMN time_format_entry   TEXT NOT NULL DEFAULT '';
ALTER TABLE weblogs ADD COLUMN date_format_comment TEXT NOT NULL DEFAULT '';
ALTER TABLE weblogs ADD COLUMN date_format_list    TEXT NOT NULL DEFAULT '';
ALTER TABLE weblogs ADD COLUMN date_format_archive TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE weblogs DROP COLUMN date_format_entry;
ALTER TABLE weblogs DROP COLUMN time_format_entry;
ALTER TABLE weblogs DROP COLUMN date_format_comment;
ALTER TABLE weblogs DROP COLUMN date_format_list;
ALTER TABLE weblogs DROP COLUMN date_format_archive;
-- +goose StatementEnd
