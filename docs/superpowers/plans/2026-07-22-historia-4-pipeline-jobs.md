# Story 4 — Background Pipeline (Job Queue) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Audio extraction and transcription run on their own in the background once a lesson is
confirmed (Story 3), via a single worker processing a table-backed queue, without blocking the
app.

**Architecture:** New `internal/jobs` package (pure Go, no Wails) with a `Worker` that selects the
next eligible job (respecting `extract_audio → transcribe` precedence and retry backoff), runs it
via injected `media.ExtractAudio`/`stt.Provider`, and reports transitions through a `Notifier`
interface — implemented in `services/` (which already imports Wails) by emitting events.
`storage_root` and the STT provider are resolved on every job (not at Worker construction), because
the first-run wizard only saves that configuration after the app has already started.

**Tech Stack:** Go stdlib (`database/sql`, `context`, `log/slog`, `encoding/json`, `time`),
`modernc.org/sqlite` (already in use via `internal/db`), `internal/media` and `internal/stt` from
Phase 0 (reused as-is).

## Global Constraints

- `internal/` never imports Wails — thin layer (CLAUDE.md, `docs/decisoes-tecnologia.md`).
- Job queue: `jobs` table + single worker, no external job-queue library (decision in effect in
  `docs/decisoes-tecnologia.md`).
- Real idempotency per artifact (not just by status); retry with `attempts` + simple backoff; jobs
  stuck in `running` go back to `pending` when the app opens.
- Transcription failure never prevents watching the video — Phase 1 resilience principle.
- Portable SQL in the repository layer (nothing driver-specific).
- Video paths in the database are always relative to `storage_root` — never absolute.
- Single-line commits, format `type: description` (`feat:`, `fix:`, `test:`, ...).
- `go vet ./...` clean before every commit. `go build ./...` produces a pre-existing, unrelated
  error in the `build/ios` package (Wails scaffold) — to verify the build of what matters here,
  use `go build ./internal/... ./services/... .` (without `build/ios`).
- No database migration in this story — the `jobs` schema already has all the necessary columns.

---

### Task 1: `internal/db` — `Job` model and queries

**Files:**
- Create: `internal/db/jobs.go`
- Test: `internal/db/jobs_test.go`

**Interfaces:**
- Produces: `type Job struct { ID, LessonID int64; Kind, Status, LastError, CreatedAt, UpdatedAt string; Attempts int }`;
  `func ListPendingJobs(conn *sql.DB) ([]Job, error)`; `func FindJob(conn *sql.DB, lessonID int64, kind string) (*Job, error)`;
  `func MarkJobRunning(conn *sql.DB, id int64) error`; `func MarkJobDone(conn *sql.DB, id int64) error`;
  `func MarkJobRetryOrError(conn *sql.DB, id int64, lastError string, maxAttempts int) (string, int, error)`;
  `func MarkJobBlocked(conn *sql.DB, id int64, reason string) error`;
  `func RequeueRunningJobs(conn *sql.DB) (int64, error)`.
  Status is always one of these strings: `"pending"`, `"running"`, `"done"`, `"error"`.

- [ ] **Step 1: Write the tests (they'll fail due to the missing implementation)**

```go
// internal/db/jobs_test.go
package db

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func mustInsertLessonForJobs(t *testing.T, conn *sql.DB, videoPath string) int64 {
	t.Helper()
	res, err := conn.Exec(
		`INSERT INTO lessons (lesson_date, tutor, video_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"2026-07-22", "Fulano", videoPath, "2026-07-22T09:00:00Z", "2026-07-22T09:00:00Z",
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

func mustInsertJob(t *testing.T, conn *sql.DB, lessonID int64, kind, status string, attempts int, createdAt, updatedAt string) int64 {
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

func TestListPendingJobs_OrdersByCreatedAtAscending(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	mustInsertJob(t, conn, lessonID, "transcribe", "pending", 0, "2026-07-22T10:00:05Z", "2026-07-22T10:00:05Z")
	olderID := mustInsertJob(t, conn, lessonID, "extract_audio", "pending", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")

	jobs, err := ListPendingJobs(conn)
	if err != nil {
		t.Fatalf("ListPendingJobs() erro inesperado: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("ListPendingJobs() = %d jobs, esperado 2", len(jobs))
	}
	if jobs[0].ID != olderID {
		t.Errorf("jobs[0].ID = %d, esperado %d (mais antigo primeiro)", jobs[0].ID, olderID)
	}
}

func TestFindJob_NotFoundReturnsNilNil(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	job, err := FindJob(conn, lessonID, "extract_audio")
	if err != nil {
		t.Fatalf("FindJob() erro inesperado: %v", err)
	}
	if job != nil {
		t.Errorf("FindJob() = %+v, esperado nil", job)
	}
}

func TestMarkJobRunning_ClaimsPendingJobAndRejectsNonPending(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	id := mustInsertJob(t, conn, lessonID, "extract_audio", "pending", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")

	if err := MarkJobRunning(conn, id); err != nil {
		t.Fatalf("MarkJobRunning() erro inesperado: %v", err)
	}
	job, err := FindJob(conn, lessonID, "extract_audio")
	if err != nil {
		t.Fatalf("FindJob() erro inesperado: %v", err)
	}
	if job.Status != "running" {
		t.Errorf("job.Status = %q, esperado running", job.Status)
	}

	if err := MarkJobRunning(conn, id); err == nil {
		t.Error("MarkJobRunning() num job já running deveria falhar, veio nil")
	}
}

func TestMarkJobDone_SetsStatusDoneAndClearsError(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	id := mustInsertJob(t, conn, lessonID, "extract_audio", "running", 1, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")
	if _, err := conn.Exec(`UPDATE jobs SET last_error = 'erro anterior' WHERE id = ?`, id); err != nil {
		t.Fatalf("preparar last_error de fixture falhou: %v", err)
	}

	if err := MarkJobDone(conn, id); err != nil {
		t.Fatalf("MarkJobDone() erro inesperado: %v", err)
	}
	job, err := FindJob(conn, lessonID, "extract_audio")
	if err != nil {
		t.Fatalf("FindJob() erro inesperado: %v", err)
	}
	if job.Status != "done" || job.LastError != "" {
		t.Errorf("job = %+v, esperado status done e last_error vazio", job)
	}
}

func TestMarkJobRetryOrError_RetriesUntilMaxAttemptsThenTerminal(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	id := mustInsertJob(t, conn, lessonID, "extract_audio", "running", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")

	status, attempts, err := MarkJobRetryOrError(conn, id, "falha 1", 3)
	if err != nil {
		t.Fatalf("MarkJobRetryOrError() erro inesperado: %v", err)
	}
	if status != "pending" || attempts != 1 {
		t.Errorf("1ª falha: status=%q attempts=%d, esperado pending/1", status, attempts)
	}

	status, attempts, err = MarkJobRetryOrError(conn, id, "falha 2", 3)
	if err != nil {
		t.Fatalf("MarkJobRetryOrError() erro inesperado: %v", err)
	}
	if status != "pending" || attempts != 2 {
		t.Errorf("2ª falha: status=%q attempts=%d, esperado pending/2", status, attempts)
	}

	status, attempts, err = MarkJobRetryOrError(conn, id, "falha 3", 3)
	if err != nil {
		t.Fatalf("MarkJobRetryOrError() erro inesperado: %v", err)
	}
	if status != "error" || attempts != 3 {
		t.Errorf("3ª falha: status=%q attempts=%d, esperado error/3 (terminal)", status, attempts)
	}
}

func TestMarkJobBlocked_SetsErrorWithoutIncrementingAttempts(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	id := mustInsertJob(t, conn, lessonID, "transcribe", "pending", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")

	if err := MarkJobBlocked(conn, id, "depende de extract_audio que falhou"); err != nil {
		t.Fatalf("MarkJobBlocked() erro inesperado: %v", err)
	}
	job, err := FindJob(conn, lessonID, "transcribe")
	if err != nil {
		t.Fatalf("FindJob() erro inesperado: %v", err)
	}
	if job.Status != "error" || job.Attempts != 0 || job.LastError != "depende de extract_audio que falhou" {
		t.Errorf("job = %+v, esperado status=error attempts=0 last_error preenchido", job)
	}
}

func TestRequeueRunningJobs_MovesRunningBackToPendingWithoutIncrementingAttempts(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	stuckID := mustInsertJob(t, conn, lessonID, "extract_audio", "running", 1, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")
	doneID := mustInsertJob(t, conn, lessonID, "transcribe", "done", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")

	n, err := RequeueRunningJobs(conn)
	if err != nil {
		t.Fatalf("RequeueRunningJobs() erro inesperado: %v", err)
	}
	if n != 1 {
		t.Errorf("RequeueRunningJobs() = %d, esperado 1", n)
	}

	stuck, err := FindJob(conn, lessonID, "extract_audio")
	if err != nil {
		t.Fatalf("FindJob() erro inesperado: %v", err)
	}
	if stuck.ID != stuckID || stuck.Status != "pending" || stuck.Attempts != 1 {
		t.Errorf("job requeued = %+v, esperado status=pending attempts=1 (inalterado)", stuck)
	}

	done, err := FindJob(conn, lessonID, "transcribe")
	if err != nil {
		t.Fatalf("FindJob() erro inesperado: %v", err)
	}
	if done.ID != doneID || done.Status != "done" {
		t.Errorf("job done = %+v, não deveria ser afetado pelo requeue", done)
	}
}
```

- [ ] **Step 2: Run the tests and confirm they fail (package doesn't compile — functions don't exist)**

Run: `go test ./internal/db/... -run TestListPendingJobs -v`
Expected: FAIL — `undefined: ListPendingJobs` (compilation error)

- [ ] **Step 3: Implement `internal/db/jobs.go`**

```go
// internal/db/jobs.go
package db

import (
	"database/sql"
	"fmt"
	"time"
)

// Job is a row in the jobs table — see the table-backed queue + single
// worker decision in docs/decisoes-tecnologia.md and Story 4 in
// docs/fase-1-mvp.md. Status is always one of "pending", "running", "done",
// "error".
type Job struct {
	ID        int64
	LessonID  int64
	Kind      string
	Status    string
	Attempts  int
	LastError string
	CreatedAt string
	UpdatedAt string
}

// ListPendingJobs lists "pending" jobs, oldest first — this is the FIFO
// order internal/jobs.Worker uses to pick the next job to process.
func ListPendingJobs(conn *sql.DB) ([]Job, error) {
	rows, err := conn.Query(
		`SELECT id, lesson_id, kind, status, attempts, COALESCE(last_error, ''), created_at, updated_at FROM jobs WHERE status = 'pending' ORDER BY created_at ASC, id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("listar jobs pendentes: %w", err)
	}
	defer rows.Close()

	var out []Job
	for rows.Next() {
		var j Job
		if err := rows.Scan(&j.ID, &j.LessonID, &j.Kind, &j.Status, &j.Attempts, &j.LastError, &j.CreatedAt, &j.UpdatedAt); err != nil {
			return nil, fmt.Errorf("ler job: %w", err)
		}
		out = append(out, j)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterar jobs pendentes: %w", err)
	}
	return out, nil
}

// FindJob looks up the job of the given kind (e.g. "extract_audio") for
// lessonID. Returns (nil, nil) if there isn't one — used by the Worker to
// check the precedence of transcribe over extract_audio.
func FindJob(conn *sql.DB, lessonID int64, kind string) (*Job, error) {
	var j Job
	err := conn.QueryRow(
		`SELECT id, lesson_id, kind, status, attempts, COALESCE(last_error, ''), created_at, updated_at FROM jobs WHERE lesson_id = ? AND kind = ?`,
		lessonID, kind,
	).Scan(&j.ID, &j.LessonID, &j.Kind, &j.Status, &j.Attempts, &j.LastError, &j.CreatedAt, &j.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("buscar job %s da lesson %d: %w", kind, lessonID, err)
	}
	return &j, nil
}

// MarkJobRunning claims a pending job, setting status="running". Fails if
// the job is no longer pending — shouldn't happen with Phase 1's single
// worker, but avoids a silent race if that changes.
func MarkJobRunning(conn *sql.DB, id int64) error {
	res, err := conn.Exec(
		`UPDATE jobs SET status = 'running', updated_at = ? WHERE id = ? AND status = 'pending'`,
		time.Now().UTC().Format(time.RFC3339), id,
	)
	if err != nil {
		return fmt.Errorf("marcar job %d como running: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("confirmar marcação de job %d como running: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("job %d não estava pending — não pode ser reivindicado", id)
	}
	return nil
}

// MarkJobDone marks a job as successfully completed.
func MarkJobDone(conn *sql.DB, id int64) error {
	_, err := conn.Exec(
		`UPDATE jobs SET status = 'done', last_error = NULL, updated_at = ? WHERE id = ?`,
		time.Now().UTC().Format(time.RFC3339), id,
	)
	if err != nil {
		return fmt.Errorf("marcar job %d como done: %w", id, err)
	}
	return nil
}

// MarkJobRetryOrError records a job's execution failure: increments
// attempts and stores lastError. If the new attempts total is still less
// than maxAttempts, the job goes back to "pending" (the Worker retries
// after the backoff); otherwise it becomes "error" — terminal, only
// reprocessed manually. Returns the new status and the new attempts total.
func MarkJobRetryOrError(conn *sql.DB, id int64, lastError string, maxAttempts int) (string, int, error) {
	var attempts int
	if err := conn.QueryRow(`SELECT attempts FROM jobs WHERE id = ?`, id).Scan(&attempts); err != nil {
		return "", 0, fmt.Errorf("ler attempts do job %d: %w", id, err)
	}
	attempts++
	status := "pending"
	if attempts >= maxAttempts {
		status = "error"
	}
	_, err := conn.Exec(
		`UPDATE jobs SET status = ?, attempts = ?, last_error = ?, updated_at = ? WHERE id = ?`,
		status, attempts, lastError, time.Now().UTC().Format(time.RFC3339), id,
	)
	if err != nil {
		return "", 0, fmt.Errorf("registrar falha do job %d: %w", id, err)
	}
	return status, attempts, nil
}

// MarkJobBlocked marks a job as "error" without running it and without
// incrementing attempts — used when its dependency (e.g. the extract_audio
// of a transcribe) has already failed definitively, so running the job
// wouldn't make sense.
func MarkJobBlocked(conn *sql.DB, id int64, reason string) error {
	_, err := conn.Exec(
		`UPDATE jobs SET status = 'error', last_error = ?, updated_at = ? WHERE id = ?`,
		reason, time.Now().UTC().Format(time.RFC3339), id,
	)
	if err != nil {
		return fmt.Errorf("bloquear job %d: %w", id, err)
	}
	return nil
}

// RequeueRunningJobs moves every "running" job back to "pending" — called
// once on Worker startup to cover a crash/kill in the middle of a job.
// attempts isn't incremented: the interruption wasn't an execution
// failure. Returns how many jobs were requeued.
func RequeueRunningJobs(conn *sql.DB) (int64, error) {
	res, err := conn.Exec(
		`UPDATE jobs SET status = 'pending', updated_at = ? WHERE status = 'running'`,
		time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return 0, fmt.Errorf("requeue de jobs running: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("confirmar requeue de jobs running: %w", err)
	}
	return n, nil
}
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `go test ./internal/db/... -v`
Expected: PASS on all tests in `jobs_test.go` and the existing ones in `internal/db`.

- [ ] **Step 5: `go vet` and commit**

```bash
go vet ./internal/db/...
git add internal/db/jobs.go internal/db/jobs_test.go
git commit -m "feat: add Job model and queries for the background queue"
```

---

### Task 2: `internal/db` — find lesson by id and persist transcript

**Files:**
- Modify: `internal/db/lessons.go` (add `FindLessonByID` after `FindLessonByHash`)
- Modify: `internal/db/lessons_test.go` (add test)
- Create: `internal/db/transcripts.go`
- Test: `internal/db/transcripts_test.go`

**Interfaces:**
- Produces: `func FindLessonByID(conn *sql.DB, id int64) (*Lesson, error)`;
  `func HasTranscript(conn *sql.DB, lessonID int64) (bool, error)`;
  `func InsertTranscript(conn *sql.DB, lessonID int64, rawJSONPath string, utterancesJSON string) error`.
- Consumes: `type Lesson struct` from `internal/db/lessons.go` (already exists, has a relative
  `VideoPath`).

- [ ] **Step 1: Write the `FindLessonByID` test (will fail due to the missing implementation)**

Add at the end of `internal/db/lessons_test.go`:

```go
func TestFindLessonByID_FindsExistingAndNilWhenMissing(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	res, err := conn.Exec(
		`INSERT INTO lessons (lesson_date, tutor, video_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"2026-07-15", "Sarah", "aula-01.mp4", "2026-07-15T10:00:00Z", "2026-07-15T10:00:00Z",
	)
	if err != nil {
		t.Fatalf("insert de fixture falhou: %v", err)
	}
	id, _ := res.LastInsertId()

	found, err := FindLessonByID(conn, id)
	if err != nil {
		t.Fatalf("FindLessonByID() erro inesperado: %v", err)
	}
	if found == nil || found.VideoPath != "aula-01.mp4" {
		t.Errorf("FindLessonByID() = %+v, esperado video_path aula-01.mp4", found)
	}

	missing, err := FindLessonByID(conn, id+999)
	if err != nil {
		t.Fatalf("FindLessonByID() erro inesperado: %v", err)
	}
	if missing != nil {
		t.Errorf("FindLessonByID() para id inexistente = %+v, esperado nil", missing)
	}
}
```

- [ ] **Step 2: Run and confirm the compilation failure**

Run: `go test ./internal/db/... -run TestFindLessonByID -v`
Expected: FAIL — `undefined: FindLessonByID`

- [ ] **Step 3: Implement `FindLessonByID`**

Add to `internal/db/lessons.go`, right after the `FindLessonByHash` function:

```go
// FindLessonByID looks up the lesson by id. Returns (nil, nil) if there isn't one.
func FindLessonByID(conn *sql.DB, id int64) (*Lesson, error) {
	var l Lesson
	err := conn.QueryRow(
		`SELECT id, lesson_date, tutor, video_path, COALESCE(video_hash, ''), COALESCE(file_size, 0), COALESCE(file_mtime, '') FROM lessons WHERE id = ?`,
		id,
	).Scan(&l.ID, &l.LessonDate, &l.Tutor, &l.VideoPath, &l.VideoHash, &l.FileSize, &l.FileMTime)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("buscar lesson por id: %w", err)
	}
	return &l, nil
}
```

- [ ] **Step 4: Run and confirm it passes**

Run: `go test ./internal/db/... -run TestFindLessonByID -v`
Expected: PASS

- [ ] **Step 5: Write the transcripts tests (will fail due to the missing implementation)**

```go
// internal/db/transcripts_test.go
package db

import (
	"path/filepath"
	"testing"
)

func TestHasTranscript_FalseThenTrueAfterInsert(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	res, err := conn.Exec(
		`INSERT INTO lessons (lesson_date, tutor, video_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"2026-07-22", "Fulano", "aula.mp4", "2026-07-22T09:00:00Z", "2026-07-22T09:00:00Z",
	)
	if err != nil {
		t.Fatalf("inserir lesson de fixture falhou: %v", err)
	}
	lessonID, _ := res.LastInsertId()

	has, err := HasTranscript(conn, lessonID)
	if err != nil {
		t.Fatalf("HasTranscript() erro inesperado: %v", err)
	}
	if has {
		t.Error("HasTranscript() = true antes de inserir, esperado false")
	}

	if err := InsertTranscript(conn, lessonID, "aula.transcript.json", `[{"speaker":"speaker_0","text":"hi"}]`); err != nil {
		t.Fatalf("InsertTranscript() erro inesperado: %v", err)
	}

	has, err = HasTranscript(conn, lessonID)
	if err != nil {
		t.Fatalf("HasTranscript() erro inesperado: %v", err)
	}
	if !has {
		t.Error("HasTranscript() = false após inserir, esperado true")
	}
}

func TestInsertTranscript_PersistsRawPathAndUtterances(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	res, err := conn.Exec(
		`INSERT INTO lessons (lesson_date, tutor, video_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"2026-07-22", "Fulano", "aula.mp4", "2026-07-22T09:00:00Z", "2026-07-22T09:00:00Z",
	)
	if err != nil {
		t.Fatalf("inserir lesson de fixture falhou: %v", err)
	}
	lessonID, _ := res.LastInsertId()

	if err := InsertTranscript(conn, lessonID, "aula.transcript.json", `[{"speaker":"speaker_0","text":"hi"}]`); err != nil {
		t.Fatalf("InsertTranscript() erro inesperado: %v", err)
	}

	var rawPath, utterances string
	err = conn.QueryRow(`SELECT raw_json_path, utterances FROM transcripts WHERE lesson_id = ?`, lessonID).Scan(&rawPath, &utterances)
	if err != nil {
		t.Fatalf("select em transcripts falhou: %v", err)
	}
	if rawPath != "aula.transcript.json" {
		t.Errorf("raw_json_path = %q, esperado aula.transcript.json", rawPath)
	}
	if utterances != `[{"speaker":"speaker_0","text":"hi"}]` {
		t.Errorf("utterances = %q, não bate com o que foi inserido", utterances)
	}
}
```

- [ ] **Step 6: Run and confirm the compilation failure**

Run: `go test ./internal/db/... -run TestHasTranscript -v`
Expected: FAIL — `undefined: HasTranscript`

- [ ] **Step 7: Implement `internal/db/transcripts.go`**

```go
// internal/db/transcripts.go
package db

import (
	"database/sql"
	"fmt"
	"time"
)

// HasTranscript reports whether a transcript is already stored for
// lessonID — used by internal/jobs.Worker for the transcribe job's
// idempotency.
func HasTranscript(conn *sql.DB, lessonID int64) (bool, error) {
	var id int64
	err := conn.QueryRow(`SELECT id FROM transcripts WHERE lesson_id = ?`, lessonID).Scan(&id)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("buscar transcript da lesson %d: %w", lessonID, err)
	}
	return true, nil
}

// InsertTranscript stores a lesson's transcript. rawJSONPath is relative
// to storage_root (same convention as lessons.video_path); utterancesJSON
// arrives already serialized ([]stt.Utterance as JSON).
func InsertTranscript(conn *sql.DB, lessonID int64, rawJSONPath string, utterancesJSON string) error {
	_, err := conn.Exec(
		`INSERT INTO transcripts (lesson_id, raw_json_path, utterances, created_at) VALUES (?, ?, ?, ?)`,
		lessonID, rawJSONPath, utterancesJSON, time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("inserir transcript da lesson %d: %w", lessonID, err)
	}
	return nil
}
```

- [ ] **Step 8: Run all `internal/db` tests and confirm they pass**

Run: `go test ./internal/db/... -v`
Expected: PASS on all.

- [ ] **Step 9: `go vet` and commit**

```bash
go vet ./internal/db/...
git add internal/db/lessons.go internal/db/lessons_test.go internal/db/transcripts.go internal/db/transcripts_test.go
git commit -m "feat: add FindLessonByID and transcript persistence"
```

---

### Task 3: `internal/config` — audio cache directory

**Files:**
- Modify: `internal/config/paths.go` (add `AudioCacheDir`)
- Modify: `internal/config/config_test.go` (add test)

**Interfaces:**
- Produces: `func AudioCacheDir() (string, error)` — resolves and creates `AppDataDir()/audio-cache`.

- [ ] **Step 1: Write the test (will fail due to the missing implementation)**

Add at the end of `internal/config/config_test.go`:

```go
func TestAudioCacheDir_IsUnderAppDataDirAudioCacheSubdirAndCreated(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	appDir, err := AppDataDir()
	if err != nil {
		t.Fatalf("AppDataDir() erro inesperado: %v", err)
	}
	cacheDir, err := AudioCacheDir()
	if err != nil {
		t.Fatalf("AudioCacheDir() erro inesperado: %v", err)
	}
	want := filepath.Join(appDir, "audio-cache")
	if cacheDir != want {
		t.Errorf("AudioCacheDir() = %q, esperado %q", cacheDir, want)
	}
	info, err := os.Stat(cacheDir)
	if err != nil {
		t.Fatalf("diretório não foi criado: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("%q não é um diretório", cacheDir)
	}
}
```

- [ ] **Step 2: Run and confirm the compilation failure**

Run: `go test ./internal/config/... -run TestAudioCacheDir -v`
Expected: FAIL — `undefined: AudioCacheDir`

- [ ] **Step 3: Implement `AudioCacheDir` in `internal/config/paths.go`**

Add at the end of the file (the `fmt`, `os`, `path/filepath` imports already exist in the file):

```go
// AudioCacheDir resolves (creating it if needed) the intermediate audio
// cache directory (WAVs extracted to call the STT API) inside AppDataDir —
// outside the synced folder, since these files are disposable as soon as
// the transcript is saved (see internal/jobs).
func AudioCacheDir() (string, error) {
	dir, err := AppDataDir()
	if err != nil {
		return "", err
	}
	cacheDir := filepath.Join(dir, "audio-cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", fmt.Errorf("criar diretório de cache de áudio: %w", err)
	}
	return cacheDir, nil
}
```

- [ ] **Step 4: Run all `internal/config` tests and confirm they pass**

Run: `go test ./internal/config/... -v`
Expected: PASS on all.

- [ ] **Step 5: `go vet` and commit**

```bash
go vet ./internal/config/...
git add internal/config/paths.go internal/config/config_test.go
git commit -m "feat: add AudioCacheDir for the worker's audio cache"
```

---

### Task 4: `internal/jobs` — Worker: construction, selection, precedence and backoff

**Files:**
- Create: `internal/jobs/worker.go`
- Test: `internal/jobs/worker_test.go`

**Interfaces:**
- Consumes: `db.Job`, `db.ListPendingJobs`, `db.FindJob`, `db.MarkJobRunning`, `db.MarkJobBlocked`,
  `db.MarkJobDone`, `db.MarkJobRetryOrError`, `db.RequeueRunningJobs`, `db.FindLessonByID`,
  `db.HasTranscript`, `db.InsertTranscript` (Tasks 1–2); `stt.Provider` and `stt.Result`/`stt.Utterance`
  (`internal/stt/stt.go`, already existing).
- Produces: `type MediaExtractorFunc func(ctx context.Context, videoPath, outputPath string) error`;
  `type StorageRootResolver func() (string, error)`; `type STTProviderFactory func() (stt.Provider, error)`;
  `type Notifier interface { JobChanged(JobEvent) }`; `type JobEvent struct { LessonID int64; Kind, Status, LastError string; Attempts int }`;
  `type Option func(*Worker)`; `func WithPollInterval(d time.Duration) Option`; `func WithLogger(l *slog.Logger) Option`;
  `func NewWorker(conn *sql.DB, storageRoot StorageRootResolver, audioCacheDir string, extractAudio MediaExtractorFunc, sttFactory STTProviderFactory, notifier Notifier, opts ...Option) *Worker`;
  method `(*Worker) Run(ctx context.Context) error`; method `(*Worker) Wake()`.
  Unexported methods used by Tasks 5–6: `claimNextEligibleJob`, `eligibleForRetry` (free function),
  `process`, `fail`, `runExtractAudio`, `runTranscribe`, `audioPathFor`, `rawJSONRelPath`.

- [ ] **Step 1: Write the selection/precedence tests (will fail — the package doesn't exist)**

```go
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
```

- [ ] **Step 2: Run and confirm the compilation failure**

Run: `go test ./internal/jobs/... -v`
Expected: FAIL — the `internal/jobs` package doesn't compile (`NewWorker`, `noopNotifier` etc. undefined)

- [ ] **Step 3: Implement `internal/jobs/worker.go`**

```go
// internal/jobs/worker.go
package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"assistente-idiomas/internal/db"
	"assistente-idiomas/internal/stt"
)

// maxAttempts is the total number of attempts (the first try + retries)
// before a failing job becomes terminal "error" — see docs/fase-1-mvp.md
// (Story 4).
const maxAttempts = 3

// backoff maps attempts (already incremented after a failure) to the
// minimum wait time before the next attempt.
var backoff = map[int]time.Duration{
	1: 10 * time.Second,
	2: 60 * time.Second,
	3: 5 * time.Minute,
}

const defaultPollInterval = 5 * time.Second

// MediaExtractorFunc has the same signature as media.ExtractAudio — lets
// tests inject a fake without depending on ffmpeg.
type MediaExtractorFunc func(ctx context.Context, videoPath, outputPath string) error

// StorageRootResolver resolves the absolute path of the storage folder.
// Re-evaluated on every job (not stored as a fixed value at Worker
// construction) because the first-run wizard saves this configuration
// after the app (and the Worker) have already started — see the Story 4
// spec in docs/superpowers/specs/.
type StorageRootResolver func() (string, error)

// STTProviderFactory builds (or returns) the stt.Provider to use. Same
// reason as StorageRootResolver: the STT credential only exists after the
// wizard.
type STTProviderFactory func() (stt.Provider, error)

// Notifier is notified on every job status transition. The real
// implementation (which emits Wails events) lives in services/ —
// internal/jobs doesn't import Wails (thin layer).
type Notifier interface {
	JobChanged(JobEvent)
}

// JobEvent is the payload passed to the Notifier on every transition.
type JobEvent struct {
	LessonID  int64
	Kind      string
	Status    string
	Attempts  int
	LastError string
}

type noopNotifier struct{}

func (noopNotifier) JobChanged(JobEvent) {}

// Worker processes the jobs queue (jobs table) sequentially, one at a
// time — see the "single worker" decision in docs/decisoes-tecnologia.md.
type Worker struct {
	conn          *sql.DB
	storageRoot   StorageRootResolver
	audioCacheDir string
	extractAudio  MediaExtractorFunc
	sttFactory    STTProviderFactory
	notifier      Notifier
	logger        *slog.Logger
	pollInterval  time.Duration
	wake          chan struct{}
}

// Option customizes a Worker at construction — used in tests to shorten
// pollInterval.
type Option func(*Worker)

func WithPollInterval(d time.Duration) Option {
	return func(w *Worker) { w.pollInterval = d }
}

func WithLogger(l *slog.Logger) Option {
	return func(w *Worker) { w.logger = l }
}

func NewWorker(
	conn *sql.DB,
	storageRoot StorageRootResolver,
	audioCacheDir string,
	extractAudio MediaExtractorFunc,
	sttFactory STTProviderFactory,
	notifier Notifier,
	opts ...Option,
) *Worker {
	if notifier == nil {
		notifier = noopNotifier{}
	}
	w := &Worker{
		conn:          conn,
		storageRoot:   storageRoot,
		audioCacheDir: audioCacheDir,
		extractAudio:  extractAudio,
		sttFactory:    sttFactory,
		notifier:      notifier,
		logger:        slog.Default(),
		pollInterval:  defaultPollInterval,
		wake:          make(chan struct{}, 1),
	}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

// Wake signals the Worker that there's a new job to look at, without
// waiting for the next fallback poll tick. Non-blocking — if a signal is
// already pending, this one is dropped (the worker will wake up anyway).
func (w *Worker) Wake() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// Run blocks processing jobs until ctx is canceled. On startup, it
// requeues any job stuck in "running" (previous crash/kill).
func (w *Worker) Run(ctx context.Context) error {
	if _, err := db.RequeueRunningJobs(w.conn); err != nil {
		return fmt.Errorf("jobs: requeue de jobs presos em running: %w", err)
	}
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()
	for {
		for {
			job, err := w.claimNextEligibleJob()
			if err != nil {
				w.logger.Error("jobs: erro ao selecionar próximo job", "erro", err)
				break
			}
			if job == nil {
				break
			}
			w.process(ctx, *job)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-w.wake:
		case <-ticker.C:
		}
	}
}

// claimNextEligibleJob picks the next eligible pending job (respecting
// backoff and the precedence of transcribe over extract_audio) and marks
// it running. Returns (nil, nil) if nothing is eligible right now.
func (w *Worker) claimNextEligibleJob() (*db.Job, error) {
	pending, err := db.ListPendingJobs(w.conn)
	if err != nil {
		return nil, fmt.Errorf("listar jobs pendentes: %w", err)
	}
	now := time.Now().UTC()
	for _, j := range pending {
		if j.Kind == "transcribe" {
			sibling, err := db.FindJob(w.conn, j.LessonID, "extract_audio")
			if err != nil {
				return nil, fmt.Errorf("buscar job extract_audio da lesson %d: %w", j.LessonID, err)
			}
			if sibling == nil {
				continue
			}
			if sibling.Status == "error" {
				reason := fmt.Sprintf("depende de extract_audio que falhou: %s", sibling.LastError)
				if err := db.MarkJobBlocked(w.conn, j.ID, reason); err != nil {
					return nil, fmt.Errorf("bloquear job transcribe %d: %w", j.ID, err)
				}
				w.notifier.JobChanged(JobEvent{LessonID: j.LessonID, Kind: j.Kind, Status: "error", Attempts: j.Attempts, LastError: reason})
				continue
			}
			if sibling.Status != "done" {
				continue
			}
		}
		if !eligibleForRetry(j, now) {
			continue
		}
		if err := db.MarkJobRunning(w.conn, j.ID); err != nil {
			return nil, fmt.Errorf("reivindicar job %d: %w", j.ID, err)
		}
		claimed := j
		claimed.Status = "running"
		w.notifier.JobChanged(JobEvent{LessonID: claimed.LessonID, Kind: claimed.Kind, Status: "running", Attempts: claimed.Attempts})
		return &claimed, nil
	}
	return nil, nil
}

// eligibleForRetry reports whether j has already passed the backoff
// window of its last attempt (if attempts == 0, it's the first attempt:
// always eligible).
func eligibleForRetry(j db.Job, now time.Time) bool {
	if j.Attempts == 0 {
		return true
	}
	wait, ok := backoff[j.Attempts]
	if !ok {
		wait = backoff[maxAttempts]
	}
	updatedAt, err := time.Parse(time.RFC3339, j.UpdatedAt)
	if err != nil {
		return true
	}
	return now.After(updatedAt.Add(wait))
}

// process runs job (already marked running) and records the result.
func (w *Worker) process(ctx context.Context, job db.Job) {
	var err error
	switch job.Kind {
	case "extract_audio":
		err = w.runExtractAudio(ctx, job)
	case "transcribe":
		err = w.runTranscribe(ctx, job)
	default:
		err = fmt.Errorf("kind de job desconhecido: %s", job.Kind)
	}
	if err != nil {
		w.fail(job, err)
		return
	}
	if markErr := db.MarkJobDone(w.conn, job.ID); markErr != nil {
		w.logger.Error("jobs: erro ao marcar job como done", "job_id", job.ID, "erro", markErr)
		return
	}
	w.notifier.JobChanged(JobEvent{LessonID: job.LessonID, Kind: job.Kind, Status: "done", Attempts: job.Attempts})
}

// fail records a job's execution failure: increments attempts and decides
// between retry (back to pending) or terminal error.
func (w *Worker) fail(job db.Job, cause error) {
	status, attempts, err := db.MarkJobRetryOrError(w.conn, job.ID, cause.Error(), maxAttempts)
	if err != nil {
		w.logger.Error("jobs: erro ao registrar falha do job", "job_id", job.ID, "erro", err)
		return
	}
	w.notifier.JobChanged(JobEvent{LessonID: job.LessonID, Kind: job.Kind, Status: status, Attempts: attempts, LastError: cause.Error()})
}

// runExtractAudio extracts the lesson's video audio into the cache
// (audioCacheDir/<lessonID>.wav), skipping if the WAV already exists
// (idempotency).
func (w *Worker) runExtractAudio(ctx context.Context, job db.Job) error {
	lesson, err := db.FindLessonByID(w.conn, job.LessonID)
	if err != nil {
		return fmt.Errorf("buscar lesson %d: %w", job.LessonID, err)
	}
	if lesson == nil {
		return fmt.Errorf("lesson %d não encontrada", job.LessonID)
	}
	audioPath := w.audioPathFor(job.LessonID)
	if info, statErr := os.Stat(audioPath); statErr == nil && info.Size() > 0 {
		return nil
	}
	root, err := w.storageRoot()
	if err != nil {
		return fmt.Errorf("resolver storage_root: %w", err)
	}
	videoPath := filepath.Join(root, filepath.FromSlash(lesson.VideoPath))
	if err := w.extractAudio(ctx, videoPath, audioPath); err != nil {
		return fmt.Errorf("extrair áudio: %w", err)
	}
	return nil
}

// runTranscribe transcribes the lesson's cached audio via STT, writing the
// raw JSON alongside the video and the mapped transcript into transcripts.
// Skips if a transcript already exists for this lesson (idempotency).
func (w *Worker) runTranscribe(ctx context.Context, job db.Job) error {
	has, err := db.HasTranscript(w.conn, job.LessonID)
	if err != nil {
		return fmt.Errorf("verificar transcrição existente: %w", err)
	}
	if has {
		return nil
	}
	lesson, err := db.FindLessonByID(w.conn, job.LessonID)
	if err != nil {
		return fmt.Errorf("buscar lesson %d: %w", job.LessonID, err)
	}
	if lesson == nil {
		return fmt.Errorf("lesson %d não encontrada", job.LessonID)
	}
	provider, err := w.sttFactory()
	if err != nil {
		return fmt.Errorf("obter provedor de STT: %w", err)
	}
	audioPath := w.audioPathFor(job.LessonID)
	result, err := provider.Transcribe(ctx, audioPath)
	if err != nil {
		return fmt.Errorf("transcrever: %w", err)
	}
	root, err := w.storageRoot()
	if err != nil {
		return fmt.Errorf("resolver storage_root: %w", err)
	}
	rawRelPath := rawJSONRelPath(lesson.VideoPath)
	rawAbsPath := filepath.Join(root, filepath.FromSlash(rawRelPath))
	if err := os.WriteFile(rawAbsPath, result.RawResponse, 0o644); err != nil {
		return fmt.Errorf("gravar JSON bruto: %w", err)
	}
	utterancesJSON, err := json.Marshal(result.Utterances)
	if err != nil {
		return fmt.Errorf("serializar utterances: %w", err)
	}
	if err := db.InsertTranscript(w.conn, job.LessonID, rawRelPath, string(utterancesJSON)); err != nil {
		return fmt.Errorf("gravar transcript: %w", err)
	}
	if err := os.Remove(audioPath); err != nil && !os.IsNotExist(err) {
		w.logger.Warn("jobs: falha ao remover WAV do cache após transcrição", "path", audioPath, "erro", err)
	}
	return nil
}

func (w *Worker) audioPathFor(lessonID int64) string {
	return filepath.Join(w.audioCacheDir, strconv.FormatInt(lessonID, 10)+".wav")
}

// rawJSONRelPath computes the path (relative to storage_root, always with
// "/") of the provider's raw JSON: same directory as the video, name
// "<basename-without-extension>.transcript.json" — without assuming any
// subfolder (consistent with Story 3's scan).
func rawJSONRelPath(videoRelPath string) string {
	dir := path.Dir(videoRelPath)
	base := strings.TrimSuffix(path.Base(videoRelPath), path.Ext(videoRelPath))
	return path.Join(dir, base+".transcript.json")
}
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `go test ./internal/jobs/... -v`
Expected: PASS on both `worker_test.go` tests.

- [ ] **Step 5: `go vet` and commit**

```bash
go vet ./internal/jobs/...
git add internal/jobs/worker.go internal/jobs/worker_test.go
git commit -m "feat: add Worker with job selection and precedence"
```

---

### Task 5: `internal/jobs` — idempotency and artifacts (extract_audio/transcribe)

**Files:**
- Modify: `internal/jobs/worker_test.go` (add `encoding/json` import and 4 tests)

**Interfaces:**
- Consumes: everything produced in Task 4 (same file/package); `db.InsertTranscript`, `db.HasTranscript`
  (Task 2); `stt.Utterance` (`internal/stt/stt.go`).

- [ ] **Step 1: Add the `encoding/json` import and the 4 tests (they'll "fail" — not because the functions are unused, but because behavior would be incorrect/incomplete without the test)**

Edit the import block at the top of `internal/jobs/worker_test.go`, adding `"encoding/json"`:

```go
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
```

Add at the end of the file:

```go
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
```

- [ ] **Step 2: Run the 4 new tests**

Run: `go test ./internal/jobs/... -run 'TestRunExtractAudio|TestRunTranscribe' -v`
Expected: PASS on all — Task 4's implementation already covers idempotency and artifact
writing, no new production code is needed here, just the test coverage.

- [ ] **Step 3: Run the full `internal/jobs` suite and confirm nothing broke**

Run: `go test ./internal/jobs/... -v`
Expected: PASS on all 6 tests (2 from Task 4 + 4 new).

- [ ] **Step 4: `go vet` and commit**

```bash
go vet ./internal/jobs/...
git add internal/jobs/worker_test.go
git commit -m "test: cover idempotency and artifacts of extract_audio/transcribe"
```

---

### Task 6: `internal/jobs` — retry/backoff, requeue and full `Run` loop

**Files:**
- Modify: `internal/jobs/worker_test.go` (add `errors` import and 4 tests/1 type)

**Interfaces:**
- Consumes: everything from Tasks 4–5 (same package).
- Produces (test-only): `type recordingNotifier struct { events *[]JobEvent }` with a
  `JobChanged(JobEvent)` method, implementing `Notifier` to inspect emitted events.

- [ ] **Step 1: Add the `errors` import and the tests**

Edit the import block at the top of `internal/jobs/worker_test.go`, adding `"errors"`:

```go
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
```

Add at the end of the file:

```go
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
		{"first attempt always eligible", 0, now, true},
		{"1 failure, still within 10s", 1, now.Add(-5 * time.Second), false},
		{"1 failure, past 10s", 1, now.Add(-11 * time.Second), true},
		{"2 failures, still within 60s", 2, now.Add(-30 * time.Second), false},
		{"2 failures, past 60s", 2, now.Add(-61 * time.Second), true},
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
	w.fail(job, errors.New("falha simulada"))
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
```

- [ ] **Step 2: Run the full `internal/jobs` suite and confirm it passes**

Run: `go test ./internal/jobs/... -v`
Expected: PASS on all tests (Tasks 4–6 combined).

- [ ] **Step 3: `go vet` and commit**

```bash
go vet ./internal/jobs/...
git add internal/jobs/worker_test.go
git commit -m "test: cover retry/backoff, requeue and the worker's Run loop"
```

---

### Task 7: `services` — Notifier that emits Wails events

**Files:**
- Create: `services/jobs_notifier.go`

**Interfaces:**
- Consumes: `jobs.Notifier`, `jobs.JobEvent` (`internal/jobs/worker.go`, Task 4).
- Produces: `type WailsJobNotifier struct{}` with a `JobChanged(e jobs.JobEvent)` method;
  `const JobUpdatedEvent = "job:updated"`.

No automated test in this task: `WailsJobNotifier.JobChanged` only makes sense when calling
`application.Get().Event.Emit(...)`, which requires an already-initialized Wails app — the same
reason `main.go` and the `SetupService` methods that use `application.Get().Dialog` have no
automated test in this project. Verification is `go build`/`go vet` (Step 2) plus a manual check
of the event reaching the frontend, once the Queue screen (Story 7) exists to consume it.

- [ ] **Step 1: Create `services/jobs_notifier.go`**

```go
// services/jobs_notifier.go
package services

import (
	"assistente-idiomas/internal/jobs"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// JobUpdatedEvent is the name of the Wails event emitted on every job
// status transition — no screen consumes this yet (that's for Stories 5
// and 7); this story only sets up the transport.
const JobUpdatedEvent = "job:updated"

// WailsJobNotifier implements jobs.Notifier by emitting Wails events — the
// only piece of the pipeline (Story 4) that knows Wails exists.
// internal/jobs itself doesn't import Wails (thin layer).
type WailsJobNotifier struct{}

func (WailsJobNotifier) JobChanged(e jobs.JobEvent) {
	application.Get().Event.Emit(JobUpdatedEvent, e)
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./services/... && go vet ./services/...`
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add services/jobs_notifier.go
git commit -m "feat: add WailsJobNotifier for job events"
```

---

### Task 8: `main.go` — start the worker in the background

**Files:**
- Modify: `main.go`

**Interfaces:**
- Consumes: `jobs.NewWorker`, `jobs.StorageRootResolver`, `jobs.STTProviderFactory` (Task 4);
  `services.WailsJobNotifier` (Task 7); `config.Load`, `config.GetSTTAPIKey`, `config.AudioCacheDir`
  (`internal/config`, already existing + Task 3); `media.ExtractAudio` (`internal/media/media.go`,
  already existing); `stt.NewElevenLabsProvider` (`internal/stt/elevenlabs.go`, already existing).

No automated test — this is `main()` wiring, following the same pattern as the rest of the file
(not tested in this project). Verification via build/vet (Step 2) and, if possible, opening the
app visually.

- [ ] **Step 1: Rewrite `main.go`**

```go
package main

import (
	"context"
	"database/sql"
	"embed"
	"log"

	"assistente-idiomas/internal/config"
	"assistente-idiomas/internal/db"
	"assistente-idiomas/internal/jobs"
	"assistente-idiomas/internal/media"
	"assistente-idiomas/internal/stt"
	"assistente-idiomas/services"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	dbPath, err := config.DBPath()
	if err != nil {
		log.Fatalf("resolver caminho do banco: %v", err)
	}
	conn, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("abrir banco de dados: %v", err)
	}
	defer conn.Close()

	startJobWorker(conn)

	app := application.New(application.Options{
		Name:        "Assistente de Idiomas",
		Description: "Arquivo e análise de aulas de inglês do Cambly",
		Services: []application.Service{
			application.NewService(services.NewSetupService()),
			application.NewService(services.NewImportService(conn)),
			application.NewService(services.NewLibraryService(conn)),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "Assistente de Idiomas",
		Width:            1200,
		Height:           760,
		BackgroundColour: application.NewRGB(20, 24, 31), // #14181F — colors.bg
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

// startJobWorker starts the background pipeline (Story 4) in a goroutine.
// storage_root and the STT credential are resolved on every job, not
// here — the first-run wizard hasn't run yet at this point in startup, so
// resolving them now would always fail on the app's first session (see
// docs/superpowers/specs/2026-07-22-historia-4-pipeline-jobs-design.md).
// Only the audio cache (which doesn't depend on the wizard) is resolved
// here; if that fails, it's a disk/permission problem and the worker
// doesn't start.
func startJobWorker(conn *sql.DB) {
	audioCacheDir, err := config.AudioCacheDir()
	if err != nil {
		log.Printf("worker de jobs não iniciado: %v", err)
		return
	}
	storageRoot := func() (string, error) {
		cfg, err := config.Load()
		if err != nil {
			return "", err
		}
		return cfg.StorageRoot, nil
	}
	sttFactory := func() (stt.Provider, error) {
		apiKey, err := config.GetSTTAPIKey()
		if err != nil {
			return nil, err
		}
		return stt.NewElevenLabsProvider(apiKey)
	}
	worker := jobs.NewWorker(conn, storageRoot, audioCacheDir, media.ExtractAudio, sttFactory, services.WailsJobNotifier{})
	go func() {
		if err := worker.Run(context.Background()); err != nil {
			log.Printf("worker de jobs encerrado: %v", err)
		}
	}()
}
```

- [ ] **Step 2: Verify it compiles and passes `go vet`**

Run: `go build ./internal/... ./services/... . && go vet ./...`
Expected: no errors (the known, unrelated error from `go build ./...` in the Wails scaffold's
`build/ios` package doesn't affect this check, which already excludes that package).

- [ ] **Step 3: Run the project's full test suite**

Run: `go test ./... -v`
Expected: PASS on all packages (`internal/db`, `internal/config`, `internal/jobs`,
`internal/media`, `internal/stt`, `services`, and the other existing ones).

- [ ] **Step 4: Commit**

```bash
git add main.go
git commit -m "feat: start the background job worker on app startup"
```

---

## Pending manual verification (outside the scope of automated tests)

After Task 8, on a machine with a display (Windows/Linux with a GUI — see risk 3 and the visual
verification already logged as pending for Stories 1 and 3 in `docs/fase-1-mvp.md`): run
`wails3 dev`, complete the wizard, confirm a test lesson, and watch in the log/backend that the
`extract_audio`/`transcribe` jobs progress to `done` (or `error` with an invalid API key, without
locking up the app or preventing watching the video). This isn't a step in this plan — it's the
same pending visual verification already noted in `docs/fase-1-mvp.md`, to be done whenever
someone opens the app on a machine with a display.
