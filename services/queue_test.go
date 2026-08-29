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
	if items[0].Stage != "Transcription" || items[0].Status != "processing" {
		t.Errorf("ListQueue()[0] = %+v, esperado Stage=Transcription Status=processing", items[0])
	}
	if items[0].LessonDate != "2026-07-23" || items[0].TeacherName != "Sarah M." {
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
	mustInsertJobWithStatus(t, conn, lessonID, "extract_audio", "error", "ffmpeg not found")
	mustInsertJobWithStatus(t, conn, lessonID, "transcribe", "error", "depends on extract_audio which failed")

	svc := NewQueueService(conn)
	items, err := svc.ListQueue()
	if err != nil {
		t.Fatalf("ListQueue() erro inesperado: %v", err)
	}
	if len(items) != 1 || items[0].Stage != "Audio extraction" || items[0].Status != "error" || items[0].LastError != "ffmpeg not found" {
		t.Errorf("ListQueue()[0] = %+v, esperado Stage=Audio extraction Status=error com a causa raiz", items[0])
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
	if len(items) != 1 || items[0].Status != "waiting" {
		t.Errorf("ListQueue() após RetryLesson = %+v, esperado status=waiting (jobs voltaram a pending)", items)
	}
}
