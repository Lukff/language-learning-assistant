# Phase 2, Story 3 — Lesson Topics: Design

> Covers Story 3 from `docs/fase-2-analise-llm.md` (`analyze_topics`). Depends on Story 2
> (DeepSeek credential and the on-demand pattern already established) and Story 1 (prompts,
> task-agnostic `Provider`, `analysis_results`/`lesson_topics`). Scope: topics generated on
> demand, displayed as chips in the Detail header, **editable by the user** (add/remove per lesson
> and rename as a global entity, the same pattern as teachers in Story 9), with the prompt evolved
> for general granularity and reuse of existing topics. **Does not** include a background job,
> topic search (Phase 3), or a fixed taxonomy.

## Context and motivation

Story 2 delivered `analyze_corrections` as a pilot, anchored to speech and displayed inline.
Topics are the first task **not anchored to speech**: the result is a list of short labels that
describe the lesson, closer to metadata than to an annotation on the transcript. This changes the
UI format (chips in the header, not inline markup) and, by the user's decision, also changes the
data model: topics become an **editable entity**, not just a cache derived from the LLM.

Three requirements raised during the design discussion (and incorporated here):

1. **Topics editable by the user** — add/remove per lesson and rename as a global entity (like
   teachers). `lesson_topics` stops being a derived cache and becomes the **source of truth** for
   each lesson's topics; `analysis_results` now only stores "did the task run?" + raw + model +
   prompt (audit/idempotency).
2. **General granularity** — the prompt instructs the model to produce general topics
   (search-label level, 2–5 words), without excessive detail; granularity is adjustable later with
   examples.
3. **Reuse** — when generating topics for a lesson, the topics that already exist (across all
   lessons) are provided to the model so it reuses them instead of creating redundant variations
   of the same subject.

## Scope decisions

- **A topic is an entity (`topics`), like a teacher (`teachers`).** Renaming is a single-row
  `UPDATE`, reflected across all lessons via a JOIN. `lesson_topics` changes from `topic TEXT` to
  `topic_id INTEGER REFERENCES topics(id)`, with `UNIQUE(lesson_id, topic_id)`.
- **`lesson_topics` is the UI's source of truth.** `GetTopics` reads from `lesson_topics` (JOIN
  `topics`), not from `result_json` in `analysis_results`. The `Analyzed` flag (for the
  "Analisar/Reprocessar" button) comes from `analysis_results` (does the `analyze_topics` row
  exist?).
- **Analisar/Reprocessar replaces, never merges.** Running the task entirely replaces the lesson's
  `lesson_topics` with the LLM's result. The button asks for confirmation whenever there are
  already topics to replace (not just on reprocess), because the action discards manual edits made
  to the chips.
- **Switching speakers preserves topics.** Topics don't depend on who is the student/tutor (the
  only task whose prompt doesn't mention Student/Tutor). The speaker-swap deletion now erases
  **everything except `analyze_topics`** and doesn't touch `lesson_topics`. Note that
  `analyze_vocabulary` and `analyze_tutor_expressions` (and `taught_terms`) also depend on the
  mapping in the prompt (even without `utterance_index`) and continue to be discarded — the
  criterion is "depends on who is student/tutor", not "is anchored to speech".
- **Reuse via the content sent, without changing the task interface.** The list of existing topics
  is appended to the transcript text (same message), not passed as a new parameter to
  `TaskDef.Execute`/`Provider.Complete` — this avoids touching the generic interface shared by the
  7 tasks. A pure (testable) helper `AppendExistingTopics` does the composition.
- **Prompt v2.** Changing the prompt's content requires bumping to `analyze-topics-v2.md` +
  `version: 2` in the constructor — `UpsertPrompt` is idempotent by (name, version) and doesn't
  overwrite the v1 already registered in the database; without the bump, the app would keep
  sending the old prompt.
- **Renaming a topic is global, via a panel in Settings** (not inline in the chips), mirroring the
  already-existing "Professores" panel.
- **Adding/removing per lesson happens in the Detail's chips**, regardless of whether the analysis
  has run — the user can add topics manually without ever calling the LLM.

## Architecture

```
internal/db/
  migrations/00006_topics.sql        # NEW: topics + lesson_topics.topic_id + backfill
  topics.go                          # NEW: Topic, ListTopics, GetOrCreateTopicByName, RenameTopic,
                                     #      ListLessonTopics, AddLessonTopic, RemoveLessonTopic
  analysis_results.go                # ReplaceLessonTopics changes to []int64 (ids);
                                     #   DeleteAnalysisResultsForLesson -> DeleteSpeakerDependentAnalysisResults

internal/analysis/
  tasks_topics.go                    # + NewTopicsTask (exported), ParseTopicsResult, AppendExistingTopics

prompts/
  analyze-topics-v2.md               # NEW (v1 stays as history)

services/
  analysis.go                        # + TopicsResult, GetTopics/AnalyzeTopics/ReprocessTopics, runTopics
  topics.go                          # NEW: TopicsService (ListTopics/RenameTopic/AddTopic/RemoveTopic)
  library.go                         # SetStudentSpeaker calls the renamed deletion (signature unchanged)

main.go                              # + TopicsService in app.Services

frontend/src/lib/
  screens/LessonDetail.svelte        # + topic chips in the header (add/remove + analyze/reprocess)
  screens/Settings.svelte            # + "Tópicos" panel (list + rename), mirroring "Professores"
  bindings/.../topicsservice.ts      # generated (TopicsService); models.ts gains Topic/TopicsResult
```

### `internal/db/migrations/00006_topics.sql`

```sql
-- +goose Up
CREATE TABLE topics (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

INSERT INTO topics (name, created_at, updated_at)
SELECT DISTINCT topic, strftime('%Y-%m-%dT%H:%M:%SZ','now'), strftime('%Y-%m-%dT%H:%M:%SZ','now')
FROM lesson_topics;

CREATE TABLE lesson_topics_new (
    id INTEGER PRIMARY KEY,
    lesson_id INTEGER NOT NULL REFERENCES lessons(id),
    topic_id INTEGER NOT NULL REFERENCES topics(id),
    UNIQUE(lesson_id, topic_id)
);
INSERT INTO lesson_topics_new (lesson_id, topic_id)
SELECT lt.lesson_id, t.id FROM lesson_topics lt JOIN topics t ON t.name = lt.topic;
DROP TABLE lesson_topics;
ALTER TABLE lesson_topics_new RENAME TO lesson_topics;

-- +goose Down
CREATE TABLE lesson_topics_old (
    id INTEGER PRIMARY KEY,
    lesson_id INTEGER NOT NULL REFERENCES lessons(id),
    topic TEXT NOT NULL,
    UNIQUE(lesson_id, topic)
);
INSERT INTO lesson_topics_old (lesson_id, topic)
SELECT lt.lesson_id, t.name FROM lesson_topics lt JOIN topics t ON t.id = lt.topic_id;
DROP TABLE lesson_topics;
ALTER TABLE lesson_topics_old RENAME TO lesson_topics;
DROP TABLE topics;
```

The `topic` (TEXT) column can't have its type changed to `topic_id` (INTEGER FK) via `ALTER
TABLE` — the table needs to be rebuilt. The backfill is defensive (same spirit as
`migration_backfill_test.go`): in practice `lesson_topics` is empty because nothing calls
`ReplaceLessonTopics` yet, but the migration doesn't assume that.

### `internal/db/topics.go` (new)

```go
type Topic struct {
    ID   int64
    Name string
}

// ListTopics lists the registered topics in alphabetical order — feeds the
// "Tópicos" panel in Settings and the prompt's reuse list.
func ListTopics(conn *sql.DB) ([]Topic, error)

// getOrCreateTopicByName (via execer, reuses the interface from teachers.go):
// trims the name (TrimSpace) before looking up/writing; creates it if it doesn't exist.
func GetOrCreateTopicByName(conn *sql.DB, name string) (int64, error)

// RenameTopic renames topic id — reflected across all lessons via JOIN.
// A collision (UNIQUE) becomes "já existe um tópico com esse nome" (reuses isUniqueConstraintError).
func RenameTopic(conn *sql.DB, id int64, newName string) error

// ListLessonTopics returns lessonID's topics (JOIN topics), in alphabetical order.
func ListLessonTopics(conn *sql.DB, lessonID int64) ([]Topic, error)

// AddLessonTopic links topicID to lessonID (INSERT OR IGNORE — already linked isn't an error).
func AddLessonTopic(conn *sql.DB, lessonID, topicID int64) error

// RemoveLessonTopic unlinks topicID from lessonID (doesn't delete the entity).
func RemoveLessonTopic(conn *sql.DB, lessonID, topicID int64) error

// ReplaceLessonTopics — SIGNATURE CHANGES from []string to []int64: deletes
// the existing links and inserts the new ones (the list is always derived
// wholesale from the most recent result). Only the tests called it (via
// string); they switch to using ids.
func ReplaceLessonTopics(conn *sql.DB, lessonID int64, topicIDs []int64) error
```

### `internal/db/analysis_results.go`

`DeleteAnalysisResultsForLesson` is renamed to `DeleteSpeakerDependentAnalysisResults` and its SQL
changes:

```go
func DeleteSpeakerDependentAnalysisResults(conn *sql.DB, lessonID int64) error {
    _, err := conn.Exec(
        `DELETE FROM analysis_results WHERE lesson_id = ? AND task != 'analyze_topics'`,
        lessonID,
    )
    // doesn't touch lesson_topics
    return err
}
```

Sole caller: `services.LibraryService.SetStudentSpeaker` (same signature, only the name changes).

### `internal/analysis/tasks_topics.go`

```go
// NewTopicsTask exports the task (it used to be newTopicsTask, lowercase)
// with version 2 and the analyze-topics-v2.md prompt.
func NewTopicsTask() TaskDef

// ParseTopicsResult decodes a persisted result_json (a JSON array of
// strings — resultJSON is json.Marshal([]string), no envelope) back into
// []string.
func ParseTopicsResult(resultJSON json.RawMessage) ([]string, error)

// AppendExistingTopics appends the list of already-existing topics to the
// transcript content, in a trailing block that the v2 prompt recognizes.
// Empty when there are no existing topics — returns the transcript unchanged
// in that case.
func AppendExistingTopics(transcript string, existing []string) string
```

`AppendExistingTopics` produces (only when `len(existing) > 0`):

```
<transcript>

Tópicos já utilizados em outras aulas (reutilize quando fizer sentido):
- viagens
- trabalho remoto
```

### `prompts/analyze-topics-v2.md` (new)

Keeps the output format (`{"topics": [...]}`) and rules 1/3/4/5/6 from v1; adds:

- **General granularity:** "Cada tópico deve ser um rótulo geral, no nível de uma etiqueta de
  busca (2–5 palavras) — prefira 'viagens' a 'visto de turista para os EUA'. Não detalhe além
  disso; a granularidade ideal será calibrada depois com exemplos."
- **Reuse:** "Se a mensagem seguinte incluir uma seção 'Tópicos já utilizados em outras aulas',
  reutilize um tópico dessa lista quando ele se aplicar, em vez de criar uma variação redundante do
  mesmo assunto."

v1 stays in the repo as history; `newTopicsTask` now points to v2.

### `services/analysis.go`

```go
type TopicsResult struct {
    Analyzed bool      `json:"analyzed"`
    Items    []Topic   `json:"items"`
}

func (s *AnalysisService) GetTopics(lessonID int64) (TopicsResult, error)
func (s *AnalysisService) AnalyzeTopics(lessonID int64) (TopicsResult, error)
func (s *AnalysisService) ReprocessTopics(lessonID int64) (TopicsResult, error)
```

`GetTopics` = `currentTopics(lessonID)`: reads `db.ListLessonTopics` (items) +
`db.FindAnalysisResult(lessonID, "analyze_topics")` (the `Analyzed` flag). Doesn't call the API.

`runTopics(lessonID, overwrite)`:

1. `db.FindLessonByID`; nil → error; `StudentSpeakerLabel == nil` → "escolha quem é você na aula
   antes de analisar tópicos".
2. `db.FindTranscriptByLessonID`; nil → error.
3. If `!overwrite` and `db.FindAnalysisResult(lessonID, "analyze_topics")` != nil → returns
   `currentTopics` directly (idempotent).
4. Builds `speakerRoles` (same code as `runCorrections`) and `FormatTranscript`.
5. `db.ListTopics` → names → `analysis.AppendExistingTopics(formatted, names)`.
6. `providerFactory()` (same handling of `keyring.ErrNotFound`).
7. `analysis.NewTopicsTask().Execute(ctx, provider, input, len(utterances))`.
8. Writes `raw` to disk even if `err != nil` (same pattern); if `err != nil`, returns the error.
9. `analysis.ParseTopicsResult(resultJSON)` → `[]string`.
10. For each name: `db.GetOrCreateTopicByName` → ids; `db.ReplaceLessonTopics(lessonID, ids)`.
11. `db.UpsertPrompt` (idempotent) → `db.UpsertAnalysisResult(lessonID, "analyze_topics", ...)`.
12. Returns `currentTopics(lessonID)`.

### `services/topics.go` (new)

```go
type Topic struct {
    ID   int64  `json:"id"`
    Name string `json:"name"`
}

type TopicsService struct{ conn *sql.DB }
func NewTopicsService(conn *sql.DB) *TopicsService

func (s *TopicsService) ListTopics() ([]Topic, error)                    // "Tópicos" panel
func (s *TopicsService) RenameTopic(id int64, newName string) error       // global (JOIN)
func (s *TopicsService) AddTopic(lessonID int64, name string) (Topic, error)    // get-or-create + link
func (s *TopicsService) RemoveTopic(lessonID, topicID int64) error        // unlink (doesn't delete)
```

`AddTopic` returns the created/resolved `Topic` (name trimmed); the frontend re-fetches
`GetTopics` afterward to reconcile (the same "mutate and reload" pattern already used for
teachers).

### `main.go`

```go
Services: []application.Service{
    ...
    application.NewService(services.NewTopicsService(conn)),
},
```

### `frontend/src/lib/screens/LessonDetail.svelte`

New state: `topics: TopicsResult | null`, `analyzingTopics`, `topicsError`.

- `fetchTopicsIfReady()` in `onMount` and `onLessonSaved` — only requires `status === "pronta"`
  (topics don't depend on who the student is, so the chips are editable even before choosing the
  speaker).
- In the header (below date/tutor), a `.topics-row` line:
  - chips: `{#each topics.items}` → `<span class="chip">` with the name + a "✕" that calls
    `RemoveTopic(lessonId, topic.id)` and re-fetches `GetTopics`.
  - a "+ adicionar tópico" input + Enter → `AddTopic(lessonId, name)` and re-fetches.
  - a button (visible only when `studentSpeakerLabel` is set, the same gating as the corrections
    button): `topics?.analyzed` ? "Reprocessar tópicos" : "Analisar tópicos". Before running, if
    `topics.items?.length` (there are topics to replace),
    `confirm("Isso substitui os tópicos atuais e gera uma nova chamada à API. Continuar?")`; if
    canceled, nothing is called.
  - "Analisando…"/"Reprocessando…" state; errors go to `topicsError` (a small banner, never taking
    down the video/transcript).
- An empty list with `analyzed` true shows a discreet "sem tópicos identificados" (no chip).

### `frontend/src/lib/screens/Settings.svelte`

New "Tópicos" panel (mirroring "Professores"): `ListTopics` + inline rename via `RenameTopic`,
with the same `renameDrafts`/`renamingId`/`renameErrors` pattern already used for teachers.

## Data flow

```
LessonDetail (onMount / onLessonSaved / after editing chips)
  GetTopics(lessonId) ──► AnalysisService.GetTopics
      db.ListLessonTopics(lessonId) ──► Items
      db.FindAnalysisResult(lessonId, "analyze_topics") ──► Analyzed

[user clicks "Analisar/Reprocessar tópicos"]
  AnalyzeTopics/ReprocessTopics ──► runTopics(overwrite)
      idempotency (FindAnalysisResult) — if !overwrite and it already ran: currentTopics
      FormatTranscript(utterances, speakerRoles)
      ListTopics ──► AppendExistingTopics(formatted, names)
      NewTopicsTask().Execute ──► resultJSON (array of strings), raw
      ParseTopicsResult ──► []string
      GetOrCreateTopicByName (by name) ──► ids ──► ReplaceLessonTopics(ids)
      UpsertPrompt + UpsertAnalysisResult("analyze_topics", ...)
      currentTopics ──► TopicsResult

[user edits chips]
  AddTopic(lessonId, name) ──► GetOrCreateTopicByName + AddLessonTopic ──► re-fetches GetTopics
  RemoveTopic(lessonId, topicId) ──► RemoveLessonTopic ──► re-fetches GetTopics

[user renames in Settings]
  RenameTopic(topicId, newName) ──► UPDATE topics.name ──► reflected across all lessons (JOIN)

[user switches speaker in Edit lesson]
  SetStudentSpeaker ──► DeleteSpeakerDependentAnalysisResults ──► deletes everything except analyze_topics;
                         lesson_topics intact ──► GetTopics keeps returning the same chips
```

## Error handling

- **Missing credential/keyring**: `providerFactory()` fails before network access; the message
  reuses the `HasAnalysisCredential` pattern; it appears in `topicsError`.
- **`StudentSpeakerLabel` null**: a clear error before the provider (defense against a direct
  call).
- **Network/API/parse error**: `raw` is still written to disk; `analysis_results` and
  `lesson_topics` don't change; the error bubbles up to `topicsError`, recoverable with another
  click.
- **Renaming a topic with a name already in use**: `RenameTopic` returns "já existe um tópico com
  esse nome".
- **Adding a topic already linked to the lesson**: `AddLessonTopic` uses INSERT OR IGNORE — not an
  error.
- **Removing a topic that isn't linked**: `RemoveLessonTopic` is a no-op (a DELETE with no
  matching row isn't an error).
- **Speaker swap canceled in the modal**: nothing is called (same current behavior).

## Out of scope for this story

- A background job for `analyze_topics` (the "Promoção a job em background" criterion — only after
  the decision in `docs/notas-analise-llm.md`).
- Search/filter by topic in the Library and automatic tags (Phase 3) — `lesson_topics` by
  `topic_id` already leaves the schema ready, but nothing is read via filter in this story.
- Fixed/hierarchical taxonomy, automatic merging of similar topics (the list is free-form per
  lesson).
- Renaming a topic directly in the Detail's chips (renaming is global, via Settings).
- Automatic re-analysis when the prompt changes (Reprocess remains an explicit action).
- The remaining 5 candidate tasks.

## Tests

**`internal/db`** (new `topics_test.go` + extended `analysis_results_test.go` + backfill):
- Migration/backfill: an old schema with a populated `lesson_topics(topic TEXT)` → migrates to
  `topic_id` with the same names and links preserved (synthetic fixture,
  `migration_backfill_test.go` pattern).
- `GetOrCreateTopicByName`: creates and returns the same id on the second call; trims whitespace.
- `ListTopics`: alphabetical order, no duplicates.
- `RenameTopic`: updates the name and is reflected in `ListLessonTopics` of linked lessons; a
  collision → a friendly error.
- `AddLessonTopic`/`RemoveLessonTopic`: links/unlinks; INSERT OR IGNORE absorbs a duplicate;
  removing doesn't delete the `topics` entity.
- `ReplaceLessonTopics` (by ids): replaces wholesale.
- `DeleteSpeakerDependentAnalysisResults`: deletes `analyze_corrections` and `analyze_vocabulary`,
  preserves `analyze_topics`, doesn't touch `lesson_topics`, doesn't touch another lesson.

**`internal/analysis`** (extended `tasks_topics_test.go`):
- `ParseTopicsResult` round-trip (array of strings; malformed JSON → error).
- `AppendExistingTopics`: empty list → transcript unchanged; with a list → the correct trailing
  block.

**`services`** (extended `analysis_test.go` + new `topics_test.go`, fake `Provider`):
- `GetTopics` without an analysis → `Analyzed=false`, empty `Items`, without calling the provider.
- `AnalyzeTopics` idempotent (2nd call doesn't invoke the provider).
- `ReprocessTopics` always invokes the provider and re-replaces `lesson_topics`.
- `AnalyzeTopics` without `StudentSpeakerLabel` → an error before the provider.
- `runTopics` writes to `analysis_results` **and** `lesson_topics` (via `ReplaceLessonTopics`).
- The fake provider captures the input and the test **asserts that the list of existing topics was
  appended** (reuse).
- Provider error: `lesson_topics` and `analysis_results` stay intact.
- `TopicsService`: `AddTopic` creates the entity + links it (a 2nd addition of the same name
  reuses the entity); `RemoveTopic` unlinks without deleting; `RenameTopic` is reflected globally.

**Manual validation** (closes the story): run `AnalyzeTopics` on real lessons through the UI,
observe granularity and reuse, test editing chips and the global rename, record the decision
(keep/refine/discard) in `docs/notas-analise-llm.md` — **the first task without Phase 0
validation**, so this observation carries double weight.

**Frontend**: no test runner — manual verification (`wails3 dev`) of the full flow.

## Acceptance criteria

- [ ] Topics generated on demand (no background job), with `StudentSpeakerLabel` required.
- [ ] Result in `analysis_results` (idempotent) **and** `lesson_topics` (by `topic_id`).
- [ ] Topics displayed as chips in the Detail header; a lesson without the task shows the
      transcript normally.
- [ ] Add/remove a topic per lesson (chips); rename a topic globally (Settings).
- [ ] Failure doesn't break the lesson — visible, recoverable error.
- [ ] Switching speakers preserves `analyze_topics` and `lesson_topics`.
- [ ] Prompt v2 with general granularity + reuse of existing topics.
- [ ] Decision recorded in `docs/notas-analise-llm.md` after observing real lessons.
