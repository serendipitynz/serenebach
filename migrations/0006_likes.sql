-- +goose Up
-- +goose StatementBegin
ALTER TABLE entries ADD COLUMN likes_count INTEGER NOT NULL DEFAULT 0;

-- entry_likes records one row per (entry, visitor-fingerprint) pair. The
-- UNIQUE constraint is what makes INSERT OR IGNORE cheap — if the row is
-- already there we skip the counter bump entirely.
CREATE TABLE entry_likes (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    entry_id    INTEGER NOT NULL,
    fingerprint TEXT    NOT NULL,
    created_at  INTEGER NOT NULL
);
CREATE UNIQUE INDEX uq_entry_likes_entry_fp ON entry_likes(entry_id, fingerprint);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE entry_likes;
ALTER TABLE entries DROP COLUMN likes_count;
-- +goose StatementEnd
