-- +goose Up
ALTER TABLE entries ADD COLUMN pinned INTEGER NOT NULL DEFAULT 0;
-- 0 = not pinned, 1 = pinned

CREATE INDEX idx_entries_wid_pinned ON entries(wid, pinned);

-- +goose Down
DROP INDEX IF EXISTS idx_entries_wid_pinned;
ALTER TABLE entries DROP COLUMN pinned;
