-- +goose Up
-- +goose StatementBegin

-- 1) kind カラム追加（既存行は image 確定なので 'image' で埋める）
ALTER TABLE images ADD COLUMN kind TEXT NOT NULL DEFAULT 'image';
UPDATE images SET kind = 'image' WHERE kind = '';

-- 2) width/height を NULL で運用するためテーブル再構築（SQLite は ALTER COLUMN 不可）
CREATE TABLE images_new (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    wid         INTEGER NOT NULL DEFAULT 1,
    uploaded_by INTEGER NOT NULL,
    kind        TEXT    NOT NULL DEFAULT 'image',
    filename    TEXT    NOT NULL,
    stored_path TEXT    NOT NULL,
    thumb_path  TEXT    NOT NULL DEFAULT '',
    mime_type   TEXT    NOT NULL,
    size_bytes  INTEGER NOT NULL,
    width       INTEGER,
    height      INTEGER,
    alt_text    TEXT    NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);

INSERT INTO images_new (id, wid, uploaded_by, kind, filename, stored_path, thumb_path,
                        mime_type, size_bytes, width, height, alt_text, created_at, updated_at)
SELECT id, wid, uploaded_by, kind, filename, stored_path, thumb_path,
       mime_type, size_bytes,
       CASE WHEN width  = 0 THEN NULL ELSE width  END,
       CASE WHEN height = 0 THEN NULL ELSE height END,
       alt_text, created_at, updated_at
FROM images;

DROP TABLE images;
ALTER TABLE images_new RENAME TO images;

CREATE INDEX idx_images_wid_created ON images(wid, created_at DESC);
CREATE INDEX idx_images_wid_kind    ON images(wid, kind);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- 1 way migration (kind / NULL width-height は復元しない)
SELECT 1;
-- +goose StatementEnd
