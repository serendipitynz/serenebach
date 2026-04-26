-- +goose Up
-- +goose StatementBegin
ALTER TABLE weblogs ADD COLUMN spam_words TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE weblogs DROP COLUMN spam_words;
-- +goose StatementEnd
