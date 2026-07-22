package services

import (
	"path/filepath"
	"testing"

	"assistente-idiomas/internal/db"
)

func TestLibraryService_ListLessons_ReturnsConfirmedLessons(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	if err := db.InsertPendingImport(conn, db.PendingImport{
		Path: "aula.mp4", FileSize: 100, FileMTime: "2026-07-20T10:00:00Z",
		SHA256: "hash-1", SuggestedDate: "2026-07-20",
	}); err != nil {
		t.Fatalf("InsertPendingImport() falhou: %v", err)
	}
	pending, err := db.ListPendingImports(conn)
	if err != nil || len(pending) != 1 {
		t.Fatalf("setup: ListPendingImports() = %+v, %v", pending, err)
	}
	if _, err := db.ConfirmPendingImport(conn, pending[0].ID, "2026-07-20", "Sarah M."); err != nil {
		t.Fatalf("ConfirmPendingImport() falhou: %v", err)
	}

	svc := NewLibraryService(conn)
	lessons, err := svc.ListLessons()
	if err != nil {
		t.Fatalf("ListLessons() erro inesperado: %v", err)
	}
	if len(lessons) != 1 {
		t.Fatalf("ListLessons() = %+v, esperado 1 aula", lessons)
	}
	if lessons[0].LessonDate != "2026-07-20" || lessons[0].Tutor != "Sarah M." || lessons[0].VideoPath != "aula.mp4" {
		t.Errorf("ListLessons()[0] = %+v, esperado data/tutor/path da confirmação", lessons[0])
	}
}

func TestLibraryService_ListLessons_EmptyReturnsEmptySlice(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	svc := NewLibraryService(conn)
	lessons, err := svc.ListLessons()
	if err != nil {
		t.Fatalf("ListLessons() erro inesperado: %v", err)
	}
	if len(lessons) != 0 {
		t.Errorf("ListLessons() = %+v, esperado vazio", lessons)
	}
}
