// internal/jobs/worker_test.go
package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
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

func TestRunExtractAudio_SkipsWhenWavAlreadyCached(t *testing.T) {
	conn := newTestDB(t)
	lessonID := insertLesson(t, conn, "aula.mp4")
	job := db.Job{ID: insertJob(t, conn, lessonID, "extract_audio", "running", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z"), LessonID: lessonID, Kind: "extract_audio"}

	audioCacheDir := t.TempDir()
	extractCalls := 0
	w := NewWorker(
		conn,
		func() (string, error) { return t.TempDir(), nil },
		audioCacheDir,
		func(ctx context.Context, videoPath, outputPath string) error {
			extractCalls++
			return nil
		},
		func() (stt.Provider, error) { return &fakeSTTProvider{}, nil },
		noopNotifier{},
	)

	if err := os.WriteFile(w.audioPathFor(lessonID), []byte("ja-existe"), 0o644); err != nil {
		t.Fatalf("preparar wav de fixture falhou: %v", err)
	}

	if err := w.runExtractAudio(context.Background(), job); err != nil {
		t.Fatalf("runExtractAudio() erro inesperado: %v", err)
	}
	if extractCalls != 0 {
		t.Errorf("extractAudio foi chamado %d vezes, esperado 0 (WAV já em cache)", extractCalls)
	}
}

func TestRunExtractAudio_CallsExtractorWhenNotCached(t *testing.T) {
	conn := newTestDB(t)
	storageRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(storageRoot, "aula.mp4"), []byte("video"), 0o644); err != nil {
		t.Fatalf("preparar vídeo de fixture falhou: %v", err)
	}
	lessonID := insertLesson(t, conn, "aula.mp4")
	job := db.Job{ID: insertJob(t, conn, lessonID, "extract_audio", "running", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z"), LessonID: lessonID, Kind: "extract_audio"}

	audioCacheDir := t.TempDir()
	var gotVideoPath, gotOutputPath string
	w := NewWorker(
		conn,
		func() (string, error) { return storageRoot, nil },
		audioCacheDir,
		func(ctx context.Context, videoPath, outputPath string) error {
			gotVideoPath, gotOutputPath = videoPath, outputPath
			return os.WriteFile(outputPath, []byte("wav"), 0o644)
		},
		func() (stt.Provider, error) { return &fakeSTTProvider{}, nil },
		noopNotifier{},
	)

	if err := w.runExtractAudio(context.Background(), job); err != nil {
		t.Fatalf("runExtractAudio() erro inesperado: %v", err)
	}
	if gotVideoPath != filepath.Join(storageRoot, "aula.mp4") {
		t.Errorf("videoPath = %q, esperado %q", gotVideoPath, filepath.Join(storageRoot, "aula.mp4"))
	}
	if gotOutputPath != w.audioPathFor(lessonID) {
		t.Errorf("outputPath = %q, esperado %q", gotOutputPath, w.audioPathFor(lessonID))
	}
	if _, err := os.Stat(w.audioPathFor(lessonID)); err != nil {
		t.Errorf("WAV não foi criado: %v", err)
	}
}

func TestRunTranscribe_SkipsWhenTranscriptAlreadyExists(t *testing.T) {
	conn := newTestDB(t)
	lessonID := insertLesson(t, conn, "aula.mp4")
	if err := db.InsertTranscript(conn, lessonID, "aula.transcript.json", "[]"); err != nil {
		t.Fatalf("InsertTranscript() de fixture falhou: %v", err)
	}
	job := db.Job{ID: insertJob(t, conn, lessonID, "transcribe", "running", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z"), LessonID: lessonID, Kind: "transcribe"}

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

	if err := w.runTranscribe(context.Background(), job); err != nil {
		t.Fatalf("runTranscribe() erro inesperado: %v", err)
	}
	if sttCalled {
		t.Error("sttFactory não deveria ser chamado — transcript já existe")
	}
}

func TestRunTranscribe_WritesRawJSONInsertsTranscriptAndCleansCache(t *testing.T) {
	conn := newTestDB(t)
	storageRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(storageRoot, "aulas", "2026"), 0o755); err != nil {
		t.Fatalf("preparar subpasta de fixture falhou: %v", err)
	}
	videoRelPath := "aulas/2026/aula-01.mp4"
	lessonID := insertLesson(t, conn, videoRelPath)
	job := db.Job{ID: insertJob(t, conn, lessonID, "transcribe", "running", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z"), LessonID: lessonID, Kind: "transcribe"}

	audioCacheDir := t.TempDir()
	w := NewWorker(
		conn,
		func() (string, error) { return storageRoot, nil },
		audioCacheDir,
		fakeExtractAudioAlwaysOK,
		func() (stt.Provider, error) {
			return &fakeSTTProvider{result: &stt.Result{
				RawResponse: []byte(`{"raw":true}`),
				Utterances: []stt.Utterance{
					{Speaker: "speaker_0", Text: "hello", Start: 0, End: time.Second},
				},
			}}, nil
		},
		noopNotifier{},
	)
	if err := os.WriteFile(w.audioPathFor(lessonID), []byte("wav-em-cache"), 0o644); err != nil {
		t.Fatalf("preparar wav de fixture falhou: %v", err)
	}

	if err := w.runTranscribe(context.Background(), job); err != nil {
		t.Fatalf("runTranscribe() erro inesperado: %v", err)
	}

	rawAbsPath := filepath.Join(storageRoot, "aulas", "2026", "aula-01.transcript.json")
	rawContent, err := os.ReadFile(rawAbsPath)
	if err != nil {
		t.Fatalf("JSON bruto não foi gravado em %q: %v", rawAbsPath, err)
	}
	if string(rawContent) != `{"raw":true}` {
		t.Errorf("conteúdo do JSON bruto = %q, esperado {\"raw\":true}", rawContent)
	}

	has, err := db.HasTranscript(conn, lessonID)
	if err != nil {
		t.Fatalf("HasTranscript() erro inesperado: %v", err)
	}
	if !has {
		t.Error("HasTranscript() = false após runTranscribe, esperado true")
	}

	var rawPath, utterancesJSON string
	if err := conn.QueryRow(`SELECT raw_json_path, utterances FROM transcripts WHERE lesson_id = ?`, lessonID).Scan(&rawPath, &utterancesJSON); err != nil {
		t.Fatalf("select em transcripts falhou: %v", err)
	}
	if rawPath != "aulas/2026/aula-01.transcript.json" {
		t.Errorf("raw_json_path = %q, esperado aulas/2026/aula-01.transcript.json", rawPath)
	}
	var utterances []stt.Utterance
	if err := json.Unmarshal([]byte(utterancesJSON), &utterances); err != nil {
		t.Fatalf("utterances gravado não é JSON válido: %v", err)
	}
	if len(utterances) != 1 || utterances[0].Text != "hello" {
		t.Errorf("utterances = %+v, esperado 1 item com Text=hello", utterances)
	}

	if _, err := os.Stat(w.audioPathFor(lessonID)); !os.IsNotExist(err) {
		t.Errorf("WAV do cache deveria ter sido removido após sucesso, err=%v", err)
	}
}
