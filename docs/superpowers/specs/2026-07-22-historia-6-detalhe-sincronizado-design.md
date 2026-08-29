# Story 6 — Lesson detail: synchronized video + transcript

> Design spec. Stories and acceptance criteria in `docs/fase-1-mvp.md`.

## Context

The Detail screen (Story 5) is today a minimal stub: header (date/tutor) + `<video controls>`
pointing at the `/media/lesson/{id}` endpoint (which already resolved technical risk 1 — range
requests). What's missing is what makes this screen the heart of the MVP: the scrollable
transcript alongside it, click-on-utterance seeking the video, and highlighting of the current
utterance following playback. This story closes M3 ("complete MVP") from `fase-1-mvp.md`.

Reference layout: `docs/prototipo-app-aulas.jsx`, component `LessonDetail` (lines
367-568) — 2-column grid (video on the left, panel on the right), student utterances in blue
(left border), tutor in green. The prototype also shows an "Analysis" tab and inline
corrections — both are Phase 2, out of scope here.

## Decisions

### The speakers' roles (student × tutor) are not persisted today

The database has never stored which `speaker_0`/`speaker_1` is the student — today this exists
only as an in-memory parameter used by `analysis.FormatTranscript` (Phase 2). Since Cambly
recordings always have exactly 2 speakers, the solution is a new column, not a generic table:

- Migration `00003_student_speaker.sql`: `ALTER TABLE lessons ADD COLUMN
  student_speaker_label TEXT`. `NULL` until the user chooses.
- `db.SetStudentSpeaker(conn, lessonID, speakerLabel string) error` saves the choice.
- **Labels before choosing:** "Speaker A" / "Speaker B", assigned by order of first utterance in
  the transcript (deterministic — doesn't depend on map/iteration order). No role color (neither
  blue nor green) until the user chooses.
- **Toggle:** two small buttons above the transcript panel ("Speaker A is you" /
  "Speaker B is you"). On choosing, it saves via `SetStudentSpeaker` and the UI reflects it
  immediately (optimistic): student in blue with a left border (like the prototype) labeled
  "You", tutor in green labeled "Tutor". Persisted — future reopenings already come colored.

### Access to the Detail screen no longer requires "ready" status

`Library.svelte` (`openLesson`) today only navigates to the Detail screen when
`status === "ready"`, which by definition (`deriveStatus`, Story 5) already requires `transcribe`
to be complete — meaning the acceptance criterion "a lesson without a transcript yet still plays
the video normally" could never be reached by navigating from the Library as it stands today.
Necessary adjustment: `openLesson` now navigates regardless of status. The video is always
watchable (resilience principle); the transcript/error are handled inside the Detail screen
itself (see "Panel without a transcript" below).

Consequence: `LibraryService.GetLesson` needs to compute `Status`/`ErrorMessage` (today it
doesn't — a code comment said it wasn't worth it because it was only ever reached with "ready").
Reuses the same `ListLessonsWithStatus`/`deriveStatus` logic from Story 5, by a single id.

### Video ↔ transcript sync: `timeupdate` event, no RAF nor WebVTT

Three approaches considered:

1. **Chosen.** An `ontimeupdate` handler on the `<video>` updates a `$state<number>` for
   `currentTime`; a `$derived` finds the last utterance with `start <= currentTime` (linear
   search — a few hundred items per lesson at most, negligible cost). Clicking an utterance sets
   `videoEl.currentTime = utterance.start`. Simple, idiomatic with runes, no new dependency.
2. A `requestAnimationFrame` loop reading `currentTime` every frame: more precise (60x/s) than
   needed — the highlight is per whole utterance, not per word, and `timeupdate` (~4x/s on
   Chromium/WebKit) already fits within the ~1s tolerance of the acceptance criterion.
3. Native WebVTT track (`<track>` + `cuechange` event): would take advantage of the browser's
   native sync, but VTT renders as a caption overlaid on the video — doesn't fit the custom side
   panel (role colors, click, auto-scroll) required by the prototype.

Clicking an utterance only adjusts `currentTime` (seek) — it doesn't force play nor pause; if
the video was paused, it stays paused at the new position. This avoids surprising someone who
just wants to check the timestamp.

Auto-scroll: an `$effect` watching the highlighted index calls `scrollIntoView({block:"nearest"})`
on the corresponding row, keeping the current utterance visible without abrupt jumps. It also
works for seeks made via the `<video>`'s native control (scrubber), since `timeupdate` fires
regardless of what caused the position change.

Granularity: only at the utterance level (whole line), not per word — `Words` is not exposed to
the frontend in this story (the `stt.Utterance.Words` structure already exists in the database
for an eventual Phase 2 word-by-word highlight, but there's no acceptance criterion for that
now).

### Panel without a transcript

Since the Detail screen now opens regardless of status, the panel to the right of the video
handles the cases where there's no row in `transcripts`:

- `status === "processing"` → "Transcript being processed…".
- `status === "error"` → error message (`ErrorMessage` from `GetLesson`) + a "Retry" button,
  reusing `LibraryService.RetryLesson` (already used in the Library since Story 5).
- `status === "ready"` with no transcript found (shouldn't happen given `deriveStatus`, but
  defensive against a race/inconsistent state) → same text as "processing".

The video always renders and plays normally in all three cases — only the panel changes.

### No tabs in the panel (Analysis is for Phase 2)

The prototype has "Transcript"/"Analysis" tabs, but LLM analysis in the UI is explicitly out of
scope for Phase 1 (`fase-1-mvp.md`). Building the tab bar now would be UI for a feature that
doesn't exist yet. The panel shows the transcript directly, with no tab bar — the Analysis tab
arrives when Phase 2 comes.

## Changes by layer

### `internal/db`
- Migration `00003_student_speaker.sql`.
- `FindTranscriptByLessonID(conn *sql.DB, lessonID int64) (*Transcript, error)` — reads
  `raw_json_path`/`utterances`, deserializes `utterances` into `[]stt.Utterance`. Returns `nil,
  nil` if there's no row (not an error — the normal state while `transcribe` hasn't finished).
- `SetStudentSpeaker(conn *sql.DB, lessonID int64, speakerLabel string) error`.
- `FindLessonByID` (or a variant) now also returns derived status/error — reuses the
  `deriveStatus`/jobs join already existing in `ListLessonsWithStatus`, adapted for a single id.

### `services/library.go`
- `Lesson` gains `StudentSpeakerLabel *string`.
- `GetLesson`: now fills in `Status`/`ErrorMessage` (previously left zeroed since there was no
  caller that needed it).
- New `Transcript`/`Utterance` (types exposed to the frontend): `Utterance{ Speaker, Text string;
  StartSeconds, EndSeconds float64 }` — the `time.Duration`-to-seconds conversion happens here,
  at the service boundary (the frontend works only in seconds, the same unit as
  `video.currentTime`).
- New `GetTranscript(lessonID int64) (*Transcript, error)`: `nil, nil` if there's no transcript
  yet.
- New `SetStudentSpeaker(lessonID int64, speakerLabel string) error`.

### Frontend
- `frontend/src/lib/screens/Library.svelte`: `openLesson` navigates regardless of status (removes
  the `status === "ready"` check).
- `frontend/src/lib/screens/LessonDetail.svelte`: 2-column grid; `<video bind:this>` with
  `ontimeupdate`; transcript panel with a speaker toggle, clickable rows
  (`videoEl.currentTime = utterance.startSeconds`), highlight + auto-scroll of the current
  utterance, "no transcript" states (processing/error with Retry).
- Wails bindings regenerated (`wails3 generate bindings -ts -i ./...`) reflecting
  `GetTranscript`/`SetStudentSpeaker`/new `Lesson` fields.

## Out of scope (not implemented here)

Analysis tab and inline corrections (Phase 2); word-level highlight/click; the Queue screen and
active-job count badge (Story 7); any change to `RetryLesson`/worker beyond the existing reuse.

## Tests

- `internal/db`: round-trip `SetStudentSpeaker`; `FindTranscriptByLessonID` with a synthetic
  utterances fixture (including the `time.Duration` conversion), `nil, nil` when there's no
  transcript; reading a lesson with derived status including the new field.
- `services`: `GetLesson` returning correct status/error across all combinations; `GetTranscript`
  with/without a transcript; `SetStudentSpeaker` reflected in a subsequent `GetLesson`.
- Frontend: no JS test suite configured in this project — validation via clean `npm run
  check`/`build`; visual verification (real window: click seeks the video, highlight follows
  playback, speaker toggle) remains pending on Windows/Linux machines, same pattern as previous
  stories.
</content>
