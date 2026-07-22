-- +goose Up
ALTER TABLE lessons ADD COLUMN file_size INTEGER;
ALTER TABLE lessons ADD COLUMN file_mtime TEXT;
CREATE UNIQUE INDEX idx_lessons_video_hash ON lessons(video_hash) WHERE video_hash IS NOT NULL;

CREATE TABLE pending_imports (
    id INTEGER PRIMARY KEY,
    path TEXT NOT NULL,
    file_size INTEGER NOT NULL,
    file_mtime TEXT NOT NULL,
    sha256 TEXT NOT NULL UNIQUE,
    suggested_date TEXT,
    created_at TEXT NOT NULL
);

-- +goose Down
DROP TABLE pending_imports;
DROP INDEX idx_lessons_video_hash;
ALTER TABLE lessons DROP COLUMN file_mtime;
ALTER TABLE lessons DROP COLUMN file_size;
