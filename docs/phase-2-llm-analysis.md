# Phase 2 — LLM-based analysis in the UI

> Goal: bring LLM-based analysis (already validated in isolation in Phase 0) into the app, **one
> task at a time**: implement on demand, observe it on real lessons, decide whether it stays
> (kept, refined, or discarded), and only then automate it in the background. There is no longer a
> fixed roadmap for the 7 originally planned tasks (`analyze_corrections`, `analyze_vocabulary`,
> `analyze_tutor_expressions`, `analyze_tutor_taught_terms`, `analyze_tutor_feedback`,
> `analyze_tutor_corrections`, `analyze_topics`) — it's possible (expected, even) that not all
> of them survive validation in real use.
>
> **Current scope (20/08/2026):** only `analyze_corrections` and `analyze_topics` remain active —
> both already implemented (Stories 2 and 3) and kept, with future work aimed at refining their
> prompts/UX, not expanding coverage. The other 5 candidate tasks are paused
> indefinitely (not discarded, just off the radar for now); they resume only if the user explicitly
> asks for them.
>
> Replanning recorded in
> `docs/superpowers/specs/2026-08-05-phase2-iterative-replanning-design.md` (replaces the
> original Stories 2–5 structure, which automated and built UI for all 7 tasks at once).
>
> Base: the `internal/analysis` and `internal/jobs` packages from Phase 1, reused and extended.
> Detailed technical specs are written story by story, at
> `docs/superpowers/specs/YYYY-MM-DD-historia-N-<slug>-design.md`, as each one is started —
> this document sets the boundaries and acceptance criteria, not the implementation design.

## Out of scope for this phase (write down ideas, don't implement)

Analysis provider/model selection and cost estimation (Phase 5) · a fixed or
hierarchical topic taxonomy (a free-form list per lesson in this phase) · full-text search (FTS5) and
automatic tags cross-referencing lessons (Phase 3) · Progress screen (Phase 3) · manual
editing of corrections/vocabulary by the user · automatic re-analysis when the prompt changes
(reprocessing remains an explicit action, same pattern as the Phase 1 Queue).

## Technical risks — tackle first, not last

1. **Quality of each task's prompt:** Phase 0 validated only 1 prompt with 3 categories, in a
   single run. Splitting it into small, focused prompts should help precision (each one does just
   one thing), but that's a hypothesis, not a fact — validate each task individually, in real use in
   the UI, before automating it in the background (no longer in batch via CLI).
2. **Anchoring by `utterance_index` (resolved, Story 1):** corrections (student's and tutor's) and
   tutor feedback reference the index of the utterance in the transcript sent to the model. An
   out-of-range or missing index is silently discarded (with `slog.Warn`), never breaking the whole
   task — implemented and tested in the 3 tasks that anchor to an utterance.
3. **Confirmed cost per task:** Phase 0 measured ~US$0.0014/lesson for 1 call with 3 categories.
   Each new task re-sends the entire transcript — measure the real cost of each task once it's
   implemented, before deciding whether it stays.

---

## Story 1 — Per-task prompts and analysis persistence

**As** a user, **I want** the app to have the prompts and storage ready for each analysis
type, **so that** analysis tasks can run and reliably store their results.

### Acceptance criteria
- [x] 7 versioned prompts, each focused on a single output: `analyze_corrections`,
  `analyze_vocabulary`, `analyze_tutor_expressions`, `analyze_tutor_taught_terms`,
  `analyze_tutor_feedback`, `analyze_tutor_corrections`, `analyze_topics` — registered in the
  existing `prompts` table (name + version), each able to evolve independently of the others.
- [x] `FormatTranscript` numbers the utterances (`utterance_index`) in the transcript sent to the
  model, to allow corrections and feedback to reference the exact utterance.
- [x] `analysis.Provider` becomes task-agnostic: `Complete(ctx, systemPrompt, transcript)
  (json.RawMessage, error)` — it no longer knows about `Correction`/`VocabularyItem`/etc. Each task
  defines its own output type and parses it from the raw JSON, reusing common generic validation.
- [x] New migration: an `analysis_results` table (one row per `lesson_id` + `task`, with
  `prompt_id`, `model`, `result_json`, `UNIQUE(lesson_id, task)`) and `lesson_topics`
  (`lesson_id`, `topic`) for use by candidate tasks that need it. (The raw provider response was
  originally also persisted to a `raw_response_path` file alongside the video, mirroring
  `transcripts.raw_json_path`; both were dropped in migration `00007` after turning out to be
  write-only — nothing ever read them back.)
- [x] Behavior defined and tested for an out-of-range or missing `utterance_index` (risk 2):
  a negative index (JSON missing the field) or one outside `[0, utteranceCount)` silently discards
  the item (with `slog.Warn`), never breaking the whole task — the same handling in the 3 tasks
  that anchor to utterances (`analyze_corrections`, `analyze_tutor_feedback`,
  `analyze_tutor_corrections`).
- [x] Infrastructure validation: instead of running all 7 tasks in a batch via a temporary CLI,
  validation now happens task by task, as each one gets a UI (starting with
  Story 2) — `cmd/validate-analysis` is removed without ever having run (part of Story 2's
  implementation plan).

### Dependencies
None (uses the existing `internal/analysis`/`internal/db` packages from Phase 0/1).

---

## Story 2 — Pilot: Student corrections

**As** a user, **I want** to see the student's corrections highlighted in the very utterance
where they occurred, **so that** I can review my errors in the exact context they happened in — and
serve as a pilot to validate whether it's worth continuing with the other planned analysis tasks.

The first of the 7 candidate tasks to be implemented — chosen for learning value, not
ease (it's the most complex to display, since it depends on anchoring to an utterance). Detailed
technical design in its own spec, written before implementation.

### Acceptance criteria
- [x] Analysis provider (DeepSeek) credential via `go-keyring` (`SaveAnalysisAPIKey`/
  `GetAnalysisAPIKey`, same pattern as `SaveSTTAPIKey`/`GetSTTAPIKey`) + a field in the
  Settings screen, next to the STT field — shows whether a credential is already configured, without revealing the value.
- [x] A manual action on the lesson (e.g., an "Analyze corrections" button) triggers
  `analyze_corrections` on demand — no background job in this slice.
- [x] Result saved to `analysis_results` (idempotent: if `(lesson_id,
  analyze_corrections)` already exists, it's shown directly without recalling the API; reprocessing
  is an explicit action that overwrites it).
- [x] A failure (network/API, parsing) doesn't break the lesson — same resilience principle already used
  in the transcription pipeline; the error is visible and recoverable, never blocking video playback.
- [x] A student utterance with an item in `corrections` shows the original passage struck through +
  the correction highlighted (prototype visual: struck-through in gray, correction in amber), inline
  in the Lesson Detail transcript.
- [x] A lesson without the task completed (not triggered, pending, or errored) shows the transcript
  normally, with no markup — same resilience principle already established in Phase 1's
  Story 6.
- [x] **Decision recorded:** after observing the result on a few real lessons, record in
  `docs/llm-analysis-notes.md` whether the task is worth keeping as-is, needs prompt refinement,
  or should be discarded — there's no fixed number of lessons, the decision is what closes the story.
- [x] `cmd/validate-analysis` removed from the repository (its role now covered by this visual validation).

### Dependencies
Story 1.

---

## Story 3 — Lesson topics

**As** a user, **I want** to see a lesson's main subjects as short topics (and be able to
correct them), **so that** I have a tag for what was discussed without rereading the whole
transcript.

The third candidate task turned into a story — chosen by the user. The first task **not
anchored to an utterance** (the result is a list of labels, not markup in the transcript), which
changes the UI (chips in the header) and the data model (topics become an editable entity, like
teachers). Technical design in its own spec.

### Acceptance criteria
- [x] Topics generated on demand (no background job), requiring `StudentSpeakerLabel` (like
      corrections).
- [x] Result persisted in `analysis_results` (idempotent) **and** `lesson_topics` (by
      `topic_id`); topics become a `topics` entity.
- [x] Topics displayed as chips in the Lesson Detail header; a lesson without the task shows the
      transcript normally.
- [x] Add/remove a topic per lesson (chips), rename a topic globally, and delete a topic
      globally (with a cascading unlink from the lessons that used it) on its own screen accessible
      from the Library (moved from Settings on 18/08/2026).
- [x] A failure doesn't break the lesson — the error is visible and recoverable.
- [x] Switching speakers preserves `analyze_topics` and `lesson_topics` (only tasks that depend on
      who is the student/tutor are discarded).
- [x] Prompt v2 with general granularity + reuse of already-existing topics; v3 adds a
      4-topics-per-lesson limit (also enforced in parsing as a safety net); v4 changes the
      output to English.

### Dependencies
Story 2.

---

## Story 4 — Filtering by topic in the Library

**As** a user, **I want** to filter the lesson list by topic, **so that** I can quickly find
lessons about a specific subject without having to open each one.

Pulled forward from Phase 3 (where only full-text search/cross-referenced tags remain) — a simple
filter over data that already exists (`topics`/`lesson_topics` from Story 3), with no dependency on
FTS5.

### Acceptance criteria
- [x] A topic filter in the Library, alongside the existing teacher and period filters
  (Phase 1's Story 5); a lesson appears if it has **any** of the selected topics (OR).
  UI: a text box with autocomplete (`datalist`) that adds removable chips, not a list
  of checkboxes — adjusted after visual feedback (see progress log).
- [x] The query combines the topic filter with the existing teacher/period filters, with no
  schema change.
- [x] A lesson with no generated topics doesn't appear while any topic filter is active.

### Dependencies
Story 3 (topics need to exist to filter by them).

---

## Candidate tasks (paused)

**Paused on 20/08/2026, at the user's request:** for now the phase continues with only `analyze_corrections`
and `analyze_topics` (refinement, not expansion). The 5 tasks below have no open story or
planned resumption date — they're recorded here only as future options, to revisit if/when the
user asks. When that happens, each one becomes a story in the same format as Story 2
(credential already resolved, on demand, minimal UI, an explicit decision recorded in
`docs/llm-analysis-notes.md`).

- `analyze_vocabulary` (new vocabulary) and `analyze_tutor_expressions` (tutor expressions) have
  a positive signal from the Phase 0 validation (single prompt) — natural candidates to come next
  if the phase is resumed.
- `analyze_tutor_taught_terms`, `analyze_tutor_feedback`, `analyze_tutor_corrections` still
  have no validation of their own.

## Promotion to a background job (a separate criterion)

Once a task is confirmed as valuable (a decision recorded in `docs/llm-analysis-notes.md`),
a small automation story is written at that point: a new job kind in the `Worker`, dependent
only on `transcribe` completing (not on the other analysis tasks), idempotent by
`(lesson_id, task)`, with Reprocess per task reusing `ResetErrorJobsForLesson` — the same
design as the Phase 1 background queue, applied per confirmed task, not in a batch.

---

## Milestones

- **M1 — "Pilot validated":** Stories 1–2. Infra closed + Student corrections running on demand
  in the UI, with a decision recorded (keep/refine/discard).
- There is no fixed M2/M3 for "complete analysis". Each confirmed task and each promotion to a
  background job advances the phase incrementally — the phase doesn't have a fixed list of
  deliverables, it ends when there are no more candidate tasks with clear value to pursue.

## Next increments (vision, no commitment)

Phase 3: automatic tags + full-text search (FTS5, with topics as one of the inputs, if
`analyze_topics` is confirmed) + Progress screen — simple topic filtering moved from here
to Phase 2 (Story 4) · Phase 5: analysis provider/model
selection, cost estimation, a possible more in-depth analysis option with `deepseek-v4-pro` per
task (see `docs/llm-analysis-notes.md`).

## Progress log

| Date | What was done | Notes |
|------|-----------------|-------------|
| 30/07/2026 | Story 1 (Tasks 1–6 of the plan) implemented: `analysis_results`/`lesson_topics` migration, a `prompts/` package with the 7 versioned files, `internal/analysis` rewritten (task-agnostic `Provider`, a `TaskDef` framework, `FormatTranscript` numbering utterances), the 7 concrete tasks with item discard on an invalid `utterance_index` (risk 2), `RegisterPrompts` wired into `main.go`, a temporary CLI `cmd/validate-analysis` created | Work paused before Task 7 (manual validation on a real lesson) to close Phase 1's Story 9 (teacher management), which was still open; `docs/llm-analysis-notes.md` still only has the Phase 0 notes (single prompt) — the 7 new tasks haven't yet been run against a real lesson, `cmd/validate-analysis` hasn't been removed yet |
| 05/08/2026 | Phase 2 replanned toward an iterative approach: instead of automating and building UI for all 7 tasks at once (the old Stories 2–5), each task now goes through its own cycle (on demand → UI → observe on real lessons → decide keep/refine/discard), and only then gains background automation. Story 1 closes with its last criterion rewritten (validation is now per task, not in batch via CLI). Story 2 becomes the Student corrections pilot, the first task to be implemented | Spec in `docs/superpowers/specs/2026-08-05-phase2-iterative-replanning-design.md`; no code changed in this entry — only planning |
| 06/08/2026 | Story 2 (Pilot: Student corrections) implemented and closed: DeepSeek credential via keyring, `AnalysisService` (`GetCorrections`/`AnalyzeCorrections`/`ReprocessCorrections`), inline correction in the Detail view (gray strikethrough + amber highlight), speaker selection in `EditLessonModal` with analysis discarded when the student is reassigned, `cmd/validate-analysis` removed | Manual verification on a real lesson: the full flow (credential → speaker selection → analysis → inline correction → reprocess → discard) worked as designed; decision recorded in `docs/llm-analysis-notes.md` — **keep the task as-is**, with prompt refinement (`prompts/analyze-corrections-v1.md`) recorded as future work: ignore repetition/hesitation in speech as not-an-error, and reduce the "not located" correction fallback in text matching |
| 18/08/2026 | Story 3 implemented: topics become a `topics` entity (migration 00006 with backfill), `lesson_topics` by `topic_id` becomes the UI's source of truth; `AnalysisService` gains Get/Analyze/ReprocessTopics (on demand, idempotent, writes to `analysis_results` + `lesson_topics`); `TopicsService` covers add/remove per lesson and global rename; prompt v2 with general granularity + reuse of existing topics (appended to the message); chips in the Detail view + a "Topics" panel in Settings; switching speakers now preserves topics (selective deletion by speaker dependency) | `go test ./...`, `go vet ./...`, `pnpm run check`/`build` confirmed clean |
| 18/08/2026 | UI adjustment (not part of a formal story): teacher and topic management move out of Settings and get their own screens (`Teachers.svelte`, `Topics.svelte`), accessible via two new buttons in the Library header ("Teachers", "Topics"); Settings goes back to containing only Storage and credentials; the new screens are mapped as `active="library"` in the Sidebar, with a "← Library" button following the same pattern as the Lesson Detail view | Frontend-only reorganization — no backend/bindings change; listing/rename logic copied as-is from `Settings.svelte`, without rewriting; `vite build` confirmed clean |
| 18/08/2026 | UI adjustment (not part of a formal story): the "Progress" tab removed from the Sidebar and from routing in `App.svelte` (a placeholder with no real content, a screen planned only for Phase 3) | `Progress.svelte` kept in the repo unused, to be reused once the screen gets real content in Phase 3; `svelte-check` confirmed clean |
| 19/08/2026 | Prompt adjustment (not part of a formal story): the `analyze_topics` task gains `prompts/analyze-topics-v3.md` (a 4-topics-per-lesson limit, also enforced as a cutoff in `parseTopics` regardless of what the LLM returns) and, next, `prompts/analyze-topics-v4.md` (output in English instead of Portuguese) | A deliberate deviation, at the user's request, from `CLAUDE.md`'s general "analysis text in PT-BR" convention — only topics now come out in English; `go build`/`go vet`/`go test ./...` confirmed clean |
| 19/08/2026 | UI adjustment (not part of a formal story): the "Topics" screen gains global topic deletion — `internal/db.DeleteTopic` deletes the entity in a transaction (unlinking `lesson_topics` first, since the FK has no `ON DELETE CASCADE` and the database runs with `foreign_keys=ON`), `TopicsService.DeleteTopic` exposes it to the frontend, a "Delete" button with `confirm()` (same pattern as `LessonDetail.svelte`) removes the topic from every lesson that used it | `go test ./...`, `go vet ./...`, `pnpm run check` confirmed clean |
| 19/08/2026 | UI adjustment (not part of a formal story, a shortcut for manual testing): a "Delete all" button on the "Topics" screen, next to the title, visible only when the list isn't empty — `internal/db.DeleteAllTopics`/`TopicsService.DeleteAllTopics` delete all of `lesson_topics` and `topics` in a transaction | Not part of normal usage, exists only to make it easier to reset data during testing; `go test ./...`, `go vet ./...`, `pnpm run check` confirmed clean |
| 19/08/2026 | Story 4 implemented: `db.LessonFilter`/`services.LessonFilter` gain `TopicIDs []int64` (OR semantics via `l.id IN (SELECT lesson_id FROM lesson_topics WHERE topic_id IN (...))`, no schema change); the Library gains a topic filter alongside the existing teacher/period filters, populated by `TopicsService.ListTopics()` | `go test ./...`, `go vet ./...`, `svelte-check`, and `vite build` confirmed clean |
| 19/08/2026 | UI adjustment (not part of a formal story, user feedback after seeing the screen): the topic filter switched from a checkbox list (took up too much space) to a text box with autocomplete (`datalist`, same pattern as `TeacherCombobox`) — typing/selecting an existing topic adds a small removable chip with an "×", the input clears for the next one | `svelte-check` and `vite build` confirmed clean |
| 19/08/2026 | **Story 3 closed.** The formal decision criterion in `docs/llm-analysis-notes.md` dropped from scope — real use already showed an acceptable result (granularity, reuse, chip editing/deletion, Library filter), the decision to keep it made without a dedicated note entry | From here on, the next candidate task (`analyze_vocabulary` or `analyze_tutor_expressions`) follows the same pattern as Story 2 whenever it's started |
| 20/08/2026 | Replanning (at the user's request): the phase continues with only `analyze_corrections` and `analyze_topics` for now — future work is refining these two, not expanding to the 5 remaining candidate tasks (`analyze_vocabulary`, `analyze_tutor_expressions`, `analyze_tutor_taught_terms`, `analyze_tutor_feedback`, `analyze_tutor_corrections`), which remain paused indefinitely | No code changed — only planning (`docs/phase-2-llm-analysis.md`) |
| 20/08/2026 | Fix (not part of a formal story, reported by the user): cmd windows opening in the background on every video import — `internal/media.ExtractAudio`/`Duration` call ffmpeg/ffprobe via `os/exec` without hiding the console, and since Wails runs without its own console on Windows, each console-subsystem process launched opens its own window | Root cause confirmed (not environmental); fixed via `SysProcAttr.HideWindow` in a dedicated `_windows.go` file (`internal/media/exec_windows.go`, with a no-op in `exec_other.go`), same pattern as platform-specific code already used in `services/move_noreplace_windows.go`; `go build`/`go vet`/`go test ./internal/media/...` confirmed clean on Linux and cross-built for `GOOS=windows` |
| 29/08/2026 | **Full switch from PT-BR to English** (at the user's request, superseding `CLAUDE.md`'s old "user-facing error messages and analysis text in PT-BR" convention — now updated): the `analyze_corrections` prompt becomes `prompts/analyze-corrections-v2.md` (English instructions and output, version bump in `tasks_corrections.go`), and `analyze_topics` becomes `prompts/analyze-topics-v5.md` (its instructions were still PT-BR even after v4 made only the topic *output* English; also fixes a stale reference to the old "Aluno:"/"Tutor:" transcript labels); `internal/analysis.FormatTranscript`'s speaker labels and the internal role keys built in `transcript.go`/`services/analysis.go` change from "Aluno"/"aluno" to "Student"/"student" (never persisted, so no migration); every Portuguese error/log string across `internal/` and `services/` (~150+ strings) translated to English, including the lesson/job status vocabulary (`"erro"/"pronta"/"processando"/"aguardando"` → `"error"/"ready"/"processing"/"waiting"` in `internal/db/lesson_status.go`'s `deriveStatus` and `services/queue.go`'s `statusLabel`) changed atomically with the matching frontend comparisons, since the frontend matches these by literal identity; the app's Wails `Name`/`Title`/`Description` in `main.go` translated too ("Assistente de Idiomas" → "Language Assistant"); all visible Svelte UI copy across the 13 screens/components translated to English, and the lesson date display switched from `DD/MM/YYYY` to a spelled-out English format (e.g. `Jul 23, 2026, 14:30`) per the user's preference, independent of the language switch | Old lessons' stored analysis results stay in PT-BR until manually reprocessed — same pattern as prior prompt version bumps, no bulk migration. Executed via 2 parallel subagents (backend Go strings, frontend Svelte UI) plus direct work on the analysis/prompt package and the shared status-vocabulary wire contract both agents correctly flagged as needing coordinated, atomic treatment; `go build`/`go vet`/`go test ./...` (with `CGO_ENABLED=1`, `go1.25.7`, `-tags gtk3` per the sandbox's Wails build requirement) and `pnpm run check`/`pnpm run build` all confirmed clean. Deliberately left untouched: the 5 paused candidate tasks' own prompt files (`analyze-vocabulary`, `analyze-tutor-*`) and their PT translation-field test fixtures (e.g. `VocabularyItem.Translation`, `TutorTaughtTerm.Translation` — a `Translation`-typed field's *purpose* is to hold a Portuguese gloss, unrelated to the app-language convention); `internal/importer/naming.go`'s accent-stripping replacer table (data, not messages); historical docs/specs; and `LessonDetail.svelte`'s internal `Role` type literals (`"aluno"/"tutor"/"neutro"`, never rendered verbatim) |
