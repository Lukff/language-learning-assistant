package services

import (
	"os"
	"path/filepath"
	"testing"

	"assistente-idiomas/internal/config"
	"assistente-idiomas/internal/db"
)

func TestImportService_ScanFolderThenListThenConfirm(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	storageRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(storageRoot, "aula-2026-07-15.mp4"), []byte("conteudo"), 0o644); err != nil {
		t.Fatalf("preparar vídeo de fixture falhou: %v", err)
	}
	if err := config.Save(&config.AppConfig{StorageRoot: storageRoot}); err != nil {
		t.Fatalf("config.Save() falhou: %v", err)
	}

	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	svc := NewImportService(conn)

	sum, err := svc.ScanFolder()
	if err != nil {
		t.Fatalf("ScanFolder() erro inesperado: %v", err)
	}
	if sum.New != 1 {
		t.Fatalf("ScanFolder() Summary = %+v, esperado New=1", sum)
	}

	pending, err := svc.ListPendingImports()
	if err != nil {
		t.Fatalf("ListPendingImports() erro inesperado: %v", err)
	}
	if len(pending) != 1 || pending[0].Path != "aula-2026-07-15.mp4" || pending[0].SuggestedDate != "2026-07-15T00:00" {
		t.Fatalf("ListPendingImports() = %+v, esperado 1 item aula-2026-07-15.mp4/2026-07-15T00:00", pending)
	}

	if err := svc.ConfirmImport(pending[0].ID, "2026-07-15T00:00", "Sarah M."); err != nil {
		t.Fatalf("ConfirmImport() erro inesperado: %v", err)
	}

	pending, err = svc.ListPendingImports()
	if err != nil {
		t.Fatalf("ListPendingImports() erro inesperado: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("ListPendingImports() após confirmar = %+v, esperado vazio", pending)
	}

	var count int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM lessons WHERE tutor = ?`, "Sarah M.").Scan(&count); err != nil {
		t.Fatalf("count de lessons falhou: %v", err)
	}
	if count != 1 {
		t.Errorf("lessons com tutor Sarah M. = %d, esperado 1", count)
	}
}

func TestImportService_ScanFolderTwiceDoesNotDuplicateCandidate(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	storageRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(storageRoot, "aula.mp4"), []byte("conteudo-estavel"), 0o644); err != nil {
		t.Fatalf("preparar vídeo de fixture falhou: %v", err)
	}
	if err := config.Save(&config.AppConfig{StorageRoot: storageRoot}); err != nil {
		t.Fatalf("config.Save() falhou: %v", err)
	}

	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	svc := NewImportService(conn)

	if _, err := svc.ScanFolder(); err != nil {
		t.Fatalf("primeira ScanFolder() erro inesperado: %v", err)
	}
	sum, err := svc.ScanFolder()
	if err != nil {
		t.Fatalf("segunda ScanFolder() erro inesperado: %v", err)
	}
	if sum.New != 0 || sum.Skipped != 1 {
		t.Errorf("segunda ScanFolder() Summary = %+v, esperado {Skipped:1}", sum)
	}

	pending, err := svc.ListPendingImports()
	if err != nil {
		t.Fatalf("ListPendingImports() erro inesperado: %v", err)
	}
	if len(pending) != 1 {
		t.Errorf("ListPendingImports() = %+v, esperado ainda 1 candidato (não duplicado)", pending)
	}
}

func TestImportService_ConfirmImport_RejectsEmptyTutorOrDate(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()
	svc := NewImportService(conn)

	if err := svc.ConfirmImport(1, "", "Sarah M."); err == nil {
		t.Error("ConfirmImport() com data vazia esperava erro, veio nil")
	}
	if err := svc.ConfirmImport(1, "2026-07-15", ""); err == nil {
		t.Error("ConfirmImport() com tutor vazio esperava erro, veio nil")
	}
}
