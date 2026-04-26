-- +goose Up
-- +goose StatementBegin
-- CountRecentCommentsFromIP ran a full SCAN on messages for every
-- comment POST because only entry / wid indexes existed. This
-- covering index lets the sliding-window rate-limit check look up by
-- ip in O(log n).
CREATE INDEX idx_messages_ip_created ON messages(ip_address, created_at);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_messages_ip_created;
-- +goose StatementEnd
