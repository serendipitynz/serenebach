-- +goose Up
-- +goose StatementBegin

-- Failed admin logins, kept in the database so the brute-force limit holds
-- under CGI, where every request is a fresh process and an in-memory
-- counter starts from zero each time. key_hash is the SHA-256 of the
-- (client IP, submitted name) pair: the submitted name can be a password
-- typed into the wrong field, so it is not stored as entered.
CREATE TABLE login_failures (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    key_hash  TEXT    NOT NULL,
    failed_at INTEGER NOT NULL
);
CREATE INDEX idx_login_failures_key ON login_failures(key_hash, failed_at);
CREATE INDEX idx_login_failures_failed_at ON login_failures(failed_at);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE login_failures;

-- +goose StatementEnd
