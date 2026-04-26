-- +goose Up
-- +goose StatementBegin
-- Binds each MCP token to the user whose name write operations should
-- be attributed to. Default 1 attributes pre-existing rows to the seed
-- admin (the only user before this column existed). New rows must set
-- this explicitly from the mint form.
ALTER TABLE mcp_tokens ADD COLUMN author_id INTEGER NOT NULL DEFAULT 1;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- SQLite pre-3.35 can't drop columns; rebuild the table to remove it.
CREATE TABLE mcp_tokens_new (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    wid           INTEGER NOT NULL DEFAULT 1,
    name          TEXT    NOT NULL,
    token_hash    TEXT    NOT NULL,
    prefix        TEXT    NOT NULL DEFAULT '',
    scope         TEXT    NOT NULL DEFAULT 'read',
    created_at    INTEGER NOT NULL,
    last_used_at  INTEGER NOT NULL DEFAULT 0,
    revoked_at    INTEGER NOT NULL DEFAULT 0
);
INSERT INTO mcp_tokens_new (id, wid, name, token_hash, prefix, scope, created_at, last_used_at, revoked_at)
SELECT id, wid, name, token_hash, prefix, scope, created_at, last_used_at, revoked_at FROM mcp_tokens;
DROP INDEX IF EXISTS idx_mcp_tokens_wid;
DROP INDEX IF EXISTS idx_mcp_tokens_hash;
DROP TABLE mcp_tokens;
ALTER TABLE mcp_tokens_new RENAME TO mcp_tokens;
CREATE UNIQUE INDEX idx_mcp_tokens_hash ON mcp_tokens(token_hash);
CREATE INDEX idx_mcp_tokens_wid ON mcp_tokens(wid, revoked_at);
-- +goose StatementEnd
