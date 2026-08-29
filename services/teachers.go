// services/teachers.go
package services

import (
	"database/sql"

	"assistente-idiomas/internal/db"
)

// TeacherService covers teacher management as an entity (Story 9):
// listing for the import/edit combobox and for the Settings panel, and
// renaming a teacher — reflects across all their lessons via JOIN, without
// touching each lesson individually.
type TeacherService struct {
	conn *sql.DB
}

func NewTeacherService(conn *sql.DB) *TeacherService {
	return &TeacherService{conn: conn}
}

// Teacher is a registered teacher, in the format exposed to the frontend.
type Teacher struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// ListTeachers lists the registered teachers in alphabetical order.
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

// RenameTeacher renames the teacher id. A collision with an already-registered name
// (UNIQUE on teachers.name) comes back as a readable error — no automatic
// merging of teachers, a decision from Story 9.
func (s *TeacherService) RenameTeacher(id int64, newName string) error {
	return db.RenameTeacher(s.conn, id, newName)
}
