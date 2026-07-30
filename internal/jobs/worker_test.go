// internal/jobs/worker_test.go
package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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
	teacherID, err := db.GetOrCreateTeacherByName(conn, "Fulano")
	if err != nil {
		t.Fatalf("GetOrCreateTeacherByName() de fixture falhou: %v", err)
	}
	res, err := conn.Exec(
		`INSERT INTO lessons (lesson_date, teacher_id, video_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"2026-07-22", teacherID, videoPath, now, now,
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

type recordingNotifier struct {
	events *[]JobEvent
}

func (r recordingNotifier) JobChanged(e JobEvent) {
	*r.events = append(*r.events, e)
}

func TestEligibleForRetry_RespectsBackoffWindow(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name     string
		attempts int
		updated  time.Time
		want     bool
	}{
		{"primeira tentativa sempre elegível", 0, now, true},
		{"1 falha, ainda dentro dos 10s", 1, now.Add(-5 * time.Second), false},
		{"1 falha, passou dos 10s", 1, now.Add(-11 * time.Second), true},
		{"2 falhas, ainda dentro de 60s", 2, now.Add(-30 * time.Second), false},
		{"2 falhas, passou de 60s", 2, now.Add(-61 * time.Second), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			j := db.Job{Attempts: tc.attempts, UpdatedAt: tc.updated.Format(time.RFC3339)}
			got := eligibleForRetry(j, now)
			if got != tc.want {
				t.Errorf("eligibleForRetry(attempts=%d, updated=%s) = %v, esperado %v", tc.attempts, tc.updated, got, tc.want)
			}
		})
	}
}

func TestFail_RetriesThenTerminatesAfterMaxAttempts(t *testing.T) {
	conn := newTestDB(t)
	lessonID := insertLesson(t, conn, "aula.mp4")
	job := db.Job{ID: insertJob(t, conn, lessonID, "extract_audio", "running", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z"), LessonID: lessonID, Kind: "extract_audio"}

	var events []JobEvent
	notifier := recordingNotifier{events: &events}
	w := NewWorker(
		conn,
		func() (string, error) { return t.TempDir(), nil },
		t.TempDir(),
		fakeExtractAudioAlwaysOK,
		func() (stt.Provider, error) { return &fakeSTTProvider{}, nil },
		notifier,
	)

	w.fail(job, errors.New("falha simulada"))
	if len(events) != 1 {
		t.Fatalf("eventos após 1ª falha = %+v, esperado 1 evento", events)
	}
	if events[0].Status != "pending" || events[0].Attempts != 1 {
		t.Errorf("eventos[0] = %+v, esperado status=pending attempts=1", events[0])
	}

	w.fail(job, errors.New("falha simulada"))
	if len(events) != 2 {
		t.Fatalf("eventos após 2ª falha = %+v, esperado 2 eventos", events)
	}
	if events[1].Status != "pending" || events[1].Attempts != 2 {
		t.Errorf("eventos[1] = %+v, esperado status=pending attempts=2", events[1])
	}

	w.fail(job, errors.New("falha simulada"))

	got, err := db.FindJob(conn, lessonID, "extract_audio")
	if err != nil {
		t.Fatalf("FindJob() erro inesperado: %v", err)
	}
	if got.Status != "error" || got.Attempts != 3 {
		t.Errorf("job após 3 falhas = %+v, esperado status=error attempts=3", got)
	}
	if len(events) != 3 || events[2].Status != "error" {
		t.Errorf("eventos notificados = %+v, esperado 3 eventos terminando em error", events)
	}
	if events[2].Status != "error" || events[2].Attempts != 3 {
		t.Errorf("eventos[2] = %+v, esperado status=error attempts=3", events[2])
	}
}

func TestRun_RequeuesRunningJobsOnStart(t *testing.T) {
	conn := newTestDB(t)
	lessonID := insertLesson(t, conn, "aula.mp4")
	stuckID := insertJob(t, conn, lessonID, "extract_audio", "running", 1, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")

	w := newTestWorker(t, conn, WithPollInterval(10*time.Millisecond))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_ = w.Run(ctx)

	job, err := db.FindJob(conn, lessonID, "extract_audio")
	if err != nil {
		t.Fatalf("FindJob() erro inesperado: %v", err)
	}
	if job.ID != stuckID {
		t.Fatalf("job errado retornado por FindJob")
	}
	if job.Status == "running" {
		t.Errorf("job.Status = running, esperado que o requeue tivesse tirado do estado preso")
	}
}

func TestRun_ProcessesExtractAudioThenTranscribeEndToEnd(t *testing.T) {
	conn := newTestDB(t)
	storageRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(storageRoot, "aula.mp4"), []byte("video"), 0o644); err != nil {
		t.Fatalf("preparar vídeo de fixture falhou: %v", err)
	}
	lessonID := insertLesson(t, conn, "aula.mp4")
	now := time.Now().UTC().Format(time.RFC3339)
	insertJob(t, conn, lessonID, "extract_audio", "pending", 0, now, now)
	insertJob(t, conn, lessonID, "transcribe", "pending", 0, now, now)

	audioCacheDir := t.TempDir()
	w := NewWorker(
		conn,
		func() (string, error) { return storageRoot, nil },
		audioCacheDir,
		func(ctx context.Context, videoPath, outputPath string) error {
			return os.WriteFile(outputPath, []byte("wav"), 0o644)
		},
		func() (stt.Provider, error) {
			return &fakeSTTProvider{result: &stt.Result{RawResponse: []byte(`{}`), Utterances: []stt.Utterance{{Speaker: "speaker_0", Text: "oi"}}}}, nil
		},
		noopNotifier{},
		WithPollInterval(10*time.Millisecond),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = w.Run(ctx)

	extractJob, err := db.FindJob(conn, lessonID, "extract_audio")
	if err != nil {
		t.Fatalf("FindJob() erro inesperado: %v", err)
	}
	if extractJob.Status != "done" {
		t.Errorf("extract_audio.Status = %q, esperado done", extractJob.Status)
	}
	transcribeJob, err := db.FindJob(conn, lessonID, "transcribe")
	if err != nil {
		t.Fatalf("FindJob() erro inesperado: %v", err)
	}
	if transcribeJob.Status != "done" {
		t.Errorf("transcribe.Status = %q, esperado done", transcribeJob.Status)
	}
	has, err := db.HasTranscript(conn, lessonID)
	if err != nil {
		t.Fatalf("HasTranscript() erro inesperado: %v", err)
	}
	if !has {
		t.Error("HasTranscript() = false, esperado true após pipeline completo")
	}
}
