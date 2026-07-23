// services/queue_test.go
package services

import (
	"path/filepath"
	"testing"

	"assistente-idiomas/internal/db"
)

func TestQueueService_ListQueue_TranslatesStageAndStatus(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLesson(t, conn, "2026-07-23", "Sarah M.", "aula.mp4")
	mustInsertJobWithStatus(t, conn, lessonID, "extract_audio", "done", "")
	mustInsertJobWithStatus(t, conn, lessonID, "transcribe", "running", "")

	svc := NewQueueService(conn)
	items, err := svc.ListQueue()
	if err != nil {
		t.Fatalf("ListQueue() erro inesperado: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("ListQueue() = %+v, esperado 1 item", items)
	}
	if items[0].Stage != "Transcrição" || items[0].Status != "processando" {
		t.Errorf("ListQueue()[0] = %+v, esperado Stage=Transcrição Status=processando", items[0])
	}
	if items[0].LessonDate != "2026-07-23" || items[0].Tutor != "Sarah M." {
		t.Errorf("ListQueue()[0] = %+v, esperado data/tutor da fixture", items[0])
	}
}

func TestQueueService_ListQueue_ErrorStatusAndMessage(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLesson(t, conn, "2026-07-23", "Sarah M.", "aula.mp4")
	mustInsertJobWithStatus(t, conn, lessonID, "extract_audio", "error", "ffmpeg não encontrado")
	mustInsertJobWithStatus(t, conn, lessonID, "transcribe", "error", "depende de extract_audio que falhou")

	svc := NewQueueService(conn)
	items, err := svc.ListQueue()
	if err != nil {
		t.Fatalf("ListQueue() erro inesperado: %v", err)
	}
	if len(items) != 1 || items[0].Stage != "Extração de áudio" || items[0].Status != "erro" || items[0].LastError != "ffmpeg não encontrado" {
		t.Errorf("ListQueue()[0] = %+v, esperado Stage=Extração de áudio Status=erro com a causa raiz", items[0])
	}
}

func TestQueueService_ListQueue_ExcludesReadyLessons(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLesson(t, conn, "2026-07-23", "Sarah M.", "aula.mp4")
	mustInsertJobWithStatus(t, conn, lessonID, "extract_audio", "done", "")
	mustInsertJobWithStatus(t, conn, lessonID, "transcribe", "done", "")

	svc := NewQueueService(conn)
	items, err := svc.ListQueue()
	if err != nil {
		t.Fatalf("ListQueue() erro inesperado: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("ListQueue() = %+v, esperado vazio (aula pronta)", items)
	}
}

func TestQueueService_RetryLesson_ResetsErrorJobsToPending(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLesson(t, conn, "2026-07-23", "Sarah M.", "aula.mp4")
	mustInsertJobWithStatus(t, conn, lessonID, "extract_audio", "error", "ffmpeg não encontrado")
	mustInsertJobWithStatus(t, conn, lessonID, "transcribe", "error", "depende de extract_audio que falhou")

	svc := NewQueueService(conn)
	if err := svc.RetryLesson(lessonID); err != nil {
		t.Fatalf("RetryLesson() erro inesperado: %v", err)
	}

	items, err := svc.ListQueue()
	if err != nil {
		t.Fatalf("ListQueue() erro inesperado: %v", err)
	}
	if len(items) != 1 || items[0].Status != "aguardando" {
		t.Errorf("ListQueue() após RetryLesson = %+v, esperado status=aguardando (jobs voltaram a pending)", items)
	}
}
