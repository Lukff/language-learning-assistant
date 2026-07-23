// services/queue.go
package services

import (
	"database/sql"

	"assistente-idiomas/internal/db"
)

// QueueService expõe a fila de processamento (aulas com pipeline ativo ou
// em erro) pra tela de Fila (História 7).
type QueueService struct {
	conn *sql.DB
}

func NewQueueService(conn *sql.DB) *QueueService {
	return &QueueService{conn: conn}
}

// stageLabel traduz o kind do job pra um rótulo de etapa em PT-BR, exibido
// na Fila.
var stageLabel = map[string]string{
	"extract_audio": "Extração de áudio",
	"transcribe":    "Transcrição",
}

// statusLabel traduz o status bruto do job pro vocabulário já usado na
// Biblioteca (Library.svelte: STATUS_LABEL) — "pending"/"running" viram
// "aguardando"/"processando", "error" vira "erro".
var statusLabel = map[string]string{
	"pending": "aguardando",
	"running": "processando",
	"error":   "erro",
}

// QueueItem é uma entrada da fila, no formato exposto ao frontend.
type QueueItem struct {
	LessonID   int64  `json:"lessonId"`
	LessonDate string `json:"lessonDate"`
	Tutor      string `json:"tutor"`
	Stage      string `json:"stage"`
	Status     string `json:"status"`
	Attempts   int    `json:"attempts"`
	LastError  string `json:"lastError"`
}

// ListQueue lista as aulas com pipeline ativo ou em erro, uma por linha,
// erro primeiro depois FIFO — ver db.ListQueueEntries.
func (s *QueueService) ListQueue() ([]QueueItem, error) {
	entries, err := db.ListQueueEntries(s.conn)
	if err != nil {
		return nil, err
	}
	out := make([]QueueItem, 0, len(entries))
	for _, e := range entries {
		out = append(out, QueueItem{
			LessonID:   e.LessonID,
			LessonDate: e.LessonDate,
			Tutor:      e.Tutor,
			Stage:      stageLabel[e.Kind],
			Status:     statusLabel[e.Status],
			Attempts:   e.Attempts,
			LastError:  e.LastError,
		})
	}
	return out, nil
}

// RetryLesson reseta os jobs com erro da lesson pra "pending" — mesma
// primitiva de dados que LibraryService.RetryLesson usa (db package,
// nenhum serviço depende do outro). Não é erro se a lesson não tiver
// nenhum job em erro no momento.
func (s *QueueService) RetryLesson(lessonID int64) error {
	_, err := db.ResetErrorJobsForLesson(s.conn, lessonID)
	return err
}
