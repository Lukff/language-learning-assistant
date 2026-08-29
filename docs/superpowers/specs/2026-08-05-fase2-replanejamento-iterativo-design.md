# Phase 2 — Replanning for an Iterative Approach: Design

> This is not an implementation story — it's a replanning of `docs/fase-2-analise-llm.md` itself.
> It defines the new structure of the phase (revised Stories 1 and 2, candidate tasks, the
> criterion for promotion to a background job, milestones) and how the existing docs change. The
> detailed technical design of the revised Story 2 (Pilot — Student corrections) is the
> responsibility of a separate implementation plan, written after this spec is approved.

## Context and motivation

Phase 2, Story 1 (infrastructure: versioned prompts, task-agnostic `Provider`,
`analysis_results`/`lesson_topics` persistence) was implemented on 30/07/2026 (see
`docs/superpowers/specs/2026-07-30-fase2-historia-1-prompts-persistencia-design.md`), but the work
was paused before the last acceptance criterion — validating the 7 tasks against a real lesson via
`cmd/validate-analysis` and recording the findings in `docs/notas-analise-llm.md` — in order to
close Phase 1's Story 9 (teacher management), which was still open.

Upon resuming, the original plan (Stories 2-5: automate the 7 tasks as background jobs, then build
the entire consumption UI — inline corrections, a full Analysis tab, topics in the Library) no
longer reflected what the dev wants to do now: implement the analyses **visually inside the app,
one at a time**, evaluating in practice whether each one is worth keeping, and refining as each is
added — instead of committing to automation and UI for all 7 tasks at once. There is an explicit
suspicion that not all 7 planned tasks should survive.

## Scope decisions

- **Story 1's infrastructure is reused, not redesigned now** — but with the explicit expectation
  of simplifying the task framework (`TaskDef`, `task[T]`) later on, once it becomes clear which
  of the 7 tasks actually stick. No simplification happens in this spec.
- **Validation now happens per task, visually in the UI — not in bulk via CLI.** Story 1's last
  criterion (running the 7 tasks via `cmd/validate-analysis` and recording everything at once in
  `docs/notas-analise-llm.md`) is replaced: each task is validated once it gets its own UI.
  `cmd/validate-analysis` is removed from the repository **without ever having run** — its
  function is taken over by the visual implementation of the first task itself.
- **First task to implement: `analyze_corrections` (Student corrections).** Chosen despite being
  the most complex to display (needs to anchor to the exact utterance via `utterance_index`,
  inline UI in the transcript) because it's the most valuable for learning — the priority
  criterion is value, not ease of implementation.
- **On demand before automation.** Every new task first goes in as a manual action (a button on
  the lesson), never as a background job. An automatic job is only written once the task is
  confirmed as valuable — this avoids spending API calls (and automation effort) on tasks that
  might be discarded.
- **The order of the 6 remaining tasks is not committed now.** They're listed as candidates in the
  phase doc, with no detailed story, no fixed order — each one becomes a real story only when its
  turn comes. `analyze_vocabulary` and `analyze_tutor_expressions` have a positive signal from
  Phase 0's validation (single prompt), which makes them natural candidates to come right after
  Corrections, but this is recorded as an observation, not a commitment to order.
- **UI placement (inline vs. its own tab) is decided task by task**, not pre-designed in bulk like
  the old Story 4 (an Analysis tab with all sections).
- **Promotion to a background job is a separate criterion**, not a pre-written story. It's written
  (as a small story) only when a specific task is confirmed — reusing the `Worker`'s existing
  pattern (new job kind, dependent only on `transcribe` having completed, idempotent by
  `(lesson_id, task)`, with Reprocess per task reusing `ResetErrorJobsForLesson`), applied per
  confirmed task instead of in bulk for all 7 at once (as in the old Story 2).

## Revised structure of Phase 2

### Story 1 — closes, with one criterion rewritten

Criteria 1-5 (prompts, `FormatTranscript`, task-agnostic `Provider`, migration, handling of an
invalid `utterance_index`) remain completed as they already were. Criterion 6 changes from
"validate the 7 tasks in bulk via CLI" to "validation happens task by task, as each one gets its
UI (starting with Story 2)" — with this, Story 1 closes in this spec, without running
`cmd/validate-analysis`.

### Story 2 (revised) — Pilot: Student corrections

Replaces the old Story 2 ("jobs for the 7 tasks in the queue"). Acceptance criteria:

- Analysis provider credential (DeepSeek) via `go-keyring` (`SaveAnalysisAPIKey`/
  `GetAnalysisAPIKey`, the same pattern as the already-existing `SaveSTTAPIKey`/`GetSTTAPIKey`) + a
  field on the Settings screen, next to the STT field — a prerequisite for any call to work
  (confirmed that no code related to `AnalysisAPIKey` exists yet in the repository).
- A manual action on the lesson (e.g., a button "Analisar correções") triggers `analyze_corrections`
  on demand — no background job in this slice.
- Result saved to `analysis_results` via `UpsertAnalysisResult` (already existing); idempotent — if
  `(lesson_id, analyze_corrections)` already exists, it's shown directly without re-calling the
  API; reprocessing is an explicit action that overwrites.
- Failure (network/API, parsing) doesn't break the lesson — the same resilience principle already
  used in the transcription pipeline (Phase 1); the error stays visible and recoverable, and never
  prevents watching the video.
- Corrections displayed inline in the lesson Detail's transcript: original passage struck through +
  correction highlighted (prototype visual: strikethrough in gray, correction in amber) — reuses
  the design already recorded in the old Story 3 of the previous plan.
- Explicit decision criterion that closes the story: after observing the result in a few real
  lessons, record in `docs/notas-analise-llm.md` whether the task is worth keeping as-is, needs
  prompt refinement, or should be discarded. There's no fixed number of lessons — the decision is
  what closes the story, not a count.

The detailed technical design of this story (binding signatures, Svelte component, button wiring,
etc.) is left for the implementation plan, written separately after this spec is approved.

### Candidate tasks (no detailed story)

`analyze_vocabulary`, `analyze_tutor_expressions`, `analyze_tutor_taught_terms`,
`analyze_tutor_feedback`, `analyze_tutor_corrections`, `analyze_topics`. Each one becomes a real
"Story N", in the same format as the revised Story 2 (credential already resolved, on demand,
minimal UI, explicit decision criterion), written when its turn to be implemented comes.

### Promotion to a background job (separate criterion)

When a task is confirmed as valuable (a decision recorded in `docs/notas-analise-llm.md`), a small
automation story is written at that moment: a new job kind in the `Worker`, dependent only on
`transcribe` having completed (not on the other analysis tasks), idempotent by
`(lesson_id, task)`, with Reprocess per task reusing `ResetErrorJobsForLesson` — the same design
that was in the old Story 2, applied per confirmed task instead of in bulk.

## Revised milestones

- **M1 — "Validated pilot":** Stories 1-2. Infrastructure closed + Student corrections running on
  demand in the UI, with a decision recorded (keep/refine/discard).
- There are no fixed M2/M3 for "complete analysis". Each confirmed task and each promotion to a
  background job advances the phase incrementally. The phase doesn't have a closed list of
  deliverables — it "ends" when there are no more candidate tasks with clear value left to pursue.

## Impact on existing docs

- **`docs/fase-2-analise-llm.md`** is rewritten to reflect this structure: goal, technical risks
  (risk 1 stops being "validate all 7 at once" and becomes "validate each task before automating";
  risk 2 is recorded as already resolved; risk 3 becomes "measure the real cost per confirmed
  task"), Story 1 closed with the rewritten criterion, Story 2 replaced by the Pilot, a
  candidate-tasks section, a promotion-to-background-job section, revised milestones, and a line
  in the progress log dated today.
- **`docs/notas-analise-llm.md`** doesn't change in this spec — it starts receiving one entry per
  confirmed/discarded task as each one is validated in the UI, starting with Student Corrections
  once the revised Story 2 is implemented.
- **`cmd/validate-analysis`** (removal) is a code change, not a doc change — recorded as an item of
  the revised Story 2's implementation plan, not carried out by this spec.

## Out of scope for this spec

- Detailed technical design of the revised Story 2 (Svelte components, Wails bindings, call
  schema) — a separate implementation plan.
- Any real simplification of Story 1's task framework (`TaskDef`/`task[T]`) — remains a recorded
  expectation, not carried out now.
- Writing detailed stories for the 6 candidate tasks — each one when its turn comes.
- Removing `cmd/validate-analysis` from the repository — done in the implementation plan, not here.
