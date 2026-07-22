// internal/jobs/worker_test.go
package jobs

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"assistente-idiomas/internal/db"
	"assistente-idiomas/internal/stt"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() erro inesperado: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func insertLesson(t *testing.T, conn *sql.DB, videoPath string) int64 {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := conn.Exec(
		`INSERT INTO lessons (lesson_date, tutor, video_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"2026-07-22", "Fulano", videoPath, now, now,
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

func insertJob(t *testing.T, conn *sql.DB, lessonID int64, kind, status string, attempts int, createdAt, updatedAt string) int64 {
	t.Helper()
	res, err := conn.Exec(
		`INSERT INTO jobs (lesson_id, kind, status, attempts, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		lessonID, kind, status, attempts, createdAt, updatedAt,
	)
	if err != nil {
		t.Fatalf("inserir job de fixture falhou: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("obter id do job de fixture falhou: %v", err)
	}
	return id
}

func fakeExtractAudioAlwaysOK(ctx context.Context, videoPath, outputPath string) error {
	return os.WriteFile(outputPath, []byte("wav-fake"), 0o644)
}

type fakeSTTProvider struct {
	result *stt.Result
	err    error
	calls  int
}

func (f *fakeSTTProvider) Name() string { return "fake" }

func (f *fakeSTTProvider) Transcribe(ctx context.Context, audioPath string) (*stt.Result, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

func newTestWorker(t *testing.T, conn *sql.DB, opts ...Option) *Worker {
	t.Helper()
	storageRoot := t.TempDir()
	audioCacheDir := t.TempDir()
	return NewWorker(
		conn,
		func() (string, error) { return storageRoot, nil },
		audioCacheDir,
		fakeExtractAudioAlwaysOK,
		func() (stt.Provider, error) { return &fakeSTTProvider{result: &stt.Result{RawResponse: []byte(`{}`)}}, nil },
		noopNotifier{},
		opts...,
	)
}

func TestClaimNextEligibleJob_PicksOldestPendingFirstAndMarksRunning(t *testing.T) {
	conn := newTestDB(t)
	lessonA := insertLesson(t, conn, "aula-a.mp4")
	lessonB := insertLesson(t, conn, "aula-b.mp4")
	insertJob(t, conn, lessonB, "extract_audio", "pending", 0, "2026-07-22T10:00:05Z", "2026-07-22T10:00:05Z")
	olderID := insertJob(t, conn, lessonA, "extract_audio", "pending", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")

	w := newTestWorker(t, conn)

	job, err := w.claimNextEligibleJob()
	if err != nil {
		t.Fatalf("claimNextEligibleJob() erro inesperado: %v", err)
	}
	if job == nil || job.ID != olderID {
		t.Fatalf("claimNextEligibleJob() = %+v, esperado job %d (mais antigo)", job, olderID)
	}
	if job.Status != "running" {
		t.Errorf("job.Status = %q, esperado running", job.Status)
	}

	persisted, err := db.FindJob(conn, lessonA, "extract_audio")
	if err != nil {
		t.Fatalf("FindJob() erro inesperado: %v", err)
	}
	if persisted.Status != "running" {
		t.Errorf("status persistido = %q, esperado running", persisted.Status)
	}
}

func TestClaimNextEligibleJob_BlocksTranscribeWhenExtractAudioErrored(t *testing.T) {
	conn := newTestDB(t)
	lessonID := insertLesson(t, conn, "aula.mp4")
	now := "2026-07-22T10:00:00Z"
	insertJob(t, conn, lessonID, "extract_audio", "error", 3, now, now)
	transcribeID := insertJob(t, conn, lessonID, "transcribe", "pending", 0, now, now)

	sttCalled := false
	w := NewWorker(
		conn,
		func() (string, error) { return t.TempDir(), nil },
		t.TempDir(),
		fakeExtractAudioAlwaysOK,
		func() (stt.Provider, error) {
			sttCalled = true
			return &fakeSTTProvider{}, nil
		},
		noopNotifier{},
	)

	job, err := w.claimNextEligibleJob()
	if err != nil {
		t.Fatalf("claimNextEligibleJob() erro inesperado: %v", err)
	}
	if job != nil {
		t.Fatalf("claimNextEligibleJob() = %+v, esperado nil (nada elegível)", job)
	}
	if sttCalled {
		t.Error("sttFactory não deveria ser chamado — extract_audio já falhou")
	}

	got, err := db.FindJob(conn, lessonID, "transcribe")
	if err != nil {
		t.Fatalf("FindJob() erro inesperado: %v", err)
	}
	if got.ID != transcribeID || got.Status != "error" {
		t.Errorf("transcribe = %+v, esperado status=error", got)
	}
}
