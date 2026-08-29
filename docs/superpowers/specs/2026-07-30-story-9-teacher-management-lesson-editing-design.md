# Story 9 — Teacher management and lesson editing

> Design for 3 small items, requested before moving on with other Phase 1 items: selecting an
> already-registered teacher (or a new name) in the import form, renaming a teacher as an
> entity (reflected across all their lessons), and editing the date/time/teacher of an already
> confirmed lesson.

## Context

Today `tutor` is a free-form `TEXT` column in `lessons` (no entity of its own). `ListTutors`
(`internal/db/lessons.go`) already runs `SELECT DISTINCT tutor` to populate the Library filter, but
there is no dedup by entity — two different spellings of the same teacher (`"Sarah M."` vs
`"Sarah M"`) end up as distinct "teachers" in the filter. There is currently no way to edit an
already confirmed `lesson` (date/time/teacher are locked to the value entered at import
confirmation, Story 3).

The video is renamed *in place* on confirmation to `AAAA-MM-DD_HHHMM_tutor-slug.ext`
(`services/import.go:renameVideoBestEffort`, see
`docs/superpowers/specs/2026-07-23-story-3-standardized-filename-design.md`). Editing the
date/teacher of a lesson needs to reuse this same best-effort rename logic so the file keeps
reflecting the current metadata.

## Decisions

1. **Teacher becomes a real entity** (`teachers` table), not just a loose string — renaming is an
   `UPDATE` on one row and reflects across all lessons automatically; name is `UNIQUE`.
2. **Editing a lesson also renames the video** — reuses the same best-effort logic from Story 3
   (rename failure does not prevent saving the edit).
3. **The "Teachers" panel lives in Settings**, alongside Storage and STT Credential.
4. **Editing a lesson includes changing the associated teacher** (one-off reassignment), in
   addition to date/time — same form, same combobox used during import.
5. **The entry point for lesson editing is an "Edit" button on the Lesson Detail view** (LessonDetail).
6. **A name collision when renaming a teacher is blocked with an error** ("já existe um professor com
   esse nome") — no automatic record merging.

## A. Data model and migration

New migration `internal/db/migrations/00005_teachers.sql`:

```sql
-- +goose Up
CREATE TABLE teachers (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

INSERT INTO teachers (name, created_at, updated_at)
SELECT DISTINCT tutor, datetime('now'), datetime('now') FROM lessons;

ALTER TABLE lessons ADD COLUMN teacher_id INTEGER REFERENCES teachers(id);

UPDATE lessons
SET teacher_id = (SELECT id FROM teachers WHERE teachers.name = lessons.tutor);

ALTER TABLE lessons DROP COLUMN tutor;

-- SQLite does not allow adding NOT NULL without a default to an already
-- populated column via ALTER TABLE; the teacher_id constraint is enforced at
-- the repository layer (every INSERT/UPDATE on lessons goes through Go, never raw SQL).

-- +goose Down
ALTER TABLE lessons ADD COLUMN tutor TEXT;
ALTER TABLE lessons DROP COLUMN teacher_id;
DROP TABLE teachers;
```

- The backfill ensures every existing `lesson` gets a `teacher_id` matching its current
  `tutor`, with no data loss.
- `Down` only reverts the structure (same pattern as `00001_initial_schema.sql`) — it does not
  recover the `tutor` values, consistent with this project's existing migrations.
- Requires SQLite ≥3.35 for `DROP COLUMN` — already bundled in `modernc.org/sqlite v1.54.0` (the
  version currently in `go.mod`).

## B. Backend (Go)

### `internal/db`

- New `teachers.go`:
  - `type Teacher struct { ID int64; Name string }`
  - `ListTeachers(conn *sql.DB) ([]Teacher, error)` — ordered by name, for the Settings panel
    and for the frontend's `TeacherCombobox`.
  - `GetOrCreateTeacherByName(conn *sql.DB, name string) (int64, error)` — looks up by exact name;
    if it doesn't exist, inserts it. Used by `ConfirmPendingImport` and by `UpdateLesson`, which
    receive the name as a free-form string coming from the combobox (the user may type a new name).
  - `RenameTeacher(conn *sql.DB, id int64, newName string) error` — `UPDATE teachers SET name = ?
    WHERE id = ?`; a `UNIQUE` violation (checked via `sqlite.Error` / the constraint code from
    `modernc.org/sqlite`) becomes `fmt.Errorf("já existe um professor com esse nome")` — never the
    driver's raw message.
- `lessons.go`: `Lesson.Tutor` → `Lesson.TeacherID` (for writes) + `Lesson.TeacherName` (read via
  `JOIN teachers ON teachers.id = lessons.teacher_id`, for display). `ListTutors` is removed —
  replaced by `ListTeachers`.
- `lesson_status.go`: `LessonFilter.Tutor string` → `LessonFilter.TeacherID int64` (zero = no
  filter); `JOIN teachers` to also expose `TeacherName` in `LessonWithStatus`.
- `queue.go`: same treatment — `QueueEntry.Tutor` → `QueueEntry.TeacherName` via join.
- `pending_imports.go` (`ConfirmPendingImport`): the signature still receives `tutor string`
  (free-form name); internally it resolves to `teacher_id` via `GetOrCreateTeacherByName` before
  the `INSERT` into `lessons`.
- New `UpdateLesson(conn *sql.DB, lessonID int64, lessonDate string, teacherName string) error`:
  resolves `teacherName` via `GetOrCreateTeacherByName`, runs `UPDATE lessons SET lesson_date = ?,
  teacher_id = ?, updated_at = ? WHERE id = ?`.

### `services` (same package — direct reuse, no new interfaces)

- New `teachers.go`:
  ```go
  type TeacherService struct{ conn *sql.DB }
  func NewTeacherService(conn *sql.DB) *TeacherService
  func (s *TeacherService) ListTeachers() ([]db.Teacher, error)
  func (s *TeacherService) RenameTeacher(id int64, newName string) error
  ```
  Registered in `main.go` alongside the other `application.NewService(...)` calls.
- `services/import.go`:
  - `ConfirmImport(id int64, lessonDate string, tutor string) error` keeps its signature (free-form
    name from the combobox) — no contract change with the frontend.
  - `renameVideoBestEffort` stops being a method on `ImportService` and becomes a package function:
    `renameVideoBestEffort(conn *sql.DB, moveFile func(string, string) error, lessonID int64)` —
    reused by `ImportService.ConfirmImport` and by the new
    `LibraryService.UpdateLesson`.
- `services/library.go`:
  - New field `moveFile func(string, string) error` on `LibraryService` (same pattern as
    `ImportService`, for test injection), defaulting to `moveFileNoReplace`.
  - New `UpdateLesson(lessonID int64, lessonDate string, teacherName string) error`: the same
    `lessonDate` format validation `ConfirmImport` already does (not empty, with time,
    layout `2006-01-02T15:04`), calls `db.UpdateLesson`, then `renameVideoBestEffort`
    (best-effort — a rename failure is only logged, not propagated).
  - `ListTutors()` is removed — replaced by delegation to `TeacherService` (the frontend now
    calls `TeacherService.ListTeachers` directly).
  - `LessonFilter.Tutor string` → `LessonFilter.TeacherID int64`.
- `internal/importer.StandardFilename` keeps its signature unchanged — callers now pass the
  resolved `TeacherName`, no longer the old `tutor` column.

## C. Frontend (Svelte 5)

- New `frontend/src/lib/TeacherCombobox.svelte`: a small, self-contained component —
  native `<input list="teachers-list-{id}">` + `<datalist>`, loading
  `TeacherService.ListTeachers()` on `onMount`. Lets the user pick an already-registered teacher or
  type a new name (no third-party component, consistent with the rest of the app). Props:
  `value` (bindable), `id` (for the consumer's `<label for>`).
- `ImportConfirmModal.svelte`: swaps the tutor `<input type="text">` for `TeacherCombobox`. No
  change to the call to `ImportService.ConfirmImport` — it still sends a string.
- New `frontend/src/lib/EditLessonModal.svelte`: structure similar to `ImportConfirmModal`
  (same overlay/card look), but with its own props (`lessonId`, `initialLessonDate`,
  `initialTeacherName`, `onSaved`, `onClose`) — it doesn't reuse the component directly because the
  input data and the service being called (`LibraryService.UpdateLesson`) are different. Fields:
  date/time (`datetime-local`) + `TeacherCombobox`.
- `LessonDetail.svelte`: "Edit" button next to the date/tutor header, opens `EditLessonModal`
  pre-filled with the lesson's current values; `onSaved` reloads the lesson via `GetLesson`
  (the `<video>` does not need to reload — the media endpoint is served by lesson id, not by
  path, see Story 5).
- `Settings.svelte`: a third `<section class="card">`, "Teachers" — lists
  `TeacherService.ListTeachers()`, each row with a text input (current value) + "Rename"
  button, same visual pattern as the STT Credential panel (per-row error/success state).
- `Library.svelte`: the tutor filter becomes a `<select>` populated from `TeacherService.ListTeachers()`
  (id as value, name as label) — `LessonFilter.teacherId` instead of `LessonFilter.tutor`.

## D. Tests and documentation

- **Go:**
  - `internal/db/teachers_test.go`: `ListTeachers` (alphabetical order), `GetOrCreateTeacherByName`
    (creates if it doesn't exist, returns the existing id if it does), `RenameTeacher` (success and
    unique-name conflict).
  - Existing tests that insert `tutor` directly via SQL (`lessons_test.go`, `db_test.go`,
    `queue_test.go`, `jobs_test.go`, `transcripts_test.go`, `analysis_results_test.go`,
    `pending_imports_test.go`, `lesson_status_test.go`, `worker_test.go`,
    `services/library_test.go`, `services/queue_test.go`, `services/import_test.go`) switch to
    inserting via `teachers` + `teacher_id`.
  - `services/library_test.go`: new `TestLibraryService_UpdateLesson_*` covering date/teacher
    editing and reuse of the best-effort rename (same pattern of deterministic injected failure
    already used in `import_test.go` for `renameVideoBestEffort`).
  - `go test ./...` and `go vet ./...` clean.
- **Frontend:** `pnpm run check` and `pnpm run build` clean. Real visual verification (clicking the
  combobox, renaming a teacher in Settings, editing a lesson in Detail) remains pending on
  Windows/Linux — same pattern already noted in this project's previous stories, does not block
  the implementation.
- **Documentation:** add **Story 9 — Teacher management and lesson editing** to
  `docs/phase-1-mvp.md`, with the 3 items above as acceptance criteria and a dependency on Story 3
  (the current sole consumer of the old `tutor` column).

## Out of scope

- Merging duplicate teachers (left for a future function, if needed).
- Fuzzy search/autocomplete in the combobox — the native `<datalist>` already filters by substring
  in the browser, sufficient for the expected volume of teachers (1:1 lessons, few tutors).
- Any other Settings option (out of scope for Phase 1, see `docs/phase-1-mvp.md`).
