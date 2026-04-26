-- +goose Up
-- +goose StatementBegin
-- Custom background image for Open Graph cards. Both columns hold a
-- stored_path relative to ImageDir (matches the images.stored_path
-- convention); empty means "use the embedded SB default". Entry-level
-- override wins over the weblog-level default at render time.
ALTER TABLE weblogs ADD COLUMN og_bg_image_path TEXT NOT NULL DEFAULT '';
ALTER TABLE entries ADD COLUMN og_bg_image_path TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE weblogs DROP COLUMN og_bg_image_path;
ALTER TABLE entries DROP COLUMN og_bg_image_path;
-- +goose StatementEnd
