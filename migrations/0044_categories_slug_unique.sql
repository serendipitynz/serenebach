-- +goose Up
-- +goose StatementBegin

-- Categories now serve their public URL and rebuild snapshot from the
-- slug (when set), so duplicates would non-deterministically resolve
-- /category/<slug>/ and let rebuild overwrite snapshots. Pre-existing
-- data may have duplicates because the admin form did not validate the
-- field — break the tie by keeping the lowest-id row's slug and
-- blanking the rest, which falls back to the legacy /category/<id>/
-- URL for those rows without further admin action required.
UPDATE categories
   SET slug = ''
 WHERE slug != ''
   AND id NOT IN (
       SELECT MIN(id) FROM categories
        WHERE slug != ''
        GROUP BY wid, slug
   );

CREATE UNIQUE INDEX idx_categories_wid_slug
    ON categories(wid, slug) WHERE slug != '';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_categories_wid_slug;
-- +goose StatementEnd
