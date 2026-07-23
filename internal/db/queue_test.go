// internal/db/queue_test.go
package db

import (
	"path/filepath"
	"testing"
)

func TestListQueueEntries_ExtractAudioRunningTakesPriorityOverTranscribePending(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	mustInsertJob(t, conn, lessonID, "extract_audio", "running", 0, "2026-07-23T10:00:00Z", "2026-07-23T10:00:00Z")
	mustInsertJob(t, conn, lessonID, "transcribe", "pending", 0, "2026-07-23T10:00:00Z", "2026-07-23T10:00:00Z")

	entries, err := ListQueueEntries(conn)
	if err != nil {
		t.Fatalf("ListQueueEntries() erro inesperado: %v", err)
	}
	if len(entries) != 1 || entries[0].Kind != "extract_audio" || entries[0].Status != "running" {
		t.Errorf("ListQueueEntries() = %+v, esperado 1 entrada extract_audio/running (transcribe ainda bloqueado, mesmo pending no banco)", entries)
	}
}

func TestListQueueEntries_ExtractAudioErrorIsRootCause(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	mustInsertJob(t, conn, lessonID, "extract_audio", "error", 3, "2026-07-23T10:00:00Z", "2026-07-23T10:00:00Z")
	if _, err := conn.Exec(`UPDATE jobs SET last_error = ? WHERE lesson_id = ? AND kind = ?`, "ffmpeg não encontrado", lessonID, "extract_audio"); err != nil {
		t.Fatalf("preparar last_error de fixture falhou: %v", err)
	}
	mustInsertJob(t, conn, lessonID, "transcribe", "error", 0, "2026-07-23T10:00:00Z", "2026-07-23T10:00:00Z")
	if _, err := conn.Exec(`UPDATE jobs SET last_error = ? WHERE lesson_id = ? AND kind = ?`, "depende de extract_audio que falhou: ffmpeg não encontrado", lessonID, "transcribe"); err != nil {
		t.Fatalf("preparar last_error de fixture falhou: %v", err)
	}

	entries, err := ListQueueEntries(conn)
	if err != nil {
		t.Fatalf("ListQueueEntries() erro inesperado: %v", err)
	}
	if len(entries) != 1 || entries[0].Kind != "extract_audio" || entries[0].Status != "error" || entries[0].LastError != "ffmpeg não encontrado" {
		t.Errorf("ListQueueEntries() = %+v, esperado extract_audio/error com a mensagem de causa raiz", entries)
	}
}

func TestListQueueEntries_TranscribeErrorWhenExtractDone(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	mustInsertJob(t, conn, lessonID, "extract_audio", "done", 0, "2026-07-23T10:00:00Z", "2026-07-23T10:00:00Z")
	mustInsertJob(t, conn, lessonID, "transcribe", "error", 2, "2026-07-23T10:05:00Z", "2026-07-23T10:05:00Z")
	if _, err := conn.Exec(`UPDATE jobs SET last_error = ? WHERE lesson_id = ? AND kind = ?`, "falha real de STT", lessonID, "transcribe"); err != nil {
		t.Fatalf("preparar last_error de fixture falhou: %v", err)
	}

	entries, err := ListQueueEntries(conn)
	if err != nil {
		t.Fatalf("ListQueueEntries() erro inesperado: %v", err)
	}
	if len(entries) != 1 || entries[0].Kind != "transcribe" || entries[0].Status != "error" || entries[0].LastError != "falha real de STT" {
		t.Errorf("ListQueueEntries() = %+v, esperado transcribe/error", entries)
	}
}

func TestListQueueEntries_TranscribePendingWhenExtractDone(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	mustInsertJob(t, conn, lessonID, "extract_audio", "done", 0, "2026-07-23T10:00:00Z", "2026-07-23T10:00:00Z")
	mustInsertJob(t, conn, lessonID, "transcribe", "pending", 0, "2026-07-23T10:05:00Z", "2026-07-23T10:05:00Z")

	entries, err := ListQueueEntries(conn)
	if err != nil {
		t.Fatalf("ListQueueEntries() erro inesperado: %v", err)
	}
	if len(entries) != 1 || entries[0].Kind != "transcribe" || entries[0].Status != "pending" {
		t.Errorf("ListQueueEntries() = %+v, esperado transcribe/pending (extract_audio já done, transcribe genuinamente elegível)", entries)
	}
}

func TestListQueueEntries_ExcludesReadyLessons(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	mustInsertJob(t, conn, lessonID, "extract_audio", "done", 0, "2026-07-23T10:00:00Z", "2026-07-23T10:00:00Z")
	mustInsertJob(t, conn, lessonID, "transcribe", "done", 0, "2026-07-23T10:00:00Z", "2026-07-23T10:00:00Z")

	entries, err := ListQueueEntries(conn)
	if err != nil {
		t.Fatalf("ListQueueEntries() erro inesperado: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("ListQueueEntries() = %+v, esperado vazio (aula pronta não entra na fila)", entries)
	}
}

func TestListQueueEntries_OrdersErrorFirstThenByUpdatedAt(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	older := mustInsertLessonForJobs(t, conn, "older.mp4")
	mustInsertJob(t, conn, older, "extract_audio", "running", 0, "2026-07-23T09:00:00Z", "2026-07-23T09:00:00Z")
	mustInsertJob(t, conn, older, "transcribe", "pending", 0, "2026-07-23T09:00:00Z", "2026-07-23T09:00:00Z")

	newer := mustInsertLessonForJobs(t, conn, "newer.mp4")
	mustInsertJob(t, conn, newer, "extract_audio", "pending", 0, "2026-07-23T11:00:00Z", "2026-07-23T11:00:00Z")
	mustInsertJob(t, conn, newer, "transcribe", "pending", 0, "2026-07-23T11:00:00Z", "2026-07-23T11:00:00Z")

	withError := mustInsertLessonForJobs(t, conn, "error.mp4")
	mustInsertJob(t, conn, withError, "extract_audio", "error", 3, "2026-07-23T10:00:00Z", "2026-07-23T10:00:00Z")
	mustInsertJob(t, conn, withError, "transcribe", "error", 0, "2026-07-23T10:00:00Z", "2026-07-23T10:00:00Z")

	entries, err := ListQueueEntries(conn)
	if err != nil {
		t.Fatalf("ListQueueEntries() erro inesperado: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("ListQueueEntries() = %+v, esperado 3 entradas", entries)
	}
	if entries[0].LessonID != withError {
		t.Errorf("ListQueueEntries()[0].LessonID = %d, esperado a aula com erro primeiro", entries[0].LessonID)
	}
	if entries[1].LessonID != older || entries[2].LessonID != newer {
		t.Errorf("ListQueueEntries()[1:] = %+v, esperado older antes de newer (FIFO por updated_at)", entries[1:])
	}
}
