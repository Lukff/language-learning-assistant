package db

import (
	"database/sql"
	"fmt"
	"time"
)

// HasTranscript indica se já existe uma transcrição gravada para
// lessonID — usado por internal/jobs.Worker pra idempotência do job
// transcribe.
func HasTranscript(conn *sql.DB, lessonID int64) (bool, error) {
	var id int64
	err := conn.QueryRow(`SELECT id FROM transcripts WHERE lesson_id = ?`, lessonID).Scan(&id)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("buscar transcript da lesson %d: %w", lessonID, err)
	}
	return true, nil
}

// InsertTranscript grava a transcrição de uma lesson. rawJSONPath é
// relativo à storage_root (mesma convenção de lessons.video_path);
// utterancesJSON já vem serializado ([]stt.Utterance em JSON).
func InsertTranscript(conn *sql.DB, lessonID int64, rawJSONPath string, utterancesJSON string) error {
	_, err := conn.Exec(
		`INSERT INTO transcripts (lesson_id, raw_json_path, utterances, created_at) VALUES (?, ?, ?, ?)`,
		lessonID, rawJSONPath, utterancesJSON, time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("inserir transcript da lesson %d: %w", lessonID, err)
	}
	return nil
}
