-- +goose Up
ALTER TABLE weblogs ADD COLUMN sitemap_enabled INTEGER NOT NULL DEFAULT 1;
ALTER TABLE weblogs ADD COLUMN robots_enabled  INTEGER NOT NULL DEFAULT 1;

-- +goose Down
ALTER TABLE weblogs DROP COLUMN robots_enabled;
ALTER TABLE weblogs DROP COLUMN sitemap_enabled;
