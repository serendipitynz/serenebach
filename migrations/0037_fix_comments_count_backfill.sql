-- +goose Up
-- +goose StatementBegin
-- Re-backfill comments_count: the original 0036 migration compared status
-- against the string 'approved' but messages.status is an INTEGER column
-- (0=waiting, 1=approved, -1=hidden). The string comparison yielded 0
-- rows, so every entry ended up with comments_count = 0 even when
-- approved comments existed. This migration re-runs the UPDATE with
-- the correct integer literal.
UPDATE entries SET comments_count = (
    SELECT COUNT(*) FROM messages
    WHERE messages.entry_id = entries.id
      AND messages.status = 1
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- No-op: reverting would lose accurate counts.
SELECT 1;
-- +goose StatementEnd