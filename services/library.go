package services

import (
	"database/sql"
	"fmt"

	"assistente-idiomas/internal/db"
)

// LibraryService expõe as aulas já confirmadas para a Biblioteca —
// listagem com status derivado dos jobs e duração (História 5), filtro por
// tutor/período, reprocessamento de aulas com erro e busca de uma aula pro
// Detalhe.
type LibraryService struct {
	conn *sql.DB
}

func NewLibraryService(conn *sql.DB) *LibraryService {
	return &LibraryService{conn: conn}
}

// Lesson é uma aula confirmada, no formato exposto ao frontend. Status é
// sempre um de "processando", "pronta", "erro" (ver db.LessonWithStatus);
// ErrorMessage só é preenchido quando Status == "erro". DurationSeconds é
// nil até o probe de duração (melhor esforço, na confirmação da
// importação) ter sucesso.
type Lesson struct {
	ID              int64  `json:"id"`
	LessonDate      string `json:"lessonDate"`
	Tutor           string `json:"tutor"`
	VideoPath       string `json:"videoPath"`
	DurationSeconds *int64 `json:"durationSeconds"`
	Status          string `json:"status"`
	ErrorMessage    string `json:"errorMessage"`
}

// LessonFilter filtra ListLessons — campos vazios são ignorados (sem
// filtro naquele critério).
type LessonFilter struct {
	Tutor    string `json:"tutor"`
	DateFrom string `json:"dateFrom"`
	DateTo   string `json:"dateTo"`
}

// ListLessons lista as aulas confirmadas com status/duração, mais recentes
// primeiro, aplicando filter.
func (s *LibraryService) ListLessons(filter LessonFilter) ([]Lesson, error) {
	rows, err := db.ListLessonsWithStatus(s.conn, db.LessonFilter{
		Tutor:    filter.Tutor,
		DateFrom: filter.DateFrom,
		DateTo:   filter.DateTo,
	})
	if err != nil {
		return nil, err
	}
	out := make([]Lesson, 0, len(rows))
	for _, r := range rows {
		out = append(out, Lesson{
			ID:              r.ID,
			LessonDate:      r.LessonDate,
			Tutor:           r.Tutor,
			VideoPath:       r.VideoPath,
			DurationSeconds: r.DurationSeconds,
			Status:          r.Status,
			ErrorMessage:    r.ErrorMessage,
		})
	}
	return out, nil
}

// ListTutors lista os tutores distintos já registrados, pro dropdown de
// filtro da Biblioteca.
func (s *LibraryService) ListTutors() ([]string, error) {
	return db.ListTutors(s.conn)
}

// RetryLesson reseta os jobs com erro da lesson pra "pending" — o worker de
// jobs (internal/jobs) retoma o pipeline sozinho no próximo poll (~5s), sem
// precisar acordá-lo explicitamente (mesma decisão da História 4). Não é
// erro se a lesson não tiver nenhum job em erro no momento.
func (s *LibraryService) RetryLesson(lessonID int64) error {
	_, err := db.ResetErrorJobsForLesson(s.conn, lessonID)
	return err
}

// GetLesson busca uma aula confirmada por id, pro Detalhe (História 5/6).
// Diferente de ListLessons, não calcula Status/ErrorMessage — o Detalhe só
// é aberto a partir de uma aula já "pronta" na Biblioteca (ver
// Library.svelte), então recalcular o status aqui seria trabalho sem uso.
func (s *LibraryService) GetLesson(id int64) (Lesson, error) {
	lesson, err := db.FindLessonByID(s.conn, id)
	if err != nil {
		return Lesson{}, err
	}
	if lesson == nil {
		return Lesson{}, fmt.Errorf("aula %d não encontrada", id)
	}
	return Lesson{
		ID:              lesson.ID,
		LessonDate:      lesson.LessonDate,
		Tutor:           lesson.Tutor,
		VideoPath:       lesson.VideoPath,
		DurationSeconds: lesson.DurationSeconds,
	}, nil
}
