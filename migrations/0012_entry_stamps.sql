-- +goose Up
-- +goose StatementBegin
ALTER TABLE entries ADD COLUMN stamps_count INTEGER NOT NULL DEFAULT 0;

-- One row per (entry, stamp_kind, fingerprint) triple. The UNIQUE
-- index makes INSERT OR IGNORE cheap on the hot path — duplicates
-- skip without error. stamp_kind is a short ASCII token (heart,
-- laugh, wow, party) — we keep rendering of the emoji in the view
-- layer so the DB can still be grep'd as text.
CREATE TABLE entry_stamps (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    entry_id    INTEGER NOT NULL,
    stamp_kind  TEXT    NOT NULL,
    fingerprint TEXT    NOT NULL,
    created_at  INTEGER NOT NULL
);
CREATE UNIQUE INDEX uq_entry_stamps_entry_kind_fp ON entry_stamps(entry_id, stamp_kind, fingerprint);
CREATE INDEX idx_entry_stamps_entry ON entry_stamps(entry_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE entry_stamps;
ALTER TABLE entries DROP COLUMN stamps_count;
-- +goose StatementEnd
