# Phase 2, Story 2 — Pilot: Student Corrections: design

> Covers the revised Story 2 from `docs/fase-2-analise-llm.md` (the `analyze_corrections` pilot).
> Depends on Story 1 (already closed — prompts, agnostic `Provider`, persistence in
> `analysis_results`). Scope: DeepSeek credential, on-demand triggering, inline display in the
> lesson Detail screen, and the UX adjustment that became necessary once analyses got tied to a
> speaker mapping (changing the speaker is no longer a loose toggle and now lives inside lesson
> editing, with a discard warning). Does **not** include a background job, nor the other 6
> candidate tasks — those wait until `analyze_corrections` is confirmed
> (`docs/notas-analise-llm.md`) and until each candidate task's turn comes.

## Context and motivation

Story 1 finished the infrastructure (7 versioned prompts, task-agnostic `Provider`,
`analysis_results`/`lesson_topics`) but nothing in the app calls it yet — only
`cmd/validate-analysis`, which never actually ran (see the replanning in
`docs/superpowers/specs/2026-08-05-fase2-replanejamento-iterativo-design.md`). This spec delivers
the first analysis task actually visible in the UI: `analyze_corrections`, chosen for its
pedagogical value despite being the most complex to display (it needs to anchor to the exact
utterance via `utterance_index`, and highlight only the wrong span within the speech, not the
whole utterance).

While designing the flow, a problem not anticipated in the replanning spec came up:
`analyze_corrections` only makes sense once the app knows who the Student is and who the Tutor is
(`lesson.StudentSpeakerLabel`, chosen in Phase 1/Story 6) — that mapping defines the
`speakerRoles` sent to `FormatTranscript`. If the user changes that mapping after an analysis has
already been saved, the old analysis ends up pointing at the wrong speech (the Student/Tutor label
the model saw no longer matches the current toggle). This spec also resolves that: the speaker
choice stops being an always-visible toggle in the transcript panel and moves into the lesson
editing flow, with an explicit warning that changing it discards analyses already done.

## Scope decisions

- **Matching the wrong span runs in the backend (Go), not the frontend.** The `original` field of
  each `Correction` is a snippet of the speech (not the whole utterance) copied by the model — it
  may not match the displayed text 100% (capitalization, punctuation, whitespace). Solving this in
  Go allows table-driven tests (the pattern already used in `internal/analysis`); the frontend has
  no test runner configured today, so string logic there would go without automated coverage.
- **Without a match, the correction doesn't disappear.** If the span isn't located in the speech
  (even normalized), the speech shows with no strikethrough and a short note below shows
  original → correction + explanation — a correction the model gave is never silently discarded.
- **On demand, three explicit actions.** `GetCorrections` (read, no API cost),
  `AnalyzeCorrections` (first time) and `ReprocessCorrections` (overwrite) are distinct methods in
  the binding — no hidden boolean flag (`reprocess bool`) mixing both cases into the same
  signature.
- **A single button that changes label and behavior.** Same pattern as the "Reprocess" button
  already used in the transcript error state (Phase 1): "Analyze corrections" when there's no
  result yet, "Reprocess corrections" when there is — clicking reprocess always goes through a
  confirmation (it overwrites and costs a new API call).
- **The correction's explanation only shows on hover** (native `title` over the amber span) —
  keeps the transcript visually clean; the explanation of why the mistake happened isn't the
  primary data for a quick read.
- **The speaker choice moves out of the transcript panel and into `EditLessonModal`.** Once
  `lesson.StudentSpeakerLabel` is set, the toggle no longer appears loose — only inside "Edit
  lesson". With exactly 2 speakers (the normal case for a 1-on-1 lesson), the UI becomes a single
  "Swap speakers" button; with 3+ (noisy diarization, a rare case already supported today by
  `neutralLabel`), the existing individual "Speaker X is you" buttons remain.
- **Changing the speaker after an analysis is already saved asks for confirmation and discards
  everything.** It isn't a selective per-task swap — every `analysis_results` row for that lesson
  is deleted (the table is generic by `(lesson_id, task)`, so this already holds for future tasks
  without needing to revisit this decision). On the first choice (`StudentSpeakerLabel` still
  null) there's nothing to discard, so there's no warning.
- **`cmd/validate-analysis` is removed in this story**, without ever having run — its role is taken
  over by Story 2's own visual validation (already decided in the replanning).

## Architecture

```
internal/config/
  credentials.go          # + SaveAnalysisAPIKey/GetAnalysisAPIKey (keyringUserDeepSeek)

internal/analysis/
  analysis.go              # Provider gains Model() string
  openai_compatible.go     # openAICompatibleProvider.Model() — returns p.model
  corrections_display.go   # NEW: CorrectionDisplay, MatchCorrections (matching + split)

internal/db/
  analysis_results.go      # + DeleteAnalysisResultsForLesson
  migrations/00004_analysis_results.sql   # unchanged (schema already supports what's missing)

services/
  analysis.go              # NEW: AnalysisService (GetCorrections/AnalyzeCorrections/ReprocessCorrections)
  settings.go              # + HasAnalysisCredential/SaveAnalysisAPIKey
  library.go                # SetStudentSpeaker: deletes analysis_results before writing the new label

main.go                    # + AnalysisService in app.Services; analysisProviderFactory

cmd/validate-analysis/     # REMOVED

frontend/src/lib/
  EditLessonModal.svelte    # + "Who are you" section (swap / individual buttons)
  screens/Settings.svelte   # + "Analysis provider credential" section
  screens/LessonDetail.svelte  # speaker toggle moves out of here; + Analyze/Reprocess button + inline
  bindings/.../analysisservice.ts   # generated by wails3 (AnalysisService)
```

### `internal/config/credentials.go`

```go
const keyringUserDeepSeek = "deepseek"

func SaveAnalysisAPIKey(apiKey string) error   // mirrors SaveSTTAPIKey
func GetAnalysisAPIKey() (string, error)       // mirrors GetSTTAPIKey
```

### `internal/analysis` — `Provider.Model()` and matching

`Provider` gains a third method, only to let the caller record which model produced the result
(the `analysis_results.model` column) without needing to know each implementation's details:

```go
type Provider interface {
    Name() string
    Model() string
    Complete(ctx context.Context, systemPrompt, transcript string) (json.RawMessage, error)
}
```

`openAICompatibleProvider.Model() string { return p.model }` — already holds `model` since
construction (`"deepseek-v4-flash"`), it just needed exposing.

New file `corrections_display.go`:

```go
// CorrectionDisplay is a Correction already prepared for the frontend to render:
// the wrong span (Wrong) already split from the rest of the speech (Before/After) via
// normalized matching against the utterance's real text. Wrong == "" when
// the span wasn't located — the frontend shows the correction as a standalone
// note in that case, never discards it.
type CorrectionDisplay struct {
    UtteranceIndex          int    `json:"utteranceIndex"`
    Before, Wrong, After    string `json:"before"` // full json tags in the implementation
    Correction, Explanation string `json:"correction"`
}

// MatchCorrections matches each Correction against the corresponding
// utterance's text (utterances[c.UtteranceIdx]), normalizing (case-insensitive,
// spaces collapsed) before comparing. Uses the first occurrence when
// original appears more than once in the speech. utterances and corrections
// already arrived with utterance_index validated (filterAnchored, Story 1) — an
// out-of-range index here would be a caller bug, not a path to handle
// gracefully again.
func MatchCorrections(utterances []stt.Utterance, corrections []Correction) []CorrectionDisplay
```

### `internal/db/analysis_results.go`

```go
// DeleteAnalysisResultsForLesson deletes every analysis already done for lessonID
// (all tasks) — called when the student/tutor mapping changes, since
// any analysis anchored on utterance_index ends up pointing at the wrong
// role as soon as the Student/Tutor labels swap speakers.
func DeleteAnalysisResultsForLesson(conn *sql.DB, lessonID int64) error
```

### `services/analysis.go` (new)

```go
type AnalysisService struct {
    conn            *sql.DB
    storageRoot     func() (string, error)
    providerFactory func() (analysis.Provider, error)
}

func NewAnalysisService(conn *sql.DB, storageRoot func() (string, error), providerFactory func() (analysis.Provider, error)) *AnalysisService

type CorrectionsResult struct {
    Items []analysis.CorrectionDisplay `json:"items"`
}

// GetCorrections returns the already-saved result, or nil if analyze_corrections
// has never run for this lesson — doesn't call the API. Internally: looks up
// analysis_results; if not found, returns (nil, nil); if found, looks up the
// transcript (db.FindTranscriptByLessonID), decodes result_json into
// []analysis.Correction and runs MatchCorrections before returning — the
// saved result is always re-matched against the transcript's current text,
// never cached already "flattened".
func (s *AnalysisService) GetCorrections(lessonID int64) (*CorrectionsResult, error)

// AnalyzeCorrections runs analyze_corrections if there's no saved result yet;
// if there already is one, returns the existing one without calling the API
// again (idempotent).
func (s *AnalysisService) AnalyzeCorrections(lessonID int64) (CorrectionsResult, error)

// ReprocessCorrections runs analyze_corrections and overwrites the existing
// result, even if there already is one — an explicit action, never automatic.
func (s *AnalysisService) ReprocessCorrections(lessonID int64) (CorrectionsResult, error)
```

`internal/analysis` gains two exports so the `services` package doesn't duplicate the schema:
`newCorrectionsTask` becomes `NewCorrectionsTask() TaskDef` (just capitalized — follows the same
shape as the other task constructors), and a new `ParseCorrectionsResult(resultJSON json.RawMessage)
([]Correction, error)` (decodes the already-persisted `result_json` back into `[]Correction`) —
reused by every path that needs to reconstruct `CorrectionDisplay` from what's saved in the
database (`GetCorrections` and the idempotent shortcut in `runCorrections`, below), instead of each
one doing its own `json.Unmarshal`.

`AnalyzeCorrections`/`ReprocessCorrections` share an internal `runCorrections(lessonID, overwrite
bool)`:

1. Looks up `lesson` (`db.FindLessonByID`); a clear error if `StudentSpeakerLabel == nil`
   ("choose who you are in the lesson before analyzing" — a user-facing message, in PT-BR).
2. Looks up the transcript (`db.FindTranscriptByLessonID`, already existing from Phase 1) —
   needed both by the idempotent shortcut (step 3) and by the path that calls the provider (step 4
   onward).
3. If `!overwrite`: `db.FindAnalysisResult(conn, lessonID, "analyze_corrections")`; if it exists,
   `analysis.ParseCorrectionsResult(result_json)` + `analysis.MatchCorrections(utterances,
   corrections)` and returns directly — without touching the provider.
4. Builds `speakerRoles` from `StudentSpeakerLabel` (that speaker → `"aluno"`, any other →
   `"tutor"`), calls `analysis.FormatTranscript`.
5. `provider, err := s.providerFactory()` — an error here already covers missing
   credential/unavailable keyring (same diagnostic message used in `HasSTTCredential`).
6. `resultJSON, raw, err := analysis.NewCorrectionsTask().Execute(ctx, provider, transcript,
   len(utterances))`.
7. Writes `raw` to disk **even if `err != nil`** (`rawJSONRelPath`-like: same directory as the
   video, `<basename>.analysis.analyze_corrections.json`, relative to `storageRoot`) — the call
   already cost money. If `err != nil`, returns the error (doesn't advance to the following steps).
8. `db.UpsertPrompt(conn, "analyze_corrections", version, prompt)` to get the `prompt_id` (same
   idempotent call already used in `RegisterPrompts` — reused here, not a new query).
9. `db.UpsertAnalysisResult(conn, lessonID, "analyze_corrections", promptID, provider.Model(),
   resultJSON, rawRelPath)`.
10. `analysis.ParseCorrectionsResult(resultJSON)` + `analysis.MatchCorrections(utterances,
    corrections)`, returns `CorrectionsResult{Items: ...}`.

### `services/settings.go`

```go
func (s *SettingsService) HasAnalysisCredential() (bool, error)   // mirrors HasSTTCredential
func (s *SettingsService) SaveAnalysisAPIKey(apiKey string) error // mirrors SaveSTTAPIKey
```

### `services/library.go` — `SetStudentSpeaker`

Now deletes existing analyses before writing the new label, only when there's an actual change
(not on the first choice):

```go
func (s *LibraryService) SetStudentSpeaker(lessonID int64, speakerLabel string) error {
    lesson, err := db.FindLessonByID(s.conn, lessonID)
    ...
    if lesson.StudentSpeakerLabel != nil && *lesson.StudentSpeakerLabel != speakerLabel {
        if err := db.DeleteAnalysisResultsForLesson(s.conn, lessonID); err != nil {
            return err
        }
    }
    // saves speakerLabel as it already does today
}
```

The confirmation warning ("changing this discards analyses already done") is the frontend's
responsibility, before calling `SetStudentSpeaker` — the backend just executes; there's no second
parameter like `confirm bool`, the confirmation is entirely a UI decision about whether to call the
binding at all.

### `main.go`

```go
analysisProviderFactory := func() (analysis.Provider, error) {
    apiKey, err := config.GetAnalysisAPIKey()
    if err != nil {
        return nil, err
    }
    return analysis.NewDeepSeekProvider(apiKey)
}
...
Services: []application.Service{
    ...
    application.NewService(services.NewAnalysisService(conn, storageRoot, analysisProviderFactory)),
},
```

Resolved per call (not at boot), same reason as the already-existing `sttFactory`: the credential
may not exist yet in the app's first session.

### `frontend/src/lib/EditLessonModal.svelte`

New props: `speakerOptions: string[]` (first-speech order, the same list that today is
`speakerOrder` in `LessonDetail.svelte`), `currentStudentSpeaker: string | null`,
`hasAnalysisResults: boolean` (computed by the caller: `!!corrections` already loaded). New section
in the modal, between "Tutor" and the action buttons:

```
Who are you
  [if speakerOptions.length === 2]:  [Swap speakers]
  [otherwise]:                       [Speaker A is you] [Speaker B is you] [Speaker C is you] ...

  (on click, if currentStudentSpeaker != null and the new value differs from the current one):
    confirm("Changing who you are discards analyses already done for this lesson — you'll need to
             reprocess. Continue?")
    — only calls LibraryService.SetStudentSpeaker if confirmed
```

No warning when `currentStudentSpeaker == null` (first choice). After saving, `onSaved()` already
reloads the `lesson` (existing pattern) — `LessonDetail` refetches `GetCorrections` in the same
`onLessonSaved` (see below), since the result may have been deleted.

### `frontend/src/lib/screens/LessonDetail.svelte`

- `speakerOrder`/`neutralLabel`/`chooseStudentSpeaker`/the `.speaker-toggle` block in the
  transcript panel **move out** of here — they become props passed to `EditLessonModal`
  (`speakerOrder` is already `$derived.by`, it just also gets read by the modal now).
- New state: `corrections: CorrectionDisplay[] | null`, `loadingCorrections`,
  `analyzingCorrections`, `correctionsError`.
- `fetchCorrectionsIfReady()` (called in `onMount`, alongside `fetchTranscriptIfReady`, and again
  in `onLessonSaved`): if `lesson.status !== "pronta"` or `!lesson.studentSpeakerLabel`,
  `corrections = null`; otherwise `AnalysisService.GetCorrections(lessonId)`.
- Button, visible only when `lesson.studentSpeakerLabel` is set and the transcript is ready:
  - `corrections == null` → `"Analyze corrections"`, calls `AnalyzeCorrections`.
  - `corrections != null` → `"Reprocess corrections"`; `onclick` opens `confirm("This overwrites
    the current analysis and makes a new API call. Continue?")`; if confirmed, calls
    `ReprocessCorrections`.
  - Loading state: `"Analyzing…"` / `"Reprocessing…"`, disabled during the call.
  - Errors go to `correctionsError` (not `actionError` nor `lessonError`) — only this section shows
    the problem, video and transcript stay intact.
- Rendering each student `utterance`: looks up `corrections?.find(c => c.utteranceIndex ===
  i)`; if found and `Wrong !== ""`, renders `Before` + `<span style="text-decoration:
  line-through; color: mut">{Wrong}</span>` + `<span style="color: amber; font-weight: 600"
  title={Explanation}>{Correction}</span>` + `After`; if found and `Wrong === ""`, renders the
  speech normally and, below, `⚠ correção não localizada: "{original}" → "{Correction}" — {Explanation}`
  (`mut` color); with no correction for that utterance, renders normally (no markup at all) — same
  resilience principle already used for lessons without a ready transcript.

### `frontend/src/lib/screens/Settings.svelte`

Second credential section, same visual pattern as the existing one:

```
Analysis provider credential
  {hasAnalysisCredential ? "Credential configured" : "No credential configured"}
  [password input] [Save]
```

## Data flow

```
LessonDetail.svelte (onMount / onLessonSaved)
  GetCorrections(lessonId) ──► AnalysisService.GetCorrections
                                  db.FindAnalysisResult(lessonID, "analyze_corrections")
                                  nil ──► null to the frontend ("Analyze corrections" button)
                                  found ──► ParseCorrectionsResult + MatchCorrections ──► CorrectionsResult

[user clicks "Analyze corrections"]
  AnalyzeCorrections(lessonId) ──► AnalysisService.runCorrections(lessonID, overwrite=false)
    FindLessonByID + checks StudentSpeakerLabel
    FindTranscriptByLessonID
    already exists (FindAnalysisResult)? ──► ParseCorrectionsResult + MatchCorrections ──► CorrectionsResult
                                          (without calling the provider)
    doesn't exist:
      FormatTranscript(utterances, speakerRoles)
      providerFactory() ──► config.GetAnalysisAPIKey + NewDeepSeekProvider
      analysis.NewCorrectionsTask().Execute(ctx, provider, transcript, len(utterances))
        raw always written to disk
        err == nil:
          UpsertPrompt (idempotent) ──► promptID
          UpsertAnalysisResult(lessonID, "analyze_corrections", promptID, provider.Model(), ...)
          ParseCorrectionsResult + MatchCorrections(utterances, corrections) ──► CorrectionsResult
        err != nil: returns error to the frontend (correctionsError), raw was already saved to disk

[user changes speaker in "Edit lesson"]
  EditLessonModal: currentStudentSpeaker != null and it changed?
    confirm() ──► canceled: nothing happens
              ──► confirmed: SetStudentSpeaker(lessonId, newLabel)
                    LibraryService.SetStudentSpeaker:
                      did the label actually change? ──► DeleteAnalysisResultsForLesson(lessonID)
                      saves the new studentSpeakerLabel
  onSaved() ──► LessonDetail reloads lesson + GetCorrections (now null again)
```

## Error handling

- **Missing credential / unavailable keyring**: `providerFactory()` returns an error before any
  network call; the message reuses the text already used in `HasSTTCredential`
  ("verifique se o gnome-keyring/kwallet está rodando"). Shows up in `correctionsError`, never
  blocks video/transcript.
- **`StudentSpeakerLabel` null**: clear error before calling the provider — in practice this
  should never happen through the UI (the button only shows up with the label set), but the
  backend validates it anyway (defense against a direct call to the binding).
- **Provider network/API error** (`provider.Complete` fails): same principle as transcription —
  `raw` (whatever was read) is written to disk regardless; the error bubbles up to
  `correctionsError`, recoverable by clicking "Analyze corrections" again (idempotent: it didn't
  find a saved result, tries again).
- **Malformed JSON/unexpected schema** (`parse` fails inside `Execute`): same handling — `raw`
  preserved, the error describes the task and the cause.
- **`original` not located in the speech** (`MatchCorrections`): not an error — `Wrong == ""`, the
  correction shows as a standalone note, never discarded.
- **Failure writing `raw` to disk** (permissions, disk full): error bubbles up to
  `correctionsError` before even attempting to persist to `analysis_results` — no inconsistent
  partial write (a result with no matching raw file).
- **Speaker change with existing analyses, user cancels the confirmation**: nothing happens —
  neither `SetStudentSpeaker` nor `DeleteAnalysisResultsForLesson` gets called.

## Out of scope for this story

- A background job for `analyze_corrections` — only once the task is confirmed in
  `docs/notas-analise-llm.md` (the "Promotion to background job" criterion from the phase doc).
- The other 6 candidate tasks (`analyze_vocabulary` etc.) — each becomes its own story when its
  turn comes.
- Manual editing of corrections by the user, an error taxonomy, any aggregation across lessons
  (Progress, Phase 3).
- Measuring/displaying cost per call in the UI — Phase 5. The `usage` already logged by
  `openai_compatible.go` (`slog.Info`) is enough for this story's manual validation.
- Changing `speakerOrder`/`neutralLabel` behavior beyond moving where they're displayed — the
  labeling logic (Speaker A/B/C...) doesn't change.

## Tests

**`internal/analysis`** (`corrections_display_test.go`, new, synthetic fixtures):
- `MatchCorrections`: exact match; capitalization difference; punctuation/whitespace difference;
  `original` not found (`Wrong == ""`, `Before`/`After` empty); `original` appearing more than
  once in the speech (uses the first occurrence); multiple corrections in the same utterance
  (distinct indices).
- `openAICompatibleProvider.Model()`: returns the model configured at construction.

**`internal/db`** (`analysis_results_test.go`, extended):
- `DeleteAnalysisResultsForLesson`: deletes every row for a lesson (multiple tasks) and doesn't
  touch rows for another lesson.

**`services`** (`analysis_test.go`, new, `Provider` faked and controlled by the test):
- `GetCorrections` with no saved result returns `nil, nil` without calling the fake provider.
- `AnalyzeCorrections` called twice: the second call doesn't invoke the fake provider again
  (idempotent).
- `ReprocessCorrections` always invokes the fake provider, even with an existing result, and
  overwrites `result_json`/`model`/`raw_response_path`.
- `AnalyzeCorrections` with no `StudentSpeakerLabel`: error before touching the fake provider.
- Fake provider error: `raw` (if any) is written to disk regardless; `analysis_results` doesn't
  get a new row.
- `SetStudentSpeaker` (`library_test.go`, extended): changing the label with an existing analysis
  deletes `analysis_results`; the first assignment (`nil` → something) deletes nothing (there was
  nothing to delete); setting the same label that's already current doesn't trigger
  `DeleteAnalysisResultsForLesson`.

**Manual validation** (the criterion that closes the story, not part of the implementation plan):
run `AnalyzeCorrections` against a few real lessons through the UI, observe the quality of the
corrections and the matching, record the decision (keep/refine/discard) in
`docs/notas-analise-llm.md`.

**Frontend**: no test runner configured in the project — verifying this slice is manual, testing
the full flow (`wails3 dev`) with a real lesson: choose the speaker, analyze, see the inline
markup, change the speaker and confirm the warning appears and the analysis disappears,
reprocess.

## Acceptance criteria (from `docs/fase-2-analise-llm.md`, Story 2)

- [ ] Analysis provider credential (DeepSeek) via `go-keyring` + a field in Settings.
- [ ] A manual action on the lesson triggers `analyze_corrections` on demand — no background job.
- [ ] Result saved to `analysis_results`, idempotent; reprocessing is an explicit action that
      overwrites.
- [ ] Failure (network/API, parsing) doesn't break the lesson — visible and recoverable error,
      never blocks watching the video.
- [ ] A student utterance with a correction shows the original span struck through + the
      correction highlighted (strikethrough in gray, correction in amber), inline in the Detail
      screen's transcript.
- [ ] A lesson without the task completed shows the transcript normally, with no markup.
- [ ] Decision recorded in `docs/notas-analise-llm.md` after observing real lessons
      (keep/refine/discard) — closes the story.
- [ ] `cmd/validate-analysis` removed from the repository.

Additional criteria introduced by this spec (a consequence of the design, not of the phase doc):
- [ ] The speaker choice is only editable inside "Edit lesson"; the loose toggle in the transcript
      panel is removed.
- [ ] Changing the speaker with an existing analysis asks for confirmation and deletes the
      lesson's `analysis_results` before writing the new label.
