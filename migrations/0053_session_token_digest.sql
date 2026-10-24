-- +goose Up
-- +goose StatementBegin

-- Sessions are now looked up by the SHA-256 of the cookie value, so a
-- leaked database or backup no longer holds usable cookies. Existing rows
-- are dropped rather than converted: SQLite has no built-in SHA-256, and a
-- one-time sign-in is a smaller cost than a Go-side migration step.
DROP TABLE sessions;
CREATE TABLE sessions (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    token_hash TEXT    NOT NULL UNIQUE,
    user_id    INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE INDEX idx_sessions_user_id ON sessions(user_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE sessions;
CREATE TABLE sessions (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    token      TEXT    NOT NULL UNIQUE,
    user_id    INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE INDEX idx_sessions_user_id ON sessions(user_id);

-- +goose StatementEnd
