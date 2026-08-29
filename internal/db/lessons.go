package db

import (
	"database/sql"
	"fmt"
	"time"
)

// Lesson is a row from lessons. Besides the data visible to the user
// (LessonDate, TeacherName, DurationSeconds), it carries the identity (path,
// hash) and the stat cache (size/mtime) used by the Story 3 scan
// to decide whether the content needs rehashing. TeacherID is the writable
// FK (INSERT/UPDATE); TeacherName comes from a JOIN with teachers, read-only.
// DurationSeconds is nil until Story 5 stores it (best-effort,
// via ffprobe, on import confirmation) — never blocks anything by being
// nil.
type Lesson struct {
	ID                  int64
	LessonDate          string
	TeacherID           int64
	TeacherName         string
	VideoPath           string
	VideoHash           string
	FileSize            int64
	FileMTime           string
	DurationSeconds     *int64
	StudentSpeakerLabel *string
}

// lessonColumns is the list of columns (in this order) that scanLessonRow expects
// — shared by FindLessonByPath/ByHash/ByID to keep the three
// queries identical in how they read the nullable duration_seconds.
const lessonColumns = `l.id, l.lesson_date, l.teacher_id, t.name, l.video_path, COALESCE(l.video_hash, ''), COALESCE(l.file_size, 0), COALESCE(l.file_mtime, ''), l.duration_seconds, l.student_speaker_label`

const lessonFromJoin = ` FROM lessons l JOIN teachers t ON t.id = l.teacher_id`

// scanLessonRow scans a row selected with lessonColumns.
// Returns (nil, nil) if the row doesn't exist (sql.ErrNoRows) — path/hash/id
// not found is the common case for callers, not an error.
func scanLessonRow(row *sql.Row) (*Lesson, error) {
	var l Lesson
	var duration sql.NullInt64
	var studentSpeaker sql.NullString
	err := row.Scan(&l.ID, &l.LessonDate, &l.TeacherID, &l.TeacherName, &l.VideoPath, &l.VideoHash, &l.FileSize, &l.FileMTime, &duration, &studentSpeaker)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if duration.Valid {
		d := duration.Int64
		l.DurationSeconds = &d
	}
	if studentSpeaker.Valid {
		s := studentSpeaker.String
		l.StudentSpeakerLabel = &s
	}
	return &l, nil
}

// FindLessonByPath looks up the lesson whose video_path is exactly path. Returns
// (nil, nil) if there is none — an already-registered path is the common case, not
// an error.
func FindLessonByPath(conn *sql.DB, path string) (*Lesson, error) {
	row := conn.QueryRow(`SELECT `+lessonColumns+lessonFromJoin+` WHERE l.video_path = ?`, path)
	l, err := scanLessonRow(row)
	if err != nil {
		return nil, fmt.Errorf("buscar lesson por path: %w", err)
	}
	return l, nil
}

// FindLessonByHash looks up the lesson whose video_hash is exactly hash. Returns
// (nil, nil) if there is none.
func FindLessonByHash(conn *sql.DB, hash string) (*Lesson, error) {
	row := conn.QueryRow(`SELECT `+lessonColumns+lessonFromJoin+` WHERE l.video_hash = ?`, hash)
	l, err := scanLessonRow(row)
	if err != nil {
		return nil, fmt.Errorf("buscar lesson por hash: %w", err)
	}
	return l, nil
}

// FindLessonByID looks up the lesson by id. Returns (nil, nil) if there is none.
func FindLessonByID(conn *sql.DB, id int64) (*Lesson, error) {
	row := conn.QueryRow(`SELECT `+lessonColumns+lessonFromJoin+` WHERE l.id = ?`, id)
	l, err := scanLessonRow(row)
	if err != nil {
		return nil, fmt.Errorf("buscar lesson por id: %w", err)
	}
	return l, nil
}

// UpdateLessonPath updates video_path/file_size/file_mtime of an already
// registered lesson — used when the scan finds the same hash at a
// different path (the file was just moved/renamed, not a new lesson).
// file_mtime is the file's mtime on disk; updated_at (the mark of when the
// database row changed) is always "now", never the file's mtime.
func UpdateLessonPath(conn *sql.DB, lessonID int64, path string, size int64, fileMTime string) error {
	_, err := conn.Exec(
		`UPDATE lessons SET video_path = ?, file_size = ?, file_mtime = ?, updated_at = ? WHERE id = ?`,
		path, size, fileMTime, time.Now().UTC().Format(time.RFC3339), lessonID,
	)
	if err != nil {
		return fmt.Errorf("atualizar path da lesson: %w", err)
	}
	return nil
}

// SetLessonDuration stores the video's duration (computed via ffprobe on
// import confirmation, best-effort — see ImportService.ConfirmImport)
// — only called when the probe succeeded.
func SetLessonDuration(conn *sql.DB, lessonID int64, seconds int64) error {
	_, err := conn.Exec(
		`UPDATE lessons SET duration_seconds = ?, updated_at = ? WHERE id = ?`,
		seconds, time.Now().UTC().Format(time.RFC3339), lessonID,
	)
	if err != nil {
		return fmt.Errorf("gravar duração da lesson %d: %w", lessonID, err)
	}
	return nil
}

// SetStudentSpeaker stores which raw speaker (e.g. "speaker_0") is the student
// in this lesson — a choice made via the Detail view's toggle (Story 6).
// Overwrites any previous value, letting the user correct it.
func SetStudentSpeaker(conn *sql.DB, lessonID int64, speakerLabel string) error {
	_, err := conn.Exec(
		`UPDATE lessons SET student_speaker_label = ?, updated_at = ? WHERE id = ?`,
		speakerLabel, time.Now().UTC().Format(time.RFC3339), lessonID,
	)
	if err != nil {
		return fmt.Errorf("gravar student_speaker_label da lesson %d: %w", lessonID, err)
	}
	return nil
}

// UpdateLesson stores the date/time and teacher of an already confirmed lesson —
// post-import editing (Story 9). teacherID must already exist (resolved
// by the caller via GetOrCreateTeacherByName from the combobox's
// free-text name).
func UpdateLesson(conn *sql.DB, lessonID int64, lessonDate string, teacherID int64) error {
	_, err := conn.Exec(
		`UPDATE lessons SET lesson_date = ?, teacher_id = ?, updated_at = ? WHERE id = ?`,
		lessonDate, teacherID, time.Now().UTC().Format(time.RFC3339), lessonID,
	)
	if err != nil {
		return fmt.Errorf("atualizar lesson %d: %w", lessonID, err)
	}
	return nil
}
