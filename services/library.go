package services

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"assistente-idiomas/internal/db"
)

// LibraryService exposes the already-confirmed lessons for the Library —
// listing with status derived from the jobs and duration (Story 5), filtering by
// teacher/period, reprocessing lessons with errors, fetching a lesson
// for the Detail view and its synced transcript (Story 6), checking the
// presence of the video file in the current storage_root (Story 8), and
// editing the date/time/teacher of an already-confirmed lesson (Story 9).
type LibraryService struct {
	conn        *sql.DB
	storageRoot func() (string, error)
	moveFile    func(string, string) error
}

func NewLibraryService(conn *sql.DB, storageRoot func() (string, error)) *LibraryService {
	return &LibraryService{conn: conn, storageRoot: storageRoot, moveFile: moveFileNoReplace}
}

// Lesson is a confirmed lesson, in the format exposed to the frontend. Status is
// always one of "processing", "ready", "error" (see db.LessonWithStatus);
// ErrorMessage is only filled in when Status == "error". DurationSeconds is
// nil until the duration probe (best effort, on import
// confirmation) succeeds. StudentSpeakerLabel is nil until the user marks
// who the student is in the Detail toggle (Story 6). VideoMissing is
// recomputed on every read (never saved to the database) — true when the
// video_path file is not found in the current storage_root (Story 8:
// folder changed without the video reappearing, or file deleted/moved outside
// the app).
type Lesson struct {
	ID                  int64   `json:"id"`
	LessonDate          string  `json:"lessonDate"`
	TeacherName         string  `json:"tutor"`
	VideoPath           string  `json:"videoPath"`
	DurationSeconds     *int64  `json:"durationSeconds"`
	Status              string  `json:"status"`
	ErrorMessage        string  `json:"errorMessage"`
	StudentSpeakerLabel *string `json:"studentSpeakerLabel"`
	VideoMissing        bool    `json:"videoMissing"`
}

// LessonFilter filters ListLessons — zero-value fields are ignored (no filter
// on that criterion).
type LessonFilter struct {
	TeacherID int64   `json:"teacherId"`
	DateFrom  string  `json:"dateFrom"`
	DateTo    string  `json:"dateTo"`
	TopicIDs  []int64 `json:"topicIds"`
}

// Transcript is a lesson's transcript, in the format exposed to the Detail view
// (Story 6).
type Transcript struct {
	Utterances []Utterance `json:"utterances"`
}

// Utterance is one utterance of the transcript. Timestamps in seconds — the same
// unit as HTMLVideoElement.currentTime in the frontend, converted here at the
// service boundary (the database stores time.Duration).
type Utterance struct {
	Speaker      string  `json:"speaker"`
	Text         string  `json:"text"`
	StartSeconds float64 `json:"startSeconds"`
	EndSeconds   float64 `json:"endSeconds"`
}

// ListLessons lists the confirmed lessons with status/duration, most recent
// first, applying filter.
func (s *LibraryService) ListLessons(filter LessonFilter) ([]Lesson, error) {
	rows, err := db.ListLessonsWithStatus(s.conn, db.LessonFilter{
		TeacherID: filter.TeacherID,
		DateFrom:  filter.DateFrom,
		DateTo:    filter.DateTo,
		TopicIDs:  filter.TopicIDs,
	})
	if err != nil {
		return nil, err
	}
	out := make([]Lesson, 0, len(rows))
	for _, r := range rows {
		out = append(out, Lesson{
			ID:                  r.ID,
			LessonDate:          r.LessonDate,
			TeacherName:         r.TeacherName,
			VideoPath:           r.VideoPath,
			DurationSeconds:     r.DurationSeconds,
			Status:              r.Status,
			ErrorMessage:        r.ErrorMessage,
			StudentSpeakerLabel: r.StudentSpeakerLabel,
			VideoMissing:        s.videoMissing(r.VideoPath),
		})
	}
	return out, nil
}

// RetryLesson resets the lesson's errored jobs back to "pending" — the job
// worker (internal/jobs) resumes the pipeline on its own on the next poll (~5s), with no
// need to explicitly wake it up (same decision as Story 4). It is not an
// error if the lesson has no jobs currently in error.
func (s *LibraryService) RetryLesson(lessonID int64) error {
	_, err := db.ResetErrorJobsForLesson(s.conn, lessonID)
	return err
}

// GetLesson fetches a lesson by id, with status/error derived from the jobs, for the
// Detail view (Story 6) — which now opens in any status: "processing"
// and "error" show the video without a transcript (see LessonDetail.svelte),
// "ready" enables GetTranscript.
func (s *LibraryService) GetLesson(id int64) (Lesson, error) {
	lws, err := db.FindLessonWithStatusByID(s.conn, id)
	if err != nil {
		return Lesson{}, err
	}
	if lws == nil {
		return Lesson{}, fmt.Errorf("lesson %d not found", id)
	}
	return Lesson{
		ID:                  lws.ID,
		LessonDate:          lws.LessonDate,
		TeacherName:         lws.TeacherName,
		VideoPath:           lws.VideoPath,
		DurationSeconds:     lws.DurationSeconds,
		Status:              lws.Status,
		ErrorMessage:        lws.ErrorMessage,
		StudentSpeakerLabel: lws.StudentSpeakerLabel,
		VideoMissing:        s.videoMissing(lws.VideoPath),
	}, nil
}

// GetTranscript fetches a lesson's transcript for the Detail view (Story 6).
// Should only be called once GetLesson has already returned Status == "ready" — the
// Detail view doesn't call this for lessons processing/erroring, which show the status
// in place of the transcript panel.
func (s *LibraryService) GetTranscript(lessonID int64) (Transcript, error) {
	t, err := db.FindTranscriptByLessonID(s.conn, lessonID)
	if err != nil {
		return Transcript{}, err
	}
	if t == nil {
		return Transcript{}, fmt.Errorf("lesson %d has no transcript yet", lessonID)
	}
	out := Transcript{Utterances: make([]Utterance, 0, len(t.Utterances))}
	for _, u := range t.Utterances {
		out.Utterances = append(out.Utterances, Utterance{
			Speaker:      u.Speaker,
			Text:         u.Text,
			StartSeconds: u.Start.Seconds(),
			EndSeconds:   u.End.Seconds(),
		})
	}
	return out, nil
}

// SetStudentSpeaker records which raw speaker (e.g. "speaker_0") is the student
// in this lesson — a choice that lives inside EditLessonModal (Phase 2,
// Story 2; previously a standalone toggle in the Detail view, Phase 1/Story 6). Changing
// an already-set label to a different one discards any analysis already done for the
// lesson: any result anchored to utterance_index would then point
// to the wrong role as soon as the Student/Tutor labels shift to a different speaker —
// changing it again requires reprocessing. Setting the label for the first time
// (StudentSpeakerLabel still nil) or repeating the current label discards
// nothing.
func (s *LibraryService) SetStudentSpeaker(lessonID int64, speakerLabel string) error {
	lesson, err := db.FindLessonByID(s.conn, lessonID)
	if err != nil {
		return fmt.Errorf("fetch lesson %d: %w", lessonID, err)
	}
	if lesson == nil {
		return fmt.Errorf("lesson %d not found", lessonID)
	}
	if lesson.StudentSpeakerLabel != nil && *lesson.StudentSpeakerLabel != speakerLabel {
		if err := db.DeleteSpeakerDependentAnalysisResults(s.conn, lessonID); err != nil {
			return fmt.Errorf("discard old analyses for lesson %d: %w", lessonID, err)
		}
	}
	return db.SetStudentSpeaker(s.conn, lessonID, speakerLabel)
}

// UpdateLesson saves the date/time and teacher of an already-confirmed lesson
// (Story 9) — the teacher can be an already-registered name or a
// new one (same combobox as the import form). After saving,
// it tries to rename the video to the current standardized name (best effort — it doesn't
// fail the edit if the rename isn't possible, same principle as
// import confirmation).
func (s *LibraryService) UpdateLesson(lessonID int64, lessonDate string, teacherName string) error {
	if lessonDate == "" {
		return fmt.Errorf("lesson date cannot be empty")
	}
	if teacherName == "" {
		return fmt.Errorf("teacher cannot be empty")
	}
	if !hasTimeComponent(lessonDate) {
		return fmt.Errorf("lesson time is required")
	}
	const lessonDateLayout = "2006-01-02T15:04"
	parsedLessonDate, err := time.Parse(lessonDateLayout, lessonDate)
	if err != nil || parsedLessonDate.Format(lessonDateLayout) != lessonDate {
		return fmt.Errorf("lesson date and time must be in YYYY-MM-DDTHH:MM format")
	}

	teacherID, err := db.GetOrCreateTeacherByName(s.conn, teacherName)
	if err != nil {
		return err
	}
	if err := db.UpdateLesson(s.conn, lessonID, lessonDate, teacherID); err != nil {
		return err
	}
	renameVideoBestEffort(s.conn, s.moveFile, lessonID)
	return nil
}

// videoMissing indicates whether a lesson's video file is not found
// in the current storage_root. Any os.Stat error (not just "doesn't exist") is
// treated as missing — resilience: never lets the Library break because
// of this, and it isn't worth distinguishing "missing" from "no permission"
// in this slice (Story 8).
func (s *LibraryService) videoMissing(videoPath string) bool {
	root, err := s.storageRoot()
	if err != nil {
		return true
	}
	_, err = os.Stat(filepath.Join(root, filepath.FromSlash(videoPath)))
	return err != nil
}
