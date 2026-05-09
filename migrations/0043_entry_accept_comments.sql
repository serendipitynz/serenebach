-- +goose Up
ALTER TABLE entries ADD COLUMN accept_comments INTEGER NOT NULL DEFAULT 1;
-- 1 = accept comments (default), 0 = author opted-out for this entry.
-- The weblog-level comment_mode still wins: when comment_mode = 'closed',
-- accept_comments has no effect.

-- +goose Down
ALTER TABLE entries DROP COLUMN accept_comments;
