-- +goose Up
-- +goose StatementBegin
ALTER TABLE weblogs ADD COLUMN entries_per_page   INTEGER NOT NULL DEFAULT 10;
ALTER TABLE weblogs ADD COLUMN entry_sort_order   TEXT    NOT NULL DEFAULT 'desc';
ALTER TABLE weblogs ADD COLUMN comment_sort_order TEXT    NOT NULL DEFAULT 'asc';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE weblogs DROP COLUMN entries_per_page;
ALTER TABLE weblogs DROP COLUMN entry_sort_order;
ALTER TABLE weblogs DROP COLUMN comment_sort_order;
-- +goose StatementEnd
