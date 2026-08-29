# Story 4 — Background Pipeline (Job Queue)

> Brainstorming design, 22/07/2026. Reference: `docs/phase-1-mvp.md` (Story 4) and
> `docs/technology-decisions.md` (table-based queue + single worker, already decided in Phase 0).

## Goal

Audio extraction and transcription run on their own in the background after a lesson is confirmed
(Story 3), without the user needing to wait or reopen the app. Network/API failure never prevents
watching the video — the pipeline is additive, not blocking.

## Architecture

New `internal/jobs` package (pure Go, no Wails import — thin layer, same as the rest of
`internal/`).

```
type Worker struct {
    conn          *sql.DB
    storageRoot   StorageRootResolver // resolved per job, not at creation (see note below)
    audioCacheDir string              // absolute, outside the synced folder
    extractAudio  MediaExtractorFunc  // same signature as media.ExtractAudio
    sttProvider   STTProviderFactory  // same — resolved per job
    notifier      Notifier
    wake          chan struct{}
}

type StorageRootResolver func() (string, error)
type STTProviderFactory func() (stt.Provider, error)
```

**Note — deferred config/credential resolution:** the first-run wizard
(`SetupService.CompleteSetup`) runs *inside* the same app session, after `main.go` has already
assembled the services. If `storageRoot`/the STT provider were resolved once at `Worker`
construction time (as an earlier version of this design proposed), the worker would never
actually manage to start in the first-use session — `config.Load()`/`config.GetSTTAPIKey()` would
still fail at that point. That's why `storageRoot` and `sttProvider` are resolved **per job**
(inside `runExtractAudio`/`runTranscribe`), rather than stored as a fixed value. This also removes
any need for special gating in `main.go`: the worker always starts together with the app; before
the wizard runs, the queue is empty anyway (jobs only exist after a lesson is confirmed, which
already requires `storage_root` to be configured), so there's nothing to process until the
resolution actually works.

```
type Notifier interface {
    JobChanged(JobEvent)
}

type JobEvent struct {
    LessonID  int64
    Kind      string
    Status    string
    Attempts  int
    LastError string
}
```

- `Worker.Run(ctx)` runs in a goroutine started in `main.go`, right after `db.Open`.
- On startup, it first does a **requeue**: every `running` job becomes `pending` (covers a
  crash/kill in the middle of a job — `attempts` is not incremented by this requeue, only the
  status changes).
- Main loop: selects the next eligible job (see "Selection and precedence" below); if none is
  eligible, it waits on the `wake` channel **or** a fallback ticker (periodic poll), whichever
  comes first.
- `Worker.Wake()` is called by whoever inserts new jobs (`db.ConfirmPendingImport`, and in the
  future Story 3b) to process almost immediately, without relying solely on the poll.
- The real `Notifier` (which imports Wails) lives in `services/`, implemented with
  `app.Event.Emit(...)` — the worker itself doesn't know Wails exists. It emits `job:updated`
  with the `JobEvent` on every status transition (`pending→running`, `running→done`,
  `running→error`). No UI consumes this in this story (that's for Stories 5 and 7); it's just
  the transport.

## Selection and precedence

Fetches all `pending` jobs ordered by `created_at, id`. In Go, iterates and picks the first
eligible one:

- `extract_audio`: eligible if within the backoff window (see below).
- `transcribe`: eligible only if the `extract_audio` job for the same `lesson_id` is `done`.
  - If that `extract_audio` is `error` (retries exhausted), the `transcribe` is marked `error`
    **immediately**, without running — `last_error` = `"depende de extract_audio que falhou"`.
    This avoids spending a paid API call on a precondition that will never be met.
  - If `extract_audio` hasn't finished yet (`pending`/`running`), `transcribe` is skipped in this
    sweep (not chosen, but stays `pending`) — in practice this shouldn't happen under the strict
    FIFO of a single worker, since `extract_audio` is always created (and therefore processed)
    before the `transcribe` for the same lesson, but the code doesn't assume this and handles it
    defensively.

## Idempotency

Real idempotency per artifact, not just per status:

- `extract_audio`: if `audioCacheDir/<lesson_id>.wav` already exists with size > 0, skips the
  extraction and marks it `done` directly (without running ffmpeg again).
- `transcribe`: if a row already exists in `transcripts` for that `lesson_id`, marks it `done`
  directly without calling the STT API again.

## Retry and backoff

No migration — reuses the `attempts` and `updated_at` columns that already exist in `jobs`.

- `max_attempts = 3`.
- On execution failure: `attempts++`, `last_error` recorded with the error message.
  - If `attempts < 3`: status goes back to `pending` (one more attempt after the backoff).
  - If `attempts == 3`: status becomes `error`, terminal — only reprocessed manually (an action
    from Story 7, which doesn't exist yet in this slice).
- Backoff: a `pending` job with `attempts > 0` is only eligible again when
  `now >= updated_at + backoff[attempts]`, with `backoff = {1: 10s, 2: 60s, 3: 5min}`. Jobs still
  within the window are skipped in the sweep (they don't block selection of other eligible jobs).

## Artifacts and paths

- **Intermediate WAV** (`extract_audio`): written to `audioCacheDir/<lesson_id>.wav`, outside the
  synced folder (`AppDataDir/audio-cache/`, next to the database). Deleted as soon as the
  `transcribe` job for that lesson finishes successfully. If `transcribe` fails (even terminally),
  the WAV stays — this avoids needlessly re-extracting it during a future manual reprocessing.
- **Raw provider JSON** (`transcribe`): written **alongside the video**, in the same directory as
  the video file (without assuming a subfolder — consistent with Story 3, which doesn't impose a
  folder structure), named `<video-basename-without-extension>.transcript.json`.
  `transcripts.raw_json_path` stores this path **relative** to `storage_root` (same convention as
  `lessons.video_path`).
- `transcripts.utterances` stores the JSON already mapped to the common domain type
  (`stt.Utterance`, serialized).

## STT provider

ElevenLabs Scribe (`internal/stt.NewElevenLabsProvider`), the only provider in this phase
(decision in effect in `technology-decisions.md`). `main.go` passes the `Worker` an
`STTProviderFactory` that reads `config.GetSTTAPIKey()` (keyring) and calls
`stt.NewElevenLabsProvider` — re-evaluated on every `transcribe` job (see the deferred-resolution
note above), not built ahead of time.

## Resilience

- A failure in `extract_audio`/`transcribe` never touches `lessons` or the video itself — the
  video remains watchable. The error stays only in `jobs` (the resilience principle in effect for
  Phase 1).
- Execution errors (ffmpeg, HTTP API, I/O) are always caught and become `last_error` — none of
  them brings the worker down; the goroutine keeps processing the next jobs.

## Out of scope for this slice

Queue screen / count badge (Story 7) · manual "reprocess" action (Story 7) · any consumption of
`job:updated` events in the frontend (Stories 5 and 7) · `analyze` job kind (Phase 2, though the
`jobs`/`prompts` schema already accommodates it).

## Tests (`internal/jobs`)

Following the pattern already used in the project (`db.Open(t.TempDir())`, no database mocks):

- The queue processes `extract_audio` → `transcribe` in sequence for a lesson, using fake
  `MediaExtractorFunc`/`stt.Provider`.
- Idempotency: reprocessing a job whose artifact already exists doesn't call the corresponding
  fake again.
- Retry: a fake that fails twice and succeeds on the 3rd call increments `attempts` and goes back
  to `pending` between attempts; 3 failures in a row ends in `error`.
- `transcribe` with `extract_audio` in `error` becomes `error` without calling the STT fake.
- Requeue: a job inserted directly as `running` in the database goes back to `pending` when the
  `Worker` is created.
- Fake `Notifier` records the calls — without depending on Wails in the tests.
