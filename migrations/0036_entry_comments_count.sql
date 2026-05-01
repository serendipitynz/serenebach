-- +goose Up
-- +goose StatementBegin
ALTER TABLE entries ADD COLUMN comments_count INTEGER NOT NULL DEFAULT 0;

-- Backfill: set comments_count to the actual number of approved comments
-- per entry so the counter is accurate from the start.
-- MessageStatus is stored as INTEGER: 0=waiting, 1=approved, -1=hidden.
UPDATE entries SET comments_count = (
    SELECT COUNT(*) FROM messages
    WHERE messages.entry_id = entries.id
      AND messages.status = 1
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE entries DROP COLUMN comments_count;
-- +goose StatementEnd