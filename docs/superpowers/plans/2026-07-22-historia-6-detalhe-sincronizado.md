# Story 6 — Lesson Detail: Synchronized Video + Transcript — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The Lesson Detail screen shows the video (already served with range requests since
Story 5) alongside the scrollable transcript, with click-on-utterance seeking the video, highlight
of the current utterance following playback, and a simple toggle to mark which speaker is the
student — persisted per lesson.

**Architecture:** The Go backend adds a column (`lessons.student_speaker_label`) and three new
operations (`FindTranscriptByLessonID`, `FindLessonWithStatusByID`, `SetStudentSpeaker`) exposed
via `LibraryService`. The Svelte 5 frontend rewrites `LessonDetail.svelte`: sync via the
`<video>`'s `timeupdate` event + derived state (no RAF, no WebVTT), a 2-column grid
(prototype `docs/prototipo-app-aulas.jsx`), a panel with no tabs (Analysis is Phase 2).

**Tech Stack:** Go (`database/sql`, `encoding/json`), SQLite via `modernc.org/sqlite` + `goose`,
Svelte 5 (runes), Wails v3 bindings generated via `wails3 generate bindings -ts -i ./...`.

## Global Constraints

- Thin layer: no Wails import in `internal/` (only in `services/`, `main.go`).
- Portable SQL in the repository layer (`internal/db`) — nothing driver-specific.
- Code/identifiers in English; error messages and UI text in PT-BR.
- No legacy Svelte syntax (always `$state`/`$derived`/`$effect`/`$props`).
- No LLM analysis in the UI, no word-level highlight/click, no Queue screen — out of scope
  for this story (see spec).
- No "live" panel update while open: after clicking "Retry" in the Detail screen, the panel
  shows "processing" once (immediate refetch of `GetLesson`) — following the job until it
  completes and watching the transcript appear on its own is out of scope (that's the
  `job:updated` event from Story 7, still with no consumer).
- Reference spec: `docs/superpowers/specs/2026-07-22-historia-6-detalhe-sincronizado-design.md`.

---

### Task 1: Migration + `student_speaker_label` column + `SetStudentSpeaker` (internal/db)

**Files:**
- Create: `internal/db/migrations/00003_student_speaker.sql`
- Modify: `internal/db/lessons.go` (struct `Lesson`, const `lessonColumns`, `scanLessonRow`; new `SetStudentSpeaker`)
- Test: `internal/db/lessons_test.go`

**Interfaces:**
- Produces: `db.Lesson.StudentSpeakerLabel *string` (new field); `db.SetStudentSpeaker(conn *sql.DB, lessonID int64, speakerLabel string) error`.

- [ ] **Step 1: Create the migration**

`internal/db/migrations/00003_student_speaker.sql`:
```sql
-- +goose Up
ALTER TABLE lessons ADD COLUMN student_speaker_label TEXT;

-- +goose Down
ALTER TABLE lessons DROP COLUMN student_speaker_label;
```

- [ ] **Step 2: Write the failing test (`SetStudentSpeaker` round-trip + read via `FindLessonByID`)**

Add to the end of `internal/db/lessons_test.go`:
```go
func TestSetStudentSpeaker_RoundTripsThroughFindLessonByID(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() unexpected error: %v", err)
	}
	defer conn.Close()

	res, err := conn.Exec(
		`INSERT INTO lessons (lesson_date, tutor, video_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"2026-07-15", "Sarah", "aula-01.mp4", "2026-07-15T10:00:00Z", "2026-07-15T10:00:00Z",
	)
	if err != nil {
		t.Fatalf("fixture insert failed: %v", err)
	}
	id, _ := res.LastInsertId()

	before, err := FindLessonByID(conn, id)
	if err != nil {
		t.Fatalf("FindLessonByID() unexpected error: %v", err)
	}
	if before.StudentSpeakerLabel != nil {
		t.Errorf("StudentSpeakerLabel = %v, expected nil before SetStudentSpeaker", *before.StudentSpeakerLabel)
	}

	if err := SetStudentSpeaker(conn, id, "speaker_1"); err != nil {
		t.Fatalf("SetStudentSpeaker() unexpected error: %v", err)
	}

	after, err := FindLessonByID(conn, id)
	if err != nil {
		t.Fatalf("FindLessonByID() unexpected error: %v", err)
	}
	if after.StudentSpeakerLabel == nil || *after.StudentSpeakerLabel != "speaker_1" {
		t.Errorf("StudentSpeakerLabel = %v, expected speaker_1", after.StudentSpeakerLabel)
	}
}
```

- [ ] **Step 3: Run the test and confirm it fails**

Run: `go test ./internal/db/... -run TestSetStudentSpeaker_RoundTripsThroughFindLessonByID -v`
Expected: FAIL — `undefined: SetStudentSpeaker` (compile error) and/or `StudentSpeakerLabel` doesn't exist on `Lesson`.

- [ ] **Step 4: Implement — column on the struct, `lessonColumns`, `scanLessonRow`, `SetStudentSpeaker`**

In `internal/db/lessons.go`, replace the `Lesson` struct (lines 15-24):
```go
type Lesson struct {
	ID                  int64
	LessonDate          string
	Tutor               string
	VideoPath           string
	VideoHash           string
	FileSize            int64
	FileMTime           string
	DurationSeconds     *int64
	StudentSpeakerLabel *string
}
```

Replace `lessonColumns` (line 29):
```go
const lessonColumns = `id, lesson_date, tutor, video_path, COALESCE(video_hash, ''), COALESCE(file_size, 0), COALESCE(file_mtime, ''), duration_seconds, student_speaker_label`
```

Replace `scanLessonRow` (lines 34-49):
```go
func scanLessonRow(row *sql.Row) (*Lesson, error) {
	var l Lesson
	var duration sql.NullInt64
	var studentSpeaker sql.NullString
	err := row.Scan(&l.ID, &l.LessonDate, &l.Tutor, &l.VideoPath, &l.VideoHash, &l.FileSize, &l.FileMTime, &duration, &studentSpeaker)
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
	if studentSpeaker.Valid {
		s := studentSpeaker.String
		l.StudentSpeakerLabel = &s
	}
	return &l, nil
}
```

Add right after `SetLessonDuration` (after line 112):
```go

// SetStudentSpeaker records which raw speaker (e.g. "speaker_0") is the
// student in this lesson — the choice made via the Detail screen's toggle
// (Story 6). Overwrites any previous value, letting the user correct it.
func SetStudentSpeaker(conn *sql.DB, lessonID int64, speakerLabel string) error {
	_, err := conn.Exec(
		`UPDATE lessons SET student_speaker_label = ?, updated_at = ? WHERE id = ?`,
		speakerLabel, time.Now().UTC().Format(time.RFC3339), lessonID,
	)
	if err != nil {
		return fmt.Errorf("write student_speaker_label for lesson %d: %w", lessonID, err)
	}
	return nil
}
```

- [ ] **Step 5: Run the test and confirm it passes**

Run: `go test ./internal/db/... -run TestSetStudentSpeaker_RoundTripsThroughFindLessonByID -v`
Expected: PASS

- [ ] **Step 6: Run the entire `internal/db` suite (make sure `lessonColumns`/`scanLessonRow` didn't break anything existing)**

Run: `go test ./internal/db/... -v`
Expected: PASS on every test (including `TestFindLessonByID_FindsExistingAndNilWhenMissing`, `TestSetLessonDuration_UpdatesDurationSeconds`, etc.)

- [ ] **Step 7: Commit**

```bash
git add internal/db/migrations/00003_student_speaker.sql internal/db/lessons.go internal/db/lessons_test.go
git commit -m "feat: adiciona student_speaker_label em lessons"
```

---

### Task 2: `FindTranscriptByLessonID` (internal/db)

**Files:**
- Modify: `internal/db/transcripts.go`
- Test: `internal/db/transcripts_test.go`

**Interfaces:**
- Consumes: `stt.Utterance{ Speaker, Text string; Start, End time.Duration; Words []stt.Word }` (`internal/stt/stt.go:25-30`).
- Produces: `db.Transcript{ LessonID int64; RawJSONPath string; Utterances []stt.Utterance }`; `db.FindTranscriptByLessonID(conn *sql.DB, lessonID int64) (*db.Transcript, error)` — returns `(nil, nil)` if there's no transcript.

- [ ] **Step 1: Write the failing test**

Add to the end of `internal/db/transcripts_test.go` (add `"encoding/json"` and `"assistente-idiomas/internal/stt"` to the imports):
```go
func TestFindTranscriptByLessonID_NilWhenMissing(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() unexpected error: %v", err)
	}
	defer conn.Close()

	res, err := conn.Exec(
		`INSERT INTO lessons (lesson_date, tutor, video_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"2026-07-22", "Fulano", "aula.mp4", "2026-07-22T09:00:00Z", "2026-07-22T09:00:00Z",
	)
	if err != nil {
		t.Fatalf("fixture lesson insert failed: %v", err)
	}
	lessonID, _ := res.LastInsertId()

	tr, err := FindTranscriptByLessonID(conn, lessonID)
	if err != nil {
		t.Fatalf("FindTranscriptByLessonID() unexpected error: %v", err)
	}
	if tr != nil {
		t.Errorf("FindTranscriptByLessonID() = %+v, expected nil with no transcript", tr)
	}
}

func TestFindTranscriptByLessonID_UnmarshalsUtterances(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() unexpected error: %v", err)
	}
	defer conn.Close()

	res, err := conn.Exec(
		`INSERT INTO lessons (lesson_date, tutor, video_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"2026-07-22", "Fulano", "aula.mp4", "2026-07-22T09:00:00Z", "2026-07-22T09:00:00Z",
	)
	if err != nil {
		t.Fatalf("fixture lesson insert failed: %v", err)
	}
	lessonID, _ := res.LastInsertId()

	want := []stt.Utterance{
		{Speaker: "speaker_0", Text: "Hello", Start: 0, End: 2 * time.Second},
		{Speaker: "speaker_1", Text: "Hi there", Start: 2 * time.Second, End: 5 * time.Second},
	}
	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("fixture json.Marshal() failed: %v", err)
	}
	if err := InsertTranscript(conn, lessonID, "aula.transcript.json", string(raw)); err != nil {
		t.Fatalf("InsertTranscript() unexpected error: %v", err)
	}

	tr, err := FindTranscriptByLessonID(conn, lessonID)
	if err != nil {
		t.Fatalf("FindTranscriptByLessonID() unexpected error: %v", err)
	}
	if tr == nil {
		t.Fatal("FindTranscriptByLessonID() = nil, expected a found transcript")
	}
	if tr.RawJSONPath != "aula.transcript.json" {
		t.Errorf("RawJSONPath = %q, expected aula.transcript.json", tr.RawJSONPath)
	}
	if len(tr.Utterances) != 2 {
		t.Fatalf("Utterances = %+v, expected 2 utterances", tr.Utterances)
	}
	if tr.Utterances[0].Speaker != "speaker_0" || tr.Utterances[0].End != 2*time.Second {
		t.Errorf("Utterances[0] = %+v, doesn't match the fixture", tr.Utterances[0])
	}
	if tr.Utterances[1].Text != "Hi there" || tr.Utterances[1].Start != 2*time.Second {
		t.Errorf("Utterances[1] = %+v, doesn't match the fixture", tr.Utterances[1])
	}
}
```

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `go test ./internal/db/... -run TestFindTranscriptByLessonID -v`
Expected: FAIL — `undefined: FindTranscriptByLessonID`

- [ ] **Step 3: Implement `FindTranscriptByLessonID`**

Replace the entire content of `internal/db/transcripts.go`:
```go
package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"assistente-idiomas/internal/stt"
)

// HasTranscript reports whether a transcript is already recorded for
// lessonID — used by internal/jobs.Worker for the transcribe job's
// idempotency.
func HasTranscript(conn *sql.DB, lessonID int64) (bool, error) {
	var id int64
	err := conn.QueryRow(`SELECT id FROM transcripts WHERE lesson_id = ?`, lessonID).Scan(&id)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("find transcript for lesson %d: %w", lessonID, err)
	}
	return true, nil
}

// InsertTranscript records the transcript of a lesson. rawJSONPath is
// relative to storage_root (same convention as lessons.video_path);
// utterancesJSON is already serialized ([]stt.Utterance in JSON).
func InsertTranscript(conn *sql.DB, lessonID int64, rawJSONPath string, utterancesJSON string) error {
	_, err := conn.Exec(
		`INSERT INTO transcripts (lesson_id, raw_json_path, utterances, created_at) VALUES (?, ?, ?, ?)`,
		lessonID, rawJSONPath, utterancesJSON, time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("insert transcript for lesson %d: %w", lessonID, err)
	}
	return nil
}

// Transcript is the complete transcript of a lesson, already deserialized.
type Transcript struct {
	LessonID    int64
	RawJSONPath string
	Utterances  []stt.Utterance
}

// FindTranscriptByLessonID looks up a lesson's transcript. Returns
// (nil, nil) if no transcript has been recorded yet — the normal state
// while the transcribe job is pending/running or has failed (see
// LibraryService.GetLesson/GetTranscript, Story 6), not an error.
func FindTranscriptByLessonID(conn *sql.DB, lessonID int64) (*Transcript, error) {
	var rawJSONPath, utterancesJSON string
	err := conn.QueryRow(
		`SELECT raw_json_path, utterances FROM transcripts WHERE lesson_id = ?`, lessonID,
	).Scan(&rawJSONPath, &utterancesJSON)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find transcript for lesson %d: %w", lessonID, err)
	}
	var utterances []stt.Utterance
	if err := json.Unmarshal([]byte(utterancesJSON), &utterances); err != nil {
		return nil, fmt.Errorf("unmarshal utterances for lesson %d: %w", lessonID, err)
	}
	return &Transcript{LessonID: lessonID, RawJSONPath: rawJSONPath, Utterances: utterances}, nil
}
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `go test ./internal/db/... -run TestFindTranscriptByLessonID -v`
Expected: PASS (both tests)

- [ ] **Step 5: Run the entire `internal/db` suite**

Run: `go test ./internal/db/... -v`
Expected: PASS on every test

- [ ] **Step 6: Commit**

```bash
git add internal/db/transcripts.go internal/db/transcripts_test.go
git commit -m "feat: adiciona leitura de transcript por lesson id"
```

---

### Task 3: `FindLessonWithStatusByID` + new column in `ListLessonsWithStatus` (internal/db)

Refactors `lesson_status.go` to share the column list/JOIN between `ListLessonsWithStatus`
(multiple rows) and the new `FindLessonWithStatusByID` (single row), avoiding two queries that
could drift apart. This is also where `student_speaker_label` starts being read alongside the
status.

**Files:**
- Modify: `internal/db/lesson_status.go` (rewritten in full)
- Test: `internal/db/lesson_status_test.go`

**Interfaces:**
- Consumes: `db.Lesson.StudentSpeakerLabel *string` (Task 1); `deriveStatus` (already existing, no signature change).
- Produces: `db.FindLessonWithStatusByID(conn *sql.DB, id int64) (*db.LessonWithStatus, error)` — returns `(nil, nil)` if the lesson doesn't exist. `db.LessonWithStatus.StudentSpeakerLabel *string` (promoted from `Lesson`, already populated).

- [ ] **Step 1: Write the failing test**

Add to the end of `internal/db/lesson_status_test.go`:
```go
func TestFindLessonWithStatusByID_FindsExistingWithStatusAndNilWhenMissing(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() unexpected error: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	mustInsertJob(t, conn, lessonID, "extract_audio", "done", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")
	mustInsertJob(t, conn, lessonID, "transcribe", "done", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")
	if err := SetStudentSpeaker(conn, lessonID, "speaker_0"); err != nil {
		t.Fatalf("SetStudentSpeaker() unexpected error: %v", err)
	}

	found, err := FindLessonWithStatusByID(conn, lessonID)
	if err != nil {
		t.Fatalf("FindLessonWithStatusByID() unexpected error: %v", err)
	}
	if found == nil || found.Status != "pronta" {
		t.Fatalf("FindLessonWithStatusByID() = %+v, expected status=pronta", found)
	}
	if found.StudentSpeakerLabel == nil || *found.StudentSpeakerLabel != "speaker_0" {
		t.Errorf("StudentSpeakerLabel = %v, expected speaker_0", found.StudentSpeakerLabel)
	}

	missing, err := FindLessonWithStatusByID(conn, lessonID+999)
	if err != nil {
		t.Fatalf("FindLessonWithStatusByID() unexpected error: %v", err)
	}
	if missing != nil {
		t.Errorf("FindLessonWithStatusByID() for a nonexistent id = %+v, expected nil", missing)
	}
}
```

- [ ] **Step 2: Run the test and confirm it fails**

Run: `go test ./internal/db/... -run TestFindLessonWithStatusByID -v`
Expected: FAIL — `undefined: FindLessonWithStatusByID`

- [ ] **Step 3: Implement — rewrite `internal/db/lesson_status.go` in full**

```go
// internal/db/lesson_status.go
package db

import (
	"database/sql"
	"fmt"
)

// LessonFilter filters ListLessonsWithStatus — all fields are optional
// (empty string = no filter), used by the Library's tutor/date-range
// filter (Story 5).
type LessonFilter struct {
	Tutor    string
	DateFrom string // YYYY-MM-DD, inclusive
	DateTo   string // YYYY-MM-DD, inclusive
}

// LessonWithStatus is a lesson with the status derived from the
// extract_audio/transcribe jobs. Status is always one of "processando",
// "pronta", "erro"; ErrorMessage is only filled in when Status == "erro" —
// see the derivation rules in deriveStatus.
type LessonWithStatus struct {
	Lesson
	Status       string
	ErrorMessage string
}

// lessonWithStatusColumns and lessonWithStatusFromJoin are shared by
// ListLessonsWithStatus (multiple rows) and FindLessonWithStatusByID
// (single row, Story 6) — the same column list/JOIN, so they can't drift
// apart.
const lessonWithStatusColumns = `
		l.id, l.lesson_date, l.tutor, l.video_path,
		COALESCE(l.video_hash, ''), COALESCE(l.file_size, 0), COALESCE(l.file_mtime, ''),
		l.duration_seconds, l.student_speaker_label,
		COALESCE(ea.status, ''), COALESCE(ea.last_error, ''),
		COALESCE(tr.status, ''), COALESCE(tr.last_error, '')`

const lessonWithStatusFromJoin = `
	FROM lessons l
	LEFT JOIN jobs ea ON ea.lesson_id = l.id AND ea.kind = 'extract_audio'
	LEFT JOIN jobs tr ON tr.lesson_id = l.id AND tr.kind = 'transcribe'`

// rowScanner is satisfied by both *sql.Row (single row) and *sql.Rows
// (multiple rows) — lets the scan be shared between
// ListLessonsWithStatus and FindLessonWithStatusByID.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanLessonWithStatusRow(s rowScanner) (LessonWithStatus, error) {
	var lws LessonWithStatus
	var duration sql.NullInt64
	var studentSpeaker sql.NullString
	var extractStatus, extractError, transcribeStatus, transcribeError string
	err := s.Scan(
		&lws.ID, &lws.LessonDate, &lws.Tutor, &lws.VideoPath,
		&lws.VideoHash, &lws.FileSize, &lws.FileMTime,
		&duration, &studentSpeaker,
		&extractStatus, &extractError,
		&transcribeStatus, &transcribeError,
	)
	if err != nil {
		return LessonWithStatus{}, err
	}
	if duration.Valid {
		d := duration.Int64
		lws.DurationSeconds = &d
	}
	if studentSpeaker.Valid {
		sp := studentSpeaker.String
		lws.StudentSpeakerLabel = &sp
	}
	lws.Status, lws.ErrorMessage = deriveStatus(extractStatus, extractError, transcribeStatus, transcribeError)
	return lws, nil
}

// ListLessonsWithStatus lists confirmed lessons with the status derived
// from the jobs, most recent first, applying filter (empty fields are
// ignored). The date filter only compares the YYYY-MM-DD part of
// lesson_date (which can carry a time, from an <input type="datetime-local">
// format), so lessons with a recorded time are included across the whole
// day of the range.
func ListLessonsWithStatus(conn *sql.DB, filter LessonFilter) ([]LessonWithStatus, error) {
	query := `SELECT` + lessonWithStatusColumns + lessonWithStatusFromJoin + ` WHERE 1=1`
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
		return nil, fmt.Errorf("list lessons with status: %w", err)
	}
	defer rows.Close()

	out := make([]LessonWithStatus, 0)
	for rows.Next() {
		lws, err := scanLessonWithStatusRow(rows)
		if err != nil {
			return nil, fmt.Errorf("read lesson with status: %w", err)
		}
		out = append(out, lws)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate lessons with status: %w", err)
	}
	return out, nil
}

// FindLessonWithStatusByID looks up a lesson by id with the status already
// derived from its jobs (same rules as ListLessonsWithStatus) — used by
// the Detail screen (Story 6), which now opens regardless of status, not
// just "pronta" (see services.LibraryService.GetLesson). Returns
// (nil, nil) if the lesson doesn't exist.
func FindLessonWithStatusByID(conn *sql.DB, id int64) (*LessonWithStatus, error) {
	row := conn.QueryRow(`SELECT`+lessonWithStatusColumns+lessonWithStatusFromJoin+` WHERE l.id = ?`, id)
	lws, err := scanLessonWithStatusRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find lesson with status by id %d: %w", id, err)
	}
	return &lws, nil
}

// deriveStatus applies the Library's status rules (Story 5): an
// extract_audio error is the root cause and takes priority over a
// transcribe error (which stays blocked when its extract_audio failed —
// see claimNextEligibleJob in internal/jobs/worker.go); "pronta" requires
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

- [ ] **Step 4: Run the new test and confirm it passes**

Run: `go test ./internal/db/... -run TestFindLessonWithStatusByID -v`
Expected: PASS

- [ ] **Step 5: Run the entire `internal/db` suite (make sure the refactor didn't break `ListLessonsWithStatus`)**

Run: `go test ./internal/db/... -v`
Expected: PASS on every test, including the 5 existing tests in `lesson_status_test.go`
(`TestListLessonsWithStatus_ProcessandoWhenNoJobsDone`,
`TestListLessonsWithStatus_ProntaWhenTranscribeDone`,
`TestListLessonsWithStatus_ErroComMensagemDaCausaRaiz`,
`TestListLessonsWithStatus_ErroQuandoSoTranscribeFalhou`,
`TestListLessonsWithStatus_FiltraPorTutorEPeriodo`).

- [ ] **Step 6: Commit**

```bash
git add internal/db/lesson_status.go internal/db/lesson_status_test.go
git commit -m "feat: adiciona busca de lesson por id com status derivado"
```

---

### Task 4: `services/library.go` — `GetLesson` with status, `GetTranscript`, `SetStudentSpeaker`

**Files:**
- Modify: `services/library.go`
- Test: `services/library_test.go`

**Interfaces:**
- Consumes: `db.FindLessonWithStatusByID` (Task 3), `db.FindTranscriptByLessonID` (Task 2), `db.SetStudentSpeaker` (Task 1).
- Produces (used by the frontend via bindings, Task 5):
  - `services.Lesson` gains `StudentSpeakerLabel *string \`json:"studentSpeakerLabel"\``.
  - `services.Transcript{ Utterances []Utterance \`json:"utterances"\` }`.
  - `services.Utterance{ Speaker string \`json:"speaker"\`; Text string \`json:"text"\`; StartSeconds float64 \`json:"startSeconds"\`; EndSeconds float64 \`json:"endSeconds"\` }`.
  - `(s *LibraryService) GetLesson(id int64) (Lesson, error)` — now with `Status`/`ErrorMessage`/`StudentSpeakerLabel` filled in.
  - `(s *LibraryService) GetTranscript(lessonID int64) (Transcript, error)` — errors if there's no transcript.
  - `(s *LibraryService) SetStudentSpeaker(lessonID int64, speakerLabel string) error`.

- [ ] **Step 1: Write the failing tests**

Add to the end of `services/library_test.go`:
```go
func TestLibraryService_GetLesson_IncludesStatusAndStudentSpeaker(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() failed: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLesson(t, conn, "2026-07-20", "Sarah M.", "aula.mp4")
	mustInsertJobWithStatus(t, conn, lessonID, "extract_audio", "done", "")
	mustInsertJobWithStatus(t, conn, lessonID, "transcribe", "running", "")

	svc := NewLibraryService(conn)
	lesson, err := svc.GetLesson(lessonID)
	if err != nil {
		t.Fatalf("GetLesson() unexpected error: %v", err)
	}
	if lesson.Status != "processando" {
		t.Errorf("GetLesson().Status = %q, expected processando (transcribe still running)", lesson.Status)
	}
	if lesson.StudentSpeakerLabel != nil {
		t.Errorf("GetLesson().StudentSpeakerLabel = %v, expected nil before the toggle", lesson.StudentSpeakerLabel)
	}

	if err := svc.SetStudentSpeaker(lessonID, "speaker_0"); err != nil {
		t.Fatalf("SetStudentSpeaker() unexpected error: %v", err)
	}
	lesson, err = svc.GetLesson(lessonID)
	if err != nil {
		t.Fatalf("GetLesson() unexpected error: %v", err)
	}
	if lesson.StudentSpeakerLabel == nil || *lesson.StudentSpeakerLabel != "speaker_0" {
		t.Errorf("GetLesson().StudentSpeakerLabel = %v, expected speaker_0", lesson.StudentSpeakerLabel)
	}
}

func TestLibraryService_GetTranscript_ReturnsUtterancesInSeconds(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() failed: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLesson(t, conn, "2026-07-20", "Sarah M.", "aula.mp4")
	mustInsertJobWithStatus(t, conn, lessonID, "extract_audio", "done", "")
	mustInsertJobWithStatus(t, conn, lessonID, "transcribe", "done", "")
	utterancesJSON := `[{"Speaker":"speaker_0","Text":"Hello","Start":0,"End":2000000000},{"Speaker":"speaker_1","Text":"Hi","Start":2000000000,"End":3500000000}]`
	if err := db.InsertTranscript(conn, lessonID, "aula.transcript.json", utterancesJSON); err != nil {
		t.Fatalf("InsertTranscript() unexpected error: %v", err)
	}

	svc := NewLibraryService(conn)
	tr, err := svc.GetTranscript(lessonID)
	if err != nil {
		t.Fatalf("GetTranscript() unexpected error: %v", err)
	}
	if len(tr.Utterances) != 2 {
		t.Fatalf("GetTranscript().Utterances = %+v, expected 2 utterances", tr.Utterances)
	}
	if tr.Utterances[0].Speaker != "speaker_0" || tr.Utterances[0].StartSeconds != 0 || tr.Utterances[0].EndSeconds != 2 {
		t.Errorf("Utterances[0] = %+v, expected speaker_0 0s-2s", tr.Utterances[0])
	}
	if tr.Utterances[1].StartSeconds != 2 || tr.Utterances[1].EndSeconds != 3.5 {
		t.Errorf("Utterances[1] = %+v, expected 2s-3.5s", tr.Utterances[1])
	}
}

func TestLibraryService_GetTranscript_ErrorsWhenNoTranscriptYet(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() failed: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLesson(t, conn, "2026-07-20", "Sarah M.", "aula.mp4")
	mustInsertJobWithStatus(t, conn, lessonID, "extract_audio", "done", "")
	mustInsertJobWithStatus(t, conn, lessonID, "transcribe", "running", "")

	svc := NewLibraryService(conn)
	if _, err := svc.GetTranscript(lessonID); err == nil {
		t.Error("GetTranscript() with no transcript expected an error, got nil")
	}
}
```

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `go test ./services/... -run "TestLibraryService_GetLesson_IncludesStatusAndStudentSpeaker|TestLibraryService_GetTranscript" -v`
Expected: FAIL — `undefined: svc.SetStudentSpeaker` / `undefined: svc.GetTranscript` (compile error)

- [ ] **Step 3: Implement — rewrite `services/library.go` in full**

```go
package services

import (
	"database/sql"
	"fmt"

	"assistente-idiomas/internal/db"
)

// LibraryService exposes already-confirmed lessons to the Library —
// listing with status derived from the jobs and duration (Story 5),
// filtering by tutor/date range, reprocessing lessons with an error,
// looking up a lesson for the Detail screen and its synchronized
// transcript (Story 6).
type LibraryService struct {
	conn *sql.DB
}

func NewLibraryService(conn *sql.DB) *LibraryService {
	return &LibraryService{conn: conn}
}

// Lesson is a confirmed lesson, in the format exposed to the frontend.
// Status is always one of "processando", "pronta", "erro" (see
// db.LessonWithStatus); ErrorMessage is only filled in when
// Status == "erro". DurationSeconds is nil until the duration probe
// (best-effort, at import confirmation time) succeeds.
// StudentSpeakerLabel is nil until the user picks who the student is via
// the Detail screen's toggle (Story 6).
type Lesson struct {
	ID                  int64   `json:"id"`
	LessonDate          string  `json:"lessonDate"`
	Tutor               string  `json:"tutor"`
	VideoPath           string  `json:"videoPath"`
	DurationSeconds     *int64  `json:"durationSeconds"`
	Status              string  `json:"status"`
	ErrorMessage        string  `json:"errorMessage"`
	StudentSpeakerLabel *string `json:"studentSpeakerLabel"`
}

// LessonFilter filters ListLessons — empty fields are ignored (no filter
// on that criterion).
type LessonFilter struct {
	Tutor    string `json:"tutor"`
	DateFrom string `json:"dateFrom"`
	DateTo   string `json:"dateTo"`
}

// Transcript is a lesson's transcript, in the format exposed to the Detail
// screen (Story 6).
type Transcript struct {
	Utterances []Utterance `json:"utterances"`
}

// Utterance is one line of the transcript. Timestamps in seconds — the
// same unit as HTMLVideoElement.currentTime on the frontend, converted
// here at the service boundary (the database stores time.Duration).
type Utterance struct {
	Speaker      string  `json:"speaker"`
	Text         string  `json:"text"`
	StartSeconds float64 `json:"startSeconds"`
	EndSeconds   float64 `json:"endSeconds"`
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
			ID:                  r.ID,
			LessonDate:          r.LessonDate,
			Tutor:               r.Tutor,
			VideoPath:           r.VideoPath,
			DurationSeconds:     r.DurationSeconds,
			Status:              r.Status,
			ErrorMessage:        r.ErrorMessage,
			StudentSpeakerLabel: r.StudentSpeakerLabel,
		})
	}
	return out, nil
}

// ListTutors lists the distinct tutors already registered, for the
// Library's filter dropdown.
func (s *LibraryService) ListTutors() ([]string, error) {
	return db.ListTutors(s.conn)
}

// RetryLesson resets the lesson's errored jobs back to "pending" — the
// jobs worker (internal/jobs) picks the pipeline back up on its own at
// the next poll (~5s), with no need to wake it explicitly (same decision
// as Story 4). It's not an error if the lesson has no job currently in
// error.
func (s *LibraryService) RetryLesson(lessonID int64) error {
	_, err := db.ResetErrorJobsForLesson(s.conn, lessonID)
	return err
}

// GetLesson looks up a lesson by id, with status/error derived from its
// jobs, for the Detail screen (Story 6) — which now opens regardless of
// status: "processando" and "erro" show the video with no transcript (see
// LessonDetail.svelte), "pronta" enables GetTranscript.
func (s *LibraryService) GetLesson(id int64) (Lesson, error) {
	lws, err := db.FindLessonWithStatusByID(s.conn, id)
	if err != nil {
		return Lesson{}, err
	}
	if lws == nil {
		return Lesson{}, fmt.Errorf("aula %d não encontrada", id)
	}
	return Lesson{
		ID:                  lws.ID,
		LessonDate:          lws.LessonDate,
		Tutor:               lws.Tutor,
		VideoPath:           lws.VideoPath,
		DurationSeconds:     lws.DurationSeconds,
		Status:              lws.Status,
		ErrorMessage:        lws.ErrorMessage,
		StudentSpeakerLabel: lws.StudentSpeakerLabel,
	}, nil
}

// GetTranscript looks up a lesson's transcript for the Detail screen
// (Story 6). Should only be called once GetLesson has already returned
// Status == "pronta" — the Detail screen doesn't call this for
// processing/error lessons, which show the status in place of the
// transcript panel.
func (s *LibraryService) GetTranscript(lessonID int64) (Transcript, error) {
	t, err := db.FindTranscriptByLessonID(s.conn, lessonID)
	if err != nil {
		return Transcript{}, err
	}
	if t == nil {
		return Transcript{}, fmt.Errorf("aula %d ainda não tem transcrição", lessonID)
	}
	out := Transcript{Utterances: make([]Utterance, 0, len(t.Utterances))}
	for _, u := range t.Utterances {
		out.Utterances = append(out.Utterances, Utterance{
			Speaker:      u.Speaker,
			Text:         u.Text,
			StartSeconds: u.Start.Seconds(),
			EndSeconds:   u.End.Seconds(),
		})
	}
	return out, nil
}

// SetStudentSpeaker records which raw speaker (e.g. "speaker_0") is the
// student in this lesson — the Detail screen's toggle (Story 6).
func (s *LibraryService) SetStudentSpeaker(lessonID int64, speakerLabel string) error {
	return db.SetStudentSpeaker(s.conn, lessonID, speakerLabel)
}
```

- [ ] **Step 4: Run the new tests and confirm they pass**

Run: `go test ./services/... -run "TestLibraryService_GetLesson_IncludesStatusAndStudentSpeaker|TestLibraryService_GetTranscript" -v`
Expected: PASS

- [ ] **Step 5: Run the entire `services` suite (make sure the existing `GetLesson`/`ListLessons` didn't break)**

Run: `go test ./services/... -v`
Expected: PASS on every test, including `TestLibraryService_GetLesson_FindsExistingAndErrorsWhenMissing`,
`TestLibraryService_ListLessons_ReturnsConfirmedLessons`, etc.

- [ ] **Step 6: `go vet` on the whole module**

Run: `go vet ./...`
Expected: no output (clean)

- [ ] **Step 7: Commit**

```bash
git add services/library.go services/library_test.go
git commit -m "feat: expoe status/transcricao/toggle de speaker no LibraryService"
```

---

### Task 5: Regenerate the Wails TS bindings

**Files:**
- Modify (generated, do not hand-edit): `frontend/bindings/assistente-idiomas/services/models.ts`, `frontend/bindings/assistente-idiomas/services/libraryservice.ts`

**Interfaces:**
- Consumes: `LibraryService.GetLesson/GetTranscript/SetStudentSpeaker` (Task 4).
- Produces: `models.ts` exports `Lesson.studentSpeakerLabel: string | null`, `Transcript`, `Utterance`; `libraryservice.ts` exports `GetTranscript(lessonID: number): $CancellablePromise<$models.Transcript>` and `SetStudentSpeaker(lessonID: number, speakerLabel: string): $CancellablePromise<void>`.

- [ ] **Step 1: Run the bindings generator**

Run: `wails3 generate bindings -ts -i ./...`
Expected: command finishes with no error (exit code 0); files under `frontend/bindings/assistente-idiomas/services/` are rewritten.

- [ ] **Step 2: Check that `models.ts` gained the expected types**

Run: `grep -n "studentSpeakerLabel\|interface Transcript\|interface Utterance" frontend/bindings/assistente-idiomas/services/models.ts`
Expected: 3 lines of output — `"studentSpeakerLabel"` inside `Lesson`, `export interface Transcript`, `export interface Utterance`.

- [ ] **Step 3: Check that `libraryservice.ts` gained the two new functions**

Run: `grep -n "export function GetTranscript\|export function SetStudentSpeaker" frontend/bindings/assistente-idiomas/services/libraryservice.ts`
Expected: 2 lines of output.

`frontend/bindings/` is in `.gitignore` (never committed — it's a build artifact regenerated
locally by `wails3 generate bindings`, not a versioned source). Nothing to commit in this task;
the generated files just stay in the working tree for Tasks 6 and 7 to use.

---

### Task 6: `Library.svelte` — the Detail screen opens regardless of status

**Files:**
- Modify: `frontend/src/lib/screens/Library.svelte:122-126,189-200`

**Interfaces:**
- Consumes: `Lesson.status` (bindings, Task 5).
- Produces: `openLesson` always calls `onOpenLesson(lesson.id)`, regardless of status.

- [ ] **Step 1: Edit `openLesson` (lines 122-126)**

Replace:
```js
  function openLesson(lesson: Lesson) {
    if (lesson.status === "pronta") {
      onOpenLesson(lesson.id);
    }
  }
```
With:
```js
  function openLesson(lesson: Lesson) {
    onOpenLesson(lesson.id);
  }
```

- [ ] **Step 2: Edit the lesson row's button (lines 189-200) — always clickable**

Replace:
```svelte
              <button
                class="lesson-main"
                onclick={() => openLesson(lesson)}
                style="cursor: {lesson.status === 'pronta' ? 'pointer' : 'default'}; opacity: {lesson.status === 'pronta' ? 1 : 0.7};"
              >
```
With:
```svelte
              <button class="lesson-main" onclick={() => openLesson(lesson)}>
```

- [ ] **Step 3: Check the frontend's types/compilation**

Run: `cd frontend && npm run check`
Expected: no new errors (same baseline as before the change)

- [ ] **Step 4: Commit**

```bash
git add frontend/src/lib/screens/Library.svelte
git commit -m "feat: Biblioteca abre o Detalhe em qualquer status da aula"
```

---

### Task 7: `LessonDetail.svelte` — grid with synchronized video + transcript

Rewrites Story 5's stub (header + video) into the full layout: 2-column grid, `timeupdate`
sync, speaker toggle, auto-scroll, panel states (processing/error/ready).

**Files:**
- Modify: `frontend/src/lib/screens/LessonDetail.svelte` (rewritten in full)

**Interfaces:**
- Consumes: `LibraryService.GetLesson`, `LibraryService.GetTranscript`, `LibraryService.SetStudentSpeaker`, `LibraryService.RetryLesson` (bindings, Task 5); `colors`/`fonts` from `../theme`.
- Produces: `LessonDetail` component with props `{ lessonId: number; onBack: () => void }` (unchanged).

- [ ] **Step 1: Rewrite `frontend/src/lib/screens/LessonDetail.svelte` in full**

```svelte
<script lang="ts">
  import { onMount } from "svelte";
  import { colors, fonts } from "../theme";
  import * as LibraryService from "../../../bindings/assistente-idiomas/services/libraryservice";
  import type { Lesson, Transcript } from "../../../bindings/assistente-idiomas/services/models";

  let { lessonId, onBack }: { lessonId: number; onBack: () => void } = $props();

  let lesson: Lesson | null = $state(null);
  let loading: boolean = $state(true);
  let lessonError: string = $state("");

  let transcript: Transcript | null = $state(null);
  let loadingTranscript: boolean = $state(false);

  let retrying: boolean = $state(false);

  let videoEl: HTMLVideoElement | undefined = $state();
  let currentTime: number = $state(0);
  let rowRefs: (HTMLElement | null)[] = [];

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

  // Order of first utterance — deterministic, doesn't depend on map
  // iteration order. "Speaker A"/"Speaker B" (or C, D... with noisy
  // diarization) are the neutral labels shown before the user picks who
  // the student is.
  const speakerOrder = $derived.by(() => {
    const seen: string[] = [];
    for (const u of transcript?.utterances ?? []) {
      if (!seen.includes(u.speaker)) seen.push(u.speaker);
    }
    return seen;
  });

  function neutralLabel(speaker: string): string {
    const idx = speakerOrder.indexOf(speaker);
    return `Speaker ${String.fromCharCode(65 + (idx < 0 ? 0 : idx))}`;
  }

  type Role = "aluno" | "tutor" | "neutro";

  function roleFor(speaker: string): Role {
    if (!lesson?.studentSpeakerLabel) return "neutro";
    return speaker === lesson.studentSpeakerLabel ? "aluno" : "tutor";
  }

  function labelFor(speaker: string, role: Role): string {
    if (role === "aluno") return "Você";
    if (role === "tutor") return "Tutor";
    return neutralLabel(speaker);
  }

  // Last utterance whose start has already passed — linear search, a few
  // hundred utterances per lesson at most, negligible cost on every
  // timeupdate tick.
  const currentIndex = $derived.by(() => {
    const utterances = transcript?.utterances ?? [];
    let idx = -1;
    for (let i = 0; i < utterances.length; i++) {
      if (utterances[i].startSeconds <= currentTime) idx = i;
      else break;
    }
    return idx;
  });

  $effect(() => {
    const el = rowRefs[currentIndex];
    el?.scrollIntoView({ block: "nearest" });
  });

  function onTimeUpdate() {
    if (videoEl) currentTime = videoEl.currentTime;
  }

  // Only adjusts the position (seek) — doesn't force play nor pause, so
  // it doesn't surprise someone who just wants to check the timestamp.
  function seekTo(startSeconds: number) {
    if (videoEl) videoEl.currentTime = startSeconds;
  }

  async function fetchTranscriptIfReady() {
    if (!lesson || lesson.status !== "pronta") {
      transcript = null;
      return;
    }
    loadingTranscript = true;
    try {
      transcript = await LibraryService.GetTranscript(lessonId);
    } catch {
      transcript = null;
    } finally {
      loadingTranscript = false;
    }
  }

  async function chooseStudentSpeaker(speaker: string) {
    if (!lesson) return;
    const previous = lesson.studentSpeakerLabel;
    lesson = { ...lesson, studentSpeakerLabel: speaker };
    try {
      await LibraryService.SetStudentSpeaker(lessonId, speaker);
    } catch (e) {
      lesson = { ...lesson, studentSpeakerLabel: previous };
      lessonError = String(e);
    }
  }

  async function retry() {
    retrying = true;
    try {
      await LibraryService.RetryLesson(lessonId);
      lesson = await LibraryService.GetLesson(lessonId);
      await fetchTranscriptIfReady();
    } catch (e) {
      lessonError = String(e);
    } finally {
      retrying = false;
    }
  }

  onMount(async () => {
    try {
      lesson = await LibraryService.GetLesson(lessonId);
      await fetchTranscriptIfReady();
    } catch (e) {
      lessonError = String(e);
    } finally {
      loading = false;
    }
  });
</script>

<div class="screen" style="font-family: {fonts.body}; color: {colors.text};">
  <button class="back" onclick={onBack} style="color: {colors.mut};">← Biblioteca</button>

  {#if loading}
    <p style="color: {colors.mut};">Carregando…</p>
  {:else if lessonError}
    <p class="error" style="color: {colors.red};">{lessonError}</p>
  {:else if lesson}
    <div class="header-row">
      <h1 style="font-family: {fonts.display};">{formatLessonDateTime(lesson.lessonDate)}</h1>
      <span class="meta" style="color: {colors.mut}; font-family: {fonts.mono};"
        >{lesson.tutor}{formatDuration(lesson.durationSeconds) ? ` · ${formatDuration(lesson.durationSeconds)}` : ""}</span
      >
    </div>

    <div class="grid">
      <div>
        <!-- eslint-disable-next-line svelte/no-navigation-without-resolve -->
        <video
          bind:this={videoEl}
          ontimeupdate={onTimeUpdate}
          controls
          src={`/media/lesson/${lesson.id}`}
          style="border: 1px solid {colors.line};"
        >
          <track kind="captions" />
        </video>
        <p class="hint" style="color: {colors.mut};">
          Clique em qualquer fala ao lado para pular o vídeo até aquele momento.
        </p>
      </div>

      <div class="panel" style="background: {colors.surface}; border: 1px solid {colors.line};">
        {#if lesson.status === "processando"}
          <p class="panel-message" style="color: {colors.mut};">Transcrição em processamento…</p>
        {:else if lesson.status === "erro"}
          <p class="panel-message" style="color: {colors.red};">{lesson.errorMessage}</p>
          <button onclick={retry} disabled={retrying}>
            {retrying ? "Reprocessando…" : "Reprocessar"}
          </button>
        {:else if loadingTranscript}
          <p class="panel-message" style="color: {colors.mut};">Carregando transcrição…</p>
        {:else if !transcript || transcript.utterances.length === 0}
          <p class="panel-message" style="color: {colors.mut};">Transcrição em processamento…</p>
        {:else}
          <div class="speaker-toggle">
            {#each speakerOrder as speaker, i (speaker)}
              <button
                class:active={lesson.studentSpeakerLabel === speaker}
                onclick={() => chooseStudentSpeaker(speaker)}
              >
                {`Speaker ${String.fromCharCode(65 + i)} é você`}
              </button>
            {/each}
          </div>

          <div class="transcript">
            {#each transcript.utterances as utterance, i (i)}
              {@const role = roleFor(utterance.speaker)}
              <button
                bind:this={rowRefs[i]}
                class="row"
                onclick={() => seekTo(utterance.startSeconds)}
                style="background: {i === currentIndex ? colors.surface2 : 'transparent'}; border-left: 3px solid {role === 'aluno' ? colors.blue : 'transparent'};"
              >
                <span
                  class="speaker-label"
                  style="color: {role === 'aluno' ? colors.blue : role === 'tutor' ? colors.green : colors.mut}; font-family: {fonts.body};"
                >
                  {labelFor(utterance.speaker, role)}
                </span>
                <p class="text" style="color: {colors.text};">{utterance.text}</p>
              </button>
            {/each}
          </div>
        {/if}
      </div>
    </div>
  {/if}
</div>

<style>
  .screen {
    padding: 2rem;
    max-width: 72rem;
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
  .header-row {
    display: flex;
    align-items: baseline;
    gap: 0.75rem;
    flex-wrap: wrap;
    margin-bottom: 1.25rem;
  }
  h1 {
    font-size: 1.4rem;
    margin: 0;
  }
  .meta {
    font-size: 0.85rem;
  }
  .grid {
    display: grid;
    grid-template-columns: 1fr;
    gap: 1.25rem;
  }
  @media (min-width: 960px) {
    .grid {
      grid-template-columns: 1fr 1fr;
    }
  }
  video {
    width: 100%;
    border-radius: 0.75rem;
    background: black;
    aspect-ratio: 16 / 9;
  }
  .hint {
    font-size: 0.75rem;
    margin-top: 0.75rem;
    line-height: 1.5;
  }
  .panel {
    border-radius: 0.75rem;
    padding: 1rem;
    max-height: 26rem;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
  }
  .panel-message {
    font-size: 0.9rem;
  }
  .speaker-toggle {
    display: flex;
    gap: 0.5rem;
    flex-wrap: wrap;
  }
  .speaker-toggle button {
    font-size: 0.75rem;
    padding: 0.35rem 0.7rem;
    border-radius: 999px;
    cursor: pointer;
  }
  .speaker-toggle button.active {
    font-weight: 600;
  }
  .transcript {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
  }
  .row {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 0.15rem;
    text-align: left;
    background: none;
    border: none;
    border-radius: 0.5rem;
    padding: 0.5rem 0.75rem;
    cursor: pointer;
    width: 100%;
  }
  .speaker-label {
    font-size: 0.7rem;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    font-weight: 600;
  }
  .text {
    font-size: 0.9rem;
    line-height: 1.5;
    margin: 0;
  }
  .error {
    font-size: 0.85rem;
  }
</style>
```

- [ ] **Step 2: Check the frontend's types/compilation**

Run: `cd frontend && npm run check`
Expected: no type errors (the `Transcript`/`Utterance` interface already exists in the bindings since Task 5)

- [ ] **Step 3: Frontend production build**

Run: `cd frontend && npm run build`
Expected: build finishes with no error

- [ ] **Step 4: Commit**

```bash
git add frontend/src/lib/screens/LessonDetail.svelte
git commit -m "feat: transcricao sincronizada no Detalhe da aula"
```

---

### Task 8: Final verification, progress update, and closing commit

**Files:**
- Modify: `docs/fase-1-mvp.md` (marks Story 6's criteria, adds a row to the progress table)

- [ ] **Step 1: Run the entire Go suite**

Run: `go test ./... -v`
Expected: PASS on every package

- [ ] **Step 2: `go vet` on the whole module**

Run: `go vet ./...`
Expected: no output

- [ ] **Step 3: Build the Go binary (makes sure main.go/services compile together)**

Run: `wails3 build`
Expected: finishes with no error, binary produced

- [ ] **Step 4: Update `docs/fase-1-mvp.md` — mark Story 6's criteria (lines 154-159) as done, with the same pending-visual-verification caveat used in the previous stories**

Edit the 5 checkboxes under Story 6's "### Acceptance criteria" (lines 155-159) from `- [ ]` to
`- [x]`, and add to the end of each (following the pattern from Stories 3/4/5) a note in
parentheses where it makes sense, for example on the seek criterion:
```
- [x] Local video served to the `<video>` element via the asset handler with range requests; seeking works (risk 1 resolved) — endpoint reused from Story 5 unchanged.
```
Apply the same pattern (mark `[x]`, short comment) to the other 4 criteria (scrollable
transcript, click seeks the video, highlight follows playback, a lesson with no transcript still
plays the video).

- [ ] **Step 5: Add a row to "Progress log" (end of the file, after Story 5's row)**

Add a new row to the markdown table, following the format of the previous ones, summarizing:
what was implemented (video+transcript grid, `timeupdate` sync, speaker toggle persisted in
`student_speaker_label`, the Library now allowed to open the Detail screen regardless of status,
tab-less panel) and the same recurring caveat that visual verification
(click-seeks-video, highlight-follows-playback, toggle) is still pending on a real Windows/Linux
window.

- [ ] **Step 6: Closing commit**

```bash
git add docs/fase-1-mvp.md
git commit -m "docs: mark Story 6 complete and record progress"
```

---

## Self-Review

**Spec coverage:** migration+column (Task 1), transcript reading (Task 2), status/toggle in the
Detail screen (Task 3-4), bindings (Task 5), Library gating (Task 6), 2-column layout/sync/toggle/
tab-less panel states (Task 7), progress update (Task 8). Every acceptance criterion of Story 6
(`fase-1-mvp.md:154-159`) and every decision in the spec
(`docs/superpowers/specs/2026-07-22-historia-6-detalhe-sincronizado-design.md`) has a
corresponding task.

**Placeholders:** no "TBD"/"implement later" — every step has complete code or an exact command
with expected output.

**Type consistency:** `Transcript`/`Utterance` (Go, Task 4) → `Transcript`/`Utterance` (generated
TS, Task 5) → `Transcript` imported in `LessonDetail.svelte` (Task 7), the same field names
(`utterances`, `speaker`, `text`, `startSeconds`, `endSeconds`) across every layer.
`StudentSpeakerLabel`/`studentSpeakerLabel` consistent between `db.Lesson` (Task 1),
`services.Lesson` (Task 4), and `Lesson.studentSpeakerLabel` in Svelte (Task 7).
</content>
