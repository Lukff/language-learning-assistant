// services/queue.go
package services

import (
	"database/sql"

	"assistente-idiomas/internal/db"
)

// QueueService exposes the processing queue (lessons with an active or
// errored pipeline) for the Queue screen (Story 7).
type QueueService struct {
	conn *sql.DB
}

func NewQueueService(conn *sql.DB) *QueueService {
	return &QueueService{conn: conn}
}

// stageLabel translates the job kind into a PT-BR stage label, displayed
// in the Queue.
var stageLabel = map[string]string{
	"extract_audio": "Extração de áudio",
	"transcribe":    "Transcrição",
}

// statusLabel translates the raw job status into the vocabulary already used in the
// Library (Library.svelte: STATUS_LABEL) — "pending"/"running" become
// "aguardando"/"processando", "error" becomes "erro".
var statusLabel = map[string]string{
	"pending": "aguardando",
	"running": "processando",
	"error":   "erro",
}

// QueueItem is a queue entry, in the format exposed to the frontend.
type QueueItem struct {
	LessonID    int64  `json:"lessonId"`
	LessonDate  string `json:"lessonDate"`
	TeacherName string `json:"tutor"`
	Stage       string `json:"stage"`
	Status      string `json:"status"`
	Attempts    int    `json:"attempts"`
	LastError   string `json:"lastError"`
}

// ListQueue lists the lessons with an active or errored pipeline, one per row,
// errors first then FIFO — see db.ListQueueEntries.
func (s *QueueService) ListQueue() ([]QueueItem, error) {
	entries, err := db.ListQueueEntries(s.conn)
	if err != nil {
		return nil, err
	}
	out := make([]QueueItem, 0, len(entries))
	for _, e := range entries {
		out = append(out, QueueItem{
			LessonID:    e.LessonID,
			LessonDate:  e.LessonDate,
			TeacherName: e.TeacherName,
			Stage:       stageLabel[e.Kind],
			Status:      statusLabel[e.Status],
			Attempts:    e.Attempts,
			LastError:   e.LastError,
		})
	}
	return out, nil
}

// RetryLesson resets the lesson's errored jobs back to "pending" — the same
// data primitive that LibraryService.RetryLesson uses (db package,
// neither service depends on the other). It is not an error if the lesson has
// no jobs currently in error.
func (s *QueueService) RetryLesson(lessonID int64) error {
	_, err := db.ResetErrorJobsForLesson(s.conn, lessonID)
	return err
}
