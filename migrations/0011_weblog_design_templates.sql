-- +goose Up
-- +goose StatementBegin
-- 0 (default) = fall back to the active template. Non-zero pins a
-- specific template id for the matching route family. SB3 exposed these
-- under "デザイン設定 > 設定" and we keep the same semantics.
ALTER TABLE weblogs ADD COLUMN archive_template_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE weblogs ADD COLUMN profile_template_id INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE weblogs DROP COLUMN profile_template_id;
ALTER TABLE weblogs DROP COLUMN archive_template_id;
-- +goose StatementEnd
