-- +goose Up
CREATE TABLE lessons (
    id INTEGER PRIMARY KEY,
    lesson_date TEXT NOT NULL,
    tutor TEXT NOT NULL,
    video_path TEXT NOT NULL,
    video_hash TEXT,
    duration_seconds INTEGER,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE transcripts (
    id INTEGER PRIMARY KEY,
    lesson_id INTEGER NOT NULL REFERENCES lessons(id),
    raw_json_path TEXT NOT NULL,
    utterances TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE prompts (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    version INTEGER NOT NULL,
    content TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE jobs (
    id INTEGER PRIMARY KEY,
    lesson_id INTEGER NOT NULL REFERENCES lessons(id),
    kind TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    attempts INTEGER NOT NULL DEFAULT 0,
    last_error TEXT,
    payload TEXT,
    prompt_id INTEGER REFERENCES prompts(id),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

-- +goose Down
DROP TABLE jobs;
DROP TABLE prompts;
DROP TABLE transcripts;
DROP TABLE lessons;
