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

func TestImportService_ConfirmImport_RenamesVideoToStandardFilename(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	storageRoot := t.TempDir()
	originalName := "cambly-download-xyz.mp4"
	if err := os.WriteFile(filepath.Join(storageRoot, originalName), []byte("conteudo-de-video"), 0o644); err != nil {
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
		t.Fatalf("ScanFolder() erro inesperado: %v", err)
	}
	pending, err := svc.ListPendingImports()
	if err != nil || len(pending) != 1 {
		t.Fatalf("setup: ListPendingImports() = %+v, %v", pending, err)
	}

	if err := svc.ConfirmImport(pending[0].ID, "2026-07-23T14:30", "Maria José"); err != nil {
		t.Fatalf("ConfirmImport() erro inesperado: %v", err)
	}

	wantPath := "2026-07-23_14H30_maria-jose.mp4"
	lesson, err := db.FindLessonByPath(conn, wantPath)
	if err != nil {
		t.Fatalf("FindLessonByPath() erro inesperado: %v", err)
	}
	if lesson == nil {
		t.Fatalf("lesson não encontrada no path padronizado %q — video_path não foi atualizado", wantPath)
	}

	if _, err := os.Stat(filepath.Join(storageRoot, wantPath)); err != nil {
		t.Errorf("arquivo renomeado não existe no disco em %q: %v", wantPath, err)
	}
	if _, err := os.Stat(filepath.Join(storageRoot, originalName)); !os.IsNotExist(err) {
		t.Errorf("arquivo original %q ainda existe no disco após rename (err=%v)", originalName, err)
	}
}

func TestImportService_ConfirmImport_ResolvesFilenameCollisionWithSuffix(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	storageRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(storageRoot, "a.mp4"), []byte("conteudo-a"), 0o644); err != nil {
		t.Fatalf("preparar vídeo a.mp4 falhou: %v", err)
	}
	if err := os.WriteFile(filepath.Join(storageRoot, "b.mp4"), []byte("conteudo-b-bem-diferente"), 0o644); err != nil {
		t.Fatalf("preparar vídeo b.mp4 falhou: %v", err)
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
		t.Fatalf("ScanFolder() erro inesperado: %v", err)
	}
	pending, err := svc.ListPendingImports()
	if err != nil || len(pending) != 2 {
		t.Fatalf("setup: ListPendingImports() = %+v, %v", pending, err)
	}

	var idA, idB int64
	for _, p := range pending {
		switch p.Path {
		case "a.mp4":
			idA = p.ID
		case "b.mp4":
			idB = p.ID
		}
	}
	if idA == 0 || idB == 0 {
		t.Fatalf("não achei os dois candidatos esperados (a.mp4/b.mp4) em %+v", pending)
	}

	// Mesma data/horário/tutor pras duas aulas — mesmo nome-alvo, força colisão.
	if err := svc.ConfirmImport(idA, "2026-07-23T14:30", "Maria José"); err != nil {
		t.Fatalf("ConfirmImport(a) erro inesperado: %v", err)
	}
	if err := svc.ConfirmImport(idB, "2026-07-23T14:30", "Maria José"); err != nil {
		t.Fatalf("ConfirmImport(b) erro inesperado: %v", err)
	}

	if _, err := os.Stat(filepath.Join(storageRoot, "2026-07-23_14H30_maria-jose.mp4")); err != nil {
		t.Errorf("primeira aula deveria ter o nome base, sem sufixo: %v", err)
	}
	if _, err := os.Stat(filepath.Join(storageRoot, "2026-07-23_14H30_maria-jose-2.mp4")); err != nil {
		t.Errorf("segunda aula deveria ter o sufixo -2: %v", err)
	}
}

func TestRenameCandidateAvailable_DistinguishesCurrentFileFromCollision(t *testing.T) {
	dir := t.TempDir()
	currentPath := filepath.Join(dir, "video.MP4")
	if err := os.WriteFile(currentPath, []byte("video atual"), 0o644); err != nil {
		t.Fatalf("preparar vídeo atual falhou: %v", err)
	}
	currentInfo, err := os.Stat(currentPath)
	if err != nil {
		t.Fatalf("os.Stat() do vídeo atual falhou: %v", err)
	}

	collisionPath := filepath.Join(dir, "outro.mp4")
	if err := os.WriteFile(collisionPath, []byte("outro vídeo"), 0o644); err != nil {
		t.Fatalf("preparar colisão falhou: %v", err)
	}

	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "mesmo arquivo", path: currentPath, want: true},
		{name: "nome livre", path: filepath.Join(dir, "livre.mp4"), want: true},
		{name: "outro arquivo", path: collisionPath, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := renameCandidateAvailable(currentInfo, tt.path)
			if err != nil {
				t.Fatalf("renameCandidateAvailable() falhou: %v", err)
			}
			if got != tt.want {
				t.Errorf("renameCandidateAvailable() = %v, esperado %v", got, tt.want)
			}
		})
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
	if err := svc.ConfirmImport(1, "2026-07-15", ""); err == nil || err.Error() != "tutor não pode ser vazio" {
		t.Errorf("ConfirmImport() com tutor vazio = %v, esperado tutor não pode ser vazio", err)
	}
}

func TestImportService_ConfirmImport_RejectsDateWithoutTime(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	storageRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(storageRoot, "aula.mp4"), []byte("conteudo"), 0o644); err != nil {
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
		t.Fatalf("ScanFolder() erro inesperado: %v", err)
	}
	pending, err := svc.ListPendingImports()
	if err != nil || len(pending) != 1 {
		t.Fatalf("setup: ListPendingImports() = %+v, %v", pending, err)
	}

	if err := svc.ConfirmImport(pending[0].ID, "2026-07-15", "Sarah M."); err == nil || err.Error() != "horário da aula é obrigatório" {
		t.Errorf("ConfirmImport() com data sem horário = %v, esperado horário da aula é obrigatório", err)
	}

	pending, err = svc.ListPendingImports()
	if err != nil {
		t.Fatalf("ListPendingImports() erro inesperado: %v", err)
	}
	if len(pending) != 1 {
		t.Errorf("candidato deveria continuar pendente após confirmação recusada, ListPendingImports() = %+v", pending)
	}

	var count int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM lessons`).Scan(&count); err != nil {
		t.Fatalf("count de lessons falhou: %v", err)
	}
	if count != 0 {
		t.Errorf("nenhuma lesson deveria ter sido criada, count = %d", count)
	}
}

func TestImportService_ConfirmImport_SucceedsEvenWhenDurationProbeFails(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	storageRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(storageRoot, "aula.mp4"), []byte("nao-e-um-video-de-verdade"), 0o644); err != nil {
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
		t.Fatalf("ScanFolder() erro inesperado: %v", err)
	}
	pending, err := svc.ListPendingImports()
	if err != nil || len(pending) != 1 {
		t.Fatalf("setup: ListPendingImports() = %+v, %v", pending, err)
	}

	if err := svc.ConfirmImport(pending[0].ID, "2026-07-22T09:00", "Sarah M."); err != nil {
		t.Fatalf("ConfirmImport() com vídeo inválido não deveria falhar (duração é melhor esforço): %v", err)
	}

	lesson, err := db.FindLessonByPath(conn, "2026-07-22_09H00_sarah-m.mp4")
	if err != nil {
		t.Fatalf("FindLessonByPath() erro inesperado: %v", err)
	}
	if lesson == nil {
		t.Fatal("lesson não foi confirmada")
	}
	if lesson.DurationSeconds != nil {
		t.Errorf("DurationSeconds = %v, esperado nil (fixture não é um vídeo real, ffprobe deveria falhar ou estar ausente)", *lesson.DurationSeconds)
	}
}
