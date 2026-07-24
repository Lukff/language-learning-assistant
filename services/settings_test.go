package services

import (
	"os"
	"path/filepath"
	"testing"

	"assistente-idiomas/internal/config"
	"assistente-idiomas/internal/db"
	"assistente-idiomas/internal/importer"
)

// configStorageRoot resolve storage_root a partir de config.Load() — mesmo
// closure que main.go monta pro worker/middleware/serviços reais.
func configStorageRoot() (string, error) {
	cfg, err := config.Load()
	if err != nil {
		return "", err
	}
	return cfg.StorageRoot, nil
}

func TestSettingsService_ChangeStorageFolder_UpdatesConfigAndReconcilesRenamedVideo(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	oldRoot := t.TempDir()
	if err := config.Save(&config.AppConfig{StorageRoot: oldRoot}); err != nil {
		t.Fatalf("config.Save() falhou: %v", err)
	}

	// Pasta nova já tem o vídeo, mas com outro nome — simula o usuário tendo
	// renomeado o arquivo fora do app antes de trocar a pasta aqui.
	newRoot := t.TempDir()
	renamedPath := filepath.Join(newRoot, "aula-renomeada.mp4")
	if err := os.WriteFile(renamedPath, []byte("conteudo-fake-do-video"), 0o644); err != nil {
		t.Fatalf("escrever vídeo de fixture falhou: %v", err)
	}
	hash, err := importer.HashFile(renamedPath)
	if err != nil {
		t.Fatalf("HashFile() falhou: %v", err)
	}

	lessonID := mustInsertLesson(t, conn, "2026-07-20", "Sarah M.", "aula-antiga.mp4")
	if _, err := conn.Exec(`UPDATE lessons SET video_hash = ? WHERE id = ?`, hash, lessonID); err != nil {
		t.Fatalf("gravar video_hash de fixture falhou: %v", err)
	}

	svc := NewSettingsService(conn, configStorageRoot)
	sum, err := svc.ChangeStorageFolder(newRoot)
	if err != nil {
		t.Fatalf("ChangeStorageFolder() erro inesperado: %v", err)
	}
	if sum.Updated != 1 {
		t.Errorf("ChangeStorageFolder() sum = %+v, esperado 1 lesson atualizada por hash", sum)
	}

	got, err := svc.GetStorageRoot()
	if err != nil {
		t.Fatalf("GetStorageRoot() erro inesperado: %v", err)
	}
	if got != newRoot {
		t.Errorf("GetStorageRoot() = %q, esperado %q", got, newRoot)
	}

	lesson, err := db.FindLessonByID(conn, lessonID)
	if err != nil {
		t.Fatalf("FindLessonByID() erro inesperado: %v", err)
	}
	if lesson.VideoPath != "aula-renomeada.mp4" {
		t.Errorf("lesson.VideoPath = %q, esperado video_path atualizado pro nome novo", lesson.VideoPath)
	}
}

func TestSettingsService_ChangeStorageFolder_ReadOnlyDirRejectedConfigUnchanged(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	oldRoot := t.TempDir()
	if err := config.Save(&config.AppConfig{StorageRoot: oldRoot}); err != nil {
		t.Fatalf("config.Save() falhou: %v", err)
	}

	readOnly := t.TempDir()
	if err := os.Chmod(readOnly, 0o500); err != nil {
		t.Fatalf("não foi possível preparar dir somente-leitura: %v", err)
	}
	t.Cleanup(func() { os.Chmod(readOnly, 0o700) })

	svc := NewSettingsService(conn, configStorageRoot)
	if _, err := svc.ChangeStorageFolder(readOnly); err == nil {
		t.Fatal("ChangeStorageFolder() esperava erro numa pasta somente-leitura, veio nil")
	}

	got, err := svc.GetStorageRoot()
	if err != nil {
		t.Fatalf("GetStorageRoot() erro inesperado: %v", err)
	}
	if got != oldRoot {
		t.Errorf("GetStorageRoot() = %q, esperado manter %q após falha", got, oldRoot)
	}
}

func TestSettingsService_ChangeStorageFolder_EmptyRejected(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	svc := NewSettingsService(conn, configStorageRoot)
	if _, err := svc.ChangeStorageFolder(""); err == nil {
		t.Fatal("ChangeStorageFolder() esperava erro pra pasta vazia, veio nil")
	}
}
