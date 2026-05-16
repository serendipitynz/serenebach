-- +goose Up

-- Outbound webhooks. Operators register URLs to receive JSON POSTs when
-- specific events fire (entry.published / comment.received / ...). The
-- `events` column holds the JSON-encoded array of subscribed event ids;
-- `secret` is the optional HMAC-SHA256 key (empty disables signing). The
-- design is fire-and-forget: no retry queue, no scheduled re-send.
CREATE TABLE webhooks (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    wid        INTEGER NOT NULL DEFAULT 1,
    url        TEXT    NOT NULL,
    secret     TEXT    NOT NULL DEFAULT '',
    events     TEXT    NOT NULL DEFAULT '[]',
    active     INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')),
    updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now'))
);

-- Per-attempt delivery log. status_code IS NULL while the request is
-- in-flight; on completion the row is updated with the HTTP status,
-- delivered_at, and any transport error message.
CREATE TABLE webhook_deliveries (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    webhook_id   INTEGER NOT NULL REFERENCES webhooks(id) ON DELETE CASCADE,
    event        TEXT    NOT NULL,
    delivery_id  TEXT    NOT NULL,
    payload      TEXT    NOT NULL,
    status_code  INTEGER,
    error        TEXT    NOT NULL DEFAULT '',
    delivered_at INTEGER,
    created_at   INTEGER NOT NULL DEFAULT (strftime('%s','now'))
);

CREATE INDEX idx_webhook_deliveries_webhook ON webhook_deliveries(webhook_id, created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_webhook_deliveries_webhook;
DROP TABLE IF EXISTS webhook_deliveries;
DROP TABLE IF EXISTS webhooks;
