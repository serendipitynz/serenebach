-- +goose Up
-- +goose StatementBegin
-- Custom text color for Open Graph cards. Value is a hex literal: "#RRGGBB" for opaque, "#RRGGBBAA" for
-- transparency (e.g. "#00000000" to hide the text entirely).
-- Empty string = use the default two-tone (entry title #475569,
-- site name #94a3b8). Applied uniformly to both strings — the
-- single-color simplification the operator asked for.
ALTER TABLE weblogs ADD COLUMN og_text_color TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE weblogs DROP COLUMN og_text_color;
-- +goose StatementEnd
