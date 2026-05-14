-- +goose Up
-- +goose StatementBegin

-- Per-category "hidden" toggle. Hidden categories drop out of every
-- listing surface (home, archive, tag, feed, sidebar category_list,
-- prev/next navigation) and the static rebuild stops emitting their
-- /category/<key>/ snapshot, but the individual entry permalink stays
-- live so authors can keep linking to the post. The dynamic
-- /category/<key>/ route also keeps responding 200 for hidden
-- categories — operators sometimes link to the archive directly from
-- non-list surfaces (e.g. a flat page), and hiding it from listings is
-- the only behaviour we want here.
ALTER TABLE categories ADD COLUMN hidden INTEGER NOT NULL DEFAULT 0;
-- 0 = visible (default), 1 = hidden

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- SQLite only supports DROP COLUMN for columns added after the table
-- was created, which is our case here.
ALTER TABLE categories DROP COLUMN hidden;
-- +goose StatementEnd
