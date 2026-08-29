# Story 5 — Real Library — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The Library lists real lessons with status derived from the jobs pipeline (`processando`/`pronta`/`erro`), duration, filter by tutor/date range, and reprocessing of errors; clicking a lesson with status `pronta` opens a minimal Detail view that already plays the local video (resolving the project's technical risk 1).

**Architecture:** Status and duration are never columns written by a separate process — status is always derived, at read time, from the two jobs (`extract_audio`/`transcribe`) of each lesson; duration is calculated via `ffprobe` on a best-effort basis at import confirmation time, decoupled from the transcription pipeline (resilience: never blocks confirmation). The local video is served to the webview by a Wails v3 `application.Middleware` that intercepts `GET /media/lesson/{id}` and delegates to the stdlib's `http.ServeFile` (which already handles `Range` requests), falling through to Wails's default handler (embedded assets in production, proxy to Vite in `wails3 dev`) for any other path.

**Tech Stack:** Go (stdlib `net/http`, `os/exec` for ffprobe), SQLite via `modernc.org/sqlite`, Svelte 5 (runes) + TypeScript, Wails v3 (`application.Middleware`, bindings generated via `wails3 generate bindings -ts -i`).

## Global Constraints

- `internal/` never imports Wails (thin layer) — `application.Middleware`/`application.Service` only appear in `services/` and `main.go`.
- Portable SQL in the repository layer — nothing driver-specific.
- Transcription/analysis failure (and, in this story, duration-calculation failure) never prevents watching the video — resilience principle from `CLAUDE.md`.
- Code and identifiers in English; user-facing error messages and UI in PT-BR.
- Svelte 5 with runes always (`$state`, `$props`, never `export let`/`$:`).
- Commit messages: single line only, semantic format (`feat:`, `fix:`, `test:`, ...).
- Frontend bindings are regenerated with **`wails3 generate bindings -ts -i ./...`** (the `-ts -i` flags are mandatory in this project — without them the generator produces `.js` classes instead of the `.ts` interfaces the frontend consumes; confirmed experimentally before this plan). `frontend/bindings/` is gitignored — regeneration doesn't show up in `git status`, but must run before any frontend change that depends on new types/methods.

---

### Task 1: `internal/media.Duration` — video duration via ffprobe

**Files:**
- Modify: `internal/media/media.go`
- Test: `internal/media/media_test.go`

**Interfaces:**
- Produces: `func Duration(ctx context.Context, videoPath string) (time.Duration, error)` — used by Task 5 (`services/import.go`).

- [ ] **Step 1: Write the failing test**

Add to the end of `internal/media/media_test.go`:

```go
func TestDuration_FfprobeNotInPath(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	_, err := Duration(context.Background(), "input.mp4")
	if err == nil {
		t.Fatal("esperava erro quando ffprobe não está no PATH, obteve nil")
	}
}
```

- [ ] **Step 2: Run the test and confirm it fails**

Run: `go test ./internal/media/... -run TestDuration_FfprobeNotInPath -v`
Expected: FAIL — `Duration` doesn't exist yet (compile error `undefined: Duration`).

- [ ] **Step 3: Implement `Duration`**

In `internal/media/media.go`, add (keeping the existing `package media` and `ExtractAudio`) the imports `strconv`, `strings`, and `time`, and the function:

```go
// Duration reads the video's duration via ffprobe (ffmpeg's companion, the
// same external dependency already assumed by ExtractAudio) — used to write
// lessons.duration_seconds on import confirmation (Story 5). It's metadata
// intrinsic to the video, not a product of the transcription pipeline: it
// must work even if extract_audio/transcribe never run.
func Duration(ctx context.Context, videoPath string) (time.Duration, error) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		return 0, fmt.Errorf("media: ffprobe não encontrado no PATH: %w", err)
	}

	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "csv=p=0",
		videoPath,
	)
	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("media: ffprobe falhou: %w", err)
	}
	seconds, err := strconv.ParseFloat(strings.TrimSpace(string(output)), 64)
	if err != nil {
		return 0, fmt.Errorf("media: duração inválida na saída do ffprobe: %w", err)
	}
	return time.Duration(seconds * float64(time.Second)), nil
}
```

The full import block becomes:

```go
import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)
```

- [ ] **Step 4: Run the test and confirm it passes**

Run: `go test ./internal/media/... -v`
Expected: PASS on `TestDuration_FfprobeNotInPath` and `TestExtractAudio_FfmpegNotInPath` (already existing).

- [ ] **Step 5: Commit**

```bash
git add internal/media/media.go internal/media/media_test.go
git commit -m "feat: adiciona internal/media.Duration via ffprobe"
```

---

### Task 2: `internal/db` — duration column on `Lesson`, `SetLessonDuration`, `ListTutors`

**Files:**
- Modify: `internal/db/lessons.go`
- Modify: `internal/db/lessons_test.go`

**Interfaces:**
- Consumes: nothing from previous tasks.
- Produces: `Lesson.DurationSeconds *int64` (new field); `func SetLessonDuration(conn *sql.DB, lessonID int64, seconds int64) error`; `func ListTutors(conn *sql.DB) ([]string, error)`. `FindLessonByPath`/`FindLessonByHash`/`FindLessonByID` keep the same signatures, but the returned `*Lesson` now carries `DurationSeconds`. `ListLessons` (the status-less version, from Story 3) **stays untouched in this task** — `services/library.go:30` still calls it, and removing it here would leave the repository failing to compile until Task 6 runs. `ListLessonsWithStatus` (Task 3) is its replacement; removing `ListLessons` and the two tests covering it (`TestListLessons_ReturnsAllOrderedByDateDesc`, `TestListLessons_EmptyReturnsEmptyNotNilError`) happens **in Task 6**, in the same commit that rewrites `services/library.go` to stop calling it — this way the repository never sits in an intermediate state that fails to compile. (Execution note: this sequencing fix was made after Task 2's review caught the broken build — Task 2 originally removed `ListLessons` too early.)

This task rewrites `internal/db/lessons.go` completely (extracting a shared scanner for the three lookups, which today repeat the same column list) — a small file, clearer to rewrite than to patch.

- [ ] **Step 1: Write the failing tests**

In `internal/db/lessons_test.go`, **do not touch** `TestListLessons_ReturnsAllOrderedByDateDesc` or `TestListLessons_EmptyReturnsEmptyNotNilError` — they stay as they are, removal only happens in Task 6. In the already-existing `TestFindLessonByID_FindsExistingAndNilWhenMissing` test, add the check that duration is nil by default, changing the block:

```go
	found, err := FindLessonByID(conn, id)
	if err != nil {
		t.Fatalf("FindLessonByID() erro inesperado: %v", err)
	}
	if found == nil || found.VideoPath != "aula-01.mp4" {
		t.Errorf("FindLessonByID() = %+v, esperado video_path aula-01.mp4", found)
	}
```

to:

```go
	found, err := FindLessonByID(conn, id)
	if err != nil {
		t.Fatalf("FindLessonByID() erro inesperado: %v", err)
	}
	if found == nil || found.VideoPath != "aula-01.mp4" {
		t.Errorf("FindLessonByID() = %+v, esperado video_path aula-01.mp4", found)
	}
	if found.DurationSeconds != nil {
		t.Errorf("DurationSeconds = %v, esperado nil antes de SetLessonDuration", *found.DurationSeconds)
	}
```

Add to the end of the file:

```go
func TestSetLessonDuration_UpdatesDurationSeconds(t *testing.T) {
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

	if err := SetLessonDuration(conn, id, 1860); err != nil {
		t.Fatalf("SetLessonDuration() erro inesperado: %v", err)
	}

	lesson, err := FindLessonByID(conn, id)
	if err != nil {
		t.Fatalf("FindLessonByID() erro inesperado: %v", err)
	}
	if lesson.DurationSeconds == nil || *lesson.DurationSeconds != 1860 {
		t.Errorf("DurationSeconds = %v, esperado 1860", lesson.DurationSeconds)
	}
}

func TestListTutors_ReturnsDistinctSortedTutors(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	insert := `INSERT INTO lessons (lesson_date, tutor, video_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`
	for _, row := range []struct{ date, tutor, path string }{
		{"2026-07-10", "Sarah M.", "a.mp4"},
		{"2026-07-11", "Sarah M.", "b.mp4"},
		{"2026-07-12", "James K.", "c.mp4"},
	} {
		if _, err := conn.Exec(insert, row.date, row.tutor, row.path, row.date+"T10:00:00Z", row.date+"T10:00:00Z"); err != nil {
			t.Fatalf("insert de fixture falhou: %v", err)
		}
	}

	tutors, err := ListTutors(conn)
	if err != nil {
		t.Fatalf("ListTutors() erro inesperado: %v", err)
	}
	if len(tutors) != 2 || tutors[0] != "James K." || tutors[1] != "Sarah M." {
		t.Errorf("ListTutors() = %+v, esperado [James K. Sarah M.] (distintos, ordem alfabética)", tutors)
	}
}
```

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `go test ./internal/db/... -run 'TestFindLessonByID_FindsExistingAndNilWhenMissing|TestSetLessonDuration_UpdatesDurationSeconds|TestListTutors_ReturnsDistinctSortedTutors' -v`
Expected: FAIL — `DurationSeconds` doesn't exist on `Lesson`, `SetLessonDuration`/`ListTutors` don't exist (compile error).

- [ ] **Step 3: Rewrite `internal/db/lessons.go`**

Full file content:

```go
package db

import (
	"database/sql"
	"fmt"
	"time"
)

// Lesson is a row of lessons. Besides the user-visible data
// (LessonDate, Tutor, DurationSeconds), it carries identity (path, hash) and
// the stat cache (size/mtime) used by Story 3's scan to
// decide whether the content needs rehashing. DurationSeconds is nil until
// Story 5 writes it (best-effort, via ffprobe, at import
// confirmation) — it never blocks anything by being nil.
type Lesson struct {
	ID              int64
	LessonDate      string
	Tutor           string
	VideoPath       string
	VideoHash       string
	FileSize        int64
	FileMTime       string
	DurationSeconds *int64
}

// lessonColumns is the list of columns (in this order) scanLessonRow expects
// — shared by FindLessonByPath/ByHash/ByID to keep the three
// queries identical in how they read the nullable duration_seconds.
const lessonColumns = `id, lesson_date, tutor, video_path, COALESCE(video_hash, ''), COALESCE(file_size, 0), COALESCE(file_mtime, ''), duration_seconds`

// scanLessonRow scans a row selected with lessonColumns.
// Returns (nil, nil) if the row doesn't exist (sql.ErrNoRows) — a path/hash/id
// not found is the common case for callers, not an error.
func scanLessonRow(row *sql.Row) (*Lesson, error) {
	var l Lesson
	var duration sql.NullInt64
	err := row.Scan(&l.ID, &l.LessonDate, &l.Tutor, &l.VideoPath, &l.VideoHash, &l.FileSize, &l.FileMTime, &duration)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if duration.Valid {
		d := duration.Int64
		l.DurationSeconds = &d
	}
	return &l, nil
}

// FindLessonByPath looks up the lesson whose video_path is exactly path. Returns
// (nil, nil) if there is none — an already-registered path is the common case, not
// an error.
func FindLessonByPath(conn *sql.DB, path string) (*Lesson, error) {
	row := conn.QueryRow(`SELECT `+lessonColumns+` FROM lessons WHERE video_path = ?`, path)
	l, err := scanLessonRow(row)
	if err != nil {
		return nil, fmt.Errorf("buscar lesson por path: %w", err)
	}
	return l, nil
}

// FindLessonByHash looks up the lesson whose video_hash is exactly hash. Returns
// (nil, nil) if there is none.
func FindLessonByHash(conn *sql.DB, hash string) (*Lesson, error) {
	row := conn.QueryRow(`SELECT `+lessonColumns+` FROM lessons WHERE video_hash = ?`, hash)
	l, err := scanLessonRow(row)
	if err != nil {
		return nil, fmt.Errorf("buscar lesson por hash: %w", err)
	}
	return l, nil
}

// FindLessonByID looks up the lesson by id. Returns (nil, nil) if there is none.
func FindLessonByID(conn *sql.DB, id int64) (*Lesson, error) {
	row := conn.QueryRow(`SELECT `+lessonColumns+` FROM lessons WHERE id = ?`, id)
	l, err := scanLessonRow(row)
	if err != nil {
		return nil, fmt.Errorf("buscar lesson por id: %w", err)
	}
	return l, nil
}

// UpdateLessonPath updates video_path/file_size/file_mtime of an already
// registered lesson — used when the scan finds the same hash under a different
// path (the file was just moved/renamed, not a new lesson).
// file_mtime is the file's mtime on disk; updated_at (the marker for when the
// database row changed) is always "now", never the file's mtime.
func UpdateLessonPath(conn *sql.DB, lessonID int64, path string, size int64, fileMTime string) error {
	_, err := conn.Exec(
		`UPDATE lessons SET video_path = ?, file_size = ?, file_mtime = ?, updated_at = ? WHERE id = ?`,
		path, size, fileMTime, time.Now().UTC().Format(time.RFC3339), lessonID,
	)
	if err != nil {
		return fmt.Errorf("atualizar path da lesson: %w", err)
	}
	return nil
}

// ListLessons lists every registered lesson, most recent first
// by lesson date — used by Story 3's Library (with no status derived
// from jobs; that's ListLessonsWithStatus, from Story 5). It stays
// in this task only until Task 6 swaps the caller in services/library.go for
// ListLessonsWithStatus and removes this function (keeps the repository
// compiling between the two tasks).
func ListLessons(conn *sql.DB) ([]Lesson, error) {
	rows, err := conn.Query(`SELECT ` + lessonColumns + ` FROM lessons ORDER BY lesson_date DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("listar lessons: %w", err)
	}
	defer rows.Close()

	var out []Lesson
	for rows.Next() {
		var l Lesson
		var duration sql.NullInt64
		if err := rows.Scan(&l.ID, &l.LessonDate, &l.Tutor, &l.VideoPath, &l.VideoHash, &l.FileSize, &l.FileMTime, &duration); err != nil {
			return nil, fmt.Errorf("ler lesson: %w", err)
		}
		if duration.Valid {
			d := duration.Int64
			l.DurationSeconds = &d
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterar lessons: %w", err)
	}
	return out, nil
}

// SetLessonDuration writes the video's duration (calculated via ffprobe at
// import confirmation, best-effort — see ImportService.ConfirmImport)
// — only called when the probe succeeded.
func SetLessonDuration(conn *sql.DB, lessonID int64, seconds int64) error {
	_, err := conn.Exec(
		`UPDATE lessons SET duration_seconds = ?, updated_at = ? WHERE id = ?`,
		seconds, time.Now().UTC().Format(time.RFC3339), lessonID,
	)
	if err != nil {
		return fmt.Errorf("gravar duração da lesson %d: %w", lessonID, err)
	}
	return nil
}

// ListTutors lists the distinct tutors already registered in lessons, in
// alphabetical order — feeds the Library's filter dropdown (Story 5).
func ListTutors(conn *sql.DB) ([]string, error) {
	rows, err := conn.Query(`SELECT DISTINCT tutor FROM lessons ORDER BY tutor ASC`)
	if err != nil {
		return nil, fmt.Errorf("listar tutores: %w", err)
	}
	defer rows.Close()

	out := make([]string, 0)
	for rows.Next() {
		var tutor string
		if err := rows.Scan(&tutor); err != nil {
			return nil, fmt.Errorf("ler tutor: %w", err)
		}
		out = append(out, tutor)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterar tutores: %w", err)
	}
	return out, nil
}
```

- [ ] **Step 4: Run the package's tests and confirm they pass**

Run: `go test ./internal/db/... -v`
Expected: PASS across the board — including `TestFindLessonByPathAndByHash_FindExistingRow`, `TestUpdateLessonPath_ChangesPathSizeAndMTime`, `TestLessons_VideoHashUniqueIndexRejectsDuplicate`, `TestListLessons_ReturnsAllOrderedByDateDesc`, `TestListLessons_EmptyReturnsEmptyNotNilError` (already existing, shouldn't break with the refactor — `ListLessons` still exists in this task).

Also confirm the whole repository still compiles (not just `internal/db`), since `services/library.go` still calls `db.ListLessons`:

Run: `go build ./internal/... ./services/... .`
Expected: no error.

- [ ] **Step 5: Commit**

```bash
git add internal/db/lessons.go internal/db/lessons_test.go
git commit -m "feat: adiciona duration_seconds e ListTutors em internal/db"
```

---

### Task 3: `internal/db` — status derived from jobs (`ListLessonsWithStatus`)

**Files:**
- Create: `internal/db/lesson_status.go`
- Create: `internal/db/lesson_status_test.go`

**Interfaces:**
- Consumes: `Lesson` (Task 2, embedded in `LessonWithStatus`); test helpers `mustInsertLessonForJobs`/`mustInsertJob` already existing in `internal/db/jobs_test.go` (same `db` package, reused without redefining).
- Produces: `type LessonFilter struct { Tutor, DateFrom, DateTo string }`; `type LessonWithStatus struct { Lesson; Status, ErrorMessage string }`; `func ListLessonsWithStatus(conn *sql.DB, filter LessonFilter) ([]LessonWithStatus, error)` — used by Task 6 (`services/library.go`).

- [ ] **Step 1: Write the failing tests**

Create `internal/db/lesson_status_test.go`:

```go
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
```

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `go test ./internal/db/... -run TestListLessonsWithStatus -v`
Expected: FAIL — `LessonFilter`/`ListLessonsWithStatus` don't exist (compile error).

- [ ] **Step 3: Create `internal/db/lesson_status.go`**

```go
// internal/db/lesson_status.go
package db

import (
	"database/sql"
	"fmt"
)

// LessonFilter filters ListLessonsWithStatus — every field is optional
// (an empty string = no filter), used by the Library's (Story 5)
// tutor/date-range filter.
type LessonFilter struct {
	Tutor    string
	DateFrom string // YYYY-MM-DD, inclusive
	DateTo   string // YYYY-MM-DD, inclusive
}

// LessonWithStatus is a lesson with the status derived from the
// extract_audio/transcribe jobs. Status is always one of "processando", "pronta",
// "erro"; ErrorMessage is only populated when Status == "erro" — see the
// derivation rules in deriveStatus.
type LessonWithStatus struct {
	Lesson
	Status       string
	ErrorMessage string
}

// ListLessonsWithStatus lists confirmed lessons with the status derived
// from jobs, most recent first, applying filter (empty fields are
// ignored). The date filter compares only the YYYY-MM-DD part of
// lesson_date (which may have a time component, the format from <input type="datetime-local">),
// so it includes lessons with a recorded time throughout the whole day of the range.
func ListLessonsWithStatus(conn *sql.DB, filter LessonFilter) ([]LessonWithStatus, error) {
	query := `
		SELECT
			l.id, l.lesson_date, l.tutor, l.video_path,
			COALESCE(l.video_hash, ''), COALESCE(l.file_size, 0), COALESCE(l.file_mtime, ''),
			l.duration_seconds,
			COALESCE(ea.status, ''), COALESCE(ea.last_error, ''),
			COALESCE(tr.status, ''), COALESCE(tr.last_error, '')
		FROM lessons l
		LEFT JOIN jobs ea ON ea.lesson_id = l.id AND ea.kind = 'extract_audio'
		LEFT JOIN jobs tr ON tr.lesson_id = l.id AND tr.kind = 'transcribe'
		WHERE 1=1`
	var args []any
	if filter.Tutor != "" {
		query += ` AND l.tutor = ?`
		args = append(args, filter.Tutor)
	}
	if filter.DateFrom != "" {
		query += ` AND substr(l.lesson_date, 1, 10) >= ?`
		args = append(args, filter.DateFrom)
	}
	if filter.DateTo != "" {
		query += ` AND substr(l.lesson_date, 1, 10) <= ?`
		args = append(args, filter.DateTo)
	}
	query += ` ORDER BY l.lesson_date DESC, l.id DESC`

	rows, err := conn.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("listar lessons com status: %w", err)
	}
	defer rows.Close()

	out := make([]LessonWithStatus, 0)
	for rows.Next() {
		var lws LessonWithStatus
		var duration sql.NullInt64
		var extractStatus, extractError, transcribeStatus, transcribeError string
		if err := rows.Scan(
			&lws.ID, &lws.LessonDate, &lws.Tutor, &lws.VideoPath,
			&lws.VideoHash, &lws.FileSize, &lws.FileMTime,
			&duration,
			&extractStatus, &extractError,
			&transcribeStatus, &transcribeError,
		); err != nil {
			return nil, fmt.Errorf("ler lesson com status: %w", err)
		}
		if duration.Valid {
			d := duration.Int64
			lws.DurationSeconds = &d
		}
		lws.Status, lws.ErrorMessage = deriveStatus(extractStatus, extractError, transcribeStatus, transcribeError)
		out = append(out, lws)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterar lessons com status: %w", err)
	}
	return out, nil
}

// deriveStatus applies the Library's status rules (Story 5): an
// extract_audio error is the root cause and takes priority over a
// transcribe error (which gets blocked when its extract_audio fails — see
// claimNextEligibleJob in internal/jobs/worker.go); "pronta" requires
// transcribe to be done, not just extract_audio.
func deriveStatus(extractStatus, extractError, transcribeStatus, transcribeError string) (status string, message string) {
	if extractStatus == "error" {
		return "erro", extractError
	}
	if transcribeStatus == "error" {
		return "erro", transcribeError
	}
	if transcribeStatus == "done" {
		return "pronta", ""
	}
	return "processando", ""
}
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `go test ./internal/db/... -v`
Expected: PASS across the board, including the already-existing ones.

- [ ] **Step 5: Commit**

```bash
git add internal/db/lesson_status.go internal/db/lesson_status_test.go
git commit -m "feat: adiciona status derivado dos jobs em internal/db"
```

---

### Task 4: `internal/db/jobs.go` — `ResetErrorJobsForLesson`

**Files:**
- Modify: `internal/db/jobs.go`
- Modify: `internal/db/jobs_test.go`

**Interfaces:**
- Produces: `func ResetErrorJobsForLesson(conn *sql.DB, lessonID int64) (int64, error)` — used by Task 6 (`services/library.go`, `RetryLesson`).

- [ ] **Step 1: Write the failing test**

Add to the end of `internal/db/jobs_test.go`:

```go
func TestResetErrorJobsForLesson_ResetsOnlyErrorJobsOfThatLesson(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	failing := mustInsertLessonForJobs(t, conn, "falhou.mp4")
	extractID := mustInsertJob(t, conn, failing, "extract_audio", "error", 3, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")
	transcribeID := mustInsertJob(t, conn, failing, "transcribe", "error", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")
	if _, err := conn.Exec(`UPDATE jobs SET last_error = 'falhou' WHERE id IN (?, ?)`, extractID, transcribeID); err != nil {
		t.Fatalf("preparar last_error de fixture falhou: %v", err)
	}

	other := mustInsertLessonForJobs(t, conn, "outra.mp4")
	otherDoneID := mustInsertJob(t, conn, other, "extract_audio", "done", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")

	n, err := ResetErrorJobsForLesson(conn, failing)
	if err != nil {
		t.Fatalf("ResetErrorJobsForLesson() erro inesperado: %v", err)
	}
	if n != 2 {
		t.Errorf("ResetErrorJobsForLesson() = %d, esperado 2 jobs resetados", n)
	}

	extract, err := FindJob(conn, failing, "extract_audio")
	if err != nil {
		t.Fatalf("FindJob() erro inesperado: %v", err)
	}
	if extract.Status != "pending" || extract.Attempts != 0 || extract.LastError != "" {
		t.Errorf("extract_audio após reset = %+v, esperado status=pending attempts=0 last_error vazio", extract)
	}

	transcribe, err := FindJob(conn, failing, "transcribe")
	if err != nil {
		t.Fatalf("FindJob() erro inesperado: %v", err)
	}
	if transcribe.Status != "pending" || transcribe.Attempts != 0 || transcribe.LastError != "" {
		t.Errorf("transcribe após reset = %+v, esperado status=pending attempts=0 last_error vazio", transcribe)
	}

	otherJob, err := FindJob(conn, other, "extract_audio")
	if err != nil {
		t.Fatalf("FindJob() erro inesperado: %v", err)
	}
	if otherJob.ID != otherDoneID || otherJob.Status != "done" {
		t.Errorf("job de outra lesson = %+v, não deveria ser afetado pelo reset", otherJob)
	}
}

func TestResetErrorJobsForLesson_NoErrorJobsReturnsZeroNoError(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	mustInsertJob(t, conn, lessonID, "extract_audio", "done", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")

	n, err := ResetErrorJobsForLesson(conn, lessonID)
	if err != nil {
		t.Fatalf("ResetErrorJobsForLesson() erro inesperado: %v", err)
	}
	if n != 0 {
		t.Errorf("ResetErrorJobsForLesson() = %d, esperado 0 (nenhum job em erro)", n)
	}
}
```

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `go test ./internal/db/... -run TestResetErrorJobsForLesson -v`
Expected: FAIL — `ResetErrorJobsForLesson` doesn't exist (compile error).

- [ ] **Step 3: Implement `ResetErrorJobsForLesson`**

Add to the end of `internal/db/jobs.go`:

```go
// ResetErrorJobsForLesson resets every "error" job of the lesson back to
// "pending" (attempts=0, last_error=NULL) — used by the Library's (Story 5)
// "Retry" button. Resets both jobs at once on purpose: when
// extract_audio fails for good, the worker already marks transcribe
// as "error" too (blocked by dependency — see claimNextEligibleJob
// in internal/jobs/worker.go), and resetting only extract_audio would leave
// transcribe stuck in error forever. Returns how many jobs were
// reset (0 is not an error — the lesson may not have any job in error).
func ResetErrorJobsForLesson(conn *sql.DB, lessonID int64) (int64, error) {
	res, err := conn.Exec(
		`UPDATE jobs SET status = 'pending', attempts = 0, last_error = NULL, updated_at = ? WHERE lesson_id = ? AND status = 'error'`,
		time.Now().UTC().Format(time.RFC3339), lessonID,
	)
	if err != nil {
		return 0, fmt.Errorf("resetar jobs com erro da lesson %d: %w", lessonID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("confirmar reset de jobs da lesson %d: %w", lessonID, err)
	}
	return n, nil
}
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `go test ./internal/db/... -v`
Expected: PASS across the board.

- [ ] **Step 5: Commit**

```bash
git add internal/db/jobs.go internal/db/jobs_test.go
git commit -m "feat: adiciona ResetErrorJobsForLesson pro reprocessamento da Biblioteca"
```

---

### Task 5: `services/import.go` — best-effort duration at confirmation

**Files:**
- Modify: `services/import.go`
- Modify: `services/import_test.go`

**Interfaces:**
- Consumes: `media.Duration` (Task 1), `db.FindLessonByID`/`db.SetLessonDuration` (Task 2).
- Produces: no new public signature — `ImportService.ConfirmImport` keeps `(id int64, lessonDate string, tutor string) error`.

- [ ] **Step 1: Write the failing test**

Add to the end of `services/import_test.go`:

```go
func TestImportService_ConfirmImport_SucceedsEvenWhenDurationProbeFails(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	storageRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(storageRoot, "aula.mp4"), []byte("nao-e-um-video-de-verdade"), 0o644); err != nil {
		t.Fatalf("preparar vídeo de fixture falhou: %v", err)
	}
	if err := config.Save(&config.AppConfig{StorageRoot: storageRoot}); err != nil {
		t.Fatalf("config.Save() falhou: %v", err)
	}

	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	svc := NewImportService(conn)
	if _, err := svc.ScanFolder(); err != nil {
		t.Fatalf("ScanFolder() erro inesperado: %v", err)
	}
	pending, err := svc.ListPendingImports()
	if err != nil || len(pending) != 1 {
		t.Fatalf("setup: ListPendingImports() = %+v, %v", pending, err)
	}

	if err := svc.ConfirmImport(pending[0].ID, "2026-07-22", "Sarah M."); err != nil {
		t.Fatalf("ConfirmImport() com vídeo inválido não deveria falhar (duração é melhor esforço): %v", err)
	}

	lesson, err := db.FindLessonByPath(conn, "aula.mp4")
	if err != nil {
		t.Fatalf("FindLessonByPath() erro inesperado: %v", err)
	}
	if lesson == nil {
		t.Fatal("lesson não foi confirmada")
	}
	if lesson.DurationSeconds != nil {
		t.Errorf("DurationSeconds = %v, esperado nil (fixture não é um vídeo real, ffprobe deveria falhar ou estar ausente)", *lesson.DurationSeconds)
	}
}
```

- [ ] **Step 2: Run the test and confirm it passes even without the change**

Run: `go test ./services/... -run TestImportService_ConfirmImport_SucceedsEvenWhenDurationProbeFails -v`
Expected: PASS — this particular test already passes with no production change at all, because `ConfirmImport` today simply doesn't write any duration (it always stays nil). The test exists to pin down the behavior **after** Step 3 adds the call to the probe: it should keep passing even with the probe running (and failing, because of the fake content). Confirming this now establishes the baseline before the change.

- [ ] **Step 3: Implement the best-effort call in `ConfirmImport`**

Rewrite `services/import.go` (only the imports section, `ConfirmImport`, and the new function — `dbRepo` and the rest of the file stay the same):

```go
package services

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"path/filepath"

	"assistente-idiomas/internal/config"
	"assistente-idiomas/internal/db"
	"assistente-idiomas/internal/importer"
	"assistente-idiomas/internal/media"
)
```

Change `ConfirmImport`'s body:

```go
// ConfirmImport writes the candidate id as a real lesson (lessonDate in
// YYYY-MM-DD format, tutor as free text) and creates the processing jobs. After
// confirming, it tries to calculate the video's duration (best-effort — see
// setDurationBestEffort).
func (s *ImportService) ConfirmImport(id int64, lessonDate string, tutor string) error {
	if lessonDate == "" {
		return fmt.Errorf("data da aula não pode ser vazia")
	}
	if tutor == "" {
		return fmt.Errorf("tutor não pode ser vazio")
	}
	lessonID, err := db.ConfirmPendingImport(s.conn, id, lessonDate, tutor)
	if err != nil {
		return err
	}
	s.setDurationBestEffort(lessonID)
	return nil
}

// setDurationBestEffort calculates the newly-confirmed video's duration via
// ffprobe and writes it to lessons.duration_seconds. Duration is metadata
// intrinsic to the video, not a product of the transcription pipeline — it
// must remain available even if the pipeline fails (resilience principle,
// CLAUDE.md). That's why any failure here (missing ffprobe, invalid file,
// etc.) is only logged: never propagated as a ConfirmImport error, which has
// already successfully confirmed the lesson.
func (s *ImportService) setDurationBestEffort(lessonID int64) {
	lesson, err := db.FindLessonByID(s.conn, lessonID)
	if err != nil || lesson == nil {
		return
	}
	cfg, err := config.Load()
	if err != nil {
		return
	}
	videoPath := filepath.Join(cfg.StorageRoot, filepath.FromSlash(lesson.VideoPath))
	dur, err := media.Duration(context.Background(), videoPath)
	if err != nil {
		slog.Warn("importer: não foi possível calcular a duração do vídeo", "lesson_id", lessonID, "erro", err)
		return
	}
	if err := db.SetLessonDuration(s.conn, lessonID, int64(dur.Seconds())); err != nil {
		slog.Warn("importer: não foi possível gravar a duração do vídeo", "lesson_id", lessonID, "erro", err)
	}
}
```

The rest of the file (`dbRepo` and its methods) stays unchanged.

- [ ] **Step 4: Run the package's tests and confirm they pass**

Run: `go test ./services/... -v`
Expected: PASS across the board, including the three already-existing `ImportService` tests and the new one.

- [ ] **Step 5: Commit**

```bash
git add services/import.go services/import_test.go
git commit -m "feat: calcula duracao do video em melhor esforco na confirmacao da importacao"
```

---

### Task 6: `services/library.go` — status/duration/filter/retry/Detail + bindings

**Files:**
- Modify: `services/library.go`
- Modify: `services/library_test.go`
- Modify: `internal/db/lessons.go` — remove the `ListLessons` function (left in on purpose in Task 2; this is the task that swaps its only caller, `services/library.go`, for `ListLessonsWithStatus` — the removal happens in the same commit so the repository is never left in a state that fails to compile in between).
- Modify: `internal/db/lessons_test.go` — remove `TestListLessons_ReturnsAllOrderedByDateDesc` and `TestListLessons_EmptyReturnsEmptyNotNilError` (they cover the function this task removes).
- Regenerate: `frontend/bindings/` (via `wails3 generate bindings -ts -i ./...`, not versioned)

**Interfaces:**
- Consumes: `db.ListLessonsWithStatus`/`db.LessonFilter` (Task 3), `db.ListTutors` (Task 2), `db.ResetErrorJobsForLesson` (Task 4), `db.FindLessonByID` (Task 2).
- Produces (consumed by the frontend's Task 7/8, and by `main.go` in the backend's Task 7):
  - `type Lesson struct { ID int64; LessonDate string; Tutor string; VideoPath string; DurationSeconds *int64; Status string; ErrorMessage string }` (JSON: `id`, `lessonDate`, `tutor`, `videoPath`, `durationSeconds`, `status`, `errorMessage`).
  - `type LessonFilter struct { Tutor, DateFrom, DateTo string }` (JSON: `tutor`, `dateFrom`, `dateTo`).
  - `func (s *LibraryService) ListLessons(filter LessonFilter) ([]Lesson, error)` — **signature changes** (previously had no parameter).
  - `func (s *LibraryService) ListTutors() ([]string, error)`.
  - `func (s *LibraryService) RetryLesson(lessonID int64) error`.
  - `func (s *LibraryService) GetLesson(id int64) (Lesson, error)`.
  - After `wails3 generate bindings -ts -i ./...`, the generated TS types (confirmed experimentally) are:
    - `export interface Lesson { "id": number; "lessonDate": string; "tutor": string; "videoPath": string; "durationSeconds": number | null; "status": string; "errorMessage": string; }`
    - `export interface LessonFilter { "tutor": string; "dateFrom": string; "dateTo": string; }`
    - `ListLessons(filter: $models.LessonFilter): $CancellablePromise<$models.Lesson[] | null>`
    - `ListTutors(): $CancellablePromise<string[] | null>`
    - `RetryLesson(lessonID: number): $CancellablePromise<void>`
    - `GetLesson(id: number): $CancellablePromise<$models.Lesson>` (rejects the promise if not found — no `| null`).

- [ ] **Step 1: Write the failing tests**

Rewrite `services/library_test.go` completely:

```go
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
```

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `go test ./services/... -run TestLibraryService -v`
Expected: FAIL — `ListLessons` still expects zero arguments, `ListTutors`/`RetryLesson`/`GetLesson`/`LessonFilter` don't exist (compile error).

- [ ] **Step 3: Rewrite `services/library.go`**

```go
package services

import (
	"database/sql"
	"fmt"

	"assistente-idiomas/internal/db"
)

// LibraryService exposes already-confirmed lessons to the Library —
// listing with status derived from jobs and duration (Story 5), filter by
// tutor/date range, reprocessing lessons with an error, and looking up a
// lesson for the Detail view.
type LibraryService struct {
	conn *sql.DB
}

func NewLibraryService(conn *sql.DB) *LibraryService {
	return &LibraryService{conn: conn}
}

// Lesson is a confirmed lesson, in the shape exposed to the frontend. Status is
// always one of "processando", "pronta", "erro" (see db.LessonWithStatus);
// ErrorMessage is only populated when Status == "erro". DurationSeconds is
// nil until the duration probe (best-effort, at import
// confirmation) succeeds.
type Lesson struct {
	ID              int64  `json:"id"`
	LessonDate      string `json:"lessonDate"`
	Tutor           string `json:"tutor"`
	VideoPath       string `json:"videoPath"`
	DurationSeconds *int64 `json:"durationSeconds"`
	Status          string `json:"status"`
	ErrorMessage    string `json:"errorMessage"`
}

// LessonFilter filters ListLessons — empty fields are ignored (no
// filter on that criterion).
type LessonFilter struct {
	Tutor    string `json:"tutor"`
	DateFrom string `json:"dateFrom"`
	DateTo   string `json:"dateTo"`
}

// ListLessons lists confirmed lessons with status/duration, most recent
// first, applying filter.
func (s *LibraryService) ListLessons(filter LessonFilter) ([]Lesson, error) {
	rows, err := db.ListLessonsWithStatus(s.conn, db.LessonFilter{
		Tutor:    filter.Tutor,
		DateFrom: filter.DateFrom,
		DateTo:   filter.DateTo,
	})
	if err != nil {
		return nil, err
	}
	out := make([]Lesson, 0, len(rows))
	for _, r := range rows {
		out = append(out, Lesson{
			ID:              r.ID,
			LessonDate:      r.LessonDate,
			Tutor:           r.Tutor,
			VideoPath:       r.VideoPath,
			DurationSeconds: r.DurationSeconds,
			Status:          r.Status,
			ErrorMessage:    r.ErrorMessage,
		})
	}
	return out, nil
}

// ListTutors lists the distinct tutors already registered, for the
// Library's filter dropdown.
func (s *LibraryService) ListTutors() ([]string, error) {
	return db.ListTutors(s.conn)
}

// RetryLesson resets the lesson's jobs with errors back to "pending" — the
// jobs worker (internal/jobs) resumes the pipeline on its own on the next poll
// (~5s), with no need to explicitly wake it (same decision as Story 4). It is
// not an error if the lesson currently has no job in error.
func (s *LibraryService) RetryLesson(lessonID int64) error {
	_, err := db.ResetErrorJobsForLesson(s.conn, lessonID)
	return err
}

// GetLesson looks up a confirmed lesson by id, for the Detail view (Story 5/6).
// Unlike ListLessons, it doesn't compute Status/ErrorMessage — the Detail view
// is only ever opened from an already "pronta" lesson in the Library (see
// Library.svelte), so recomputing status here would be wasted work.
func (s *LibraryService) GetLesson(id int64) (Lesson, error) {
	lesson, err := db.FindLessonByID(s.conn, id)
	if err != nil {
		return Lesson{}, err
	}
	if lesson == nil {
		return Lesson{}, fmt.Errorf("aula %d não encontrada", id)
	}
	return Lesson{
		ID:              lesson.ID,
		LessonDate:      lesson.LessonDate,
		Tutor:           lesson.Tutor,
		VideoPath:       lesson.VideoPath,
		DurationSeconds: lesson.DurationSeconds,
	}, nil
}
```

- [ ] **Step 3b: Remove `ListLessons` from `internal/db`**

This method (from Story 3) is left with no caller after Step 3 above — `services/library.go` now calls `db.ListLessonsWithStatus`, not `db.ListLessons` anymore. Removing it in this same commit (not before: Task 2 left `ListLessons` untouched on purpose, so the repository never failed to compile between the two tasks).

In `internal/db/lessons.go`, remove the entire `ListLessons` function (the one that lists all lessons ordered by `lesson_date DESC, id DESC`, without status).

In `internal/db/lessons_test.go`, remove `TestListLessons_ReturnsAllOrderedByDateDesc` and `TestListLessons_EmptyReturnsEmptyNotNilError` (they cover the removed function).

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `go test ./internal/db/... ./services/... -v`
Expected: PASS across the board — no test should reference `db.ListLessons` after this step.

Run: `go build ./internal/... ./services/... .`
Expected: no error (confirms removing `ListLessons` didn't leave any other caller forgotten in any package).

- [ ] **Step 5: Regenerate the frontend bindings**

Run: `wails3 generate bindings -ts -i ./...`
Expected: output `INFO Processed: ... Services, ... Methods, ... Models ...` with no error. Check with `cat frontend/bindings/assistente-idiomas/services/models.ts` and `cat frontend/bindings/assistente-idiomas/services/libraryservice.ts` that the types match what's listed under **Interfaces** above (field names, `| null` where expected).

- [ ] **Step 6: Commit**

```bash
git add services/library.go services/library_test.go internal/db/lessons.go internal/db/lessons_test.go
git commit -m "feat: status/duracao/filtro/reprocessar/detalhe na LibraryService"
```

(`frontend/bindings/` is gitignored — it's not part of the commit; it's regenerated locally by whoever builds the app.)

---

### Task 7: Local video to the webview — `VideoAssetMiddleware` + wiring in `main.go`

**Files:**
- Create: `services/video_asset.go`
- Create: `services/video_asset_test.go`
- Modify: `main.go`

**Interfaces:**
- Consumes: `db.FindLessonByID` (Task 2), `jobs.StorageRootResolver` (already existing in `internal/jobs/worker.go`), `mustInsertLesson(t, conn, date, tutor, videoPath) int64` (test helper defined in `services/library_test.go` by Task 6, reused here without redefining — same `services` package).
- Produces: `func VideoAssetMiddleware(conn *sql.DB, storageRoot jobs.StorageRootResolver) application.Middleware` — served at `GET /media/lesson/{id}`, used by the frontend's Task 8 as the `<video>`'s `src`.

- [ ] **Step 1: Write the failing tests**

Create `services/video_asset_test.go`:

```go
package services

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"assistente-idiomas/internal/db"
)

func TestVideoAssetMiddleware_ServesVideoWithRangeSupport(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	storageRoot := t.TempDir()
	content := []byte("conteudo-de-video-fake-para-teste-de-range")
	if err := os.WriteFile(filepath.Join(storageRoot, "aula.mp4"), content, 0o644); err != nil {
		t.Fatalf("preparar vídeo de fixture falhou: %v", err)
	}
	lessonID := mustInsertLesson(t, conn, "2026-07-20", "Sarah M.", "aula.mp4")

	handler := VideoAssetMiddleware(conn, func() (string, error) { return storageRoot, nil })(http.NotFoundHandler())

	req := httptest.NewRequest(http.MethodGet, "/media/lesson/"+strconv.FormatInt(lessonID, 10), nil)
	req.Header.Set("Range", "bytes=0-4")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, esperado 206 Partial Content", rec.Code)
	}
	if rec.Body.String() != string(content[:5]) {
		t.Errorf("body = %q, esperado os 5 primeiros bytes do vídeo", rec.Body.String())
	}
}

func TestVideoAssetMiddleware_UnknownLessonReturns404(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	handler := VideoAssetMiddleware(conn, func() (string, error) { return t.TempDir(), nil })(http.NotFoundHandler())

	req := httptest.NewRequest(http.MethodGet, "/media/lesson/999", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, esperado 404", rec.Code)
	}
}

func TestVideoAssetMiddleware_OtherPathsDelegateToNext(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { nextCalled = true })
	handler := VideoAssetMiddleware(conn, func() (string, error) { return t.TempDir(), nil })(next)

	req := httptest.NewRequest(http.MethodGet, "/wails/runtime.js", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !nextCalled {
		t.Error("path fora de /media/lesson/ deveria ser delegado a next, mas next não foi chamado")
	}
}
```

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `go test ./services/... -run TestVideoAssetMiddleware -v`
Expected: FAIL — `VideoAssetMiddleware` doesn't exist (compile error).

- [ ] **Step 3: Create `services/video_asset.go`**

```go
// services/video_asset.go
package services

import (
	"database/sql"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"assistente-idiomas/internal/db"
	"assistente-idiomas/internal/jobs"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// videoAssetPrefix is the base path of the endpoint that serves a lesson's
// .mp4 to the webview — resolves fase-1-mvp.md's technical risk 1 (local
// video with range-request support) via the stdlib's http.ServeFile, which
// already handles Range for free. See
// docs/superpowers/specs/2026-07-22-historia-5-biblioteca-real-design.md.
const videoAssetPrefix = "/media/lesson/"

// VideoAssetMiddleware serves GET /media/lesson/{id} with lesson id's
// video; any other path is delegated to next (Wails's default
// AssetServer — embedded in production, proxied to the dev server in
// `wails3 dev`, confirmed by reading internal/assetserver/build_dev.go
// in the dependency: the webview always talks to the Go server, which only
// proxies to Vite internally when FRONTEND_DEVSERVER_URL is set).
// storageRoot is re-evaluated on every request, not just once at the
// middleware's creation — same reason as internal/jobs.Worker:
// storage_root only exists after the first-run wizard, which runs after the
// app is already up.
func VideoAssetMiddleware(conn *sql.DB, storageRoot jobs.StorageRootResolver) application.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			idStr, ok := strings.CutPrefix(r.URL.Path, videoAssetPrefix)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			id, err := strconv.ParseInt(idStr, 10, 64)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			lesson, err := db.FindLessonByID(conn, id)
			if err != nil || lesson == nil {
				http.NotFound(w, r)
				return
			}
			root, err := storageRoot()
			if err != nil {
				http.Error(w, "storage_root não configurado", http.StatusInternalServerError)
				return
			}
			videoPath := filepath.Join(root, filepath.FromSlash(lesson.VideoPath))
			http.ServeFile(w, r, videoPath)
		})
	}
}
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `go test ./services/... -v`
Expected: PASS across the board.

- [ ] **Step 5: Wire the middleware into `main.go`**

Rewrite `main.go` completely:

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

	storageRoot := func() (string, error) {
		cfg, err := config.Load()
		if err != nil {
			return "", err
		}
		return cfg.StorageRoot, nil
	}

	startJobWorker(conn, storageRoot)

	app := application.New(application.Options{
		Name:        "Assistente de Idiomas",
		Description: "Arquivo e análise de aulas de inglês do Cambly",
		Services: []application.Service{
			application.NewService(services.NewSetupService()),
			application.NewService(services.NewImportService(conn)),
			application.NewService(services.NewLibraryService(conn)),
		},
		Assets: application.AssetOptions{
			Handler:    application.AssetFileServerFS(assets),
			Middleware: services.VideoAssetMiddleware(conn, storageRoot),
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

// startJobWorker starts the background pipeline (Story 4) in a
// goroutine. storageRoot is resolved on every job, not just once here — the
// first-run wizard hasn't run yet at this point in startup, so resolving it
// eagerly would always fail on the app's first session (see
// docs/superpowers/specs/2026-07-22-historia-4-pipeline-jobs-design.md).
// Only the audio cache directory (which doesn't depend on the wizard) is resolved
// here; if that fails, it's a disk/permission problem and the worker doesn't
// start.
func startJobWorker(conn *sql.DB, storageRoot jobs.StorageRootResolver) {
	audioCacheDir, err := config.AudioCacheDir()
	if err != nil {
		log.Printf("worker de jobs não iniciado: %v", err)
		return
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

- [ ] **Step 6: Confirm the binary builds**

Run: `go build ./internal/... ./services/... .`
Expected: no error. (The `build/ios` package already fails before this change for an unrelated reason — don't use `go build ./...` for this check.)

Run: `go vet ./...`
Expected: no error.

- [ ] **Step 7: Commit**

```bash
git add services/video_asset.go services/video_asset_test.go main.go
git commit -m "feat: serve video local ao webview via asset handler (risco 1)"
```

---

### Task 8: Frontend — filter/status/duration in the Library + navigation to the Detail view

**Files:**
- Modify: `frontend/src/lib/screens/Library.svelte`
- Create: `frontend/src/lib/screens/LessonDetail.svelte`
- Modify: `frontend/src/App.svelte`

**Interfaces:**
- Consumes (bindings generated in Task 6): `LibraryService.ListLessons(filter: LessonFilter)`, `LibraryService.ListTutors()`, `LibraryService.RetryLesson(lessonID)`, `LibraryService.GetLesson(id)`, the `Lesson`/`LessonFilter` types from `../../bindings/assistente-idiomas/services/models`. Task 7's video endpoint: `GET /media/lesson/{id}`.
- Produces: `Library.svelte` gains an `onOpenLesson: (lessonId: number) => void` prop; `LessonDetail.svelte` (new) receives `{ lessonId: number; onBack: () => void }`.

This project has no automated Svelte component tests (none exist today) — verification for this task is `npm run check` (type-check) plus the manual visual verification already pending from previous stories.

- [ ] **Step 1: Rewrite `frontend/src/lib/screens/Library.svelte`**

```svelte
<script lang="ts">
  import { onMount } from "svelte";
  import { colors, fonts } from "../theme";
  import * as ImportService from "../../../bindings/assistente-idiomas/services/importservice";
  import * as LibraryService from "../../../bindings/assistente-idiomas/services/libraryservice";
  import type { PendingImport, Lesson, LessonFilter } from "../../../bindings/assistente-idiomas/services/models";
  import ImportConfirmModal from "../ImportConfirmModal.svelte";

  let { onOpenLesson }: { onOpenLesson: (lessonId: number) => void } = $props();

  let pending: PendingImport[] = $state([]);
  let lessons: Lesson[] = $state([]);
  let tutors: string[] = $state([]);
  let loading: boolean = $state(true);
  let syncing: boolean = $state(false);
  let syncMessage: string = $state("");
  let error: string = $state("");
  let reviewing: PendingImport | null = $state(null);
  let retryingId: number | null = $state(null);

  let filterTutor: string = $state("");
  let filterDateFrom: string = $state("");
  let filterDateTo: string = $state("");

  const STATUS_LABEL: Record<string, string> = {
    pronta: "pronta",
    processando: "processando…",
    erro: "erro",
  };

  async function loadPending() {
    pending = (await ImportService.ListPendingImports()) ?? [];
  }

  async function loadLessons() {
    const filter: LessonFilter = { tutor: filterTutor, dateFrom: filterDateFrom, dateTo: filterDateTo };
    lessons = (await LibraryService.ListLessons(filter)) ?? [];
  }

  async function loadTutors() {
    tutors = (await LibraryService.ListTutors()) ?? [];
  }

  // lessonDate is stored as "YYYY-MM-DD" or "YYYY-MM-DDTHH:MM" (the format
  // from <input type="datetime-local">); here it's only reformatted for
  // display in pt-BR without relying on a timezone (it's not a timestamp
  // with a "Z", it's a local time).
  function formatLessonDateTime(value: string): string {
    const [datePart, timePart] = value.split("T");
    const [year, month, day] = datePart.split("-");
    const formattedDate = `${day}/${month}/${year}`;
    return timePart ? `${formattedDate} ${timePart}` : formattedDate;
  }

  function formatDuration(seconds: number | null): string {
    if (seconds == null) return "";
    const totalMinutes = Math.round(seconds / 60);
    const hours = Math.floor(totalMinutes / 60);
    const minutes = totalMinutes % 60;
    return hours > 0 ? `${hours}h ${minutes}min` : `${minutes}min`;
  }

  async function loadAll() {
    try {
      await Promise.all([loadPending(), loadLessons(), loadTutors()]);
    } catch (e) {
      error = String(e);
    } finally {
      loading = false;
    }
  }

  async function applyFilter() {
    error = "";
    try {
      await loadLessons();
    } catch (e) {
      error = String(e);
    }
  }

  async function syncFolder() {
    error = "";
    syncMessage = "";
    syncing = true;
    try {
      const summary = await ImportService.ScanFolder();
      syncMessage = `${summary.new} novas, ${summary.updated} atualizadas, ${summary.errors} erros`;
      await loadPending();
    } catch (e) {
      error = String(e);
    } finally {
      syncing = false;
    }
  }

  function closeReview() {
    reviewing = null;
  }

  async function onConfirmed() {
    reviewing = null;
    try {
      await Promise.all([loadPending(), loadLessons(), loadTutors()]);
    } catch (e) {
      error = String(e);
    }
  }

  async function retry(lessonId: number) {
    error = "";
    retryingId = lessonId;
    try {
      await LibraryService.RetryLesson(lessonId);
      await loadLessons();
    } catch (e) {
      error = String(e);
    } finally {
      retryingId = null;
    }
  }

  function openLesson(lesson: Lesson) {
    if (lesson.status === "pronta") {
      onOpenLesson(lesson.id);
    }
  }

  onMount(loadAll);
</script>

<div class="screen" style="font-family: {fonts.body}; color: {colors.text};">
  <div class="header-row">
    <h1 style="font-family: {fonts.display};">Biblioteca</h1>
    <button onclick={syncFolder} disabled={syncing}>
      {syncing ? "Sincronizando…" : "Sincronizar pasta"}
    </button>
  </div>

  {#if syncMessage}
    <p class="hint" style="color: {colors.mut};">{syncMessage}</p>
  {/if}
  {#if error}
    <p class="error" style="color: {colors.red};">{error}</p>
  {/if}

  {#if loading}
    <p style="color: {colors.mut};">Carregando…</p>
  {:else}
    {#if pending.length > 0}
      <section class="pending" style="background: {colors.surface}; border: 1px solid {colors.line};">
        <h2 style="font-family: {fonts.display};">{pending.length} aulas aguardando revisão</h2>
        <ul>
          {#each pending as item (item.id)}
            <li>
              <span class="path" style="font-family: {fonts.mono}; color: {colors.mut};">{item.path}</span>
              <button onclick={() => (reviewing = item)}>Revisar</button>
            </li>
          {/each}
        </ul>
      </section>
    {/if}

    <section class="filters" style="background: {colors.surface}; border: 1px solid {colors.line};">
      <label>
        Tutor
        <select bind:value={filterTutor} onchange={applyFilter}>
          <option value="">Todos</option>
          {#each tutors as tutor (tutor)}
            <option value={tutor}>{tutor}</option>
          {/each}
        </select>
      </label>
      <label>
        De
        <input type="date" bind:value={filterDateFrom} onchange={applyFilter} />
      </label>
      <label>
        Até
        <input type="date" bind:value={filterDateTo} onchange={applyFilter} />
      </label>
    </section>

    {#if lessons.length > 0}
      <section class="lessons" style="background: {colors.surface}; border: 1px solid {colors.line};">
        <h2 style="font-family: {fonts.display};">{lessons.length} aulas</h2>
        <ul>
          {#each lessons as lesson (lesson.id)}
            <li>
              <button
                class="lesson-main"
                onclick={() => openLesson(lesson)}
                style="cursor: {lesson.status === 'pronta' ? 'pointer' : 'default'}; opacity: {lesson.status === 'pronta' ? 1 : 0.7};"
              >
                <span class="date" style="color: {colors.text};">{formatLessonDateTime(lesson.lessonDate)}</span>
                <span class="tutor" style="color: {colors.mut};"
                  >{lesson.tutor}{formatDuration(lesson.durationSeconds)
                    ? ` · ${formatDuration(lesson.durationSeconds)}`
                    : ""}</span
                >
              </button>
              {#if lesson.status === "erro"}
                <div class="status-block">
                  <span class="badge" style="color: {colors.red}; background: rgba(224,108,108,.1);">erro</span>
                  <span class="error-message" style="color: {colors.mut};">{lesson.errorMessage}</span>
                  <button onclick={() => retry(lesson.id)} disabled={retryingId === lesson.id}>
                    {retryingId === lesson.id ? "Reprocessando…" : "Reprocessar"}
                  </button>
                </div>
              {:else}
                <span
                  class="badge"
                  style="color: {lesson.status === 'pronta'
                    ? colors.green
                    : colors.blue}; background: {lesson.status === 'pronta'
                    ? 'rgba(111,191,142,.1)'
                    : 'rgba(110,168,254,.1)'};"
                >
                  {STATUS_LABEL[lesson.status] ?? lesson.status}
                </span>
              {/if}
            </li>
          {/each}
        </ul>
      </section>
    {/if}

    {#if pending.length === 0 && lessons.length === 0}
      <p style="color: {colors.mut};">Nenhuma aula importada ainda.</p>
    {/if}
  {/if}
</div>

{#if reviewing}
  <ImportConfirmModal pending={reviewing} {onConfirmed} onClose={closeReview} />
{/if}

<style>
  .screen {
    padding: 2rem;
    max-width: 64rem;
    margin: 0 auto;
    width: 100%;
  }
  .header-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 1rem;
  }
  h1 {
    font-size: 1.4rem;
    margin: 0;
  }
  button {
    padding: 0.5rem 1rem;
    border-radius: 0.5rem;
    cursor: pointer;
  }
  .hint {
    font-size: 0.85rem;
    margin: 0 0 1rem;
  }
  .error {
    font-size: 0.85rem;
    margin: 0 0 1rem;
  }
  .pending,
  .lessons,
  .filters {
    border-radius: 0.75rem;
    padding: 1rem 1.25rem;
    margin-bottom: 1rem;
  }
  .filters {
    display: flex;
    gap: 1.5rem;
    flex-wrap: wrap;
  }
  .filters label {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
    font-size: 0.8rem;
  }
  .pending h2,
  .lessons h2 {
    font-size: 1rem;
    margin: 0 0 0.75rem;
  }
  .pending ul,
  .lessons ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }
  .pending li {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
  }
  .lessons li {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
  }
  .lesson-main {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 0.25rem;
    background: none;
    border: none;
    padding: 0;
    text-align: left;
    flex: 1;
    min-width: 0;
  }
  .lessons .date {
    font-size: 0.85rem;
  }
  .lessons .tutor {
    font-size: 0.8rem;
  }
  .path {
    font-size: 0.8rem;
    word-break: break-all;
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
</style>
```

- [ ] **Step 2: Create `frontend/src/lib/screens/LessonDetail.svelte`**

```svelte
<script lang="ts">
  import { onMount } from "svelte";
  import { colors, fonts } from "../theme";
  import * as LibraryService from "../../../bindings/assistente-idiomas/services/libraryservice";
  import type { Lesson } from "../../../bindings/assistente-idiomas/services/models";

  let { lessonId, onBack }: { lessonId: number; onBack: () => void } = $props();

  let lesson: Lesson | null = $state(null);
  let loading: boolean = $state(true);
  let error: string = $state("");

  function formatLessonDateTime(value: string): string {
    const [datePart, timePart] = value.split("T");
    const [year, month, day] = datePart.split("-");
    const formattedDate = `${day}/${month}/${year}`;
    return timePart ? `${formattedDate} ${timePart}` : formattedDate;
  }

  onMount(async () => {
    try {
      lesson = await LibraryService.GetLesson(lessonId);
    } catch (e) {
      error = String(e);
    } finally {
      loading = false;
    }
  });
</script>

<div class="screen" style="font-family: {fonts.body}; color: {colors.text};">
  <button class="back" onclick={onBack} style="color: {colors.mut};">← Biblioteca</button>

  {#if loading}
    <p style="color: {colors.mut};">Carregando…</p>
  {:else if error}
    <p class="error" style="color: {colors.red};">{error}</p>
  {:else if lesson}
    <h1 style="font-family: {fonts.display};">{formatLessonDateTime(lesson.lessonDate)}</h1>
    <p class="tutor" style="color: {colors.mut};">{lesson.tutor}</p>
    <!-- eslint-disable-next-line svelte/no-navigation-without-resolve -->
    <video controls src={`/media/lesson/${lesson.id}`}>
      <track kind="captions" />
    </video>
  {/if}
</div>

<style>
  .screen {
    padding: 2rem;
    max-width: 64rem;
    margin: 0 auto;
    width: 100%;
  }
  .back {
    background: none;
    border: none;
    cursor: pointer;
    font-size: 0.85rem;
    margin-bottom: 1rem;
    padding: 0;
  }
  h1 {
    font-size: 1.4rem;
    margin: 0 0 0.25rem;
  }
  .tutor {
    font-size: 0.9rem;
    margin: 0 0 1.5rem;
  }
  video {
    width: 100%;
    border-radius: 0.75rem;
    background: black;
  }
  .error {
    font-size: 0.85rem;
  }
</style>
```

- [ ] **Step 3: Wire up the Detail route in `frontend/src/App.svelte`**

Rewrite the whole file:

```svelte
<script lang="ts">
  import { onMount } from "svelte";
  import Sidebar from "./lib/Sidebar.svelte";
  import Header from "./lib/Header.svelte";
  import Library from "./lib/screens/Library.svelte";
  import LessonDetail from "./lib/screens/LessonDetail.svelte";
  import Progress from "./lib/screens/Progress.svelte";
  import Queue from "./lib/screens/Queue.svelte";
  import SetupWizard from "./lib/SetupWizard.svelte";
  import { colors, fonts } from "./lib/theme";
  import * as SetupService from "../bindings/assistente-idiomas/services/setupservice";

  type NavScreen = "library" | "progress" | "queue";
  type Route =
    | { screen: "library" }
    | { screen: "lesson-detail"; lessonId: number }
    | { screen: "progress" }
    | { screen: "queue" };

  let route: Route = $state({ screen: "library" });
  let checkingFirstRun = $state(true);
  let firstRun = $state(false);

  function navigate(screen: NavScreen) {
    route = { screen };
  }

  function openLesson(lessonId: number) {
    route = { screen: "lesson-detail", lessonId };
  }

  onMount(async () => {
    try {
      firstRun = await SetupService.IsFirstRun();
    } finally {
      checkingFirstRun = false;
    }
  });
</script>

{#if checkingFirstRun}
  <div class="shell" style="background: {colors.bg};"></div>
{:else if firstRun}
  <SetupWizard onComplete={() => (firstRun = false)} />
{:else}
  <div class="shell" style="background: {colors.bg}; font-family: {fonts.body};">
    <Sidebar active={route.screen === "lesson-detail" ? "library" : route.screen} onNavigate={navigate} />
    <main class="main">
      <Header />
      <div class="content">
        {#if route.screen === "library"}
          <Library onOpenLesson={openLesson} />
        {:else if route.screen === "lesson-detail"}
          <LessonDetail lessonId={route.lessonId} onBack={() => navigate("library")} />
        {:else if route.screen === "progress"}
          <Progress />
        {:else}
          <Queue />
        {/if}
      </div>
    </main>
  </div>
{/if}

<style>
  .shell {
    display: flex;
    width: 100%;
    height: 100vh;
  }
  .main {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .content {
    flex: 1;
    overflow-y: auto;
  }
</style>
```

- [ ] **Step 4: Frontend type-check**

Run: `cd frontend && npm run check`
Expected: `0 ERRORS`. If a type error shows up, it's most likely that Task 6's generated shape ended up different from what's documented under **Interfaces** — check `frontend/bindings/assistente-idiomas/services/models.ts` and `libraryservice.ts` against the code above and adjust names/types, not the logic.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/lib/screens/Library.svelte frontend/src/lib/screens/LessonDetail.svelte frontend/src/App.svelte
git commit -m "feat: filtro/status/duracao na Biblioteca e navegacao para o Detalhe"
```

---

### Task 9: Final verification and progress log entry

**Files:**
- Modify: `docs/fase-1-mvp.md`

- [ ] **Step 1: Full Go suite**

Run: `go vet ./...`
Expected: no output (clean).

Run: `go test ./...`
Expected: `ok` on every package with tests (`internal/analysis`, `internal/config`, `internal/db`, `internal/importer`, `internal/jobs`, `internal/media`, `internal/stt`, `services`); `[no test files]` on the rest.

- [ ] **Step 2: Frontend**

Run: `cd frontend && npm run check`
Expected: `0 ERRORS`.

Run: `cd frontend && npm run build:dev`
Expected: the development build finishes with no error (confirms Svelte/TS compiles end to end, including the regenerated bindings).

- [ ] **Step 3: Build the binary**

Run: `go build ./internal/... ./services/... .`
Expected: no error. (Don't use `go build ./...`: `build/ios` already fails before this story for a reason unrelated to it — pre-existing, out of scope.)

- [ ] **Step 4: Update `docs/fase-1-mvp.md`**

Check off Story 5's acceptance criteria (`## Story 5 — Real library` section), keeping the note about pending visual verification (same pattern as Stories 1, 3, and 4):

```markdown
### Acceptance criteria
- [x] A real list from the database: date, tutor, duration, status (processing/ready/error), in the prototype's layout.
- [x] Simple filter by tutor and period (full-text search is left for a future phase).
- [x] A `ready` lesson opens the Detail view (minimal stub: date/tutor/video, no transcript — sync is Story 6); `processing` shows its state; `error` shows a message and a reprocess action (recreate the job).
```

Add a row to the `## Progress log` table (end of the file):

```markdown
| 22/07/2026 | Story 5 implemented: Library status derived from jobs (extract_audio/transcribe) on every read — never a separately stored column; duration computed via ffprobe on a best-effort basis at import confirmation (never blocks confirmation); filter by tutor (dropdown)/period; a "Reprocess" button resets a lesson's errored jobs (including a transcribe blocked by a dependency) without explicitly waking the worker (fallback poll); the minimal Detail view (date/tutor/video) resolves the project's technical risk 1 — a `GET /media/lesson/{id}` endpoint via the stdlib `http.ServeFile`, which already handles range requests, plugged in as a Wails v3 `application.Middleware` | Risk 1 truly resolved (not a placeholder): confirmed by reading Wails v3's source code that the webview always talks to the Go server, both in production (embedded assets) and in `wails3 dev` (proxied to Vite) — the middleware intercepts before either path; Story 6 reuses the same endpoint, just adding the synchronized transcript; frontend bindings need the `-i` flag in addition to `-ts` (`wails3 generate bindings -ts -i ./...`) to generate interfaces instead of classes — without it the generator produces `.js` with classes, a format this project's frontend doesn't use; the full flow (Library → Detail → video playing) still hasn't been visually verified in a real window (no display in this build environment), same pattern as previous stories |
```

- [ ] **Step 5: Commit**

```bash
git add docs/fase-1-mvp.md
git commit -m "docs: mark Story 5 complete and record progress"
```

---

## Final note for the executor

After Task 9, run the real app (`wails3 dev` on a machine with a display) to visually confirm: the Library showing status/duration/filter, the "Reprocessar" button on a lesson with a deliberate error (e.g.: remove `ffmpeg` from the PATH before confirming an import), and the Detail view playing the video with seeking working (dragging the progress bar) — this closes the visual verification pending since Story 1, specific to this slice.
