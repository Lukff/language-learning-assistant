# Story 5 — Real Library — design

> Design spec. Stories and acceptance criteria in `docs/phase-1-mvp.md`.

## Context

The Library today (Story 3) only lists raw lessons — date, tutor, path — and candidates
pending review. Story 4 got the jobs pipeline (`extract_audio` → `transcribe`) running in the
background, but no screen consumes job status yet. This story closes that gap: the Library now
shows real status (processing/ready/error), duration, filter by tutor/date range, and opens a
Detail view (minimal, but with a real video) when a ready lesson is clicked.

As a bonus, this slice resolves the project's **technical risk 1** (`phase-1-mvp.md`) — serving
the local video to the webview with range-request support — earlier than planned. Instead of a
stub with no video, the Detail view already plays the local file through a dedicated endpoint.
Story 6 inherits this ready-made endpoint and only adds the synchronized transcript on top.

## Decisions

### Video duration

Calculated via `ffprobe` (ffmpeg's companion, the same external dependency already assumed) at
the moment of **import confirmation** (`ImportService.ConfirmImport`), not during the
`extract_audio` job. Duration is metadata intrinsic to the video, not a product of the
transcription pipeline — it must remain available even if the pipeline fails entirely,
consistent with the resilience principle ("a transcription/analysis failure never prevents
watching the video").

An `ffprobe` failure (missing binary, invalid fixture file in tests, etc.) **never** blocks
confirmation: it's best-effort, logged, and ignored — `duration_seconds` stays `NULL` and the
lesson is confirmed normally. This also keeps the existing tests in `services/import_test.go`
(which use a fake `.mp4` with arbitrary content) passing without requiring a real `ffprobe` in
the test environment.

### Status derived from jobs

There is no status column on `lessons` — it is always derived, at read time, from the lesson's
two jobs (`extract_audio`, `transcribe`):

1. `extract_audio.status == "error"` → **erro**, message = `extract_audio.last_error` (root
   cause).
2. else `transcribe.status == "error"` → **erro**, message = `transcribe.last_error` (a real STT
   failure, since the "blocked by dependency" case is already covered by item 1).
3. else `transcribe.status == "done"` → **pronta**.
4. else (any combination of `pending`/`running`) → **processando**.

### Retry

Resets **all** jobs of the lesson that are in `error` at once (`status='pending'`, `attempts=0`,
`last_error=NULL`) — not just the one that caused the root error. This matters because when
`extract_audio` fails definitively, the worker already marks `transcribe` as `error` too (a
blocked job, see `claimNextEligibleJob` in `internal/jobs/worker.go`). Resetting only
`extract_audio` would leave `transcribe` stuck in `error` permanently. No new job is created —
this avoids duplicating rows and breaking the "one job per kind per lesson" assumption that
`db.FindJob` already relies on.

Does not call `Worker.Wake()` — the same deliberate decision as Story 4: the fallback poll
(~5s) is already imperceptible in a background queue, and wiring `Wake()` into the retry flow
would require exposing the `Worker` instance to `services/`, increasing coupling for no
noticeable gain.

### Filter

A tutor dropdown (populated from `SELECT DISTINCT tutor FROM lessons`, no free typing) + two
date fields (from/to, same `YYYY-MM-DD` format as `lesson_date`). All parameters are optional;
full-text search over conversation snippets is left for a future phase (out of scope, already
noted in `phase-1-mvp.md`).

### Detail view: real video via a dedicated asset handler

`AssetOptions.Middleware` (Wails v3) intercepts `GET /media/lesson/{id}` before the default
handler (`AssetFileServerFS` in production, the dev server in `wails3 dev`): it looks up the
lesson by id in the database, builds the absolute path (`storage_root` + `video_path`), and
serves it with the stdlib's `http.ServeContent` — which already handles `Range` requests, with
no manual range logic. A nonexistent id or a missing file on disk → `404`. Every other path
passes straight through to the default handler, with no interference with the rest of the app.

This is the real resolution of risk 1 (not a placeholder): Story 6 reuses the same endpoint,
only adding the scrollable transcript, click-to-seek, and playback highlighting.

The Detail view in this story is deliberately minimal: header (date/tutor), a "← Library"
button, `<video controls>` pointing at the endpoint. No transcript, no sync — that's the full
scope of Story 6.

## Changes by layer

### `internal/media`
- `Duration(ctx context.Context, videoPath string) (time.Duration, error)` via `ffprobe`,
  following the same pattern as `ExtractAudio` (checks `exec.LookPath`, runs the command, wraps
  the error with context).

### `internal/db`
- `SetLessonDuration(conn *sql.DB, lessonID int64, seconds int64) error`.
- `ListTutors(conn *sql.DB) ([]string, error)`.
- `LessonFilter{ Tutor, DateFrom, DateTo string }` (all optional).
- `LessonWithStatus` — `Lesson` + `DurationSeconds *int64` + `ExtractStatus/ExtractError` +
  `TranscribeStatus/TranscribeError`.
- `ListLessonsWithStatus(conn *sql.DB, filter LessonFilter) ([]LessonWithStatus, error)` — via
  `LEFT JOIN jobs` twice (once per kind), with a conditional `WHERE` for the filters present.
- `ResetErrorJobsForLesson(conn *sql.DB, lessonID int64) (int64, error)` — `UPDATE jobs SET
  status='pending', attempts=0, last_error=NULL WHERE lesson_id=? AND status='error'`, returns
  how many jobs were reset.
- `ListLessons` (without status) stays only if it still has a caller after `library.go`'s
  migration — otherwise it's removed (the previous slice introduced it only for this screen).

### `services`
- `ImportService.ConfirmImport`: after `db.ConfirmPendingImport`, looks up the newly-created
  lesson and calls `media.Duration` on a best-effort basis (log + ignore error), writing it via
  `db.SetLessonDuration`.
- `LibraryService.ListLessons(filter LessonFilterInput) ([]Lesson, error)`: changes the current
  signature (no args) — calls `db.ListLessonsWithStatus`, derives `Status`/`ErrorMessage` per
  lesson according to the rules above, exposes `DurationSeconds *int64`.
- `LibraryService.ListTutors() ([]string, error)`.
- `LibraryService.RetryLesson(lessonID int64) error` — calls `db.ResetErrorJobsForLesson`;
  it is not an error if zero jobs were reset (idempotent, no need to check state beforehand).
- No service method for the video URL: the `/media/lesson/{id}` path is predictable from just
  the `lessonId` the frontend already has (coming from `ListLessons`), so Svelte builds the
  string directly (`` `/media/lesson/${lesson.id}` ``) — a service method just for that would be
  indirection with no benefit.
- New `services/video_asset.go`: `VideoAssetMiddleware(conn *sql.DB, storageRoot
  StorageRootResolver) application.Middleware` — lives in `services/` (not `internal/`) because
  it needs the `application.Middleware`/`application.Handler` type, and `internal/` never
  imports Wails (thin layer, see `CLAUDE.md`). Reuses the same `StorageRootResolver` type from
  `internal/jobs` (resolved on every request, not just once — same reason as Story 4: the
  first-run wizard runs after the app is already up).

### `main.go`
- Registers `services.VideoAssetMiddleware(conn, storageRoot)` in `AssetOptions.Middleware`,
  using the same `storageRoot` closure already built in `startJobWorker`.

### Frontend
- `frontend/src/lib/screens/Library.svelte`: status badges, formatted duration, filter (tutor
  dropdown + date range), a "Retry" button on lessons with errors, clicking a ready lesson
  navigates to the Detail view.
- `frontend/src/lib/screens/LessonDetail.svelte` (new): header, `<video controls>`, back button.
- `frontend/src/App.svelte`: simple route state (`library` | `lesson-detail`), no routing library
  (too small a scope to justify one).
- Regenerated Wails bindings (`wails3 dev`/`generate bindings`) reflecting the new signatures.

## Out of scope (not to implement here)

Full-text search, transcript in the Detail view, click-to-seek, playback highlighting (all
Story 6); Queue screen and active-jobs count badge (Story 7); any LLM analysis.

## Tests

- `internal/media`: `Duration` test without `ffprobe` on the PATH (same pattern already used in
  `TestExtractAudio_FfmpegNotInPath`).
- `internal/db`: coverage of `ListLessonsWithStatus` (every combination of derived status),
  `ResetErrorJobsForLesson`, `ListTutors`, filter by tutor/date range.
- `services`: `LibraryService.ListLessons` with a filter, `RetryLesson` (resets both jobs when
  `transcribe` was blocked), `ImportService.ConfirmImport` still passes with a fake video
  fixture (duration stays `NULL`, without failing confirmation).
- Video endpoint: a lightweight integration test validating that a request with `Range` returns
  `206 Partial Content` and the expected body (via `httptest`), and that a nonexistent id
  returns `404`.
- Visual verification (real window) of the Library → Detail → video-playing flow remains
  pending on Windows/Linux machines, same pattern as the previous stories.
