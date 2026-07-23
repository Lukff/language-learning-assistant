package services

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

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

func TestImportService_ConfirmImport_SucceedsEvenWhenRenameFails(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	storageRoot := t.TempDir()
	originalName := "aula-original.mp4"
	if err := os.WriteFile(filepath.Join(storageRoot, originalName), []byte("conteudo"), 0o644); err != nil {
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
	svc.moveFile = func(_, _ string) error {
		return errors.New("falha injetada")
	}
	if _, err := svc.ScanFolder(); err != nil {
		t.Fatalf("ScanFolder() erro inesperado: %v", err)
	}
	pending, err := svc.ListPendingImports()
	if err != nil || len(pending) != 1 {
		t.Fatalf("setup: ListPendingImports() = %+v, %v", pending, err)
	}

	if err := svc.ConfirmImport(pending[0].ID, "2026-07-23T14:30", "Maria José"); err != nil {
		t.Fatalf("ConfirmImport() não deveria falhar mesmo com rename impossível: %v", err)
	}

	lesson, err := db.FindLessonByPath(conn, originalName)
	if err != nil {
		t.Fatalf("FindLessonByPath() erro inesperado: %v", err)
	}
	if lesson == nil {
		t.Fatal("lesson deveria ter sido confirmada com o path original, já que o rename falhou")
	}
	if _, err := os.Stat(filepath.Join(storageRoot, originalName)); err != nil {
		t.Errorf("arquivo original deveria permanecer após falha do move: %v", err)
	}
}

func TestImportService_ConfirmImport_DoesNotClobberDestinationCreatedBeforeMove(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	storageRoot := t.TempDir()
	originalName := "aula-original.mp4"
	originalPath := filepath.Join(storageRoot, originalName)
	if err := os.WriteFile(originalPath, []byte("video original"), 0o644); err != nil {
		t.Fatalf("preparar vídeo original falhou: %v", err)
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

	intruderContent := []byte("arquivo intruso")
	svc.moveFile = func(oldPath, newPath string) error {
		if err := os.WriteFile(newPath, intruderContent, 0o644); err != nil {
			return err
		}
		return moveFileNoReplace(oldPath, newPath)
	}
	if err := svc.ConfirmImport(pending[0].ID, "2026-07-23T14:30", "Maria José"); err != nil {
		t.Fatalf("ConfirmImport() não deveria falhar com colisão TOCTOU: %v", err)
	}

	targetName := "2026-07-23_14H30_maria-jose.mp4"
	gotIntruder, err := os.ReadFile(filepath.Join(storageRoot, targetName))
	if err != nil {
		t.Fatalf("ler arquivo intruso falhou: %v", err)
	}
	if string(gotIntruder) != string(intruderContent) {
		t.Errorf("destino foi sobrescrito: conteúdo = %q", gotIntruder)
	}
	if _, err := os.Stat(originalPath); err != nil {
		t.Errorf("arquivo original deveria permanecer: %v", err)
	}
	if lesson, err := db.FindLessonByPath(conn, originalName); err != nil || lesson == nil {
		t.Errorf("banco deveria continuar no path original: lesson=%+v err=%v", lesson, err)
	}
}

func TestImportService_ConfirmImport_RollsBackMoveWhenDatabasePathUpdateFails(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	storageRoot := t.TempDir()
	originalName := "aula-original.mp4"
	originalPath := filepath.Join(storageRoot, originalName)
	if err := os.WriteFile(originalPath, []byte("video original"), 0o644); err != nil {
		t.Fatalf("preparar vídeo original falhou: %v", err)
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
	if _, err := conn.Exec(`CREATE TRIGGER reject_video_path_update BEFORE UPDATE OF video_path ON lessons BEGIN SELECT RAISE(ABORT, 'falha injetada'); END`); err != nil {
		t.Fatalf("criar trigger de falha falhou: %v", err)
	}

	if err := svc.ConfirmImport(pending[0].ID, "2026-07-23T14:30", "Maria José"); err != nil {
		t.Fatalf("ConfirmImport() não deveria propagar falha de atualização do path: %v", err)
	}

	targetPath := filepath.Join(storageRoot, "2026-07-23_14H30_maria-jose.mp4")
	if _, err := os.Stat(originalPath); err != nil {
		t.Errorf("rollback deveria restaurar arquivo original: %v", err)
	}
	if _, err := os.Stat(targetPath); !os.IsNotExist(err) {
		t.Errorf("target não deveria existir após rollback, err=%v", err)
	}
	if lesson, err := db.FindLessonByPath(conn, originalName); err != nil || lesson == nil {
		t.Errorf("banco deveria apontar para o path original: lesson=%+v err=%v", lesson, err)
	}
}

func TestImportService_ConfirmImport_NormalizesPathAlreadyPointingToTargetFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	storageRoot := t.TempDir()
	targetName := "2026-07-23_14H30_maria-jose.mp4"
	targetPath := filepath.Join(storageRoot, targetName)
	if err := os.WriteFile(targetPath, []byte("video original"), 0o644); err != nil {
		t.Fatalf("preparar vídeo original falhou: %v", err)
	}
	if err := config.Save(&config.AppConfig{StorageRoot: storageRoot}); err != nil {
		t.Fatalf("config.Save() falhou: %v", err)
	}
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()
	info, err := os.Stat(targetPath)
	if err != nil {
		t.Fatalf("stat do vídeo falhou: %v", err)
	}
	if err := db.InsertPendingImport(conn, db.PendingImport{
		Path:          "./" + targetName,
		FileSize:      info.Size(),
		FileMTime:     info.ModTime().UTC().Format(time.RFC3339),
		SHA256:        "hash-sintetico",
		SuggestedDate: "2026-07-23T14:30",
	}); err != nil {
		t.Fatalf("InsertPendingImport() falhou: %v", err)
	}

	svc := NewImportService(conn)
	pending, err := svc.ListPendingImports()
	if err != nil || len(pending) != 1 {
		t.Fatalf("setup: ListPendingImports() = %+v, %v", pending, err)
	}
	if err := svc.ConfirmImport(pending[0].ID, "2026-07-23T14:30", "Maria José"); err != nil {
		t.Fatalf("ConfirmImport() erro inesperado: %v", err)
	}

	if lesson, err := db.FindLessonByPath(conn, targetName); err != nil || lesson == nil {
		t.Errorf("path textual deveria ser normalizado sem sufixo: lesson=%+v err=%v", lesson, err)
	}
	if _, err := os.Stat(filepath.Join(storageRoot, "2026-07-23_14H30_maria-jose-2.mp4")); !os.IsNotExist(err) {
		t.Errorf("não deveria criar arquivo com sufixo -2, err=%v", err)
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

func TestClassifySameFilePath(t *testing.T) {
	tests := []struct {
		name    string
		goos    string
		oldPath string
		newPath string
		want    sameFilePathKind
	}{
		{
			name:    "path limpo exatamente igual",
			goos:    "linux",
			oldPath: filepath.Join("tmp", "dir", "..", "video.mp4"),
			newPath: filepath.Join("tmp", "video.mp4"),
			want:    sameFileExactPath,
		},
		{
			name:    "alias case-only no Windows",
			goos:    "windows",
			oldPath: filepath.Join("tmp", "Video.MP4"),
			newPath: filepath.Join("tmp", "video.mp4"),
			want:    sameFileCaseOnlyPath,
		},
		{
			name:    "hard link com nome distinto",
			goos:    "windows",
			oldPath: filepath.Join("tmp", "original.mp4"),
			newPath: filepath.Join("tmp", "padronizado.mp4"),
			want:    sameFileDistinctHardLink,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifySameFilePath(tt.oldPath, tt.newPath, tt.goos); got != tt.want {
				t.Errorf("classifySameFilePath() = %v, esperado %v", got, tt.want)
			}
		})
	}
}

func TestMoveToExistingSameFile_KeepsDistinctHardLinkNames(t *testing.T) {
	dir := t.TempDir()
	originalPath := filepath.Join(dir, "original.mp4")
	targetPath := filepath.Join(dir, "target.mp4")
	if err := os.WriteFile(originalPath, []byte("video"), 0o644); err != nil {
		t.Fatalf("preparar arquivo original falhou: %v", err)
	}
	if err := os.Link(originalPath, targetPath); err != nil {
		t.Skipf("filesystem não suporta hard links: %v", err)
	}

	unexpectedMove := func(_, _ string) error {
		t.Fatal("hard link distinto já existente não deveria executar move")
		return nil
	}
	if err := moveToExistingSameFile(originalPath, targetPath, sameFileDistinctHardLink, unexpectedMove); err != nil {
		t.Fatalf("moveToExistingSameFile() falhou: %v", err)
	}
	if _, err := os.Stat(originalPath); err != nil {
		t.Errorf("nome original deveria permanecer: %v", err)
	}
	if got, err := os.ReadFile(targetPath); err != nil || string(got) != "video" {
		t.Errorf("destino deveria permanecer intacto: conteúdo=%q err=%v", got, err)
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

	if err := svc.ConfirmImport(1, "", ""); err == nil || err.Error() != "data da aula não pode ser vazia" {
		t.Errorf("ConfirmImport() com data e tutor vazios = %v, esperado data da aula não pode ser vazia", err)
	}
	if err := svc.ConfirmImport(1, "../foraT12:30", ""); err == nil || err.Error() != "tutor não pode ser vazio" {
		t.Errorf("ConfirmImport() com tutor vazio = %v, esperado tutor não pode ser vazio", err)
	}
}

func TestImportService_ConfirmImport_RejectsMalformedLessonDate(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()
	svc := NewImportService(conn)

	for _, lessonDate := range []string{
		"../foraT12:30",
		"2026-02-30T12:30",
		"2026-07-15T12:30:00",
		"2026-07-15T12:30Z",
	} {
		t.Run(lessonDate, func(t *testing.T) {
			err := svc.ConfirmImport(1, lessonDate, "Sarah M.")
			if err == nil || err.Error() != "data e horário da aula devem estar no formato AAAA-MM-DDTHH:MM" {
				t.Errorf("ConfirmImport(%q) = %v, esperado erro de formato", lessonDate, err)
			}
		})
	}

	var count int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM lessons`).Scan(&count); err != nil {
		t.Fatalf("count de lessons falhou: %v", err)
	}
	if count != 0 {
		t.Errorf("datas inválidas não deveriam criar lessons, count = %d", count)
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
