-- +goose Up
CREATE TABLE topics (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

INSERT INTO topics (name, created_at, updated_at)
SELECT DISTINCT topic, strftime('%Y-%m-%dT%H:%M:%SZ','now'), strftime('%Y-%m-%dT%H:%M:%SZ','now')
FROM lesson_topics;

CREATE TABLE lesson_topics_new (
    id INTEGER PRIMARY KEY,
    lesson_id INTEGER NOT NULL REFERENCES lessons(id),
    topic_id INTEGER NOT NULL REFERENCES topics(id),
    UNIQUE(lesson_id, topic_id)
);
INSERT INTO lesson_topics_new (lesson_id, topic_id)
SELECT lt.lesson_id, t.id FROM lesson_topics lt JOIN topics t ON t.name = lt.topic;
DROP TABLE lesson_topics;
ALTER TABLE lesson_topics_new RENAME TO lesson_topics;

-- +goose Down
CREATE TABLE lesson_topics_old (
    id INTEGER PRIMARY KEY,
    lesson_id INTEGER NOT NULL REFERENCES lessons(id),
    topic TEXT NOT NULL,
    UNIQUE(lesson_id, topic)
);
INSERT INTO lesson_topics_old (lesson_id, topic)
SELECT lt.lesson_id, t.name FROM lesson_topics lt JOIN topics t ON t.id = lt.topic_id;
DROP TABLE lesson_topics;
ALTER TABLE lesson_topics_old RENAME TO lesson_topics;
DROP TABLE topics;
