// internal/db/teachers_test.go
package db

import (
	"path/filepath"
	"testing"
)

func TestOpen_TeachersTableExistsAndLessonsHasTeacherID(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	var name string
	if err := conn.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='teachers'`).Scan(&name); err != nil {
		t.Errorf("tabela teachers não encontrada: %v", err)
	}

	teacherID, err := GetOrCreateTeacherByName(conn, "Sarah M.")
	if err != nil {
		t.Fatalf("GetOrCreateTeacherByName() erro inesperado: %v", err)
	}
	if _, err := conn.Exec(
		`INSERT INTO lessons (lesson_date, teacher_id, video_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"2026-07-21", teacherID, "aulas/2026/x.mp4", "2026-07-21T10:00:00Z", "2026-07-21T10:00:00Z",
	); err != nil {
		t.Fatalf("insert em lessons com teacher_id falhou: %v", err)
	}

	var gotTeacherID int64
	if err := conn.QueryRow(`SELECT teacher_id FROM lessons WHERE video_path = ?`, "aulas/2026/x.mp4").Scan(&gotTeacherID); err != nil {
		t.Fatalf("select em lessons falhou: %v", err)
	}
	if gotTeacherID != teacherID {
		t.Errorf("teacher_id = %d, esperado %d", gotTeacherID, teacherID)
	}
}

func TestGetOrCreateTeacherByName_ReturnsSameIDOnSecondCall(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	id1, err := GetOrCreateTeacherByName(conn, "Sarah M.")
	if err != nil {
		t.Fatalf("GetOrCreateTeacherByName() erro inesperado: %v", err)
	}
	id2, err := GetOrCreateTeacherByName(conn, "Sarah M.")
	if err != nil {
		t.Fatalf("segunda GetOrCreateTeacherByName() erro inesperado: %v", err)
	}
	if id1 != id2 {
		t.Errorf("id2 = %d, esperado igual a id1 = %d (mesmo nome não deveria duplicar)", id2, id1)
	}
}

func TestListTeachers_ReturnsDistinctSortedByName(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	if _, err := GetOrCreateTeacherByName(conn, "Sarah M."); err != nil {
		t.Fatalf("GetOrCreateTeacherByName() erro inesperado: %v", err)
	}
	if _, err := GetOrCreateTeacherByName(conn, "James K."); err != nil {
		t.Fatalf("GetOrCreateTeacherByName() erro inesperado: %v", err)
	}

	teachers, err := ListTeachers(conn)
	if err != nil {
		t.Fatalf("ListTeachers() erro inesperado: %v", err)
	}
	if len(teachers) != 2 || teachers[0].Name != "James K." || teachers[1].Name != "Sarah M." {
		t.Errorf("ListTeachers() = %+v, esperado [James K. Sarah M.] (ordem alfabética)", teachers)
	}
}

func TestRenameTeacher_UpdatesNameAndReflectsOnLessons(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	teacherID, err := GetOrCreateTeacherByName(conn, "Sarah M.")
	if err != nil {
		t.Fatalf("GetOrCreateTeacherByName() erro inesperado: %v", err)
	}
	if _, err := conn.Exec(
		`INSERT INTO lessons (lesson_date, teacher_id, video_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"2026-07-21", teacherID, "aula.mp4", "2026-07-21T10:00:00Z", "2026-07-21T10:00:00Z",
	); err != nil {
		t.Fatalf("insert de lesson falhou: %v", err)
	}

	if err := RenameTeacher(conn, teacherID, "Sarah Miller"); err != nil {
		t.Fatalf("RenameTeacher() erro inesperado: %v", err)
	}

	lesson, err := FindLessonByPath(conn, "aula.mp4")
	if err != nil {
		t.Fatalf("FindLessonByPath() erro inesperado: %v", err)
	}
	if lesson == nil || lesson.TeacherName != "Sarah Miller" {
		t.Errorf("lesson.TeacherName = %+v, esperado Sarah Miller refletido pelo join", lesson)
	}
}

func TestRenameTeacher_CollidingNameReturnsFriendlyError(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	if _, err := GetOrCreateTeacherByName(conn, "Sarah M."); err != nil {
		t.Fatalf("GetOrCreateTeacherByName() erro inesperado: %v", err)
	}
	jamesID, err := GetOrCreateTeacherByName(conn, "James K.")
	if err != nil {
		t.Fatalf("GetOrCreateTeacherByName() erro inesperado: %v", err)
	}

	err = RenameTeacher(conn, jamesID, "Sarah M.")
	if err == nil || err.Error() != "a teacher with this name already exists" {
		t.Errorf("RenameTeacher() colidindo = %v, esperado \"a teacher with this name already exists\"", err)
	}
}
