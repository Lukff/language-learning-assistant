-- +goose Up
ALTER TABLE lessons ADD COLUMN student_speaker_label TEXT;

-- +goose Down
ALTER TABLE lessons DROP COLUMN student_speaker_label;
