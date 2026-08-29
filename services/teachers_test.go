package services

import (
	"path/filepath"
	"testing"

	"assistente-idiomas/internal/db"
)

func TestTeacherService_ListTeachers_ReturnsAlphabeticalOrder(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	if _, err := db.GetOrCreateTeacherByName(conn, "Sarah M."); err != nil {
		t.Fatalf("GetOrCreateTeacherByName() erro inesperado: %v", err)
	}
	if _, err := db.GetOrCreateTeacherByName(conn, "James K."); err != nil {
		t.Fatalf("GetOrCreateTeacherByName() erro inesperado: %v", err)
	}

	svc := NewTeacherService(conn)
	teachers, err := svc.ListTeachers()
	if err != nil {
		t.Fatalf("ListTeachers() erro inesperado: %v", err)
	}
	if len(teachers) != 2 || teachers[0].Name != "James K." || teachers[1].Name != "Sarah M." {
		t.Errorf("ListTeachers() = %+v, esperado [James K. Sarah M.]", teachers)
	}
}

func TestTeacherService_RenameTeacher_RejectsCollidingName(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	if _, err := db.GetOrCreateTeacherByName(conn, "Sarah M."); err != nil {
		t.Fatalf("GetOrCreateTeacherByName() erro inesperado: %v", err)
	}
	jamesID, err := db.GetOrCreateTeacherByName(conn, "James K.")
	if err != nil {
		t.Fatalf("GetOrCreateTeacherByName() erro inesperado: %v", err)
	}

	svc := NewTeacherService(conn)
	err = svc.RenameTeacher(jamesID, "Sarah M.")
	if err == nil || err.Error() != "a teacher with this name already exists" {
		t.Errorf("RenameTeacher() colidindo = %v, esperado erro de nome já existente", err)
	}
}
