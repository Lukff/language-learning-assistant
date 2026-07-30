-- +goose Up
CREATE TABLE teachers (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

INSERT INTO teachers (name, created_at, updated_at)
SELECT DISTINCT tutor, datetime('now'), datetime('now') FROM lessons;

ALTER TABLE lessons ADD COLUMN teacher_id INTEGER REFERENCES teachers(id);

UPDATE lessons
SET teacher_id = (SELECT id FROM teachers WHERE teachers.name = lessons.tutor);

ALTER TABLE lessons DROP COLUMN tutor;

-- +goose Down
ALTER TABLE lessons ADD COLUMN tutor TEXT;
ALTER TABLE lessons DROP COLUMN teacher_id;
DROP TABLE teachers;
