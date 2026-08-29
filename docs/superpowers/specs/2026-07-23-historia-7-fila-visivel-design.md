# Story 7 — Visible Queue

> Design spec. Stories and acceptance criteria in `docs/fase-1-mvp.md`.

## Context

Story 4 set up the jobs pipeline (`extract_audio` → `transcribe`) and already emits the Wails
`job:updated` event on every status transition — but no screen consumes that event yet
("transport ready, no screen consuming it yet", note from Story 4). The Library (Story 5)
shows status per lesson (processing/ready/error), but doesn't say which pipeline stage a lesson
is in, nor does it expose `attempts`/`last_error` in a dedicated way. This story closes the
"Queue" screen — today a static placeholder (`Queue.svelte`) — and wires up the count badge in
the sidebar, consuming `job:updated` for real in the frontend for the first time.

## Decisions

### What the Queue lists

Only lessons with an **active or errored** pipeline — `pending`/`running`/`error` in either of
the two jobs. Lessons with `transcribe.status == "done"` (ready) don't appear: that's already
visible in the Library, and the Queue exists to answer "what's processing and what just failed
right now", not to serve as history. There's no tab/toggle to see completed jobs — out of scope.

### Granularity: one row per lesson, not per job

A lesson has up to 2 jobs. The Queue shows **one row per lesson**, with the stage
(`extract_audio` or `transcribe`) that's currently active or that failed — the same display unit
as the Library, avoiding two near-identical rows for the same lesson (e.g., `extract_audio done`
next to `transcribe running`, which would just be confusing).

A lesson's "current" stage follows the same priority as `deriveStatus`
(`internal/db/lesson_status.go`, Story 5), just without collapsing into "processando"/"erro" —
here we need to know **which job** and **which exact state**. Important: `transcribe` stays with
status `"pending"` in the database for the entire time it's blocked waiting for `extract_audio`
to finish — the Worker only skips it in memory (`claimNextEligibleJob`), without changing that
status. That's why the audio extraction must be checked **before** the transcription, otherwise
a lesson still extracting audio would show up as "Transcrição — aguardando":

1. `extract_audio.status == "error"` → stage = audio extraction, state = error (root cause).
2. otherwise `extract_audio.status IN ("pending", "running")` → stage = audio extraction, state =
   waiting/processing.
3. otherwise (`extract_audio.status == "done"`) `transcribe.status == "error"` → stage =
   transcription, state = error.
4. otherwise `transcribe.status IN ("pending", "running")` → stage = transcription, state =
   waiting/processing (here `transcribe` is already genuinely eligible, not blocked).
5. otherwise (`transcribe.status == "done"`) → lesson ready, **doesn't enter the list**.

### Ordering

`status == 'error'` first (needs user action), then the rest by `updated_at ASC` of the active
job — the same FIFO order the Worker already uses to pick the next job (`db.ListPendingJobs`), so
the screen's order matches the actual processing order.

### Retry

Reuses the data primitive the Library already uses: `db.ResetErrorJobsForLesson`. The
`QueueService` calls this function directly (same primitive, without one service depending on
the other) — there's no need for a shared type/layer just for this, it's a function in
`internal/db` that both services can already call.

### Sidebar badge

Counts only `pending + running` (jobs active right now) — errors **do not** enter the number, to
avoid confusing "processing X things" with "there are X lessons with a problem" (the Queue
already highlights errors visually, without needing to duplicate that in the badge). The badge
doesn't appear (or show "0") when the count is zero.

### Real-time update: shared store, no polling

`Queue.svelte` and the `Sidebar.svelte` badge need the same data (list of active jobs) at the
same time. Instead of each screen subscribing to `job:updated` and fetching on its own (two
concurrent subscriptions to the same event, duplicated refetch logic), a single module
`frontend/src/lib/jobsStore.svelte.ts` holds the state with Svelte 5 runes (`$state`) and is
initialized once in `App.svelte` (`onMount`, the same level that already calls
`SetupService.IsFirstRun()`).

On receiving `job:updated`, the store **redoes the full fetch** (`QueueService.ListQueue()`)
instead of trying to apply an incremental patch: the event payload (`jobs.JobEvent`) only carries
`LessonID/Kind/Status/Attempts/LastError`, not `LessonDate/Tutor` — there wouldn't be enough data
to update an existing row without a second call anyway. Since the worker is single and
sequential (processes one job at a time), events arrive spaced out, not in a burst — there's no
need for debounce.

This is the first screen to consume `job:updated` — the transport built in Story 4 now has a
real consumer.

## Changes by layer

### `internal/db`

New file `internal/db/queue.go`, separate from `lesson_status.go` (which only serves the
Library) to keep each query with a single purpose:

- `QueueEntry` — `LessonID int64`, `LessonDate string`, `Tutor string`, `Kind string` (
  `"extract_audio"` or `"transcribe"`), `Status string` (`"pending"`, `"running"`, `"error"`),
  `Attempts int`, `LastError string`, `UpdatedAt string`.
- `ListQueueEntries(conn *sql.DB) ([]QueueEntry, error)` — `LEFT JOIN jobs` twice (same pattern
  as `lessonWithStatusFromJoin`), applies in Go the priority described above to decide the active
  stage of each lesson, discards ready lessons, sorts error-first then `updated_at ASC`.

### `services`

New `services/queue.go`:

- `QueueService` — takes `*sql.DB`, same pattern as `LibraryService`.
- `QueueItem` (JSON) — mirrors `db.QueueEntry` plus `Stage string` (`"Extração de áudio"` /
  `"Transcrição"`, translated in the service, not in the frontend) and `Status string` already
  translated (`"aguardando"` / `"processando"` / `"erro"`, same vocabulary as `STATUS_LABEL` that
  `Library.svelte` already uses for "processando"/"erro").
- `ListQueue() ([]QueueItem, error)` — calls `db.ListQueueEntries`, maps to the format above.
- `RetryLesson(lessonID int64) error` — calls `db.ResetErrorJobsForLesson`, identical in
  behavior to `LibraryService.RetryLesson` (idempotent, it's not an error if zero jobs were
  reset). Duplicating this thin shell instead of re-exposing `LibraryService`'s method keeps the
  two services independent (one doesn't call the other).

### `main.go`

Registers `application.NewService(services.NewQueueService(conn))` alongside the existing
services.

### Frontend

- `frontend/src/lib/jobsStore.svelte.ts` (new) — `items: QueueItem[]` via `$state`,
  `activeCount` via `$derived` (`items.filter(i => i.status !== "erro").length`),
  `initJobsStore()` (initial fetch + `Events.On("job:updated", refetch)` from
  `@wailsio/runtime`).
- `frontend/src/App.svelte` — calls `initJobsStore()` in the existing `onMount`.
- `frontend/src/lib/screens/Queue.svelte` — reads `items` from the store; each row shows
  date/tutor · stage · state; rows with `status === "erro"` show `lastError` and a "Reprocessar"
  button (same visual pattern as `Library.svelte`, calls `QueueService.RetryLesson`). The empty
  state keeps the current text ("Nada na fila no momento").
- `frontend/src/lib/Sidebar.svelte` — shows a numeric badge next to the "Fila" item when
  `activeCount > 0` (read from the store).
- Wails bindings regenerated (`wails3 generate bindings -ts -i ./...`) reflecting `QueueService`.

## Out of scope (not implementing here)

History of completed jobs in the Queue; percentage progress within a job (there's no data for
this — STT doesn't expose incremental progress); cancelling a job in progress; any second badge
(e.g., a separate error count).

## Tests

- `internal/db`: `ListQueueEntries` covering the 5 priority combinations described above (error
  in extract_audio, error in transcribe, transcribe pending/running, extract_audio
  pending/running, ready lesson excluded), and the error-first ordering.
- `services`: `QueueService.ListQueue` (Stage/Status translation), `RetryLesson` (idempotent,
  resets both jobs when transcribe was blocked — same case already covered in
  `LibraryService.RetryLesson`).
- Visual verification (real window) of the badge updating live and the Queue list changing
  during real processing remains pending on the Windows/Linux machines, same pattern as
  previous stories.
