# Phase 2, Story 1 — Per-task prompts and analysis persistence: design

> Covers all of Story 1 (`docs/fase-2-analise-llm.md`). Scope: prompts + task-agnostic `Provider`
> + persistence (`analysis_results`/`lesson_topics`) + manual validation on a real lesson.
> **Does not** include wiring the tasks to the `Worker`/queue (Story 2) or any consuming UI
> (Stories 3-5) — that's the explicit responsibility of the following stories.

## Context and motivation

`internal/analysis` has existed since Phase 0, but with a "one call, three categories" design
(`Provider.Analyze` returning `Result{Corrections, Vocabulary, TutorExpressions}`) validated once
against a real lesson (see `docs/notas-analise-llm.md`). No code outside the package itself calls
this today — the one historical caller (`cmd/spike`) was removed when Phase 0 closed, and the
`Worker` (`internal/jobs`) only knows `extract_audio`/`transcribe`.

Phase 2 breaks the analysis into 7 independent tasks (one per prompt, one job per task in Story
2), a decision already recorded in `docs/fase-2-analise-llm.md`: easier to refine prompt by
prompt, and a failure in one task doesn't bring down the others. This story prepares prompts and
storage; there's no worker or UI consuming the result yet — the "done" criterion is validating the
7 tasks manually against a real lesson and persisting the result in an isolated test, not in real
use.

## Scope decisions

- **`Provider` becomes task-agnostic.** It no longer knows about `Correction`/`VocabularyItem`/
  etc.; each task defines its own output type and does its own parsing from the raw JSON.
- **An invalid utterance index is silently discarded** (with a log): an item from an anchored
  task (`corrections`, `tutor_corrections`, `tutor_feedback`) whose `utterance_index` is out of
  range, or missing, is removed from the result before persisting — it never reaches
  `result_json`. This guarantees that anything a future UI (Stories 3/4) reads already has a valid
  anchor, with no exception to handle there.
- **The manual-validation harness is a temporary CLI** (`cmd/validate-analysis`), the same
  pattern as Phase 0's `cmd/spike`: it fulfills the role of validating the 7 tasks against a real
  lesson, the findings go into `docs/notas-analise-llm.md`, and the CLI is removed — it doesn't
  stay as a permanent tool in the repository (the automated suite in
  `internal/analysis`/`internal/db` is what remains).
- **`analyze-v1.md` is removed.** Phase 0's single prompt, with no caller since `cmd/spike` was
  removed; replaced by the 7 new prompts.
- **Prompts are registered at app startup** (`main.go`, right after `db.Open`), not on demand —
  the same spirit as the migrations, which already run at that same point.
- **Reprocessing (Story 2, outside this slice) overwrites the existing row** — that's why
  `analysis_results` is born with `UNIQUE(lesson_id, task)` and the insert is already an upsert.

## Architecture

```
prompts/
  embed.go                          # NEW: package prompts; //go:embed *.md; var FS embed.FS
  analyze-v1.md                     # REMOVED
  analyze-corrections-v1.md         # NEW
  analyze-vocabulary-v1.md          # NEW
  analyze-tutor-expressions-v1.md   # NEW
  analyze-tutor-taught-terms-v1.md  # NEW
  analyze-tutor-feedback-v1.md      # NEW
  analyze-tutor-corrections-v1.md   # NEW
  analyze-topics-v1.md              # NEW

internal/analysis/
  analysis.go            # task-agnostic Provider; old Result/analysisJSON removed
  openai_compatible.go    # Complete(ctx, systemPrompt, transcript) — drops the fixed systemPrompt field
  parsing.go              # just the shared code-fence-strip helper + generic json.Unmarshal
  task.go                 # NEW: TaskDef, task[T], filterAnchored[T anchored], mustLoadPrompt, var Tasks
  tasks_corrections.go    # NEW: Correction, parseCorrections (anchored)
  tasks_vocabulary.go     # NEW: VocabularyItem, parseVocabulary (not anchored)
  tasks_tutor_expressions.go     # NEW
  tasks_tutor_taught_terms.go    # NEW
  tasks_tutor_feedback.go        # NEW (anchored)
  tasks_tutor_corrections.go     # NEW (anchored)
  tasks_topics.go                # NEW
  prompts.go              # NEW: RegisterPrompts(conn *sql.DB) error
  transcript.go            # FormatTranscript now numbers each utterance; SpeakerExamples unchanged

internal/db/
  migrations/00004_analysis_results.sql   # NEW
  analysis_results.go                     # NEW: UpsertPrompt, UpsertAnalysisResult, FindAnalysisResult, ReplaceLessonTopics

cmd/validate-analysis/main.go   # NEW, temporary (removed when the story closes)

main.go   # + analysis.RegisterPrompts(conn) right after db.Open
```

### `prompts/embed.go` (new)

`prompts/` remains the top-level directory documented in `CLAUDE.md` (where the `.md` files are
read and reviewed by humans) — but `//go:embed` doesn't accept `..` in its pattern, so a package
under `internal/analysis` can't embed files from outside its own tree. Solution: `prompts/` also
becomes a minimal Go package, whose only role is to embed the very `.md` files that already live
there:

```go
// prompts/embed.go
package prompts

import "embed"

//go:embed *.md
var FS embed.FS
```

`internal/analysis` imports `assistente-idiomas/prompts` and reads each file via `prompts.FS`
(the `mustLoadPrompt` helper, see `task.go` below) — no other package needs to know that
`prompts/` is now also Go code, and the `.md` content stays identical to the format already used
by `analyze-v1.md`.

### `internal/analysis/analysis.go`

```go
type Provider interface {
    Name() string
    Complete(ctx context.Context, systemPrompt, transcript string) (json.RawMessage, error)
}
```

`Result`, `Correction`/`VocabularyItem`/`Expression` (the old, non-anchored versions), and
`analysisJSON` are removed from here — each output type now lives in its own task's file.

### `internal/analysis/openai_compatible.go`

- `newOpenAICompatibleProvider` drops the `systemPrompt` parameter/field (no longer fixed at
  construction time).
- `NewDeepSeekProvider(apiKey string) (Provider, error)` — signature drops `systemPrompt`.
- `Complete(ctx, systemPrompt, transcript)` builds the `system` message with the `systemPrompt`
  received per call; all the prefill logic (```` ```json ```` + `stop`) stays the same — it's
  schema-independent, it just forces the content to start as JSON.
- `do`/envelope/choice-parsing stay the same; the only change is that the choice's content is now
  returned as raw `json.RawMessage` (after the code-fence strip), without trying to map it to a
  common domain.

### `internal/analysis/transcript.go` (`FormatTranscript` numbers the utterances)

Today `FormatTranscript` generates `"Aluno: ...\n"`/`"Tutor: ...\n"` with no numbering — nothing
anchors a correction to a specific utterance. It now prefixes each line with the index (0-based,
same order as `utterances`, the same one `utteranceCount` uses in `filterAnchored`):

```go
fmt.Fprintf(&b, "[%d] %s: %s\n", i, label, u.Text)
```

The 7 prompts (next section) instruct the model to cite that same number in `utterance_index` on
the anchored tasks — e.g.: "each utterance in the transcript is numbered as `[N] Aluno:`/`[N]
Tutor:`; when referencing a specific utterance, use that N in `utterance_index`". `SpeakerExamples`
doesn't change (it doesn't deal with the formatted text, only with the raw `utterances`).

### `internal/analysis/task.go` (new)

```go
// TaskDef is the common interface for the 7 analysis tasks — it lets them
// all be iterated over a single list (var Tasks) even though each one has
// a different result type (generics don't allow a slice of task[T] with a
// variable T, hence this non-generic interface on top).
type TaskDef interface {
    Name() string    // e.g.: "analyze_corrections" — same value stored in prompts.name and analysis_results.task
    Version() int    // prompt version (manual bump in code when the .md content changes)
    Prompt() string  // prompt content (embed.FS)

    // Execute calls provider.Complete, parses it, and (when the task is
    // anchored) discards items with an invalid utterance_index. Returns the
    // already-validated JSON (ready to store in
    // analysis_results.result_json) and the provider's raw envelope (ready
    // to write to disk/raw_response_path). err != nil doesn't stop the
    // caller from writing raw to disk (same principle as runTranscribe:
    // the call already cost money).
    Execute(ctx context.Context, provider Provider, transcript string, utteranceCount int) (resultJSON json.RawMessage, raw json.RawMessage, err error)
}

type task[T any] struct {
    name    string
    version int
    prompt  string
    parse   func(raw json.RawMessage, utteranceCount int) (T, error)
}

func (t task[T]) Name() string    { return t.name }
func (t task[T]) Version() int    { return t.version }
func (t task[T]) Prompt() string  { return t.prompt }

func (t task[T]) Execute(ctx context.Context, provider Provider, transcript string, utteranceCount int) (json.RawMessage, json.RawMessage, error) {
    raw, err := provider.Complete(ctx, t.prompt, transcript)
    if err != nil {
        return nil, raw, fmt.Errorf("analysis: tarefa %s: %w", t.name, err)
    }
    parsed, err := t.parse(raw, utteranceCount)
    if err != nil {
        return nil, raw, fmt.Errorf("analysis: tarefa %s: parsear: %w", t.name, err)
    }
    resultJSON, err := json.Marshal(parsed)
    if err != nil {
        return nil, raw, fmt.Errorf("analysis: tarefa %s: serializar resultado: %w", t.name, err)
    }
    return resultJSON, raw, nil
}

// anchored is implemented by item types whose parsing references a
// specific utterance in the transcript (Correction, TutorCorrection,
// TutorFeedbackItem) — the conventional "-1" for UtteranceIndex represents
// "absent from the model's JSON", treated the same as an out-of-range
// index.
type anchored interface {
    UtteranceIndex() int
}

// filterAnchored discards (also returning the discarded count, for
// logging) items whose UtteranceIndex doesn't fall within [0, utteranceCount).
func filterAnchored[T anchored](items []T, utteranceCount int) (kept []T, discarded int) {
    kept = items[:0]
    for _, it := range items {
        idx := it.UtteranceIndex()
        if idx < 0 || idx >= utteranceCount {
            discarded++
            continue
        }
        kept = append(kept, it)
    }
    return kept, discarded
}

// mustLoadPrompt reads a prompt embedded in prompts.FS (see
// prompts/embed.go) — panicking when it's missing is intentional: a
// missing prompt is a build/packaging error, not a runtime condition to
// handle gracefully (same spirit as a template.Must).
func mustLoadPrompt(filename string) string {
    b, err := prompts.FS.ReadFile(filename)
    if err != nil {
        panic(fmt.Sprintf("analysis: prompt %s não encontrado: %v", filename, err))
    }
    return string(b)
}

// Tasks lists the 7 analysis tasks, in the order the prompts were defined
// — order doesn't matter for execution (they're independent of each
// other), only for human readability and for RegisterPrompts.
var Tasks = []TaskDef{
    newCorrectionsTask(),
    newVocabularyTask(),
    newTutorExpressionsTask(),
    newTutorTaughtTermsTask(),
    newTutorFeedbackTask(),
    newTutorCorrectionsTask(),
    newTopicsTask(),
}
```

Each `tasks_*.go` follows the same format; example (`tasks_corrections.go`):

```go
type Correction struct {
    UtteranceIdx int    `json:"utterance_index"`
    Original     string `json:"original"`
    CorrectionTx string `json:"correction"`
    Explanation  string `json:"explanation"`
}

func (c Correction) UtteranceIndex() int { return c.UtteranceIdx }

func parseCorrections(raw json.RawMessage, utteranceCount int) ([]Correction, error) {
    var parsed struct {
        Corrections []Correction `json:"corrections"`
    }
    if err := unmarshalJSON(raw, &parsed); err != nil {
        return nil, err
    }
    kept, discarded := filterAnchored(parsed.Corrections, utteranceCount)
    if discarded > 0 {
        slog.Warn("analysis: itens descartados por utterance_index inválido", "tarefa", "analyze_corrections", "descartados", discarded)
    }
    return kept, nil
}

func newCorrectionsTask() TaskDef {
    return task[[]Correction]{name: "analyze_corrections", version: 1, prompt: mustLoadPrompt("analyze-corrections-v1.md"), parse: parseCorrections}
}
```

`unmarshalJSON` (moved to `parsing.go`) is just the generic `stripTrailingCodeFence` +
`json.Unmarshal` that today lives in `parseAnalysisResponse` — it becomes the only code truly
shared across the 7 tasks (the schema parsing itself is specific to each one).

The non-anchored tasks (`vocabulary`, `tutor_expressions`, `tutor_taught_terms`, `topics`) have
the same format, just without `UtteranceIndex()`/`filterAnchored`. `topics` is the only one whose
output type is a plain `[]string` (list of topics), with no dedicated struct.

### `internal/analysis/prompts.go` (new)

```go
// RegisterPrompts stores (name, version, content) for each TaskDef in
// Tasks into the prompts table, if it doesn't already exist — idempotent
// across app restarts. Called once in main.go, right after db.Open.
func RegisterPrompts(conn *sql.DB) error {
    for _, t := range Tasks {
        if _, err := db.UpsertPrompt(conn, t.Name(), t.Version(), t.Prompt()); err != nil {
            return fmt.Errorf("analysis: registrar prompt %s: %w", t.Name(), err)
        }
    }
    return nil
}
```

Each task carries its own `version` (a field in `task[T]`, currently `1` for all 7); bumping it
is manual in the code (along with the `.md` file name, which follows the same `-vN` convention)
whenever the prompt content changes enough to be worth distinguishing from the history already
persisted in `analysis_results.prompt_id`.

### `internal/db/migrations/00004_analysis_results.sql` (new)

```sql
-- +goose Up
CREATE UNIQUE INDEX idx_prompts_name_version ON prompts(name, version);

CREATE TABLE analysis_results (
    id INTEGER PRIMARY KEY,
    lesson_id INTEGER NOT NULL REFERENCES lessons(id),
    task TEXT NOT NULL,
    prompt_id INTEGER NOT NULL REFERENCES prompts(id),
    model TEXT NOT NULL,
    result_json TEXT NOT NULL,
    raw_response_path TEXT NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE(lesson_id, task)
);

CREATE TABLE lesson_topics (
    id INTEGER PRIMARY KEY,
    lesson_id INTEGER NOT NULL REFERENCES lessons(id),
    topic TEXT NOT NULL,
    UNIQUE(lesson_id, topic)
);

-- +goose Down
DROP TABLE lesson_topics;
DROP TABLE analysis_results;
DROP INDEX idx_prompts_name_version;
```

`lesson_topics` is a separate table (instead of one more row in `analysis_results`) because
topics are multi-valued per lesson and the Library (Phase 2's Story 5) needs to filter by
individual topic — a `result_json` with a serialized array wouldn't allow indexing/filtering via
SQL.

### `internal/db/analysis_results.go` (new)

```go
// UpsertPrompt inserts (name, version, content) if it doesn't already
// exist. Divergent content for the same already-registered (name, version)
// signals a version bump forgotten in the code (the "-vN" naming
// convention for the file/prompt); it logs a warning and keeps the content
// already stored — it never overwrites, because analysis_results may
// already reference that prompt_id.
func UpsertPrompt(conn *sql.DB, name string, version int, content string) (id int64, err error)

// UpsertAnalysisResult stores (or replaces, if one already exists) the
// result of task for lessonID — reprocessing (Story 2) overwrites the
// existing row via ON CONFLICT(lesson_id, task).
func UpsertAnalysisResult(conn *sql.DB, lessonID int64, task string, promptID int64, model, resultJSON, rawResponsePath string) error

type AnalysisResult struct {
    LessonID        int64
    Task            string
    PromptID        int64
    Model           string
    ResultJSON      string
    RawResponsePath string
}

// FindAnalysisResult returns (nil, nil) if the task hasn't run yet for
// that lesson — a normal state while the corresponding job (Story 2) is
// pending/running/error, not an error.
func FindAnalysisResult(conn *sql.DB, lessonID int64, task string) (*AnalysisResult, error)

// ReplaceLessonTopics deletes lessonID's existing topics and inserts the
// new ones — the list is always derived wholesale from the most recent
// analyze_topics result, never an incremental merge.
func ReplaceLessonTopics(conn *sql.DB, lessonID int64, topics []string) error
```

### `cmd/validate-analysis/main.go` (new, temporary)

```
go run ./cmd/validate-analysis -db=<path/data.db> -lesson-id=<id>
```

- Opens the real database via `internal/db.Open`, fetches the lesson's already-persisted
  `transcript` (`db.FindTranscriptByLessonID`) — reuses a real lesson already transcribed by
  Phase 1, no synthetic fixture.
- Asks on stdin for each `Speaker`'s role (`analysis.SpeakerExamples` + an interactive prompt, the
  same text used by the defunct `cmd/spike`), builds `speakerRoles`, and calls
  `analysis.FormatTranscript`.
- Builds `analysis.NewDeepSeekProvider(apiKey)` with `DEEPSEEK_API_KEY` read from `.env`
  (`loadDotEnv`, copied from `cmd/spike` before removing it).
- Runs the 7 `analysis.Tasks` sequentially (no parallelism — the goal is inspection, not
  measuring throughput), prints tokens/cost per task (DeepSeek's envelope already carries
  `usage`), and writes:
  - `local/output/analysis-validation/<task>/raw.json`
  - `local/output/analysis-validation/<task>/result.json`
- Writes nothing to the database (`analysis_results`/`lesson_topics` only start being written for
  real by the `Worker` in Story 2) — this CLI is just for looking at the result and feeding
  `docs/notas-analise-llm.md`.
- Removed from the repository once validation is done and the notes are recorded — the same fate
  as `cmd/spike`.

### `main.go`

```go
conn, err := db.Open(dbPath) // already runs the migrations, including 00004
...
if err := analysis.RegisterPrompts(conn); err != nil {
    log.Fatalf("registrar prompts de análise: %v", err)
}
```

A failure to register prompts is fatal (the same treatment a migration failure already gets
today) — there's no way for the Phase 2 queue to work without the prompts in the table, and
failing early, loud, and clear is preferable to only discovering this when the first analysis job
runs (days later, Story 2).

## Data flow (running one task, internal/CLI use in this story)

```
cmd/validate-analysis (or, in Story 2, the Worker):
  transcript := analysis.FormatTranscript(utterances, speakerRoles)
  for _, t := range analysis.Tasks {
      resultJSON, raw, err := t.Execute(ctx, provider, transcript, len(utterances))
      // always write raw to disk (even with err != nil — the call already cost money);
      // write resultJSON to analysis_results only if err == nil (Story 2)
  }
```

There's no UI data flow in this story — the nearest UI that consumes this is Story 3.

## Error handling

- **Network/API error on the provider call** (`provider.Complete` fails): `Execute` returns
  `resultJSON == nil`, `raw` with whatever was read (can be empty), and `err != nil` — the same
  resilience principle from Phase 1 (a failure blocks nothing beyond the task itself).
- **Malformed JSON or unexpected schema** (`parse` fails): same thing — `raw` preserved for
  debugging, `err != nil` describes the task and the cause.
- **`utterance_index` out of range or missing**: not an error — the item is discarded, `slog.Warn`
  with the count, the result proceeds with the valid items.
- **`RegisterPrompts` fails to write** (database unavailable, etc.): fatal at app startup — see
  the `main.go` section above.
- **Divergent prompt content for the same `(name, version)`** (a dev forgot to bump the version
  after editing the `.md`): not an error — `slog.Warn`, keeps the content already registered in
  the database.

## Out of scope for this story

- Any new job in the `Worker`/queue, an analysis credential via keyring, or a Settings field for
  it — all of that is Story 2.
- Any UI (inline corrections, an Analysis tab, topic chips) — Stories 3-5.
- Selecting the analysis provider/model and estimating cost — Phase 5.
- `cmd/validate-analysis` as a permanent tool — it's removed once the story closes.
- Automatic reprocessing when a prompt changes — once it exists (Story 2), it stays an explicit
  action, the same pattern as Phase 1's Queue.

## Tests

**`internal/analysis`** (synthetic fixtures, no real API call):
- `FormatTranscript` (`transcript_test.go`, extended): confirms the numbered `[N]` prefix on each
  line, in the same order as `utterances`.
- One parser test per task: valid JSON → expected items; for the 3 anchored tasks, JSON with an
  out-of-range `utterance_index` (and a missing/negative case) → item discarded, correct discard
  count; malformed JSON → error, no panics.
- `openAICompatibleProvider`'s `Complete`: a test with a fake HTTP server confirming that the
  `systemPrompt` passed per call (no longer fixed at construction) is what goes into the request
  body.
- `RegisterPrompts` (via `internal/db` with a `t.TempDir()` database): calling it twice doesn't
  duplicate rows in `prompts`; divergent content for the same `(name, version)` logs a warning
  (captured via test `slog`) and doesn't change the existing row.

**`internal/db`** (`analysis_results_test.go`, new, same pattern as `transcripts_test.go`):
- `UpsertAnalysisResult` inserts and, on a second call with the same `(lesson_id, task)`, replaces
  the row (confirmed by reading `result_json`/`raw_response_path` afterward).
- `FindAnalysisResult` returns `(nil, nil)` when the task hasn't run yet for that lesson.
- `ReplaceLessonTopics`: a second call with a different list fully replaces the previous one (no
  leftover old topics).

**Manual validation on a real lesson** (Story 1's explicit criterion): run
`cmd/validate-analysis` against an already-transcribed lesson, review the 7 outputs, and record
quality + real cost (tokens/USD) in `docs/notas-analise-llm.md`, comparing against Phase 0's
single measurement. Only after that is the CLI removed.

## Acceptance criteria (from `docs/fase-2-analise-llm.md`, Story 1)

- [ ] 7 versioned prompts, each focused on a single output, registered in the `prompts` table.
- [ ] `FormatTranscript` numbers the utterances (`utterance_index`) in the transcript sent to the
      model.
- [ ] `analysis.Provider` becomes task-agnostic (`Complete(ctx, systemPrompt, transcript)
      (json.RawMessage, error)`); each task defines its own output type and parsing.
- [ ] New migration: `analysis_results` (`UNIQUE(lesson_id, task)`) and `lesson_topics`.
- [ ] Defined and tested behavior for an out-of-range or missing `utterance_index` (silent discard
      with a log).
- [ ] Validation on a real lesson: the 7 tasks run manually, result observed and recorded in
      `docs/notas-analise-llm.md` (quality per task + real cost in tokens/USD).
