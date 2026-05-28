-- +goose Up
-- +goose StatementBegin

CREATE VIRTUAL TABLE entries_fts USING fts5(
    title,
    body,
    more,
    keywords,
    tokenize = 'trigram'
);

INSERT INTO entries_fts (rowid, title, body, more, keywords)
SELECT id, title, body, COALESCE(more,''), COALESCE(keywords,'') FROM entries;

CREATE TRIGGER entries_fts_ai AFTER INSERT ON entries BEGIN
    INSERT INTO entries_fts (rowid, title, body, more, keywords)
    VALUES (new.id, new.title, new.body, COALESCE(new.more,''), COALESCE(new.keywords,''));
END;

CREATE TRIGGER entries_fts_au AFTER UPDATE ON entries BEGIN
    UPDATE entries_fts
       SET title = new.title,
           body = new.body,
           more = COALESCE(new.more,''),
           keywords = COALESCE(new.keywords,'')
     WHERE rowid = new.id;
END;

CREATE TRIGGER entries_fts_ad AFTER DELETE ON entries BEGIN
    DELETE FROM entries_fts WHERE rowid = old.id;
END;

ALTER TABLE weblogs ADD COLUMN static_search_form_enabled INTEGER NOT NULL DEFAULT 0;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE weblogs DROP COLUMN static_search_form_enabled;
DROP TRIGGER IF EXISTS entries_fts_ad;
DROP TRIGGER IF EXISTS entries_fts_au;
DROP TRIGGER IF EXISTS entries_fts_ai;
DROP TABLE IF EXISTS entries_fts;
-- +goose StatementEnd
