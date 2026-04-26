-- +goose Up
-- +goose StatementBegin
ALTER TABLE weblogs ADD COLUMN comment_mode TEXT NOT NULL DEFAULT 'moderated';

CREATE TABLE messages (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    wid          INTEGER NOT NULL DEFAULT 1,
    entry_id     INTEGER NOT NULL,
    status       INTEGER NOT NULL DEFAULT 0,  -- 0=waiting, 1=approved, -1=hidden/closed
    posted_at    INTEGER NOT NULL,
    author_name  TEXT    NOT NULL DEFAULT '',
    author_email TEXT    NOT NULL DEFAULT '',
    author_url   TEXT    NOT NULL DEFAULT '',
    body         TEXT    NOT NULL DEFAULT '',
    ip_address   TEXT    NOT NULL DEFAULT '',
    user_agent   TEXT    NOT NULL DEFAULT '',
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL
);
CREATE INDEX idx_messages_entry_status ON messages(entry_id, status, posted_at);
CREATE INDEX idx_messages_wid_status ON messages(wid, status, posted_at);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE messages;
ALTER TABLE weblogs DROP COLUMN comment_mode;
-- +goose StatementEnd
