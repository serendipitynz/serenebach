-- +goose Up
-- +goose StatementBegin

-- Per-entry legacy URL inputs (from SB3 sb_entry).
-- legacy_id  : SB3 entry_id used by ?eid={id} and {prefix}{id}{suffix}.html.
--              NULL = no legacy id recorded. SB3 ids are 0-based, so 0 must
--              remain a valid value; do not use it as a sentinel.
-- legacy_file: SB3 entry_file (custom save name); empty for default eid{id} runs.
ALTER TABLE entries ADD COLUMN legacy_id   INTEGER;
ALTER TABLE entries ADD COLUMN legacy_file TEXT NOT NULL DEFAULT '';

CREATE INDEX idx_entries_legacy_id   ON entries(wid, legacy_id)   WHERE legacy_id   IS NOT NULL;
CREATE INDEX idx_entries_legacy_file ON entries(wid, legacy_file) WHERE legacy_file != '';

-- Per-category legacy URL inputs (from SB3 sb_category).
ALTER TABLE categories ADD COLUMN legacy_id  INTEGER;
ALTER TABLE categories ADD COLUMN legacy_dir TEXT NOT NULL DEFAULT '';

CREATE INDEX idx_categories_legacy_id  ON categories(wid, legacy_id)  WHERE legacy_id  IS NOT NULL;
CREATE INDEX idx_categories_legacy_dir ON categories(wid, legacy_dir) WHERE legacy_dir != '';

-- Per-weblog Perl URL reconstruction inputs.
-- All optional. Empty string means "no legacy URL pattern recorded; redirect off."
-- archive_type: 'Individual' | 'Monthly' | '' (== dynamic only)
-- log_path    : SB3 conf_dir_log, normalised to trailing slash (e.g. 'log/').
-- base_path   : SB3 conf_srv_base, normalised to a path prefix (e.g. '/blog/').
-- cgi_name    : SB3 basic_sb (e.g. 'sb.cgi') used to match /sb.cgi?eid=.
-- id_prefix   : SB3 basic_preid (default 'eid').
-- suffix      : SB3 basic_suffix (default '.html').
ALTER TABLE weblogs ADD COLUMN legacy_archive_type TEXT NOT NULL DEFAULT '';
ALTER TABLE weblogs ADD COLUMN legacy_log_path     TEXT NOT NULL DEFAULT '';
ALTER TABLE weblogs ADD COLUMN legacy_base_path    TEXT NOT NULL DEFAULT '';
ALTER TABLE weblogs ADD COLUMN legacy_cgi_name     TEXT NOT NULL DEFAULT '';
ALTER TABLE weblogs ADD COLUMN legacy_id_prefix    TEXT NOT NULL DEFAULT '';
ALTER TABLE weblogs ADD COLUMN legacy_suffix       TEXT NOT NULL DEFAULT '';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_entries_legacy_id;
DROP INDEX IF EXISTS idx_entries_legacy_file;
DROP INDEX IF EXISTS idx_categories_legacy_id;
DROP INDEX IF EXISTS idx_categories_legacy_dir;

ALTER TABLE entries    DROP COLUMN legacy_id;
ALTER TABLE entries    DROP COLUMN legacy_file;
ALTER TABLE categories DROP COLUMN legacy_id;
ALTER TABLE categories DROP COLUMN legacy_dir;
ALTER TABLE weblogs    DROP COLUMN legacy_archive_type;
ALTER TABLE weblogs    DROP COLUMN legacy_log_path;
ALTER TABLE weblogs    DROP COLUMN legacy_base_path;
ALTER TABLE weblogs    DROP COLUMN legacy_cgi_name;
ALTER TABLE weblogs    DROP COLUMN legacy_id_prefix;
ALTER TABLE weblogs    DROP COLUMN legacy_suffix;
-- +goose StatementEnd
