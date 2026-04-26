-- +goose Up
-- +goose StatementBegin
-- Per-user AI writing-assist configuration. Kind is the
-- provider selector ("openai-compat" | "claude"); empty string means
-- "AI disabled for this user". api_key_enc holds ciphertext produced by
-- internal/ai/crypto.go (AES-GCM with SB_AI_SECRET as the master key),
-- hex-encoded. auto_alt toggles whether uploaded images get an alt
-- suggestion generated automatically — defaults to 1 so the feature is
-- discoverable, but a user who doesn't want the API hit can clear it.
ALTER TABLE users ADD COLUMN ai_kind     TEXT    NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN ai_base_url TEXT    NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN ai_model    TEXT    NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN ai_api_key_enc TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN ai_auto_alt INTEGER NOT NULL DEFAULT 1;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- SQLite pre-3.35 can't drop columns; rebuild would be overkill here.
-- Leave the columns behind on down-migration — they're observational
-- on the user side and zero-defaulted for existing rows.
SELECT 1;
-- +goose StatementEnd
