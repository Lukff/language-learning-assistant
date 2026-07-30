package services

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"assistente-idiomas/internal/config"
	"assistente-idiomas/internal/db"
)

func mustInsertLesson(t *testing.T, conn *sql.DB, date, teacherName, videoPath string) int64 {
	t.Helper()
	teacherID, err := db.GetOrCreateTeacherByName(conn, teacherName)
	if err != nil {
		t.Fatalf("GetOrCreateTeacherByName() de fixture falhou: %v", err)
	}
	res, err := conn.Exec(
		`INSERT INTO lessons (lesson_date, teacher_id, video_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		date, teacherID, videoPath, "2026-07-22T09:00:00Z", "2026-07-22T09:00:00Z",
	)
	if err != nil {
		t.Fatalf("inserir lesson de fixture falhou: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("obter id da lesson de fixture falhou: %v", err)
	}
	return id
}

func mustInsertJobWithStatus(t *testing.T, conn *sql.DB, lessonID int64, kind, status, lastError string) {
	t.Helper()
	_, err := conn.Exec(
		`INSERT INTO jobs (lesson_id, kind, status, last_error, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		lessonID, kind, status, lastError, "2026-07-22T09:00:00Z", "2026-07-22T09:00:00Z",
	)
	if err != nil {
		t.Fatalf("inserir job de fixture falhou: %v", err)
	}
}

func TestLibraryService_ListLessons_ReturnsConfirmedLessons(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLesson(t, conn, "2026-07-20", "Sarah M.", "aula.mp4")
	mustInsertJobWithStatus(t, conn, lessonID, "extract_audio", "done", "")
	mustInsertJobWithStatus(t, conn, lessonID, "transcribe", "done", "")

	svc := NewLibraryService(conn, testStorageRoot(t))
	lessons, err := svc.ListLessons(LessonFilter{})
	if err != nil {
		t.Fatalf("ListLessons() erro inesperado: %v", err)
	}
	if len(lessons) != 1 {
		t.Fatalf("ListLessons() = %+v, esperado 1 aula", lessons)
	}
	if lessons[0].LessonDate != "2026-07-20" || lessons[0].TeacherName != "Sarah M." || lessons[0].VideoPath != "aula.mp4" {
		t.Errorf("ListLessons()[0] = %+v, esperado data/tutor/path da fixture", lessons[0])
	}
	if lessons[0].Status != "pronta" {
		t.Errorf("ListLessons()[0].Status = %q, esperado pronta (extract_audio e transcribe done)", lessons[0].Status)
	}
}

func TestLibraryService_ListLessons_EmptyReturnsEmptySlice(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	svc := NewLibraryService(conn, testStorageRoot(t))
	lessons, err := svc.ListLessons(LessonFilter{})
	if err != nil {
		t.Fatalf("ListLessons() erro inesperado: %v", err)
	}
	if len(lessons) != 0 {
		t.Errorf("ListLessons() = %+v, esperado vazio", lessons)
	}
}

func TestLibraryService_ListLessons_FiltersByTeacher(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	l1 := mustInsertLesson(t, conn, "2026-07-20", "Sarah M.", "a.mp4")
	mustInsertJobWithStatus(t, conn, l1, "extract_audio", "done", "")
	mustInsertJobWithStatus(t, conn, l1, "transcribe", "done", "")
	l2 := mustInsertLesson(t, conn, "2026-07-21", "James K.", "b.mp4")
	mustInsertJobWithStatus(t, conn, l2, "extract_audio", "done", "")
	mustInsertJobWithStatus(t, conn, l2, "transcribe", "done", "")

	james, err := db.GetOrCreateTeacherByName(conn, "James K.")
	if err != nil {
		t.Fatalf("GetOrCreateTeacherByName() erro inesperado: %v", err)
	}

	svc := NewLibraryService(conn, testStorageRoot(t))
	lessons, err := svc.ListLessons(LessonFilter{TeacherID: james})
	if err != nil {
		t.Fatalf("ListLessons() erro inesperado: %v", err)
	}
	if len(lessons) != 1 || lessons[0].TeacherName != "James K." {
		t.Errorf("ListLessons(TeacherID=James K.) = %+v, esperado só a aula de James K.", lessons)
	}
}

func TestLibraryService_ListLessons_ErrorStatusAndMessageFromExtractAudio(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLesson(t, conn, "2026-07-20", "Sarah M.", "aula.mp4")
	mustInsertJobWithStatus(t, conn, lessonID, "extract_audio", "error", "ffmpeg não encontrado")
	mustInsertJobWithStatus(t, conn, lessonID, "transcribe", "error", "depende de extract_audio que falhou: ffmpeg não encontrado")

	svc := NewLibraryService(conn, testStorageRoot(t))
	lessons, err := svc.ListLessons(LessonFilter{})
	if err != nil {
		t.Fatalf("ListLessons() erro inesperado: %v", err)
	}
	if len(lessons) != 1 || lessons[0].Status != "erro" || lessons[0].ErrorMessage != "ffmpeg não encontrado" {
		t.Errorf("ListLessons()[0] = %+v, esperado status=erro com a mensagem do extract_audio (causa raiz)", lessons[0])
	}
}

func TestLibraryService_RetryLesson_ResetsErrorJobsToPending(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLesson(t, conn, "2026-07-20", "Sarah M.", "aula.mp4")
	mustInsertJobWithStatus(t, conn, lessonID, "extract_audio", "error", "ffmpeg não encontrado")
	mustInsertJobWithStatus(t, conn, lessonID, "transcribe", "error", "depende de extract_audio que falhou")

	svc := NewLibraryService(conn, testStorageRoot(t))
	if err := svc.RetryLesson(lessonID); err != nil {
		t.Fatalf("RetryLesson() erro inesperado: %v", err)
	}

	lessons, err := svc.ListLessons(LessonFilter{})
	if err != nil {
		t.Fatalf("ListLessons() erro inesperado: %v", err)
	}
	if len(lessons) != 1 || lessons[0].Status != "processando" {
		t.Errorf("ListLessons()[0] após RetryLesson = %+v, esperado status=processando (jobs voltaram a pending)", lessons[0])
	}
}

func TestLibraryService_GetLesson_FindsExistingAndErrorsWhenMissing(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLesson(t, conn, "2026-07-20", "Sarah M.", "aula.mp4")

	svc := NewLibraryService(conn, testStorageRoot(t))
	lesson, err := svc.GetLesson(lessonID)
	if err != nil {
		t.Fatalf("GetLesson() erro inesperado: %v", err)
	}
	if lesson.TeacherName != "Sarah M." || lesson.VideoPath != "aula.mp4" {
		t.Errorf("GetLesson() = %+v, esperado tutor/path da fixture", lesson)
	}

	if _, err := svc.GetLesson(lessonID + 999); err == nil {
		t.Error("GetLesson() com id inexistente esperava erro, veio nil")
	}
}

func TestLibraryService_GetLesson_IncludesStatusAndStudentSpeaker(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLesson(t, conn, "2026-07-20", "Sarah M.", "aula.mp4")
	mustInsertJobWithStatus(t, conn, lessonID, "extract_audio", "done", "")
	mustInsertJobWithStatus(t, conn, lessonID, "transcribe", "running", "")

	svc := NewLibraryService(conn, testStorageRoot(t))
	lesson, err := svc.GetLesson(lessonID)
	if err != nil {
		t.Fatalf("GetLesson() erro inesperado: %v", err)
	}
	if lesson.Status != "processando" {
		t.Errorf("GetLesson().Status = %q, esperado processando (transcribe ainda rodando)", lesson.Status)
	}
	if lesson.StudentSpeakerLabel != nil {
		t.Errorf("GetLesson().StudentSpeakerLabel = %v, esperado nil antes do toggle", lesson.StudentSpeakerLabel)
	}

	if err := svc.SetStudentSpeaker(lessonID, "speaker_0"); err != nil {
		t.Fatalf("SetStudentSpeaker() erro inesperado: %v", err)
	}
	lesson, err = svc.GetLesson(lessonID)
	if err != nil {
		t.Fatalf("GetLesson() erro inesperado: %v", err)
	}
	if lesson.StudentSpeakerLabel == nil || *lesson.StudentSpeakerLabel != "speaker_0" {
		t.Errorf("GetLesson().StudentSpeakerLabel = %v, esperado speaker_0", lesson.StudentSpeakerLabel)
	}
}

func TestLibraryService_GetTranscript_ReturnsUtterancesInSeconds(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLesson(t, conn, "2026-07-20", "Sarah M.", "aula.mp4")
	mustInsertJobWithStatus(t, conn, lessonID, "extract_audio", "done", "")
	mustInsertJobWithStatus(t, conn, lessonID, "transcribe", "done", "")
	utterancesJSON := `[{"Speaker":"speaker_0","Text":"Hello","Start":0,"End":2000000000},{"Speaker":"speaker_1","Text":"Hi","Start":2000000000,"End":3500000000}]`
	if err := db.InsertTranscript(conn, lessonID, "aula.transcript.json", utterancesJSON); err != nil {
		t.Fatalf("InsertTranscript() erro inesperado: %v", err)
	}

	svc := NewLibraryService(conn, testStorageRoot(t))
	tr, err := svc.GetTranscript(lessonID)
	if err != nil {
		t.Fatalf("GetTranscript() erro inesperado: %v", err)
	}
	if len(tr.Utterances) != 2 {
		t.Fatalf("GetTranscript().Utterances = %+v, esperado 2 falas", tr.Utterances)
	}
	if tr.Utterances[0].Speaker != "speaker_0" || tr.Utterances[0].StartSeconds != 0 || tr.Utterances[0].EndSeconds != 2 {
		t.Errorf("Utterances[0] = %+v, esperado speaker_0 0s-2s", tr.Utterances[0])
	}
	if tr.Utterances[1].StartSeconds != 2 || tr.Utterances[1].EndSeconds != 3.5 {
		t.Errorf("Utterances[1] = %+v, esperado 2s-3.5s", tr.Utterances[1])
	}
}

func TestLibraryService_GetTranscript_ErrorsWhenNoTranscriptYet(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLesson(t, conn, "2026-07-20", "Sarah M.", "aula.mp4")
	mustInsertJobWithStatus(t, conn, lessonID, "extract_audio", "done", "")
	mustInsertJobWithStatus(t, conn, lessonID, "transcribe", "running", "")

	svc := NewLibraryService(conn, testStorageRoot(t))
	if _, err := svc.GetTranscript(lessonID); err == nil {
		t.Error("GetTranscript() sem transcrição esperava erro, veio nil")
	}
}

// testStorageRoot retorna um resolver de storage_root fixo, apontando pra
// um diretório temporário vazio — usado pelos testes que não têm relação
// com a checagem de vídeo ausente (essa tem testes próprios abaixo).
func testStorageRoot(t *testing.T) func() (string, error) {
	t.Helper()
	dir := t.TempDir()
	return func() (string, error) { return dir, nil }
}

func TestLibraryService_ListLessons_VideoMissingWhenFileNotFound(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLesson(t, conn, "2026-07-20", "Sarah M.", "aula.mp4")
	mustInsertJobWithStatus(t, conn, lessonID, "extract_audio", "done", "")
	mustInsertJobWithStatus(t, conn, lessonID, "transcribe", "done", "")

	root := t.TempDir() // vazio — o arquivo "aula.mp4" não existe aqui
	svc := NewLibraryService(conn, func() (string, error) { return root, nil })
	lessons, err := svc.ListLessons(LessonFilter{})
	if err != nil {
		t.Fatalf("ListLessons() erro inesperado: %v", err)
	}
	if len(lessons) != 1 || !lessons[0].VideoMissing {
		t.Errorf("ListLessons()[0].VideoMissing = %v, esperado true (arquivo não existe)", lessons[0].VideoMissing)
	}
}

func TestLibraryService_ListLessons_VideoNotMissingWhenFileExists(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "aula.mp4"), []byte("conteudo-fake"), 0o644); err != nil {
		t.Fatalf("escrever vídeo de fixture falhou: %v", err)
	}

	lessonID := mustInsertLesson(t, conn, "2026-07-20", "Sarah M.", "aula.mp4")
	mustInsertJobWithStatus(t, conn, lessonID, "extract_audio", "done", "")
	mustInsertJobWithStatus(t, conn, lessonID, "transcribe", "done", "")

	svc := NewLibraryService(conn, func() (string, error) { return root, nil })
	lessons, err := svc.ListLessons(LessonFilter{})
	if err != nil {
		t.Fatalf("ListLessons() erro inesperado: %v", err)
	}
	if len(lessons) != 1 || lessons[0].VideoMissing {
		t.Errorf("ListLessons()[0].VideoMissing = %v, esperado false (arquivo existe)", lessons[0].VideoMissing)
	}
}

func TestLibraryService_GetLesson_VideoMissingWhenFileNotFound(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLesson(t, conn, "2026-07-20", "Sarah M.", "aula.mp4")

	root := t.TempDir()
	svc := NewLibraryService(conn, func() (string, error) { return root, nil })
	lesson, err := svc.GetLesson(lessonID)
	if err != nil {
		t.Fatalf("GetLesson() erro inesperado: %v", err)
	}
	if !lesson.VideoMissing {
		t.Errorf("GetLesson().VideoMissing = %v, esperado true (arquivo não existe)", lesson.VideoMissing)
	}
}

func TestLibraryService_UpdateLesson_ChangesDateAndTeacher(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLesson(t, conn, "2026-07-20", "Sarah M.", "aula.mp4")

	svc := NewLibraryService(conn, testStorageRoot(t))
	if err := svc.UpdateLesson(lessonID, "2026-07-25T10:00", "James K."); err != nil {
		t.Fatalf("UpdateLesson() erro inesperado: %v", err)
	}

	lesson, err := svc.GetLesson(lessonID)
	if err != nil {
		t.Fatalf("GetLesson() erro inesperado: %v", err)
	}
	if lesson.LessonDate != "2026-07-25T10:00" || lesson.TeacherName != "James K." {
		t.Errorf("GetLesson() após UpdateLesson = %+v, esperado data/professor atualizados", lesson)
	}
}

func TestLibraryService_UpdateLesson_RenamesVideoToNewStandardFilename(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "2026-07-20_10H00_sarah-m.mp4"), []byte("conteudo"), 0o644); err != nil {
		t.Fatalf("preparar vídeo de fixture falhou: %v", err)
	}
	if err := config.Save(&config.AppConfig{StorageRoot: root}); err != nil {
		t.Fatalf("config.Save() falhou: %v", err)
	}

	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLesson(t, conn, "2026-07-20T10:00", "Sarah M.", "2026-07-20_10H00_sarah-m.mp4")

	svc := NewLibraryService(conn, func() (string, error) { return root, nil })
	if err := svc.UpdateLesson(lessonID, "2026-07-25T14:30", "James K."); err != nil {
		t.Fatalf("UpdateLesson() erro inesperado: %v", err)
	}

	wantPath := "2026-07-25_14H30_james-k.mp4"
	lesson, err := db.FindLessonByPath(conn, wantPath)
	if err != nil {
		t.Fatalf("FindLessonByPath() erro inesperado: %v", err)
	}
	if lesson == nil {
		t.Fatalf("lesson não encontrada no path renomeado %q", wantPath)
	}
	if _, err := os.Stat(filepath.Join(root, wantPath)); err != nil {
		t.Errorf("arquivo renomeado não existe no disco em %q: %v", wantPath, err)
	}
}

func TestLibraryService_UpdateLesson_SucceedsEvenWhenRenameFails(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "aula.mp4"), []byte("conteudo"), 0o644); err != nil {
		t.Fatalf("preparar vídeo de fixture falhou: %v", err)
	}
	if err := config.Save(&config.AppConfig{StorageRoot: root}); err != nil {
		t.Fatalf("config.Save() falhou: %v", err)
	}

	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLesson(t, conn, "2026-07-20T10:00", "Sarah M.", "aula.mp4")

	svc := NewLibraryService(conn, func() (string, error) { return root, nil })
	svc.moveFile = func(_, _ string) error { return errors.New("falha injetada") }

	if err := svc.UpdateLesson(lessonID, "2026-07-25T14:30", "James K."); err != nil {
		t.Fatalf("UpdateLesson() não deveria falhar mesmo com rename impossível: %v", err)
	}

	lesson, err := svc.GetLesson(lessonID)
	if err != nil {
		t.Fatalf("GetLesson() erro inesperado: %v", err)
	}
	if lesson.LessonDate != "2026-07-25T14:30" || lesson.TeacherName != "James K." {
		t.Errorf("GetLesson() = %+v, esperado data/professor atualizados mesmo com rename falho", lesson)
	}
	if lesson.VideoPath != "aula.mp4" {
		t.Errorf("VideoPath = %q, esperado inalterado (aula.mp4) já que o rename falhou", lesson.VideoPath)
	}
}
