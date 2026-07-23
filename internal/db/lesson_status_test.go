package db

import (
	"path/filepath"
	"testing"
)

func TestListLessonsWithStatus_ProcessandoWhenNoJobsDone(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	mustInsertJob(t, conn, lessonID, "extract_audio", "running", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")
	mustInsertJob(t, conn, lessonID, "transcribe", "pending", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")

	lessons, err := ListLessonsWithStatus(conn, LessonFilter{})
	if err != nil {
		t.Fatalf("ListLessonsWithStatus() erro inesperado: %v", err)
	}
	if len(lessons) != 1 || lessons[0].Status != "processando" {
		t.Errorf("ListLessonsWithStatus() = %+v, esperado status=processando", lessons)
	}
}

func TestListLessonsWithStatus_ProntaWhenTranscribeDone(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	mustInsertJob(t, conn, lessonID, "extract_audio", "done", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")
	mustInsertJob(t, conn, lessonID, "transcribe", "done", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")

	lessons, err := ListLessonsWithStatus(conn, LessonFilter{})
	if err != nil {
		t.Fatalf("ListLessonsWithStatus() erro inesperado: %v", err)
	}
	if len(lessons) != 1 || lessons[0].Status != "pronta" {
		t.Errorf("ListLessonsWithStatus() = %+v, esperado status=pronta", lessons)
	}
}

func TestListLessonsWithStatus_ErroComMensagemDaCausaRaiz(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	mustInsertJob(t, conn, lessonID, "extract_audio", "error", 3, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")
	if _, err := conn.Exec(`UPDATE jobs SET last_error = ? WHERE lesson_id = ? AND kind = ?`, "ffmpeg não encontrado", lessonID, "extract_audio"); err != nil {
		t.Fatalf("preparar last_error de fixture falhou: %v", err)
	}
	mustInsertJob(t, conn, lessonID, "transcribe", "error", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")
	if _, err := conn.Exec(`UPDATE jobs SET last_error = ? WHERE lesson_id = ? AND kind = ?`, "depende de extract_audio que falhou: ffmpeg não encontrado", lessonID, "transcribe"); err != nil {
		t.Fatalf("preparar last_error de fixture falhou: %v", err)
	}

	lessons, err := ListLessonsWithStatus(conn, LessonFilter{})
	if err != nil {
		t.Fatalf("ListLessonsWithStatus() erro inesperado: %v", err)
	}
	if len(lessons) != 1 || lessons[0].Status != "erro" || lessons[0].ErrorMessage != "ffmpeg não encontrado" {
		t.Errorf("ListLessonsWithStatus() = %+v, esperado status=erro com a mensagem do extract_audio (causa raiz, não a do transcribe bloqueado)", lessons)
	}
}

func TestListLessonsWithStatus_ErroQuandoSoTranscribeFalhou(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	mustInsertJob(t, conn, lessonID, "extract_audio", "done", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")
	mustInsertJob(t, conn, lessonID, "transcribe", "error", 3, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")
	if _, err := conn.Exec(`UPDATE jobs SET last_error = ? WHERE lesson_id = ? AND kind = ?`, "falha real de STT", lessonID, "transcribe"); err != nil {
		t.Fatalf("preparar last_error de fixture falhou: %v", err)
	}

	lessons, err := ListLessonsWithStatus(conn, LessonFilter{})
	if err != nil {
		t.Fatalf("ListLessonsWithStatus() erro inesperado: %v", err)
	}
	if len(lessons) != 1 || lessons[0].Status != "erro" || lessons[0].ErrorMessage != "falha real de STT" {
		t.Errorf("ListLessonsWithStatus() = %+v, esperado status=erro com a mensagem do transcribe", lessons)
	}
}

func TestListLessonsWithStatus_FiltraPorTutorEPeriodo(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	sarah := mustInsertLessonForJobs(t, conn, "sarah.mp4")
	if _, err := conn.Exec(`UPDATE lessons SET tutor = ?, lesson_date = ? WHERE id = ?`, "Sarah M.", "2026-07-10", sarah); err != nil {
		t.Fatalf("ajustar fixture sarah falhou: %v", err)
	}
	mustInsertJob(t, conn, sarah, "extract_audio", "done", 0, "2026-07-10T10:00:00Z", "2026-07-10T10:00:00Z")
	mustInsertJob(t, conn, sarah, "transcribe", "done", 0, "2026-07-10T10:00:00Z", "2026-07-10T10:00:00Z")

	james := mustInsertLessonForJobs(t, conn, "james.mp4")
	if _, err := conn.Exec(`UPDATE lessons SET tutor = ?, lesson_date = ? WHERE id = ?`, "James K.", "2026-07-20T14:00", james); err != nil {
		t.Fatalf("ajustar fixture james falhou: %v", err)
	}
	mustInsertJob(t, conn, james, "extract_audio", "done", 0, "2026-07-20T10:00:00Z", "2026-07-20T10:00:00Z")
	mustInsertJob(t, conn, james, "transcribe", "done", 0, "2026-07-20T10:00:00Z", "2026-07-20T10:00:00Z")

	byTutor, err := ListLessonsWithStatus(conn, LessonFilter{Tutor: "James K."})
	if err != nil {
		t.Fatalf("ListLessonsWithStatus(Tutor) erro inesperado: %v", err)
	}
	if len(byTutor) != 1 || byTutor[0].Tutor != "James K." {
		t.Errorf("ListLessonsWithStatus(Tutor=James K.) = %+v, esperado só a aula de James K.", byTutor)
	}

	byDate, err := ListLessonsWithStatus(conn, LessonFilter{DateFrom: "2026-07-15", DateTo: "2026-07-31"})
	if err != nil {
		t.Fatalf("ListLessonsWithStatus(DateFrom/DateTo) erro inesperado: %v", err)
	}
	if len(byDate) != 1 || byDate[0].Tutor != "James K." {
		t.Errorf("ListLessonsWithStatus(2026-07-15..2026-07-31) = %+v, esperado só a aula de 20/07 (inclui horário, filtra só pela data)", byDate)
	}
}

func TestFindLessonWithStatusByID_FindsExistingWithStatusAndNilWhenMissing(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	mustInsertJob(t, conn, lessonID, "extract_audio", "done", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")
	mustInsertJob(t, conn, lessonID, "transcribe", "done", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")
	if err := SetStudentSpeaker(conn, lessonID, "speaker_0"); err != nil {
		t.Fatalf("SetStudentSpeaker() erro inesperado: %v", err)
	}

	found, err := FindLessonWithStatusByID(conn, lessonID)
	if err != nil {
		t.Fatalf("FindLessonWithStatusByID() erro inesperado: %v", err)
	}
	if found == nil || found.Status != "pronta" {
		t.Fatalf("FindLessonWithStatusByID() = %+v, esperado status=pronta", found)
	}
	if found.StudentSpeakerLabel == nil || *found.StudentSpeakerLabel != "speaker_0" {
		t.Errorf("StudentSpeakerLabel = %v, esperado speaker_0", found.StudentSpeakerLabel)
	}

	missing, err := FindLessonWithStatusByID(conn, lessonID+999)
	if err != nil {
		t.Fatalf("FindLessonWithStatusByID() erro inesperado: %v", err)
	}
	if missing != nil {
		t.Errorf("FindLessonWithStatusByID() para id inexistente = %+v, esperado nil", missing)
	}
}
