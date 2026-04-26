-- +goose Up
-- +goose StatementBegin
ALTER TABLE entries ADD COLUMN slug TEXT NOT NULL DEFAULT '';

-- Partial unique index: two entries can both have empty slug (= no
-- custom slug assigned, fall back to /entry/<id>/), but non-empty
-- slugs must be unique per weblog so /entry/<slug>/ resolves
-- deterministically.
CREATE UNIQUE INDEX idx_entries_wid_slug_unique ON entries(wid, slug) WHERE slug != '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_entries_wid_slug_unique;
ALTER TABLE entries DROP COLUMN slug;
-- +goose StatementEnd
