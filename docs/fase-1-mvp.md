# Phase 1 — MVP: import → transcribe → watch

> Goal: the first **daily-usable** version: import a Cambly recording, transcribe it
> in the background, and watch the video with the transcript synchronized. Nothing beyond that.
>
> Base: Wails v3 (alpha, pinned) + Svelte 5 + the Go stack defined in `decisoes-tecnologia.md`.
> The `media`, `stt`, and `analysis` packages from Phase 0 are reused as-is.
>
> Resilience principle (current decision): the app is useful with just video + transcript;
> analysis is an additional layer and is left for Phase 2.

## Out of scope for this phase (write down ideas, don't implement)

LLM analysis in the UI · tags/topics · Progress screen · pull-work-push sync and backups ·
multi-machine onboarding · STT/LLM provider selection · full-text search (FTS5) ·
transcript editing. The database schema, however, is already built to accommodate analysis and
versioned prompts (costs little and avoids a painful migration later).

**Exception added on 24/07/2026:** a **minimal** Settings menu is in scope for this phase
(Story 8) — view/change (repoint, without moving files) the storage root and re-register
the STT provider credential, covering the known gap from Story 2 (a lost/cleared keyring
credential
had no recovery UI). STT/LLM provider selection, cost estimation, and the rest of the full
Settings screen remain out of scope, planned for Phase 5.

Minor ideas and improvements observed along the way (that don't rise to the level of a story
criterion) are recorded in `docs/backlog.md`.

## Technical risks — tackle first, not last

1. **Serving local video to the webview with seek (truly resolved on 28/08/2026):** click-a-line-
   to-jump-the-video requires the video to be served with support for **HTTP range
   requests** — without it, `<video>` can't seek to an arbitrary position. The original solution
   (a `GET /media/lesson/{id}` endpoint via `http.ServeFile`, plugged in as Wails' AssetServer
   `application.Middleware`, served over the `wails://` scheme on Linux) seemed to solve this — the
   automated range-request tests passed and the app opened — but the video never actually played
   on Linux (WebKitGTK/GTK4): the Range request arrived correctly at the Go handler, but the
   206 response never completed GStreamer's media pipeline (an instant `FormatError`,
   without requesting the rest of the file), while the same `.mp4` played fine in Chrome/Firefox
   — a known limitation of WebKitGTK with custom URI schemes + `<video>` elements. Actual fix:
   the video is served by a real `http.Server` on `127.0.0.1` (a free port chosen
   by the OS), outside Wails' AssetServer — `services/video_server.go`, `VideoServerService`. See
   the 28/08/2026 entry.
2. **File drag-and-drop in Wails v3 (resolved, Story 3b):** the native API
   (`EnableFileDrop` + `WindowFilesDropped` event) works on all three platforms at the pinned
   version `v3.0.0-alpha2.117` — the file-dialog fallback wasn't needed.
3. **Keyring on Linux:** `zalando/go-keyring` depends on the Secret Service (gnome-keyring/kwallet).
   Test early on your Linux machines; documented fallback if needed.

---

## Story 1 — App skeleton

**As** a user, **I want** the app to open with the navigation and visual identity defined,
**so that** I have the base the other stories build on.

### Acceptance criteria
- [x] Wails v3 project (pinned version in `go.mod`) + Svelte 5 (runes) building on Windows and Linux machines.
- [x] The window actually opens (visual verification — `wails3 dev` or the `wails3 build` binary) on Windows and Linux machines. Visually confirmed on both machines.
- [x] Thin-layer principle respected: `internal/` has no Wails imports; the app references the Phase 0 packages without copying them.
- [x] Sidebar with Library / Progress (placeholder) / Queue, and a header — ported from the React prototype (dark theme, Sora/Inter/JetBrains Mono).
- [x] Svelte 5 conventions from `CLAUDE.md` applied (no legacy syntax).

---

## Story 2 — Local database and machine configuration

**As** a user, **I want** the app to create/open its database and know where the storage
root is, **so that** imported lessons persist across sessions.

### Acceptance criteria
- [x] SQLite (`modernc.org/sqlite`, WAL) created in the OS data directory — **outside** the synced folder.
- [x] `goose` migrations embedded (`embed.FS`); schema v1: `lessons`, `transcripts`, `jobs`, `prompts` (the last two already in the format defined in `decisoes-tecnologia.md`, even without full use in this phase).
- [x] Local config (storage root path) in the OS config directory; first run asks for the folder with visual validation.
- [x] STT provider credentials written/read via `go-keyring`; never in plain text. Full unit coverage (round-trip + simulated keyring failure); manual verification on Windows and Linux (risk 3) confirmed.

### Dependencies
Story 1.

---

## Story 3 — Import lesson: scanning the existing folder

**As** a user, **I want** the app to find, on its own, the lesson videos already in the
storage folder and let me confirm each one's date/tutor, **so that** I can register them in
the library and trigger processing — without needing to have organized the folder beforehand or
importing videos one at a time manually.

**Current expectation (adjusted on 22/07/2026):** the storage folder chosen in the wizard
likely **already** has videos of previous lessons, loose, with no subfolder convention
whatsoever. The app must not assume or impose any directory structure to recognize them.
Identification and dedup are always by **filename + SHA-256**, never by path convention.
The scope of this story is just scanning + review; manual drag-and-drop import was
split off into **Story 3b**, since the folder already having the lessons is the more urgent case. Full
design in `docs/superpowers/specs/2026-07-22-historia-3-importar-aula-design.md`.

### Acceptance criteria
- [x] **Storage folder scan:** recursive, without assuming any subfolder structure; identifies `.mp4` videos and computes each one's SHA-256 (measured cost: ~1-3s per typical lesson video, even without hardware acceleration — not a bottleneck).
- [x] **Stat cache before hashing:** for already-registered videos, the scan first checks the saved path + size + mtime; it only recomputes the SHA-256 if something changed. Avoids re-reading the full contents of unchanged files on every "Sync folder" run.
- [x] A video whose hash is already registered is ignored; the same hash at a different path **updates the lesson's path** (the file was only moved/renamed), without becoming a candidate or a duplicate.
- [x] New videos become candidates in a **pending review** list in the Library (no "Ignore" concept — the folder should only contain lessons). Confirming a candidate (date/tutor modal) writes the `lesson` + `extract_audio`/`transcribe` jobs as `pending`, and removes it from the pending list.
- [x] The scan runs automatically at the end of the first-run wizard (after choosing the folder) and is also available as an on-demand action afterward ("Sync folder"), to pick up videos manually dropped into the folder after setup. Verified visually on Windows/Linux.
- [x] A duplicate import (same hash) is detected and not duplicated — guaranteed by a unique index in the database.
- [x] Once confirmed (date/time/tutor in the modal), the video file is renamed *in place* to `YYYY-MM-DD_HHHMM_tutor-slug.ext` (e.g., `2026-07-23_14H30_maria-jose.mp4`); a rename failure doesn't block confirmation (best-effort, logged). Time is now required at confirmation — a candidate missing date, time, or tutor stays pending. Design in `docs/superpowers/specs/2026-07-23-historia-3-renomeacao-padronizada-design.md`.

### Dependencies
Story 2.

---

## Story 3b — Import a lesson manually (drag-and-drop)

**As** a user, **I want** to drag a new lesson's video into the app and confirm date/tutor,
**so that** I can register it without waiting for the next folder scan.

### Acceptance criteria
- [x] Drag-and-drop (or file-dialog fallback — risk 2) opens the same confirmation modal as Story 3 (date pre-filled from the filename/file metadata when possible, free-text tutor field).
- [x] The video is copied into the storage root using a predictable structure (e.g., `aulas/2026/2026-07-15/`); the database stores **only the relative path**.
- [x] Entry created in `lessons` + `extract_audio` and `transcribe` jobs created as `pending`, reusing the same confirmation/dedup-by-hash logic from Story 3.
- [x] A duplicate import (same file/hash) is detected and flagged, not duplicated.

### Dependencies
Story 3.

---

## Story 4 — Background pipeline (job queue)

**As** a user, **I want** extraction and transcription to run on their own in the background,
**so that** I can keep using the app (or close it) without waiting after importing.

### Acceptance criteria
- [x] A single worker (goroutine) processes jobs sequentially: `extract_audio` → `transcribe` (ElevenLabs Scribe, configuration recorded in Phase 0).
- [x] `pending/running/done/error` states with `attempts`, `last_error`, and simple backoff; idempotency (a job checks its output artifact before running).
- [x] On app startup, jobs stuck in `running` revert to `pending`.
- [x] Transcript persisted in `transcripts` (diarized utterances + word-level timestamps); the provider's raw JSON is saved in the storage root alongside the lesson.
- [x] A network/API failure leaves the lesson intact (video still watchable) with a readable error — the resilience principle is guaranteed at the data layer (a failure never touches `lessons`); the visual surface of "readable error in the Queue" belongs to Story 7, not yet built.
- [x] Wails events notify the frontend of progress/state (a `job:updated` event is emitted on every transition) — the transport is ready, no screen consumes it yet (consumption happens in Stories 5 and 7).

### Dependencies
Story 3.

### Implementation notes
`storage_root`/STT credential are resolved on every job (not once when the worker is created),
because the first-run wizard runs after the app has already started — resolving them upfront would
mean the worker never starts during the first-use session. `Worker.Wake()` (waking the worker
immediately when a lesson is confirmed, instead of waiting for the poll) exists but wasn't wired
into the import confirmation flow in this slice — a deliberate decision, since the ~5s fallback
poll is imperceptible in a background queue. Full design in
`docs/superpowers/specs/2026-07-22-historia-4-pipeline-jobs-design.md`.

---

## Story 5 — Real library

**As** a user, **I want** to see my lessons listed with status, **so that** I can find and open
any archived lesson.

### Acceptance criteria
- [x] A real list from the database: date, tutor, duration, status (processing/ready/error), in the prototype's layout.
- [x] Simple filter by tutor and period (full-text search is left for a future phase).
- [x] A `ready` lesson opens the Detail view (minimal stub: date/tutor/video, no transcript — sync is Story 6); `processing` shows its state; `error` shows a message and a reprocess action (recreate the job).

### Dependencies
Story 4 (real data to list).

---

## Story 6 — Lesson detail: synchronized video + transcript

**As** a user, **I want** to watch the lesson with the transcript alongside it and jump the video
by clicking a line, **so that** I can review specific moments of the conversation.

*This is the heart of the MVP — do the risk-1 spike before starting.*

### Acceptance criteria
- [x] Local video served to the `<video>` element with range requests; seeking works (risk 1
      truly resolved only on 28/08/2026 — the original verification for this story missed that the
      video didn't play on Linux; see technical risk 1 and the 28/08/2026 entry).
- [x] Scrollable transcript alongside it, student utterances visually distinct from the tutor's (prototype layout, no inline corrections — Phase 2) — a 2-column grid with no tabs; a speaker toggle (neutral labels "Speaker A"/"Speaker B" until the user indicates which is the student, then it becomes "You"/"Tutor" with theme colors), the choice persisted in `student_speaker_label`.
- [x] Clicking a line seeks the video to that timestamp (tolerance ~1s) — the click writes to `video.currentTime`.
- [x] The current line is highlighted as the video plays (the highlight follows playback) — synced via the `<video>` element's `timeupdate` event (index derived from the utterance list, no `requestAnimationFrame`/WebVTT), with auto-scroll to keep the current line visible.
- [x] A lesson with no transcript (pending/error) still plays the video normally — the panel has three states depending on lesson status ("processing", "error" with a Reprocess button, "ready" fetches and shows the transcript); the Library now opens the Detail view for any status, not just "ready".

### Dependencies
Stories 4 and 5.

---

## Story 7 — Visible queue

**As** a user, **I want** to see what's processing and what's failed, **so that** I can trust
the background pipeline.

### Acceptance criteria
- [x] A Queue screen with jobs, state, progress, and a readable `last_error`; a reprocess action on errors.
- [x] A sidebar badge with the active job count, updated via events.

### Dependencies
Story 4.

---

## Story 8 — Basic settings (path + credential)

**As** a user, **I want** a Settings screen where I can view/change the storage
root and re-register the STT provider credential, **so that** I can recover on my own
if the keyring credential is lost or cleared, and repoint the app when I move the lesson
folder myself (new drive, reorganization, etc.), without having to edit
`config.json` by hand.

### Acceptance criteria
- [x] A Settings screen accessible via a gear icon in the Header (not a Sidebar
  navigation item), shows the configured storage root and allows changing it (native dialog +
  write validation), without moving files — the user has already moved them manually. The
  switch is never blocked by missing videos.
- [x] Switching folders runs the same hash-based reconciliation as Story 3: videos with a
  different name in the new folder have their `video_path` updated automatically; videos whose
  hash isn't found in the new folder are flagged as "missing video" in the Library (recalculated
  on every load, never a persisted column) until the file reappears at the expected path.
- [x] A field to (re-)register the STT provider (ElevenLabs) credential via `go-keyring`,
  reusing `config.SaveSTTAPIKey`; the screen shows whether a credential is already configured
  (without revealing the value) — covers the known gap from Story 2.
- [x] No provider selection, cost estimation, or any other configuration option —
  that's Phase 5.

### Dependencies
Story 2.

---

## Story 9 — Teacher management and lesson editing

**As** a user, **I want** to pick a lesson's teacher from an already-registered list (or type a
new name), rename a teacher as an entity, and edit the date/time/teacher of an already-confirmed
lesson, **so that** I can keep the data correct without retyping names and without depending on
getting everything right at import confirmation.

### Acceptance criteria
- [x] Teacher becomes an entity (`teachers`), no longer a free-text column on `lessons` — renaming
  is reflected across all of that teacher's lessons automatically; the name is unique (a collision
  on rename produces a readable error).
- [x] The import form (Stories 3 and 3b) shows already-registered teachers in a combobox
  (`<input list>`/`<datalist>`), with the option to type a new name.
- [x] A "Teachers" panel lists registered teachers with a rename action (initially in
  Settings; moved to its own screen accessible from the Library on 18/08/2026 — see the Phase 2
  progress log).
- [x] An "Edit" button on the Lesson Detail screen opens a date/time/teacher form; saving
  also tries to rename the video to the current standardized name (best effort, same logic as
  Story 3 — a rename failure doesn't block saving the edit).

### Dependencies
Story 3 (the only consumer of the old `tutor` column).

## Milestones

- **M1 — "Imports and stores":** Stories 1–3 (3b optional, manual import). The app opens, maps the lessons already in the folder, and registers them.
- **M2 — "Transcribes on its own":** Story 4 (+7 optional). Import at night, transcript ready in the morning.
- **M3 — Complete MVP:** Stories 5–6. Watch with the synchronized transcript. **From here on, the app goes into real use for your lessons.**
- **Story 8 (Basic settings)** doesn't belong to any milestone above — it's independent, fitting in at any point after Story 2.

## Next increments (vision, no commitment)

Phase 2: LLM analysis in the UI (inline corrections, an Analysis tab, active versioned prompts) ·
Phase 3: automatic tags + full-text search (FTS5) + Progress screen ·
Phase 4: pull-work-push sync between machines + backups + new-machine onboarding ·
Phase 5: the rest of Settings (provider selection, cost estimation, optional deeper analysis
with `deepseek-v4-pro`) — storage path (viewing) and STT credential re-registration already
moved into Phase 1 (Story 8).

## Progress log

| Date | What was done | Notes |
|------|-----------------|-------------|
| 20/07/2026 | Story 1 implemented: Wails v3 + Svelte 5 skeleton (sidebar, empty header, 3 placeholder screens); production build confirmed | wails3 v3.0.0-alpha2.117 pinned; fonts self-hosted via @fontsource; still missing visual verification (window opening) on Windows and Linux before closing the story |
| 21/07/2026 | Story 2 implemented: SQLite database (schema v1: lessons/transcripts/jobs/prompts, goose migrations), local config (config.json), and ElevenLabs credential via keyring, all in the first-run wizard | On Linux, `go build`/`go vet`/`go test` for any package importing Wails requires `CGO_ENABLED=1` + `libgtk-4-dev libwebkitgtk-6.0-dev` installed (gtk4/webkitgtk-6.0, not gtk3) — worth documenting/installing this early on a new Linux machine; known gap: if the keyring credential is lost/cleared after setup, the app doesn't detect it (config.json still exists) and there's no UI to re-register it until the Phase 5 Settings screen |
| 22/07/2026 | Story 3 implemented: recursive scan of the storage folder (identification by name+SHA-256, no assumed structure), stat-cache before hashing, pending candidates reviewed one by one in the Library (date/tutor modal), lesson+jobs created on confirmation; a unique index on video_hash guarantees no duplication | No extension besides `.mp4` recognized in this slice (easy to extend later); the `wails3` CLI must be installed manually (`go install .../cmd/wails3@v3.0.0-alpha2.117`) on a new machine, it's not a go.mod dependency; the full wizard→scan→review flow hasn't yet been visually verified in a real window (no display in this build environment) |
| 22/07/2026 | Story 4 implemented: a single worker in `internal/jobs` processes `extract_audio`→`transcribe` sequentially with precedence (transcribe is blocked if extract_audio failed, without spending an STT call), real idempotency by artifact (cached WAV / row in `transcripts`), retry with backoff (3 attempts, 10s/60s/5min), and requeue of jobs stuck in `running` on startup; `storage_root`/credential resolved per job (not at worker creation) to survive the first-run wizard's timing; the Wails `job:updated` event is emitted on every transition (transport ready, no consumer yet) | Executed via subagent-driven-development (8 tasks, per-task review + final branch review); the final review caught a real cross-task bug (the worker could call `application.Get()` before `application.New()` ran, causing a panic if there was a pending job from a previous session) — fixed with a nil-guard in the notifier; `Worker.Wake()` exists but wasn't wired into the import confirmation flow (deliberate decision — the ~5s fallback poll is acceptable); the full flow (pipeline actually processing a real lesson) still hasn't been visually verified in a real window (no display in this build environment) |
| 22/07/2026 | Story 5 implemented: Library status derived from jobs (extract_audio/transcribe) on every read — never a separately stored column; duration computed via ffprobe on a best-effort basis at import confirmation (never blocks confirmation); filter by tutor (dropdown)/period; a "Reprocess" button resets a lesson's errored jobs (including a transcribe blocked by a dependency) without explicitly waking the worker (fallback poll); the minimal Detail view (date/tutor/video) resolves the project's technical risk 1 — a `GET /media/lesson/{id}` endpoint via the stdlib `http.ServeFile`, which already handles range requests, plugged in as a Wails v3 `application.Middleware` | Risk 1 truly resolved (not a placeholder): confirmed by reading Wails v3's source code that the webview always talks to the Go server, both in production (embedded assets) and in `wails3 dev` (proxied to Vite) — the middleware intercepts before either path; Story 6 reuses the same endpoint, just adding the synchronized transcript; frontend bindings need the `-i` flag in addition to `-ts` (`wails3 generate bindings -ts -i ./...`) to generate interfaces instead of classes — without it the generator produces `.js` with classes, a format this project's frontend doesn't use; the full flow (Library → Detail → video playing) still hasn't been visually verified in a real window (no display in this build environment), same pattern as previous stories |
| 23/07/2026 | Story 6 implemented: Lesson Detail redesigned as a 2-column grid (video on the left, transcript panel on the right, no tabs); video↔transcript sync via the `<video>` element's `timeupdate` event (index derived from the utterance list, no `requestAnimationFrame`/WebVTT) — clicking a line jumps the video, the current line is highlighted with auto-scroll; a speaker toggle (raw diarization comes in as neutral "Speaker A"/"Speaker B" until the user indicates which is the student, then it becomes "You"/"Tutor" with theme colors), the choice persisted in `student_speaker_label` via `SetStudentSpeaker`; the Library can now open the Detail view for any lesson status (previously only "ready"), with three panel states depending on status ("processing", "error" + Reprocess, "ready" fetches the transcript) | Code review caught a real bug: a failure when clicking the speaker toggle or the Reprocess button crashed the entire video+panel UI, because it wrote to the same error state used for the page's initial load failure — fixed by giving those actions their own error state, which renders as a small banner without unmounting the video (the video keeps playing even if the toggle/reprocess fails); real verification in a window (clicking a line and confirming the video jumps, watching the highlight follow playback in real time, clicking the speaker toggle) is still pending on Windows/Linux — same pattern as previous stories; `go test ./...`, `go vet ./...`, and `wails3 build` confirmed clean in this environment (Linux sandbox with no display) |
| 23/07/2026 | Story 7 implemented: `internal/db.ListQueueEntries` derives, per lesson, which job (extract_audio or transcribe) is currently active or errored — checking extract_audio before transcribe, since the transcribe job's DB status stays "pending" the whole time it's blocked waiting on extract_audio (the Worker only skips it in memory); `QueueService` translates this for the frontend (Stage/Status in PT-BR), reusing `db.ResetErrorJobsForLesson` for Reprocess; `jobsStore.svelte.ts` is the first real consumer of the `job:updated` event (transport ready since Story 4) — it fetches the queue once and refetches on every event, shared between `Queue.svelte` and the Sidebar's numeric badge (pending+running only, errors excluded from the count) | Visual verification (a real window) of the badge updating live during real processing and of the Queue list changing alongside it is still pending on Windows/Linux, same pattern as previous stories |
| 23/07/2026 | Additional slice of Story 3 completed: confirmation now requires a time and renames the video *in place* to the standardized name, with collision resolution and a best-effort rename; a deterministic injected failure confirms that a move error doesn't block confirmation, a TOCTOU race confirms the destination is never overwritten, and a SQLite trigger confirms the rollback after a failed path update | The test suite and vet confirm the automated flow; no real visual verification was done in this slice and the pending item follows the same pattern as previous stories |
| 24/07/2026 | Story 3b implemented: Wails v3's native drag-and-drop (`EnableFileDrop` + `WindowFilesDropped` event, no HTML5 File API) resolves technical risk 2 — works on all three platforms at the pinned version; dropped onto the Library screen (`data-file-drop-target`), it opens `ImportConfirmModal` right away (a local queue drains one modal at a time on multiple drops); files are copied into `storage_root` with no subfolder, with a collision suffix, or registered in place if the file is already inside the storage root; an unrecognized extension or an already-imported/pending hash is rejected without copying, with the error reported via the `import:drop-error` event | Reuses 100% of Story 3's confirmation/dedup flow (`ImportConfirmModal`, `pending_imports`, `ConfirmImport`) unchanged; `internal/importer.HashFile`/`HasVideoExtension`/`SuggestDate` were exported for DropImport to reuse without duplicating logic; `db.InsertPendingImport` now returns the id of the created row; real visual verification (dragging a file in a real window) is still pending on Windows/Linux, same pattern as previous stories |
| 24/07/2026 | Story 8 implemented: a Settings screen (gear icon in the Header, outside the Sidebar) with two panels — Storage (view the current folder, a change button with a native dialog + write validation, the switch is never blocked) and STT Credential (boolean status, a field always available for (re-)registration via keyring); hash-based reconciliation reuses `importer.Scan` from Story 3 (videos renamed in the new folder have their `video_path` updated, missing ones flagged as 'missing video' — recalculated on every read, never persisted); a "missing video" badge appears in Library.svelte and LessonDetail.svelte | Full unit coverage: `services/settings_test.go`, `services/storage_folder_test.go`, extended `services/library_test.go`; `go test ./...` and `go vet ./...` confirmed clean, `pnpm run check`/`pnpm run build` confirmed clean; manual verification (opening Settings from the Header icon, switching folders with a renamed video confirming reconciliation, deleting a video confirming the 'missing' badge, re-registering the credential) is still pending on Windows/Linux, same pattern as previous stories |
| 30/07/2026 | Story 9 implemented: teacher becomes a `teachers` entity (migration with backfill from the `tutor` column), a teacher combobox in the import form, a "Teachers" panel in Settings (renaming is reflected across all lessons via JOIN), date/time/teacher editing in the Lesson Detail view (renames the video in place, best effort) | `go test ./...`, `go vet ./...`, `pnpm run check`/`pnpm run build` confirmed clean; real visual verification (combobox, renaming a teacher, editing a lesson in a real window) is still pending on Windows/Linux, same pattern as previous stories |
| 01/08/2026 | **Phase 1 completed.** All the real-window visual verifications that had been pending since Story 1 were done (Windows and Linux) — the window opening, keyring (risk 3), the automatic scan at the end of the wizard (Story 3), and the flows from Stories 3b–9 (drag-and-drop, the pipeline processing a real lesson, Detail view with synchronized video+transcript, live Queue, Settings, teachers) | Milestones M1–M3 closed; the app is in real use for lessons. Next step is Phase 2 (LLM-based analysis), already started on 30/07/2026 — see `docs/fase-2-analise-llm.md` |
| 28/08/2026 | **Real usage bug found and fixed: video wouldn't play on Linux** (a crossed-out play icon, no audio or image), despite the 01/08/2026 visual verification having marked the flow as confirmed — that verification missed the problem (hypothesis: a different machine/WebKitGTK version, or the play click wasn't actually tested). Diagnosis: a debug log confirmed the Range request arrived correctly at the Go handler (`bytes=0-1445`, a typical GStreamer typefind probe), with no follow-up request afterward — and the same `.mp4` played fine outside the app (Chrome/Firefox), ruling out the codec/file. Root cause: WebKitGTK (GTK4/webkitgtk-6.0, still "experimental" in Wails v3) doesn't properly deliver 206/Range responses to the GStreamer pipeline when the source is the custom URI scheme (`wails://`) Wails uses to serve everything on Linux — a known friction point of this combination. Fix: `services/video_server.go` spins up a real `http.Server` on `127.0.0.1` (a free port chosen by the OS) just for video, registered as `VideoServerService`; the frontend's `<video src>` now uses `${VideoServerService.BaseURL()}/media/lesson/{id}` instead of the relative path served by the AssetServer. `VideoAssetMiddleware` was removed (it became `VideoAssetHandler`, no longer depending on Wails) — the `Middleware` in `AssetOptions` in `main.go` is no longer used. As a bonus, `Linux.WebviewGpuPolicy` is now also explicitly set to `Never` (the actual zero-value is `Always`, not `Never` as Wails' docs suggest — that only applies inside `wails.Run()`, which this app doesn't use; see `github.com/wailsapp/wails/issues/2977`) — not the cause of this bug, but a real pitfall in the same area (video decoding), and there's no cost to keeping it off | `go vet ./...`, `go test ./...`, `pnpm run check`, `wails3 build` confirmed clean; verified in a real window on Linux that the video now plays. Still need to re-verify in a real window on Windows — Wails' default AssetServer was never actually confirmed working there for video (only listed as "pending" in previous stories), and the app now depends on `VideoServerService` on every platform; also worth reassessing the "video seek is slow" backlog item in light of this server change |
