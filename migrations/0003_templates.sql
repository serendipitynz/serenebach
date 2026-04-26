-- +goose Up
-- +goose StatementBegin
ALTER TABLE weblogs ADD COLUMN base_url TEXT NOT NULL DEFAULT '';
ALTER TABLE weblogs ADD COLUMN lang     TEXT NOT NULL DEFAULT 'ja';

CREATE TABLE templates (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    wid         INTEGER NOT NULL DEFAULT 1,
    name        TEXT    NOT NULL,
    is_active   INTEGER NOT NULL DEFAULT 0,  -- 1 = active; only one per wid expected
    main_body   TEXT    NOT NULL DEFAULT '',
    entry_body  TEXT    NOT NULL DEFAULT '',
    css         TEXT    NOT NULL DEFAULT '',
    info        TEXT    NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);
CREATE INDEX idx_templates_wid_active ON templates(wid, is_active);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE templates;
ALTER TABLE weblogs DROP COLUMN lang;
ALTER TABLE weblogs DROP COLUMN base_url;
-- +goose StatementEnd
