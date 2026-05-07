-- +goose Up
-- +goose StatementBegin
ALTER TABLE pages ADD COLUMN og_bg_image_path TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE pages DROP COLUMN og_bg_image_path;
-- +goose StatementEnd
