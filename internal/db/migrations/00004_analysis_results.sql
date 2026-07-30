-- +goose Up
CREATE UNIQUE INDEX idx_prompts_name_version ON prompts(name, version);

CREATE TABLE analysis_results (
    id INTEGER PRIMARY KEY,
    lesson_id INTEGER NOT NULL REFERENCES lessons(id),
    task TEXT NOT NULL,
    prompt_id INTEGER NOT NULL REFERENCES prompts(id),
    model TEXT NOT NULL,
    result_json TEXT NOT NULL,
    raw_response_path TEXT NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE(lesson_id, task)
);

CREATE TABLE lesson_topics (
    id INTEGER PRIMARY KEY,
    lesson_id INTEGER NOT NULL REFERENCES lessons(id),
    topic TEXT NOT NULL,
    UNIQUE(lesson_id, topic)
);

-- +goose Down
DROP TABLE lesson_topics;
DROP TABLE analysis_results;
DROP INDEX idx_prompts_name_version;
