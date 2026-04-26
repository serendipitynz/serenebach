-- +goose Up
-- +goose StatementBegin
ALTER TABLE users ADD COLUMN description    TEXT    NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN auto_linebreak INTEGER NOT NULL DEFAULT 1;
ALTER TABLE users ADD COLUMN list_visible   INTEGER NOT NULL DEFAULT 1;
ALTER TABLE users ADD COLUMN sort_order     INTEGER NOT NULL DEFAULT 0;

-- Role canonicalisation. Older installs stored 0 for every seeded
-- admin; promote that to the new RoleAdmin=1 constant so the
-- permission checks match. RolePower=2 / RoleRegular=3 will be
-- assigned by the admin UI going forward.
UPDATE users SET role = 1 WHERE role = 0;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
UPDATE users SET role = 0 WHERE role = 1;
ALTER TABLE users DROP COLUMN description;
ALTER TABLE users DROP COLUMN auto_linebreak;
ALTER TABLE users DROP COLUMN list_visible;
ALTER TABLE users DROP COLUMN sort_order;
-- +goose StatementEnd
