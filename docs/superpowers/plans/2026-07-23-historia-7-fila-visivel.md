# Story 7 — Visible Queue — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Queue screen showing lessons with an active/failed pipeline (one row per lesson, stage +
state, readable `last_error`, Retry button) and an active-job count badge in the sidebar,
both updated live by the Wails `job:updated` event (already emitted since Story 4, with no
consumer until now).

**Architecture:** A new query in `internal/db` derives, per lesson, which job (extract_audio or
transcribe) is "active" right now or in error, with a priority that respects the dependency block
between the two jobs. `services.QueueService` exposes this translated for the frontend. On the
frontend, a single reactive store (`jobsStore.svelte.ts`, Svelte 5 runes) fetches the list once
and refetches on every `job:updated`; `Queue.svelte` and the badge in `Sidebar.svelte` only read
from this store, with no duplicate event subscription.

**Tech Stack:** Go (stdlib `database/sql`, `sort`), SQLite via `modernc.org/sqlite` (already
configured), Wails v3 (`application.Service`, `job:updated` event already existing), Svelte 5
(runes), `@wailsio/runtime` (`Events.On`).

## Global Constraints

- Frontend always Svelte 5 with runes (`$state`, `$derived`, `$props`) — never legacy Svelte
  3/4 syntax (`CLAUDE.md`).
- Code and identifiers in English; user-facing text and error messages in PT-BR
  (`CLAUDE.md`).
- `internal/` never imports Wails — only `services/` may (thin-layer principle,
  `CLAUDE.md`).
- SQL in the repository layer (`internal/db`) is portable across drivers — nothing specific to
  `modernc.org/sqlite` (`CLAUDE.md`).
- Commit messages: a single line, semantic format (`type: description`) (`CLAUDE.md`).
- No percentage-progress column — jobs only have status `pending/running/done/error`, no
  incremental progress data (see spec).

Full spec: `docs/superpowers/specs/2026-07-23-historia-7-fila-visivel-design.md`.

---

### Task 1: `internal/db/queue.go` — queue query

**Files:**
- Create: `internal/db/queue.go`
- Test: `internal/db/queue_test.go`

**Interfaces:**
- Consumes: nothing from previous tasks. Reuses `mustInsertLessonForJobs` and `mustInsertJob`,
  already defined in `internal/db/jobs_test.go` (same `db` package, same pattern used by
  `internal/db/lesson_status_test.go`).
- Produces: `type QueueEntry struct { LessonID int64; LessonDate string; Tutor string; Kind
  string; Status string; Attempts int; LastError string; UpdatedAt string }` and `func
  ListQueueEntries(conn *sql.DB) ([]QueueEntry, error)` — used by Task 2.

- [ ] **Step 1: Write the tests (they will fail — `ListQueueEntries` doesn't exist yet)**

Create `internal/db/queue_test.go`:

```go
// internal/db/queue_test.go
package db

import (
	"path/filepath"
	"testing"
)

func TestListQueueEntries_ExtractAudioRunningTakesPriorityOverTranscribePending(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() unexpected error: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	mustInsertJob(t, conn, lessonID, "extract_audio", "running", 0, "2026-07-23T10:00:00Z", "2026-07-23T10:00:00Z")
	mustInsertJob(t, conn, lessonID, "transcribe", "pending", 0, "2026-07-23T10:00:00Z", "2026-07-23T10:00:00Z")

	entries, err := ListQueueEntries(conn)
	if err != nil {
		t.Fatalf("ListQueueEntries() unexpected error: %v", err)
	}
	if len(entries) != 1 || entries[0].Kind != "extract_audio" || entries[0].Status != "running" {
		t.Errorf("ListQueueEntries() = %+v, expected 1 extract_audio/running entry (transcribe still blocked, even though pending in the DB)", entries)
	}
}

func TestListQueueEntries_ExtractAudioErrorIsRootCause(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() unexpected error: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	mustInsertJob(t, conn, lessonID, "extract_audio", "error", 3, "2026-07-23T10:00:00Z", "2026-07-23T10:00:00Z")
	if _, err := conn.Exec(`UPDATE jobs SET last_error = ? WHERE lesson_id = ? AND kind = ?`, "ffmpeg não encontrado", lessonID, "extract_audio"); err != nil {
		t.Fatalf("failed to prepare fixture last_error: %v", err)
	}
	mustInsertJob(t, conn, lessonID, "transcribe", "error", 0, "2026-07-23T10:00:00Z", "2026-07-23T10:00:00Z")
	if _, err := conn.Exec(`UPDATE jobs SET last_error = ? WHERE lesson_id = ? AND kind = ?`, "depende de extract_audio que falhou: ffmpeg não encontrado", lessonID, "transcribe"); err != nil {
		t.Fatalf("failed to prepare fixture last_error: %v", err)
	}

	entries, err := ListQueueEntries(conn)
	if err != nil {
		t.Fatalf("ListQueueEntries() unexpected error: %v", err)
	}
	if len(entries) != 1 || entries[0].Kind != "extract_audio" || entries[0].Status != "error" || entries[0].LastError != "ffmpeg não encontrado" {
		t.Errorf("ListQueueEntries() = %+v, expected extract_audio/error with the root-cause message", entries)
	}
}

func TestListQueueEntries_TranscribeErrorWhenExtractDone(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() unexpected error: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	mustInsertJob(t, conn, lessonID, "extract_audio", "done", 0, "2026-07-23T10:00:00Z", "2026-07-23T10:00:00Z")
	mustInsertJob(t, conn, lessonID, "transcribe", "error", 2, "2026-07-23T10:05:00Z", "2026-07-23T10:05:00Z")
	if _, err := conn.Exec(`UPDATE jobs SET last_error = ? WHERE lesson_id = ? AND kind = ?`, "falha real de STT", lessonID, "transcribe"); err != nil {
		t.Fatalf("failed to prepare fixture last_error: %v", err)
	}

	entries, err := ListQueueEntries(conn)
	if err != nil {
		t.Fatalf("ListQueueEntries() unexpected error: %v", err)
	}
	if len(entries) != 1 || entries[0].Kind != "transcribe" || entries[0].Status != "error" || entries[0].LastError != "falha real de STT" {
		t.Errorf("ListQueueEntries() = %+v, expected transcribe/error", entries)
	}
}

func TestListQueueEntries_TranscribePendingWhenExtractDone(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() unexpected error: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	mustInsertJob(t, conn, lessonID, "extract_audio", "done", 0, "2026-07-23T10:00:00Z", "2026-07-23T10:00:00Z")
	mustInsertJob(t, conn, lessonID, "transcribe", "pending", 0, "2026-07-23T10:05:00Z", "2026-07-23T10:05:00Z")

	entries, err := ListQueueEntries(conn)
	if err != nil {
		t.Fatalf("ListQueueEntries() unexpected error: %v", err)
	}
	if len(entries) != 1 || entries[0].Kind != "transcribe" || entries[0].Status != "pending" {
		t.Errorf("ListQueueEntries() = %+v, expected transcribe/pending (extract_audio already done, transcribe genuinely eligible)", entries)
	}
}

func TestListQueueEntries_ExcludesReadyLessons(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() unexpected error: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	mustInsertJob(t, conn, lessonID, "extract_audio", "done", 0, "2026-07-23T10:00:00Z", "2026-07-23T10:00:00Z")
	mustInsertJob(t, conn, lessonID, "transcribe", "done", 0, "2026-07-23T10:00:00Z", "2026-07-23T10:00:00Z")

	entries, err := ListQueueEntries(conn)
	if err != nil {
		t.Fatalf("ListQueueEntries() unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("ListQueueEntries() = %+v, expected empty (a ready lesson doesn't enter the queue)", entries)
	}
}

func TestListQueueEntries_OrdersErrorFirstThenByUpdatedAt(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() unexpected error: %v", err)
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
		t.Fatalf("ListQueueEntries() unexpected error: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("ListQueueEntries() = %+v, expected 3 entries", entries)
	}
	if entries[0].LessonID != withError {
		t.Errorf("ListQueueEntries()[0].LessonID = %d, expected the lesson with an error first", entries[0].LessonID)
	}
	if entries[1].LessonID != older || entries[2].LessonID != newer {
		t.Errorf("ListQueueEntries()[1:] = %+v, expected older before newer (FIFO by updated_at)", entries[1:])
	}
}
```

- [ ] **Step 2: Run the tests and confirm they fail because `ListQueueEntries`/`QueueEntry` don't exist**

Run: `go test ./internal/db/... -run TestListQueueEntries -v`
Expected: FAIL — `undefined: ListQueueEntries` (package compilation error)

- [ ] **Step 3: Implement `internal/db/queue.go`**

```go
// internal/db/queue.go
package db

import (
	"database/sql"
	"fmt"
	"sort"
)

// QueueEntry is a lesson with an active (pending/running) or failed pipeline,
// in the format the Queue (Story 7) needs: which job is "current" right now,
// not just the collapsed status that LessonWithStatus uses for the Library.
type QueueEntry struct {
	LessonID   int64
	LessonDate string
	Tutor      string
	Kind       string // "extract_audio" or "transcribe"
	Status     string // "pending", "running" or "error"
	Attempts   int
	LastError  string
	UpdatedAt  string
}

// ListQueueEntries lists lessons with an active or failed pipeline, one row
// per lesson (never two), with each one's "current" job. Ready lessons
// (transcribe done) don't enter the list — that's already visible in the
// Library.
//
// Priority for deciding the current job (extract_audio checked before
// transcribe): the transcribe job stays with status "pending" in the DB the
// entire time it's blocked waiting for extract_audio to finish — the
// Worker only skips it in memory (claimNextEligibleJob in
// internal/jobs/worker.go), without changing that status. Checking
// transcribe before extract_audio would show "Transcrição — aguardando" for
// a lesson that is actually still extracting audio.
//
// Ordering: error first (needs user action), then by UpdatedAt of the
// current job, oldest first (same FIFO order the Worker uses in
// ListPendingJobs).
func ListQueueEntries(conn *sql.DB) ([]QueueEntry, error) {
	rows, err := conn.Query(`
		SELECT
			l.id, l.lesson_date, l.tutor,
			COALESCE(ea.status, ''), COALESCE(ea.attempts, 0), COALESCE(ea.last_error, ''), COALESCE(ea.updated_at, ''),
			COALESCE(tr.status, ''), COALESCE(tr.attempts, 0), COALESCE(tr.last_error, ''), COALESCE(tr.updated_at, '')
		FROM lessons l
		LEFT JOIN jobs ea ON ea.lesson_id = l.id AND ea.kind = 'extract_audio'
		LEFT JOIN jobs tr ON tr.lesson_id = l.id AND tr.kind = 'transcribe'
	`)
	if err != nil {
		return nil, fmt.Errorf("listar aulas com jobs pra fila: %w", err)
	}
	defer rows.Close()

	out := make([]QueueEntry, 0)
	for rows.Next() {
		var lessonID int64
		var lessonDate, tutor string
		var extractStatus, extractError, extractUpdatedAt string
		var extractAttempts int
		var transcribeStatus, transcribeError, transcribeUpdatedAt string
		var transcribeAttempts int
		if err := rows.Scan(
			&lessonID, &lessonDate, &tutor,
			&extractStatus, &extractAttempts, &extractError, &extractUpdatedAt,
			&transcribeStatus, &transcribeAttempts, &transcribeError, &transcribeUpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("ler linha da fila: %w", err)
		}

		entry := QueueEntry{LessonID: lessonID, LessonDate: lessonDate, Tutor: tutor}
		switch {
		case extractStatus == "error":
			entry.Kind, entry.Status = "extract_audio", "error"
			entry.Attempts, entry.LastError, entry.UpdatedAt = extractAttempts, extractError, extractUpdatedAt
		case extractStatus == "pending" || extractStatus == "running":
			entry.Kind, entry.Status = "extract_audio", extractStatus
			entry.Attempts, entry.LastError, entry.UpdatedAt = extractAttempts, extractError, extractUpdatedAt
		case transcribeStatus == "error":
			entry.Kind, entry.Status = "transcribe", "error"
			entry.Attempts, entry.LastError, entry.UpdatedAt = transcribeAttempts, transcribeError, transcribeUpdatedAt
		case transcribeStatus == "pending" || transcribeStatus == "running":
			entry.Kind, entry.Status = "transcribe", transcribeStatus
			entry.Attempts, entry.LastError, entry.UpdatedAt = transcribeAttempts, transcribeError, transcribeUpdatedAt
		default:
			// transcribe done (or no job at all — shouldn't happen, both
			// jobs are always created together at import confirmation):
			// lesson ready, doesn't enter the queue.
			continue
		}
		out = append(out, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterar fila: %w", err)
	}

	sortQueueEntries(out)
	return out, nil
}

// sortQueueEntries sorts in-place: status "error" first, then by
// UpdatedAt ascending (FIFO) — see the ordering rule in ListQueueEntries's
// comment.
func sortQueueEntries(entries []QueueEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		iErr, jErr := entries[i].Status == "error", entries[j].Status == "error"
		if iErr != jErr {
			return iErr
		}
		return entries[i].UpdatedAt < entries[j].UpdatedAt
	})
}
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `go test ./internal/db/... -run TestListQueueEntries -v`
Expected: PASS on all 6 tests

- [ ] **Step 5: `go vet` and commit**

```bash
go vet ./internal/db/...
git add internal/db/queue.go internal/db/queue_test.go
git commit -m "feat: add ListQueueEntries for the active/failed jobs queue"
```

---

### Task 2: `services/queue.go` — QueueService

**Files:**
- Create: `services/queue.go`
- Test: `services/queue_test.go`

**Interfaces:**
- Consumes: `db.QueueEntry`, `db.ListQueueEntries(conn *sql.DB) ([]db.QueueEntry, error)`
  (Task 1); `db.ResetErrorJobsForLesson(conn *sql.DB, lessonID int64) (int64, error)` (already
  exists, used by `LibraryService.RetryLesson`); test helpers `mustInsertLesson` and
  `mustInsertJobWithStatus`, already defined in `services/library_test.go` (same package
  `services`).
- Produces: `type QueueItem struct { LessonID int64 \`json:"lessonId"\`; LessonDate string
  \`json:"lessonDate"\`; Tutor string \`json:"tutor"\`; Stage string \`json:"stage"\`; Status
  string \`json:"status"\`; Attempts int \`json:"attempts"\`; LastError string
  \`json:"lastError"\` }`, `func NewQueueService(conn *sql.DB) *QueueService`, `func
  (s *QueueService) ListQueue() ([]QueueItem, error)`, `func (s *QueueService)
  RetryLesson(lessonID int64) error` — used by Task 3 (registration in `main.go`) and by the
  frontend via generated bindings.

- [ ] **Step 1: Write the tests (they will fail — `QueueService` doesn't exist yet)**

Create `services/queue_test.go`:

```go
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
		t.Fatalf("db.Open() failed: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLesson(t, conn, "2026-07-23", "Sarah M.", "aula.mp4")
	mustInsertJobWithStatus(t, conn, lessonID, "extract_audio", "done", "")
	mustInsertJobWithStatus(t, conn, lessonID, "transcribe", "running", "")

	svc := NewQueueService(conn)
	items, err := svc.ListQueue()
	if err != nil {
		t.Fatalf("ListQueue() unexpected error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("ListQueue() = %+v, expected 1 item", items)
	}
	if items[0].Stage != "Transcrição" || items[0].Status != "processando" {
		t.Errorf("ListQueue()[0] = %+v, expected Stage=Transcrição Status=processando", items[0])
	}
	if items[0].LessonDate != "2026-07-23" || items[0].Tutor != "Sarah M." {
		t.Errorf("ListQueue()[0] = %+v, expected fixture date/tutor", items[0])
	}
}

func TestQueueService_ListQueue_ErrorStatusAndMessage(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() failed: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLesson(t, conn, "2026-07-23", "Sarah M.", "aula.mp4")
	mustInsertJobWithStatus(t, conn, lessonID, "extract_audio", "error", "ffmpeg não encontrado")
	mustInsertJobWithStatus(t, conn, lessonID, "transcribe", "error", "depende de extract_audio que falhou")

	svc := NewQueueService(conn)
	items, err := svc.ListQueue()
	if err != nil {
		t.Fatalf("ListQueue() unexpected error: %v", err)
	}
	if len(items) != 1 || items[0].Stage != "Extração de áudio" || items[0].Status != "erro" || items[0].LastError != "ffmpeg não encontrado" {
		t.Errorf("ListQueue()[0] = %+v, expected Stage=Extração de áudio Status=erro with the root cause", items[0])
	}
}

func TestQueueService_ListQueue_ExcludesReadyLessons(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() failed: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLesson(t, conn, "2026-07-23", "Sarah M.", "aula.mp4")
	mustInsertJobWithStatus(t, conn, lessonID, "extract_audio", "done", "")
	mustInsertJobWithStatus(t, conn, lessonID, "transcribe", "done", "")

	svc := NewQueueService(conn)
	items, err := svc.ListQueue()
	if err != nil {
		t.Fatalf("ListQueue() unexpected error: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("ListQueue() = %+v, expected empty (ready lesson)", items)
	}
}

func TestQueueService_RetryLesson_ResetsErrorJobsToPending(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() failed: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLesson(t, conn, "2026-07-23", "Sarah M.", "aula.mp4")
	mustInsertJobWithStatus(t, conn, lessonID, "extract_audio", "error", "ffmpeg não encontrado")
	mustInsertJobWithStatus(t, conn, lessonID, "transcribe", "error", "depende de extract_audio que falhou")

	svc := NewQueueService(conn)
	if err := svc.RetryLesson(lessonID); err != nil {
		t.Fatalf("RetryLesson() unexpected error: %v", err)
	}

	items, err := svc.ListQueue()
	if err != nil {
		t.Fatalf("ListQueue() unexpected error: %v", err)
	}
	if len(items) != 1 || items[0].Status != "aguardando" {
		t.Errorf("ListQueue() after RetryLesson = %+v, expected status=aguardando (jobs went back to pending)", items)
	}
}
```

- [ ] **Step 2: Run the tests and confirm they fail because `QueueService` doesn't exist**

Run: `go test ./services/... -run TestQueueService -v`
Expected: FAIL — `undefined: NewQueueService` (package compilation error)

- [ ] **Step 3: Implement `services/queue.go`**

```go
// services/queue.go
package services

import (
	"database/sql"

	"assistente-idiomas/internal/db"
)

// QueueService exposes the processing queue (lessons with an active or
// failed pipeline) for the Queue screen (Story 7).
type QueueService struct {
	conn *sql.DB
}

func NewQueueService(conn *sql.DB) *QueueService {
	return &QueueService{conn: conn}
}

// stageLabel translates the job kind into a PT-BR stage label, shown
// on the Queue screen.
var stageLabel = map[string]string{
	"extract_audio": "Extração de áudio",
	"transcribe":    "Transcrição",
}

// statusLabel translates the job's raw status into the vocabulary already
// used in the Library (Library.svelte: STATUS_LABEL) — "pending"/"running"
// become "aguardando"/"processando", "error" becomes "erro".
var statusLabel = map[string]string{
	"pending": "aguardando",
	"running": "processando",
	"error":   "erro",
}

// QueueItem is a queue entry, in the format exposed to the frontend.
type QueueItem struct {
	LessonID   int64  `json:"lessonId"`
	LessonDate string `json:"lessonDate"`
	Tutor      string `json:"tutor"`
	Stage      string `json:"stage"`
	Status     string `json:"status"`
	Attempts   int    `json:"attempts"`
	LastError  string `json:"lastError"`
}

// ListQueue lists lessons with an active or failed pipeline, one per row,
// error first then FIFO — see db.ListQueueEntries.
func (s *QueueService) ListQueue() ([]QueueItem, error) {
	entries, err := db.ListQueueEntries(s.conn)
	if err != nil {
		return nil, err
	}
	out := make([]QueueItem, 0, len(entries))
	for _, e := range entries {
		out = append(out, QueueItem{
			LessonID:   e.LessonID,
			LessonDate: e.LessonDate,
			Tutor:      e.Tutor,
			Stage:      stageLabel[e.Kind],
			Status:     statusLabel[e.Status],
			Attempts:   e.Attempts,
			LastError:  e.LastError,
		})
	}
	return out, nil
}

// RetryLesson resets the lesson's failed jobs back to "pending" — the same
// data primitive that LibraryService.RetryLesson uses (db package, neither
// service depends on the other). It's not an error if the lesson currently
// has no failed job.
func (s *QueueService) RetryLesson(lessonID int64) error {
	_, err := db.ResetErrorJobsForLesson(s.conn, lessonID)
	return err
}
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `go test ./services/... -run TestQueueService -v`
Expected: PASS on all 4 tests

- [ ] **Step 5: `go vet` and commit**

```bash
go vet ./services/...
git add services/queue.go services/queue_test.go
git commit -m "feat: add QueueService to list and retry the queue"
```

---

### Task 3: Register `QueueService` in `main.go` and regenerate bindings

**Files:**
- Modify: `main.go:46-50`

**Interfaces:**
- Consumes: `services.NewQueueService(conn *sql.DB) *services.QueueService` (Task 2).
- Produces: `application.Service` registered, generated TS bindings in
  `frontend/bindings/assistente-idiomas/services/queueservice.ts` (`ListQueue`,
  `RetryLesson` functions) and `QueueItem` added to
  `frontend/bindings/assistente-idiomas/services/models.ts` — consumed by Task 4.

- [ ] **Step 1: Register the service in `main.go`**

Edit the `Services` block in `main.go` (lines 46-50):

From:
```go
		Services: []application.Service{
			application.NewService(services.NewSetupService()),
			application.NewService(services.NewImportService(conn)),
			application.NewService(services.NewLibraryService(conn)),
		},
```

To:
```go
		Services: []application.Service{
			application.NewService(services.NewSetupService()),
			application.NewService(services.NewImportService(conn)),
			application.NewService(services.NewLibraryService(conn)),
			application.NewService(services.NewQueueService(conn)),
		},
```

- [ ] **Step 2: Confirm the app compiles**

`go build ./...` includes `build/ios`, a Wails scaffold stub that **already fails on `main`,
with no changes from this story** (`function main is undeclared in the main package` —
pre-existing, out of scope). That's why the verification build is scoped to the packages that
matter: root (`main.go`), `internal/...`, `services/...`.

Run: `go build ./internal/... ./services/... .`
Expected: no output (clean build)

- [ ] **Step 3: Regenerate the TypeScript bindings**

Run (from the project root): `wails3 generate bindings -ts -i ./...`
Expected: command finishes without error; `frontend/bindings/assistente-idiomas/services/queueservice.ts`
is created or updated.

- [ ] **Step 4: Verify the generated content**

Run: `grep -E "export function (ListQueue|RetryLesson)" frontend/bindings/assistente-idiomas/services/queueservice.ts`
Expected: two lines, one for each function

Run: `grep -A8 "interface QueueItem" frontend/bindings/assistente-idiomas/services/models.ts`
Expected: interface with the fields `lessonId`, `lessonDate`, `tutor`, `stage`, `status`,
`attempts`, `lastError`

- [ ] **Step 5: Run the full Go suite and `go vet`**

Run: `go test ./... && go vet ./...`
Expected: `ok` for all packages, `go vet` with no output

- [ ] **Step 6: Commit**

`frontend/bindings` is in `.gitignore` (generated locally by `wails3 generate bindings`, not
versioned — same treatment as `frontend/dist`/`frontend/node_modules`), so only `main.go`
goes into the commit:

```bash
git add main.go
git commit -m "feat: register QueueService in the app"
```

---

### Task 4: `frontend/src/lib/jobsStore.svelte.ts` — shared reactive store

**Files:**
- Create: `frontend/src/lib/jobsStore.svelte.ts`

**Interfaces:**
- Consumes: `QueueService.ListQueue(): $CancellablePromise<QueueItem[] | null>` and `QueueItem`
  (Task 3, generated bindings); `Events.On(eventName: string, callback: (ev) => void): () => void`
  from `@wailsio/runtime` (already used as the transport since Story 4, `"job:updated"` event —
  see `services/jobs_notifier.go:13`).
- Produces: `export const jobsStore: { items: QueueItem[]; activeCount: number }` (reactive
  getters) and `export function initJobsStore(): void` — consumed by Tasks 5 and 6.

- [ ] **Step 1: Create the file**

```ts
// frontend/src/lib/jobsStore.svelte.ts
import { Events } from "@wailsio/runtime";
import * as QueueService from "../../bindings/assistente-idiomas/services/queueservice";
import type { QueueItem } from "../../bindings/assistente-idiomas/services/models";

let items: QueueItem[] = $state([]);
let initialized = false;

// jobsStore is the single read point for the queue state on the frontend —
// Queue.svelte and the badge in Sidebar.svelte read from here, instead of
// each one subscribing separately to "job:updated" (see
// docs/superpowers/specs/2026-07-23-historia-7-fila-visivel-design.md).
// Getters (not a direct export of `items`) because `export let` doesn't
// propagate reactivity across modules in Svelte 5 — functions/objects with
// getters are the recommended pattern for shared state in .svelte.ts.
export const jobsStore = {
  get items() {
    return items;
  },
  get activeCount() {
    return items.filter((i) => i.status !== "erro").length;
  },
};

async function refetch() {
  items = (await QueueService.ListQueue()) ?? [];
}

// initJobsStore fetches the queue once and subscribes to "job:updated" to
// refetch on every job status transition. Called once in App.svelte — repeat
// calls are a no-op (avoids duplicate event subscriptions).
export function initJobsStore() {
  if (initialized) return;
  initialized = true;
  refetch();
  Events.On("job:updated", refetch);
}
```

- [ ] **Step 2: Check types**

Run (inside `frontend/`): `corepack pnpm run check`
Expected: `0 ERRORS 0 WARNINGS` (the file isn't imported by any component yet, so
`svelte-check` only validates the module's own syntax/types)

- [ ] **Step 3: Commit**

```bash
git add frontend/src/lib/jobsStore.svelte.ts
git commit -m "feat: add reactive store for the jobs queue"
```

---

### Task 5: `frontend/src/lib/screens/Queue.svelte` — Queue list

**Files:**
- Modify: `frontend/src/lib/screens/Queue.svelte` (complete rewrite — currently a static
  18-line placeholder)

**Interfaces:**
- Consumes: `jobsStore.items: QueueItem[]` (Task 4); `QueueService.RetryLesson(lessonId:
  number): $CancellablePromise<void>` (Task 3, bindings).
- Produces: nothing consumed by another task.

- [ ] **Step 1: Rewrite the file**

```svelte
<script lang="ts">
  import { colors, fonts } from "../theme";
  import * as QueueService from "../../../bindings/assistente-idiomas/services/queueservice";
  import { jobsStore } from "../jobsStore.svelte";

  let retryingId: number | null = $state(null);
  let error: string = $state("");

  // Same format as Library.svelte (lessonDate is "YYYY-MM-DD" or
  // "YYYY-MM-DDTHH:MM", no timezone — not a timestamp with "Z").
  function formatLessonDateTime(value: string): string {
    const [datePart, timePart] = value.split("T");
    const [year, month, day] = datePart.split("-");
    const formattedDate = `${day}/${month}/${year}`;
    return timePart ? `${formattedDate} ${timePart}` : formattedDate;
  }

  async function retry(lessonId: number) {
    error = "";
    retryingId = lessonId;
    try {
      await QueueService.RetryLesson(lessonId);
    } catch (e) {
      error = String(e);
    } finally {
      retryingId = null;
    }
  }
</script>

<div class="screen" style="font-family: {fonts.body}; color: {colors.text};">
  <h1 style="font-family: {fonts.display};">Fila</h1>

  {#if error}
    <p class="error" style="color: {colors.red};">{error}</p>
  {/if}

  {#if jobsStore.items.length === 0}
    <p style="color: {colors.mut};">Nada na fila no momento.</p>
  {:else}
    <ul>
      {#each jobsStore.items as item (item.lessonId)}
        <li style="background: {colors.surface}; border: 1px solid {colors.line};">
          <div class="main">
            <span class="date" style="color: {colors.text};">{formatLessonDateTime(item.lessonDate)}</span>
            <span class="tutor" style="color: {colors.mut};">{item.tutor} · {item.stage}</span>
          </div>
          {#if item.status === "erro"}
            <div class="status-block">
              <span class="badge" style="color: {colors.red}; background: rgba(224,108,108,.1);">erro</span>
              <span class="error-message" style="color: {colors.mut};">{item.lastError}</span>
              <button onclick={() => retry(item.lessonId)} disabled={retryingId === item.lessonId}>
                {retryingId === item.lessonId ? "Reprocessando…" : "Reprocessar"}
              </button>
            </div>
          {:else}
            <span class="badge" style="color: {colors.blue}; background: rgba(110,168,254,.1);">
              {item.status}
            </span>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}
</div>

<style>
  .screen {
    padding: 2rem;
    max-width: 64rem;
    margin: 0 auto;
    width: 100%;
  }
  h1 {
    font-size: 1.4rem;
    margin: 0 0 1rem;
  }
  .error {
    font-size: 0.85rem;
    margin: 0 0 1rem;
  }
  ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }
  li {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
    border-radius: 0.75rem;
    padding: 0.75rem 1.25rem;
  }
  .main {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
    min-width: 0;
  }
  .date {
    font-size: 0.85rem;
  }
  .tutor {
    font-size: 0.8rem;
  }
  .status-block {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    flex-shrink: 0;
  }
  .error-message {
    font-size: 0.75rem;
    max-width: 16rem;
  }
  .badge {
    font-size: 0.75rem;
    padding: 0.25rem 0.6rem;
    border-radius: 999px;
    flex-shrink: 0;
  }
  button {
    padding: 0.5rem 1rem;
    border-radius: 0.5rem;
    cursor: pointer;
  }
</style>
```

- [ ] **Step 2: Check types**

Run (inside `frontend/`): `corepack pnpm run check`
Expected: `0 ERRORS 0 WARNINGS`

- [ ] **Step 3: Commit**

```bash
git add frontend/src/lib/screens/Queue.svelte
git commit -m "feat: list the active/failed jobs queue with retry"
```

---

### Task 6: Badge in `Sidebar.svelte` and store initialization in `App.svelte`

**Files:**
- Modify: `frontend/src/lib/Sidebar.svelte`
- Modify: `frontend/src/App.svelte:1-38`

**Interfaces:**
- Consumes: `jobsStore.activeCount: number`, `initJobsStore(): void` (Task 4).
- Produces: nothing consumed by another task.

- [ ] **Step 1: Add the badge in `Sidebar.svelte`**

From:
```svelte
<script lang="ts">
  import { colors, fonts } from "./theme";

  type Screen = "library" | "progress" | "queue";

  let { active, onNavigate }: { active: Screen; onNavigate: (screen: Screen) => void } = $props();

  const NAV: { key: Screen; label: string; icon: string }[] = [
    { key: "library", label: "Biblioteca", icon: "▤" },
    { key: "progress", label: "Progresso", icon: "◔" },
    { key: "queue", label: "Fila", icon: "≡" },
  ];
</script>
```

To:
```svelte
<script lang="ts">
  import { colors, fonts } from "./theme";
  import { jobsStore } from "./jobsStore.svelte";

  type Screen = "library" | "progress" | "queue";

  let { active, onNavigate }: { active: Screen; onNavigate: (screen: Screen) => void } = $props();

  const NAV: { key: Screen; label: string; icon: string }[] = [
    { key: "library", label: "Biblioteca", icon: "▤" },
    { key: "progress", label: "Progresso", icon: "◔" },
    { key: "queue", label: "Fila", icon: "≡" },
  ];
</script>
```

From:
```svelte
      <span class="icon">{item.icon}</span>
      {item.label}
    </button>
```

To:
```svelte
      <span class="icon">{item.icon}</span>
      {item.label}
      {#if item.key === "queue" && jobsStore.activeCount > 0}
        <span class="badge" style="background: {colors.blue}; color: {colors.bg};">
          {jobsStore.activeCount}
        </span>
      {/if}
    </button>
```

Add to the `<style>` block (after the `.icon` rule):
```css
  .badge {
    margin-left: auto;
    font-size: 0.7rem;
    font-weight: 600;
    padding: 0.1rem 0.45rem;
    border-radius: 999px;
    flex-shrink: 0;
  }
```

- [ ] **Step 2: Initialize the store in `App.svelte`**

From:
```svelte
  import Sidebar from "./lib/Sidebar.svelte";
  import Header from "./lib/Header.svelte";
  import Library from "./lib/screens/Library.svelte";
  import LessonDetail from "./lib/screens/LessonDetail.svelte";
  import Progress from "./lib/screens/Progress.svelte";
  import Queue from "./lib/screens/Queue.svelte";
  import SetupWizard from "./lib/SetupWizard.svelte";
  import { colors, fonts } from "./lib/theme";
  import * as SetupService from "../bindings/assistente-idiomas/services/setupservice";
```

To:
```svelte
  import Sidebar from "./lib/Sidebar.svelte";
  import Header from "./lib/Header.svelte";
  import Library from "./lib/screens/Library.svelte";
  import LessonDetail from "./lib/screens/LessonDetail.svelte";
  import Progress from "./lib/screens/Progress.svelte";
  import Queue from "./lib/screens/Queue.svelte";
  import SetupWizard from "./lib/SetupWizard.svelte";
  import { colors, fonts } from "./lib/theme";
  import { initJobsStore } from "./lib/jobsStore.svelte";
  import * as SetupService from "../bindings/assistente-idiomas/services/setupservice";
```

From:
```svelte
  onMount(async () => {
    try {
      firstRun = await SetupService.IsFirstRun();
    } finally {
      checkingFirstRun = false;
    }
  });
```

To:
```svelte
  onMount(async () => {
    initJobsStore();
    try {
      firstRun = await SetupService.IsFirstRun();
    } finally {
      checkingFirstRun = false;
    }
  });
```

- [ ] **Step 3: Check types**

Run (inside `frontend/`): `corepack pnpm run check`
Expected: `0 ERRORS 0 WARNINGS`

- [ ] **Step 4: Commit**

```bash
git add frontend/src/lib/Sidebar.svelte frontend/src/App.svelte
git commit -m "feat: active-jobs badge in the sidebar and initialize the queue store"
```

---

### Task 7: Final verification and story closeout

**Files:**
- Modify: `docs/fase-1-mvp.md:171-176` (Story 7 acceptance criteria)
- Modify: `docs/fase-1-mvp.md` (the "Progress log" table, new row at the end)

**Interfaces:** none — verification and documentation task, produces no symbols.

- [ ] **Step 1: Full Go suite**

`go build ./...` includes `build/ios` (Wails scaffold stub, broken even at `main` with no
relation to this story — see Task 3 Step 2), so the build is scoped accordingly.

Run: `go build ./internal/... ./services/... . && go vet ./... && go test ./...`
Expected: clean build, vet with no output, `ok` for all packages

- [ ] **Step 2: Full frontend suite**

Run (inside `frontend/`):
```bash
corepack pnpm run check
corepack pnpm run build
```
Expected: `check` with `0 ERRORS 0 WARNINGS`; `build` finishes without error (generates
`frontend/dist`)

- [ ] **Step 3: App binary build**

Run (from the project root): `wails3 build`
Expected: binary generated without error (same verification Stories 4/5/6 already did in
this display-less environment — opening a real window is still pending on Windows/Linux,
same pattern as previous stories)

- [ ] **Step 4: Mark Story 7's acceptance criteria in `docs/fase-1-mvp.md`**

From:
```markdown
### Acceptance criteria
- [ ] Queue screen with jobs, state, progress, and readable `last_error`; a reprocess action on errors.
- [ ] Sidebar badge with a count of active jobs, updated by events.
```

To:
```markdown
### Acceptance criteria
- [x] Queue screen with jobs, state, progress, and readable `last_error`; a reprocess action on errors.
- [x] Sidebar badge with a count of active jobs, updated by events.
```

- [ ] **Step 5: Add a row to the "Progress log" table**

Add, after the 23/07/2026 row for Story 6 (the last row in the table):
```markdown
| 23/07/2026 | Story 7 implemented: `internal/db.ListQueueEntries` derives, per lesson, which job (extract_audio or transcribe) is currently active or in error — checking extract_audio before transcribe, since the transcribe job stays with status "pending" in the database the whole time it's blocked waiting on extract_audio (the Worker only skips it in memory); `QueueService` translates it for the frontend (Stage/Status in PT-BR), reusing `db.ResetErrorJobsForLesson` for Reprocess; `jobsStore.svelte.ts` is the first real consumer of the `job:updated` event (transport ready since Story 4) — it fetches the queue once and refetches on every event, shared between `Queue.svelte` and the Sidebar's numeric badge (only pending+running; errors are left out of the count) | Visual verification (real window) of the badge updating live during real processing and of the Queue list changing along with it remains pending on Windows/Linux, the same pattern as previous stories |
```

- [ ] **Step 6: Final commit**

```bash
git add docs/fase-1-mvp.md
git commit -m "docs: mark Story 7 complete and record progress"
```
