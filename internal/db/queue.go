// internal/db/queue.go
package db

import (
	"database/sql"
	"fmt"
	"sort"
)

// QueueEntry is a lesson with an active pipeline (pending/running) or in error,
// in the format the Queue (Story 7) needs: which job is "current" right now,
// not just the collapsed status LessonWithStatus uses for the Library.
type QueueEntry struct {
	LessonID    int64
	LessonDate  string
	TeacherName string
	Kind        string // "extract_audio" or "transcribe"
	Status      string // "pending", "running" or "error"
	Attempts    int
	LastError   string
	UpdatedAt   string
}

// ListQueueEntries lists lessons with an active pipeline or in error, one row
// per lesson (never two), with each one's "current" job. Ready lessons
// (transcribe done) don't enter the list — that's already visible in the Library.
//
// Priority for deciding the current job (extract_audio checked before
// transcribe): the transcribe job stays with status "pending" in the database
// the whole time it's blocked waiting for extract_audio to finish — the
// Worker only skips it in memory (claimNextEligibleJob in
// internal/jobs/worker.go), without changing that status. Checking transcribe
// before extract_audio would show "Transcription — waiting" for a lesson that
// is actually still extracting audio.
//
// Ordering: error first (needs user action), then by the current job's
// UpdatedAt, oldest first (same FIFO order the
// Worker uses in ListPendingJobs).
func ListQueueEntries(conn *sql.DB) ([]QueueEntry, error) {
	rows, err := conn.Query(`
		SELECT
			l.id, l.lesson_date, t.name,
			COALESCE(ea.status, ''), COALESCE(ea.attempts, 0), COALESCE(ea.last_error, ''), COALESCE(ea.updated_at, ''),
			COALESCE(tr.status, ''), COALESCE(tr.attempts, 0), COALESCE(tr.last_error, ''), COALESCE(tr.updated_at, '')
		FROM lessons l
		JOIN teachers t ON t.id = l.teacher_id
		LEFT JOIN jobs ea ON ea.lesson_id = l.id AND ea.kind = 'extract_audio'
		LEFT JOIN jobs tr ON tr.lesson_id = l.id AND tr.kind = 'transcribe'
	`)
	if err != nil {
		return nil, fmt.Errorf("list lessons with jobs for queue: %w", err)
	}
	defer rows.Close()

	out := make([]QueueEntry, 0)
	for rows.Next() {
		var lessonID int64
		var lessonDate, teacherName string
		var extractStatus, extractError, extractUpdatedAt string
		var extractAttempts int
		var transcribeStatus, transcribeError, transcribeUpdatedAt string
		var transcribeAttempts int
		if err := rows.Scan(
			&lessonID, &lessonDate, &teacherName,
			&extractStatus, &extractAttempts, &extractError, &extractUpdatedAt,
			&transcribeStatus, &transcribeAttempts, &transcribeError, &transcribeUpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("read queue row: %w", err)
		}

		entry := QueueEntry{LessonID: lessonID, LessonDate: lessonDate, TeacherName: teacherName}
		switch {
		case extractStatus == "error":
			entry.Kind, entry.Status = "extract_audio", "error"
			entry.Attempts, entry.LastError, entry.UpdatedAt = extractAttempts, extractError, extractUpdatedAt
		case extractStatus == "pending" || extractStatus == "running":
			entry.Kind, entry.Status = "extract_audio", extractStatus
			entry.Attempts, entry.LastError, entry.UpdatedAt = extractAttempts, extractError, extractUpdatedAt
		case transcribeStatus == "error":
			entry.Kind, entry.Status = "transcribe", "error"
			entry.Attempts, entry.LastError, entry.UpdatedAt = transcribeAttempts, transcribeError, transcribeUpdatedAt
		case transcribeStatus == "pending" || transcribeStatus == "running":
			entry.Kind, entry.Status = "transcribe", transcribeStatus
			entry.Attempts, entry.LastError, entry.UpdatedAt = transcribeAttempts, transcribeError, transcribeUpdatedAt
		default:
			// transcribe done (or no job — shouldn't happen, both
			// jobs are always created together on import confirmation):
			// lesson ready, doesn't enter the queue.
			continue
		}
		out = append(out, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate queue: %w", err)
	}

	sortQueueEntries(out)
	return out, nil
}

// sortQueueEntries sorts in-place: "error" status first, then by
// ascending UpdatedAt (FIFO) — see the ordering rule in ListQueueEntries's
// comment.
func sortQueueEntries(entries []QueueEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		iErr, jErr := entries[i].Status == "error", entries[j].Status == "error"
		if iErr != jErr {
			return iErr
		}
		return entries[i].UpdatedAt < entries[j].UpdatedAt
	})
}
