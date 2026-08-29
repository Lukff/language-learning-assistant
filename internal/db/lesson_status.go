// internal/db/lesson_status.go
package db

import (
	"database/sql"
	"fmt"
	"strings"
)

// LessonFilter filters ListLessonsWithStatus — all fields are optional
// (zero value = no filter), used by the Library's teacher/period filter
// (Story 5, TeacherID since Story 9).
type LessonFilter struct {
	TeacherID int64
	DateFrom  string // YYYY-MM-DD, inclusive
	DateTo    string // YYYY-MM-DD, inclusive
	TopicIDs  []int64 // empty = no filter; OR semantics (Phase 2, Story 4)
}

// LessonWithStatus is a lesson with the status derived from the
// extract_audio/transcribe jobs. Status is always one of "processing", "ready",
// "error"; ErrorMessage is only filled when Status == "error" — see the
// derivation rules in deriveStatus.
type LessonWithStatus struct {
	Lesson
	Status       string
	ErrorMessage string
}

// lessonWithStatusColumns and lessonWithStatusFromJoin are shared by
// ListLessonsWithStatus (multiple rows) and FindLessonWithStatusByID (one
// row, Story 6) — same column list/JOIN, so they don't diverge.
const lessonWithStatusColumns = `
		l.id, l.lesson_date, l.teacher_id, t.name, l.video_path,
		COALESCE(l.video_hash, ''), COALESCE(l.file_size, 0), COALESCE(l.file_mtime, ''),
		l.duration_seconds, l.student_speaker_label,
		COALESCE(ea.status, ''), COALESCE(ea.last_error, ''),
		COALESCE(tr.status, ''), COALESCE(tr.last_error, '')`

const lessonWithStatusFromJoin = `
	FROM lessons l
	JOIN teachers t ON t.id = l.teacher_id
	LEFT JOIN jobs ea ON ea.lesson_id = l.id AND ea.kind = 'extract_audio'
	LEFT JOIN jobs tr ON tr.lesson_id = l.id AND tr.kind = 'transcribe'`

// rowScanner is satisfied by both *sql.Row (one row) and *sql.Rows
// (multiple rows) — allows sharing the scan between
// ListLessonsWithStatus and FindLessonWithStatusByID.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanLessonWithStatusRow(s rowScanner) (LessonWithStatus, error) {
	var lws LessonWithStatus
	var duration sql.NullInt64
	var studentSpeaker sql.NullString
	var extractStatus, extractError, transcribeStatus, transcribeError string
	err := s.Scan(
		&lws.ID, &lws.LessonDate, &lws.TeacherID, &lws.TeacherName, &lws.VideoPath,
		&lws.VideoHash, &lws.FileSize, &lws.FileMTime,
		&duration, &studentSpeaker,
		&extractStatus, &extractError,
		&transcribeStatus, &transcribeError,
	)
	if err != nil {
		return LessonWithStatus{}, err
	}
	if duration.Valid {
		d := duration.Int64
		lws.DurationSeconds = &d
	}
	if studentSpeaker.Valid {
		sp := studentSpeaker.String
		lws.StudentSpeakerLabel = &sp
	}
	lws.Status, lws.ErrorMessage = deriveStatus(extractStatus, extractError, transcribeStatus, transcribeError)
	return lws, nil
}

// ListLessonsWithStatus lists confirmed lessons with the status derived
// from the jobs, most recent first, applying filter (zero fields are
// ignored). The date filter compares only the YYYY-MM-DD part of
// lesson_date (which may have a time, in <input type="datetime-local"> format),
// to include lessons with a recorded time on any day within the range.
func ListLessonsWithStatus(conn *sql.DB, filter LessonFilter) ([]LessonWithStatus, error) {
	query := `SELECT` + lessonWithStatusColumns + lessonWithStatusFromJoin + ` WHERE 1=1`
	var args []any
	if filter.TeacherID != 0 {
		query += ` AND l.teacher_id = ?`
		args = append(args, filter.TeacherID)
	}
	if filter.DateFrom != "" {
		query += ` AND substr(l.lesson_date, 1, 10) >= ?`
		args = append(args, filter.DateFrom)
	}
	if filter.DateTo != "" {
		query += ` AND substr(l.lesson_date, 1, 10) <= ?`
		args = append(args, filter.DateTo)
	}
	if len(filter.TopicIDs) > 0 {
		placeholders := strings.Repeat("?,", len(filter.TopicIDs))
		placeholders = placeholders[:len(placeholders)-1]
		query += ` AND l.id IN (SELECT lesson_id FROM lesson_topics WHERE topic_id IN (` + placeholders + `))`
		for _, topicID := range filter.TopicIDs {
			args = append(args, topicID)
		}
	}
	query += ` ORDER BY l.lesson_date DESC, l.id DESC`

	rows, err := conn.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list lessons with status: %w", err)
	}
	defer rows.Close()

	out := make([]LessonWithStatus, 0)
	for rows.Next() {
		lws, err := scanLessonWithStatusRow(rows)
		if err != nil {
			return nil, fmt.Errorf("read lesson with status: %w", err)
		}
		out = append(out, lws)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate lessons with status: %w", err)
	}
	return out, nil
}

// FindLessonWithStatusByID looks up a lesson by id already with the status
// derived from the jobs (same rules as ListLessonsWithStatus) — used by the
// Detail view (Story 6), which now opens at any status, not just
// "ready" (see services.LibraryService.GetLesson). Returns (nil, nil) if
// the lesson doesn't exist.
func FindLessonWithStatusByID(conn *sql.DB, id int64) (*LessonWithStatus, error) {
	row := conn.QueryRow(`SELECT`+lessonWithStatusColumns+lessonWithStatusFromJoin+` WHERE l.id = ?`, id)
	lws, err := scanLessonWithStatusRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fetch lesson with status by id %d: %w", id, err)
	}
	return &lws, nil
}

// deriveStatus applies the Library's status rules (Story 5): an
// extract_audio error is the root cause and takes priority over the
// transcribe error (which gets blocked when its extract_audio fails — see
// claimNextEligibleJob in internal/jobs/worker.go); "ready" requires
// transcribe to be done, not just extract_audio.
func deriveStatus(extractStatus, extractError, transcribeStatus, transcribeError string) (status string, message string) {
	if extractStatus == "error" {
		return "error", extractError
	}
	if transcribeStatus == "error" {
		return "error", transcribeError
	}
	if transcribeStatus == "done" {
		return "ready", ""
	}
	return "processing", ""
}
