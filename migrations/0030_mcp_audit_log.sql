-- +goose Up
-- +goose StatementBegin
-- Persist MCP write-tool invocations so admins can review "who did
-- what when" without scraping the process log. The row is
-- observational — insert failures never fail the mutation (same rule
-- as analytics + mcp_tokens.last_used_at). Operators who want the
-- audit in a separate file set SB_MCP_AUDIT_DB; when it points at
-- a different path, this table stays empty and the mcpaudit package
-- manages the schema in the external file directly.
CREATE TABLE IF NOT EXISTS mcp_audit_log (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    wid        INTEGER NOT NULL DEFAULT 1,
    token_id   INTEGER NOT NULL DEFAULT 0,
    author_id  INTEGER NOT NULL DEFAULT 0,
    tool       TEXT    NOT NULL,
    target_id  INTEGER NOT NULL DEFAULT 0,
    extra      TEXT    NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_mcp_audit_log_wid_created ON mcp_audit_log(wid, created_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_mcp_audit_log_wid_created;
DROP TABLE IF EXISTS mcp_audit_log;
-- +goose StatementEnd
