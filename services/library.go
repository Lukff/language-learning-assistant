package services

import (
	"database/sql"

	"assistente-idiomas/internal/db"
)

// LibraryService expõe as aulas já confirmadas para a Biblioteca. Nesta
// fatia (História 3) é só a lista crua — data, tutor, path; status,
// duração e filtro ficam para a História 5.
type LibraryService struct {
	conn *sql.DB
}

func NewLibraryService(conn *sql.DB) *LibraryService {
	return &LibraryService{conn: conn}
}

// Lesson é uma aula confirmada, no formato exposto ao frontend.
type Lesson struct {
	ID         int64  `json:"id"`
	LessonDate string `json:"lessonDate"`
	Tutor      string `json:"tutor"`
	VideoPath  string `json:"videoPath"`
}

// ListLessons lista as aulas confirmadas, mais recentes primeiro.
func (s *LibraryService) ListLessons() ([]Lesson, error) {
	rows, err := db.ListLessons(s.conn)
	if err != nil {
		return nil, err
	}
	out := make([]Lesson, 0, len(rows))
	for _, r := range rows {
		out = append(out, Lesson{ID: r.ID, LessonDate: r.LessonDate, Tutor: r.Tutor, VideoPath: r.VideoPath})
	}
	return out, nil
}
