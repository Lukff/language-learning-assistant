package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"assistente-idiomas/internal/stt"
)

// HasTranscript reports whether a transcript has already been stored for
// lessonID — used by internal/jobs.Worker for the transcribe
// job's idempotency.
func HasTranscript(conn *sql.DB, lessonID int64) (bool, error) {
	var id int64
	err := conn.QueryRow(`SELECT id FROM transcripts WHERE lesson_id = ?`, lessonID).Scan(&id)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("fetch transcript for lesson %d: %w", lessonID, err)
	}
	return true, nil
}

// InsertTranscript stores the transcript of a lesson. utterancesJSON comes
// already serialized ([]stt.Utterance as JSON).
func InsertTranscript(conn *sql.DB, lessonID int64, utterancesJSON string) error {
	_, err := conn.Exec(
		`INSERT INTO transcripts (lesson_id, utterances, created_at) VALUES (?, ?, ?)`,
		lessonID, utterancesJSON, time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("insert transcript for lesson %d: %w", lessonID, err)
	}
	return nil
}

// Transcript is the full transcript of a lesson, already deserialized.
type Transcript struct {
	LessonID   int64
	Utterances []stt.Utterance
}

// FindTranscriptByLessonID looks up the transcript of a lesson. Returns
// (nil, nil) if no transcript has been stored yet — a normal state
// while the transcribe job is pending/running or has failed (see
// LibraryService.GetLesson/GetTranscript, Story 6), not an error.
func FindTranscriptByLessonID(conn *sql.DB, lessonID int64) (*Transcript, error) {
	var utterancesJSON string
	err := conn.QueryRow(
		`SELECT utterances FROM transcripts WHERE lesson_id = ?`, lessonID,
	).Scan(&utterancesJSON)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fetch transcript for lesson %d: %w", lessonID, err)
	}
	var utterances []stt.Utterance
	if err := json.Unmarshal([]byte(utterancesJSON), &utterances); err != nil {
		return nil, fmt.Errorf("unmarshal utterances for lesson %d: %w", lessonID, err)
	}
	return &Transcript{LessonID: lessonID, Utterances: utterances}, nil
}
