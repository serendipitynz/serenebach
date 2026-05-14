-- +goose Up
-- +goose StatementBegin

-- Categories now serve their public URL and rebuild snapshot from the
-- slug (when set). Two classes of pre-existing rows would break that
-- contract because the admin form did not validate the field before
-- this PR landed, so clean them up before the unique index lands.

-- 1. Format-invalid slugs. domain.IsValidSlug enforces
--    ^[a-z0-9]+(-[a-z0-9]+)*$ at 1-100 chars; values like `foo/bar`,
--    `../x`, strings containing whitespace or uppercase letters, and
--    multi-byte text all fall outside that grammar. Such values would
--    produce broken /category/<slug>/ URLs and unintended rebuild
--    paths. Blank them so the row falls back to the legacy
--    /category/<id>/ surface until the operator picks a clean slug.
--    The GLOB byte class is ASCII-only; multi-byte UTF-8 strings
--    therefore match `[^a-z0-9-]` and are correctly blanked.
UPDATE categories
   SET slug = ''
 WHERE slug != ''
   AND (
        length(slug) > 100
     OR slug GLOB '*[^a-z0-9-]*'
     OR slug GLOB '-*'
     OR slug GLOB '*-'
     OR slug LIKE '%--%'
   );

-- 2. Duplicates inside one weblog. Without the unique index two rows
--    could share a slug, which would non-deterministically resolve
--    /category/<slug>/ and let rebuild overwrite snapshots. Break the
--    tie by keeping the lowest-id row's slug and blanking the rest;
--    those rows fall back to the legacy /category/<id>/ URL.
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
