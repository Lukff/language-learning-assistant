package services

import (
	"database/sql"
	"path/filepath"
	"testing"

	"assistente-idiomas/internal/db"
)

func mustInsertLesson(t *testing.T, conn *sql.DB, date, tutor, videoPath string) int64 {
	t.Helper()
	res, err := conn.Exec(
		`INSERT INTO lessons (lesson_date, tutor, video_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		date, tutor, videoPath, "2026-07-22T09:00:00Z", "2026-07-22T09:00:00Z",
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

	svc := NewLibraryService(conn)
	lessons, err := svc.ListLessons(LessonFilter{})
	if err != nil {
		t.Fatalf("ListLessons() erro inesperado: %v", err)
	}
	if len(lessons) != 1 {
		t.Fatalf("ListLessons() = %+v, esperado 1 aula", lessons)
	}
	if lessons[0].LessonDate != "2026-07-20" || lessons[0].Tutor != "Sarah M." || lessons[0].VideoPath != "aula.mp4" {
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

	svc := NewLibraryService(conn)
	lessons, err := svc.ListLessons(LessonFilter{})
	if err != nil {
		t.Fatalf("ListLessons() erro inesperado: %v", err)
	}
	if len(lessons) != 0 {
		t.Errorf("ListLessons() = %+v, esperado vazio", lessons)
	}
}

func TestLibraryService_ListLessons_FiltersByTutor(t *testing.T) {
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

	svc := NewLibraryService(conn)
	lessons, err := svc.ListLessons(LessonFilter{Tutor: "James K."})
	if err != nil {
		t.Fatalf("ListLessons() erro inesperado: %v", err)
	}
	if len(lessons) != 1 || lessons[0].Tutor != "James K." {
		t.Errorf("ListLessons(Tutor=James K.) = %+v, esperado só a aula de James K.", lessons)
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

	svc := NewLibraryService(conn)
	lessons, err := svc.ListLessons(LessonFilter{})
	if err != nil {
		t.Fatalf("ListLessons() erro inesperado: %v", err)
	}
	if len(lessons) != 1 || lessons[0].Status != "erro" || lessons[0].ErrorMessage != "ffmpeg não encontrado" {
		t.Errorf("ListLessons()[0] = %+v, esperado status=erro com a mensagem do extract_audio (causa raiz)", lessons[0])
	}
}

func TestLibraryService_ListTutors_ReturnsDistinctTutors(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	mustInsertLesson(t, conn, "2026-07-20", "Sarah M.", "a.mp4")
	mustInsertLesson(t, conn, "2026-07-21", "Sarah M.", "b.mp4")
	mustInsertLesson(t, conn, "2026-07-22", "James K.", "c.mp4")

	svc := NewLibraryService(conn)
	tutors, err := svc.ListTutors()
	if err != nil {
		t.Fatalf("ListTutors() erro inesperado: %v", err)
	}
	if len(tutors) != 2 {
		t.Fatalf("ListTutors() = %+v, esperado 2 tutores distintos", tutors)
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

	svc := NewLibraryService(conn)
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

	svc := NewLibraryService(conn)
	lesson, err := svc.GetLesson(lessonID)
	if err != nil {
		t.Fatalf("GetLesson() erro inesperado: %v", err)
	}
	if lesson.Tutor != "Sarah M." || lesson.VideoPath != "aula.mp4" {
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

	svc := NewLibraryService(conn)
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

	svc := NewLibraryService(conn)
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

	svc := NewLibraryService(conn)
	if _, err := svc.GetTranscript(lessonID); err == nil {
		t.Error("GetTranscript() sem transcrição esperava erro, veio nil")
	}
}
