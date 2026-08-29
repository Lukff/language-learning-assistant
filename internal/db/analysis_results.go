// internal/db/analysis_results.go
package db

import (
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

// UpsertPrompt inserts (name, version, content) into the prompts table if it
// doesn't already exist. If (name, version) already exists with different content, it's
// a sign of a forgotten version bump in the code (the "-vN" convention in the file name
// under prompts/); logs a warning and keeps the already-stored content — it doesn't
// overwrite, because analysis_results may already reference this prompt_id.
func UpsertPrompt(conn *sql.DB, name string, version int, content string) (int64, error) {
	var id int64
	var existingContent string
	err := conn.QueryRow(`SELECT id, content FROM prompts WHERE name = ? AND version = ?`, name, version).Scan(&id, &existingContent)
	if err == nil {
		if existingContent != content {
			slog.Warn("analysis: prompt content diverges from already registered version", "name", name, "version", version)
		}
		return id, nil
	}
	if err != sql.ErrNoRows {
		return 0, fmt.Errorf("fetch prompt %s v%d: %w", name, version, err)
	}

	res, err := conn.Exec(
		`INSERT INTO prompts (name, version, content, created_at) VALUES (?, ?, ?, ?)`,
		name, version, content, time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return 0, fmt.Errorf("insert prompt %s v%d: %w", name, version, err)
	}
	return res.LastInsertId()
}

// UpsertAnalysisResult stores (or replaces, if it already exists) the result of
// task for lessonID — reprocessing (Story 2) overwrites the
// existing row.
func UpsertAnalysisResult(conn *sql.DB, lessonID int64, task string, promptID int64, model, resultJSON string) error {
	_, err := conn.Exec(
		`INSERT INTO analysis_results (lesson_id, task, prompt_id, model, result_json, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(lesson_id, task) DO UPDATE SET
		     prompt_id = excluded.prompt_id,
		     model = excluded.model,
		     result_json = excluded.result_json,
		     created_at = excluded.created_at`,
		lessonID, task, promptID, model, resultJSON, time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("write analysis_result for lesson %d, task %s: %w", lessonID, task, err)
	}
	return nil
}

// AnalysisResult is the persisted result of an analysis task for a lesson.
type AnalysisResult struct {
	LessonID   int64
	Task       string
	PromptID   int64
	Model      string
	ResultJSON string
}

// FindAnalysisResult returns (nil, nil) if the task hasn't run yet for
// this lesson — a normal state while the corresponding job (Story 2)
// is pending/running/error, not an error.
func FindAnalysisResult(conn *sql.DB, lessonID int64, task string) (*AnalysisResult, error) {
	r := AnalysisResult{LessonID: lessonID, Task: task}
	err := conn.QueryRow(
		`SELECT prompt_id, model, result_json FROM analysis_results WHERE lesson_id = ? AND task = ?`,
		lessonID, task,
	).Scan(&r.PromptID, &r.Model, &r.ResultJSON)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fetch analysis_result for lesson %d, task %s: %w", lessonID, task, err)
	}
	return &r, nil
}

// ReplaceLessonTopics deletes the existing links and inserts the new ones (by
// topic_id) — the list is always derived wholesale from the most
// recent analyze_topics result, never an incremental merge. INSERT OR IGNORE
// absorbs a duplicate topic the caller might repeat.
func ReplaceLessonTopics(conn *sql.DB, lessonID int64, topicIDs []int64) error {
	tx, err := conn.Begin()
	if err != nil {
		return fmt.Errorf("start lesson_topics transaction for lesson %d: %w", lessonID, err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM lesson_topics WHERE lesson_id = ?`, lessonID); err != nil {
		return fmt.Errorf("delete old lesson_topics for lesson %d: %w", lessonID, err)
	}
	for _, topicID := range topicIDs {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO lesson_topics (lesson_id, topic_id) VALUES (?, ?)`, lessonID, topicID); err != nil {
			return fmt.Errorf("insert topic %d for lesson %d: %w", topicID, lessonID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit lesson_topics for lesson %d: %w", lessonID, err)
	}
	return nil
}

// DeleteSpeakerDependentAnalysisResults deletes the analyses that depend on
// who is the student/tutor in the lesson (everything except analyze_topics) — called when the
// student/tutor mapping changes (services.LibraryService.SetStudentSpeaker).
// analyze_topics doesn't mention Student/Tutor in the prompt and survives; lesson_topics
// also stays intact (the table is the source of truth for the chips, independent
// of the analysis).
func DeleteSpeakerDependentAnalysisResults(conn *sql.DB, lessonID int64) error {
	if _, err := conn.Exec(`DELETE FROM analysis_results WHERE lesson_id = ? AND task != 'analyze_topics'`, lessonID); err != nil {
		return fmt.Errorf("delete speaker-dependent analyses for lesson %d: %w", lessonID, err)
	}
	return nil
}
