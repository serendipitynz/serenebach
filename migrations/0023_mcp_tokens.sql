-- +goose Up
-- +goose StatementBegin
CREATE TABLE mcp_tokens (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    wid           INTEGER NOT NULL DEFAULT 1,
    name          TEXT    NOT NULL,
    token_hash    TEXT    NOT NULL,
    prefix        TEXT    NOT NULL DEFAULT '',
    created_at    INTEGER NOT NULL,
    last_used_at  INTEGER NOT NULL DEFAULT 0,
    revoked_at    INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX idx_mcp_tokens_hash ON mcp_tokens(token_hash);
CREATE INDEX idx_mcp_tokens_wid ON mcp_tokens(wid, revoked_at);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_mcp_tokens_wid;
DROP INDEX IF EXISTS idx_mcp_tokens_hash;
DROP TABLE IF EXISTS mcp_tokens;
-- +goose StatementEnd
