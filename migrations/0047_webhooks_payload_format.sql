-- +goose Up
-- +goose StatementBegin

-- Per-subscription payload shape. Default keeps every existing row on
-- the envelope JSON form (id / event / timestamp / weblog / data...).
-- "flat" applies the slack.dev "flatten JSON for Workflow Builder"
-- rule (dots → underscores, array indices joined with underscores) so
-- the payload becomes a single-level object whose keys can be picked
-- up as trigger variables by tools that only accept top-level keys.
ALTER TABLE webhooks ADD COLUMN payload_format TEXT NOT NULL DEFAULT 'envelope';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE webhooks DROP COLUMN payload_format;
-- +goose StatementEnd
