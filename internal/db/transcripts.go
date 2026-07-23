package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"assistente-idiomas/internal/stt"
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

// Transcript é a transcrição completa de uma lesson, já desserializada.
type Transcript struct {
	LessonID    int64
	RawJSONPath string
	Utterances  []stt.Utterance
}

// FindTranscriptByLessonID busca a transcrição de uma lesson. Retorna
// (nil, nil) se ainda não houver transcrição gravada — estado normal
// enquanto o job transcribe está pendente/rodando ou falhou (ver
// LibraryService.GetLesson/GetTranscript, História 6), não um erro.
func FindTranscriptByLessonID(conn *sql.DB, lessonID int64) (*Transcript, error) {
	var rawJSONPath, utterancesJSON string
	err := conn.QueryRow(
		`SELECT raw_json_path, utterances FROM transcripts WHERE lesson_id = ?`, lessonID,
	).Scan(&rawJSONPath, &utterancesJSON)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("buscar transcript da lesson %d: %w", lessonID, err)
	}
	var utterances []stt.Utterance
	if err := json.Unmarshal([]byte(utterancesJSON), &utterances); err != nil {
		return nil, fmt.Errorf("desserializar utterances da lesson %d: %w", lessonID, err)
	}
	return &Transcript{LessonID: lessonID, RawJSONPath: rawJSONPath, Utterances: utterances}, nil
}
