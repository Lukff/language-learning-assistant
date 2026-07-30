// services/teachers.go
package services

import (
	"database/sql"

	"assistente-idiomas/internal/db"
)

// TeacherService cobre a gestão de professores como entidade (História 9):
// listar pro combobox de importação/edição e pro painel de Configurações, e
// renomear um professor — reflete em todas as aulas dele via JOIN, sem
// tocar em cada lesson individualmente.
type TeacherService struct {
	conn *sql.DB
}

func NewTeacherService(conn *sql.DB) *TeacherService {
	return &TeacherService{conn: conn}
}

// Teacher é um professor cadastrado, no formato exposto ao frontend.
type Teacher struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// ListTeachers lista os professores cadastrados em ordem alfabética.
func (s *TeacherService) ListTeachers() ([]Teacher, error) {
	rows, err := db.ListTeachers(s.conn)
	if err != nil {
		return nil, err
	}
	out := make([]Teacher, 0, len(rows))
	for _, r := range rows {
		out = append(out, Teacher{ID: r.ID, Name: r.Name})
	}
	return out, nil
}

// RenameTeacher renomeia o professor id. Colisão com um nome já cadastrado
// (UNIQUE em teachers.name) volta como erro legível — sem merge automático
// de professores, decisão da História 9.
func (s *TeacherService) RenameTeacher(id int64, newName string) error {
	return db.RenameTeacher(s.conn, id, newName)
}
