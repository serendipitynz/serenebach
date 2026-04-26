-- +goose Up
-- +goose StatementBegin
-- Per-user AI request timeout in seconds. 0 means "use the per-feature
-- code default" (currently 120 s for compose, 45 s for vision/alt-text);
-- a positive value overrides that ceiling for both the compose and
-- alt-text paths. Bounded at the form layer (1..600) so a malformed
-- POST can't park a request indefinitely.
ALTER TABLE users ADD COLUMN ai_timeout_seconds INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- SQLite pre-3.35 can't drop columns; rebuild would be overkill.
-- Leave the column behind on down-migration — observational, zero default.
SELECT 1;
-- +goose StatementEnd
