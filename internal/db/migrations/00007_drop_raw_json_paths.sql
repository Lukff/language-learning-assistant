-- +goose Up
ALTER TABLE transcripts DROP COLUMN raw_json_path;
ALTER TABLE analysis_results DROP COLUMN raw_response_path;

-- +goose Down
ALTER TABLE transcripts ADD COLUMN raw_json_path TEXT NOT NULL DEFAULT '';
ALTER TABLE analysis_results ADD COLUMN raw_response_path TEXT NOT NULL DEFAULT '';
