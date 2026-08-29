# Phase 2, Story 1 — Per-task prompts and analysis persistence: Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Break the LLM analysis into 7 independent tasks (one prompt/schema per task), make
`internal/analysis.Provider` task-agnostic, and persist the results into two new tables
(`analysis_results`, `lesson_topics`) — without yet wiring anything to the `Worker`/queue (Story 2)
or the UI (Stories 3-5).

**Architecture:** `prompts/` also becomes a minimal Go package (`embed.FS`) for the 7 new `.md`
files; `internal/analysis` gains a generic `TaskDef`/`task[T]` abstraction on top of a
task-agnostic `Provider` (`Complete(ctx, systemPrompt, transcript) (json.RawMessage, error)`);
`internal/db` gains the `analysis_results`/`lesson_topics` repository and idempotent prompt
registration; a temporary CLI (`cmd/validate-analysis`, removed at the end) validates the 7 tasks
against a real lesson.

**Tech Stack:** Go stdlib (`embed`, `database/sql`, `encoding/json`, `log/slog`), `modernc.org/sqlite`
(driver already in use), `goose` (migrations already in use). No new dependency.

## Global Constraints

- Packages under `internal/` never import Wails — thin layer (`CLAUDE.md`).
- Portable SQL at the repository layer — nothing driver-specific (`CLAUDE.md`).
- Code and identifiers in English; user-facing error messages, documentation, and prompt content
  in PT-BR (`CLAUDE.md`).
- No real lesson data (transcript, provider JSON) enters the repository — `local/` and `.env` are
  already in `.gitignore` (`CLAUDE.md`, Privacy section).
- A PT/ES word in the student's speech is a native-language fallback (a vocabulary candidate),
  never an English mistake (`CLAUDE.md`).
- Reprocessing stays an explicit action — no job/task reruns on its own when a prompt changes
  (`docs/fase-2-analise-llm.md`).
- Commits are single-line, semantic format (`type: description`) (`CLAUDE.md`).
- `go vet ./...` clean before any commit touching `.go` (`CLAUDE.md`).

---

## Task 1: Persistence — migration + `analysis_results`/`lesson_topics` repository

**Files:**
- Create: `internal/db/migrations/00004_analysis_results.sql`
- Create: `internal/db/analysis_results.go`
- Create: `internal/db/analysis_results_test.go`

**Interfaces:**
- Consumes: nothing (just the existing `internal/db` infrastructure: `Open`, migrations via
  `goose`, the already-existing `lessons` table for test fixtures).
- Produces (used by Task 4 and by `cmd/validate-analysis` in Task 6):
  - `func UpsertPrompt(conn *sql.DB, name string, version int, content string) (int64, error)`
  - `func UpsertAnalysisResult(conn *sql.DB, lessonID int64, task string, promptID int64, model, resultJSON, rawResponsePath string) error`
  - `type AnalysisResult struct { LessonID int64; Task string; PromptID int64; Model string; ResultJSON string; RawResponsePath string }`
  - `func FindAnalysisResult(conn *sql.DB, lessonID int64, task string) (*AnalysisResult, error)`
  - `func ReplaceLessonTopics(conn *sql.DB, lessonID int64, topics []string) error`

- [ ] **Step 1: Write the migration**

Create `internal/db/migrations/00004_analysis_results.sql`:

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

`internal/db/db.go` already embeds `migrations/*.sql` (`//go:embed migrations/*.sql`) and already
runs `goose.Up` in `Open()` — no code change is needed for this migration to be applied.

- [ ] **Step 2: Write the failing test for `UpsertPrompt`**

Create `internal/db/analysis_results_test.go`:

```go
package db

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestUpsertPrompt_InsertsAndIsIdempotent(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	id1, err := UpsertPrompt(conn, "analyze_corrections", 1, "conteúdo v1")
	if err != nil {
		t.Fatalf("UpsertPrompt() erro inesperado: %v", err)
	}

	id2, err := UpsertPrompt(conn, "analyze_corrections", 1, "conteúdo v1")
	if err != nil {
		t.Fatalf("segunda UpsertPrompt() erro inesperado: %v", err)
	}
	if id1 != id2 {
		t.Errorf("id2 = %d, esperado igual a id1 = %d (idempotente)", id2, id1)
	}

	var count int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM prompts WHERE name = ?`, "analyze_corrections").Scan(&count); err != nil {
		t.Fatalf("contar prompts falhou: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, esperado 1 (sem duplicar)", count)
	}
}

func TestUpsertPrompt_DivergentContentKeepsExisting(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	id1, err := UpsertPrompt(conn, "analyze_corrections", 1, "conteúdo original")
	if err != nil {
		t.Fatalf("UpsertPrompt() erro inesperado: %v", err)
	}

	id2, err := UpsertPrompt(conn, "analyze_corrections", 1, "conteúdo diferente")
	if err != nil {
		t.Fatalf("segunda UpsertPrompt() erro inesperado: %v", err)
	}
	if id1 != id2 {
		t.Errorf("id2 = %d, esperado igual a id1 = %d", id2, id1)
	}

	var content string
	if err := conn.QueryRow(`SELECT content FROM prompts WHERE id = ?`, id1).Scan(&content); err != nil {
		t.Fatalf("select em prompts falhou: %v", err)
	}
	if content != "conteúdo original" {
		t.Errorf("content = %q, esperado \"conteúdo original\" (não sobrescrito)", content)
	}
}

func insertLessonFixture(t *testing.T, conn *sql.DB) int64 {
	t.Helper()
	res, err := conn.Exec(
		`INSERT INTO lessons (lesson_date, tutor, video_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"2026-07-29", "Fulano", "aula.mp4", "2026-07-29T09:00:00Z", "2026-07-29T09:00:00Z",
	)
	if err != nil {
		t.Fatalf("inserir lesson de fixture falhou: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

func TestUpsertAnalysisResult_InsertThenReplace(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := insertLessonFixture(t, conn)
	promptID, err := UpsertPrompt(conn, "analyze_corrections", 1, "conteúdo")
	if err != nil {
		t.Fatalf("UpsertPrompt() erro inesperado: %v", err)
	}

	if err := UpsertAnalysisResult(conn, lessonID, "analyze_corrections", promptID, "deepseek", `{"corrections":[]}`, "aula.corrections.json"); err != nil {
		t.Fatalf("UpsertAnalysisResult() erro inesperado: %v", err)
	}
	if err := UpsertAnalysisResult(conn, lessonID, "analyze_corrections", promptID, "deepseek", `{"corrections":[{"original":"x"}]}`, "aula.corrections.v2.json"); err != nil {
		t.Fatalf("segunda UpsertAnalysisResult() erro inesperado: %v", err)
	}

	r, err := FindAnalysisResult(conn, lessonID, "analyze_corrections")
	if err != nil {
		t.Fatalf("FindAnalysisResult() erro inesperado: %v", err)
	}
	if r == nil {
		t.Fatal("FindAnalysisResult() = nil, esperado resultado encontrado")
	}
	if r.ResultJSON != `{"corrections":[{"original":"x"}]}` {
		t.Errorf("ResultJSON = %q, esperado a segunda gravação (substituída)", r.ResultJSON)
	}
	if r.RawResponsePath != "aula.corrections.v2.json" {
		t.Errorf("RawResponsePath = %q, esperado atualizado", r.RawResponsePath)
	}

	var count int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM analysis_results WHERE lesson_id = ? AND task = ?`, lessonID, "analyze_corrections").Scan(&count); err != nil {
		t.Fatalf("contar analysis_results falhou: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, esperado 1 (substituído, não duplicado)", count)
	}
}

func TestFindAnalysisResult_NilWhenMissing(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := insertLessonFixture(t, conn)

	r, err := FindAnalysisResult(conn, lessonID, "analyze_corrections")
	if err != nil {
		t.Fatalf("FindAnalysisResult() erro inesperado: %v", err)
	}
	if r != nil {
		t.Errorf("FindAnalysisResult() = %+v, esperado nil", r)
	}
}

func TestReplaceLessonTopics_ReplacesEntirely(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := insertLessonFixture(t, conn)

	if err := ReplaceLessonTopics(conn, lessonID, []string{"viagens", "trabalho remoto"}); err != nil {
		t.Fatalf("ReplaceLessonTopics() erro inesperado: %v", err)
	}
	if err := ReplaceLessonTopics(conn, lessonID, []string{"receitas de família"}); err != nil {
		t.Fatalf("segunda ReplaceLessonTopics() erro inesperado: %v", err)
	}

	rows, err := conn.Query(`SELECT topic FROM lesson_topics WHERE lesson_id = ? ORDER BY topic`, lessonID)
	if err != nil {
		t.Fatalf("query em lesson_topics falhou: %v", err)
	}
	defer rows.Close()
	var topics []string
	for rows.Next() {
		var topic string
		if err := rows.Scan(&topic); err != nil {
			t.Fatalf("scan de topic falhou: %v", err)
		}
		topics = append(topics, topic)
	}
	if len(topics) != 1 || topics[0] != "receitas de família" {
		t.Errorf("topics = %+v, esperado apenas [\"receitas de família\"]", topics)
	}
}
```

- [ ] **Step 3: Run the tests to confirm they fail (functions don't exist yet)**

Run: `go test ./internal/db/... -run 'TestUpsertPrompt|TestUpsertAnalysisResult|TestFindAnalysisResult|TestReplaceLessonTopics' -v`
Expected: FAIL — `undefined: UpsertPrompt` (and the other functions).

- [ ] **Step 4: Implement `internal/db/analysis_results.go`**

```go
// internal/db/analysis_results.go
package db

import (
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

// UpsertPrompt inserts (name, version, content) into the prompts table if it
// doesn't already exist. If (name, version) already exists with different
// content, that signals a version bump forgotten in the code (the "-vN"
// naming convention for files under prompts/); it logs a warning and keeps
// the content already stored — it never overwrites, because
// analysis_results may already reference that prompt_id.
func UpsertPrompt(conn *sql.DB, name string, version int, content string) (int64, error) {
	var id int64
	var existingContent string
	err := conn.QueryRow(`SELECT id, content FROM prompts WHERE name = ? AND version = ?`, name, version).Scan(&id, &existingContent)
	if err == nil {
		if existingContent != content {
			slog.Warn("analysis: conteúdo do prompt divergente pra versão já registrada", "nome", name, "versao", version)
		}
		return id, nil
	}
	if err != sql.ErrNoRows {
		return 0, fmt.Errorf("buscar prompt %s v%d: %w", name, version, err)
	}

	res, err := conn.Exec(
		`INSERT INTO prompts (name, version, content, created_at) VALUES (?, ?, ?, ?)`,
		name, version, content, time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return 0, fmt.Errorf("inserir prompt %s v%d: %w", name, version, err)
	}
	return res.LastInsertId()
}

// UpsertAnalysisResult stores (or replaces, if one already exists) the
// result of task for lessonID — reprocessing (Story 2) overwrites the
// existing row.
func UpsertAnalysisResult(conn *sql.DB, lessonID int64, task string, promptID int64, model, resultJSON, rawResponsePath string) error {
	_, err := conn.Exec(
		`INSERT INTO analysis_results (lesson_id, task, prompt_id, model, result_json, raw_response_path, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(lesson_id, task) DO UPDATE SET
		     prompt_id = excluded.prompt_id,
		     model = excluded.model,
		     result_json = excluded.result_json,
		     raw_response_path = excluded.raw_response_path,
		     created_at = excluded.created_at`,
		lessonID, task, promptID, model, resultJSON, rawResponsePath, time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("gravar analysis_result da lesson %d, task %s: %w", lessonID, task, err)
	}
	return nil
}

// AnalysisResult is the persisted result of an analysis task for a lesson.
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
func FindAnalysisResult(conn *sql.DB, lessonID int64, task string) (*AnalysisResult, error) {
	r := AnalysisResult{LessonID: lessonID, Task: task}
	err := conn.QueryRow(
		`SELECT prompt_id, model, result_json, raw_response_path FROM analysis_results WHERE lesson_id = ? AND task = ?`,
		lessonID, task,
	).Scan(&r.PromptID, &r.Model, &r.ResultJSON, &r.RawResponsePath)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("buscar analysis_result da lesson %d, task %s: %w", lessonID, task, err)
	}
	return &r, nil
}

// ReplaceLessonTopics deletes lessonID's existing topics and inserts the
// new ones — the list is always derived wholesale from the most recent
// analyze_topics result, never an incremental merge. INSERT OR IGNORE
// absorbs a duplicate topic the model itself might repeat within the same
// response, without failing the whole transaction because of
// UNIQUE(lesson_id, topic).
func ReplaceLessonTopics(conn *sql.DB, lessonID int64, topics []string) error {
	tx, err := conn.Begin()
	if err != nil {
		return fmt.Errorf("iniciar transação de lesson_topics da lesson %d: %w", lessonID, err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM lesson_topics WHERE lesson_id = ?`, lessonID); err != nil {
		return fmt.Errorf("apagar lesson_topics antigos da lesson %d: %w", lessonID, err)
	}
	for _, topic := range topics {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO lesson_topics (lesson_id, topic) VALUES (?, ?)`, lessonID, topic); err != nil {
			return fmt.Errorf("inserir tópico %q da lesson %d: %w", topic, lessonID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commitar lesson_topics da lesson %d: %w", lessonID, err)
	}
	return nil
}
```

- [ ] **Step 5: Run the tests again and confirm they pass**

Run: `go test ./internal/db/... -v`
Expected: PASS on all tests in the package, including the 5 new ones.

- [ ] **Step 6: `go vet` and commit**

Run: `go vet ./internal/db/...`
Expected: no output (clean).

```bash
git add internal/db/migrations/00004_analysis_results.sql internal/db/analysis_results.go internal/db/analysis_results_test.go
git commit -m "feat: add persistence for analysis_results and lesson_topics"
```

---

## Task 2: `prompts/` package + 7 prompt files

**Files:**
- Create: `prompts/embed.go`
- Create: `prompts/embed_test.go`
- Create: `prompts/analyze-corrections-v1.md`
- Create: `prompts/analyze-vocabulary-v1.md`
- Create: `prompts/analyze-tutor-expressions-v1.md`
- Create: `prompts/analyze-tutor-taught-terms-v1.md`
- Create: `prompts/analyze-tutor-feedback-v1.md`
- Create: `prompts/analyze-tutor-corrections-v1.md`
- Create: `prompts/analyze-topics-v1.md`
- Delete: `prompts/analyze-v1.md`

**Interfaces:**
- Consumes: nothing.
- Produces (used by Task 4): `var prompts.FS embed.FS` — Go package
  `assistente-idiomas/prompts`, with `FS.ReadFile("analyze-corrections-v1.md")` etc. returning the
  content of each file.

- [ ] **Step 1: Write the failing embed test**

Create `prompts/embed_test.go`:

```go
// prompts/embed_test.go
package prompts

import "testing"

func TestFS_ContainsAllTaskPrompts(t *testing.T) {
	files := []string{
		"analyze-corrections-v1.md",
		"analyze-vocabulary-v1.md",
		"analyze-tutor-expressions-v1.md",
		"analyze-tutor-taught-terms-v1.md",
		"analyze-tutor-feedback-v1.md",
		"analyze-tutor-corrections-v1.md",
		"analyze-topics-v1.md",
	}
	for _, f := range files {
		data, err := FS.ReadFile(f)
		if err != nil {
			t.Errorf("FS.ReadFile(%q) erro: %v", f, err)
			continue
		}
		if len(data) == 0 {
			t.Errorf("FS.ReadFile(%q) retornou conteúdo vazio", f)
		}
	}
}
```

- [ ] **Step 2: Run the test and confirm it fails (package/files don't exist yet)**

Run: `go test ./prompts/... -v`
Expected: FAIL — `no Go files in prompts` or similar.

- [ ] **Step 3: Create `prompts/embed.go`**

```go
// prompts/embed.go
package prompts

import "embed"

//go:embed *.md
var FS embed.FS
```

- [ ] **Step 4: Remove the single Phase 0 prompt**

```bash
git rm prompts/analyze-v1.md
```

(No caller since `cmd/spike` was removed when Phase 0 closed — replaced by the 7 below.)

- [ ] **Step 5: Create the 7 prompt files**

Create `prompts/analyze-corrections-v1.md`:

````markdown
# Prompt de análise — Correções do Aluno (v1)

Você é um assistente que analisa a transcrição diarizada de uma aula particular de inglês entre
um Aluno e um Tutor (plataforma Cambly). A aula é majoritariamente em inglês, com eventual troca
para português ou espanhol (code-switching) por parte do Aluno.

A transcrição vem com cada fala numerada, no formato `[N] Aluno: ...` ou `[N] Tutor: ...` — N é o
índice da fala (começando em 0), na ordem em que ocorreram.

Sua única tarefa é apontar erros de inglês nas falas do **Aluno**. Produza **apenas um objeto
JSON**, sem nenhum texto antes ou depois, seguindo exatamente este formato:

```json
{
  "corrections": [
    {"utterance_index": 0, "original": "...", "correction": "...", "explanation": "..."}
  ]
}
```

## Regras

1. `utterance_index` é o N exato da fala do Aluno onde o erro ocorreu — copie o número que
   aparece entre colchetes na transcrição, nunca invente um índice.
2. `original` é o trecho da fala do Aluno com o erro; `correction` é a versão corrigida;
   `explanation` é uma explicação curta em português do porquê do erro.
3. Não invente correções para frases já corretas — se o Aluno não cometeu nenhum erro de inglês,
   devolva uma lista vazia (`[]`).
4. Uma palavra ou expressão em português ou espanhol no meio da fala em inglês **não é um erro de
   inglês** — é um recurso ao idioma nativo (isso é assunto de outra tarefa, não desta).
5. A resposta deve ser **apenas o objeto JSON** acima: sem markdown, sem comentários, sem texto
   explicativo fora do JSON.

A transcrição da aula será enviada na mensagem seguinte.
````

Create `prompts/analyze-vocabulary-v1.md`:

````markdown
# Prompt de análise — Vocabulário novo (v1)

Você é um assistente que analisa a transcrição diarizada de uma aula particular de inglês entre
um Aluno e um Tutor (plataforma Cambly). A aula é majoritariamente em inglês, com eventual troca
para português ou espanhol (code-switching) por parte do Aluno.

Sua única tarefa é listar palavras ou expressões em inglês, usadas por qualquer um dos dois
falantes, que valham a pena o Aluno aprender. Produza **apenas um objeto JSON**, sem nenhum texto
antes ou depois, seguindo exatamente este formato:

```json
{
  "vocabulary": [
    {"term": "...", "translation": "..."}
  ]
}
```

## Regras

1. `term` é a palavra ou expressão em inglês; `translation` é a tradução pro português.
2. **Importante:** se o Aluno usar uma palavra ou frase em português ou espanhol no meio da fala
   em inglês, isso é um recurso ao idioma nativo — a palavra/expressão em inglês que faltou ao
   Aluno naquele momento é candidata a `vocabulary`.
3. Inclua tanto vocabulário temático da conversa quanto expressões idiomáticas relevantes.
4. Não repita o mesmo termo mais de uma vez, mesmo que apareça em falas diferentes.
5. Se não houver nada relevante, devolva uma lista vazia (`[]`) — nunca omita a chave.
6. A resposta deve ser **apenas o objeto JSON** acima: sem markdown, sem comentários, sem texto
   explicativo fora do JSON.

A transcrição da aula será enviada na mensagem seguinte, com cada fala numerada e rotulada
"Aluno:" ou "Tutor:".
````

Create `prompts/analyze-tutor-expressions-v1.md`:

````markdown
# Prompt de análise — Expressões do Tutor (v1)

Você é um assistente que analisa a transcrição diarizada de uma aula particular de inglês entre
um Aluno e um Tutor (plataforma Cambly).

Sua única tarefa é listar expressões que o **Tutor** usou naturalmente na conversa (não que ele
tenha explicado ou ensinado explicitamente — isso é assunto de outra tarefa) e que seriam úteis
para o Aluno reutilizar no futuro. Produza **apenas um objeto JSON**, sem nenhum texto antes ou
depois, seguindo exatamente este formato:

```json
{
  "tutor_expressions": [
    {"text": "...", "note": "..."}
  ]
}
```

## Regras

1. `text` é a expressão exata usada pelo Tutor; `note` é uma nota curta em português sobre o
   contexto de uso (quando/como usar essa expressão).
2. Priorize expressões idiomáticas, conectores de conversa e formas naturais de dizer algo que o
   Aluno tentou dizer de um jeito mais rebuscado ou menos natural.
3. Não inclua vocabulário técnico isolado nem palavras únicas sem valor idiomático — isso é
   assunto da tarefa de vocabulário.
4. Se não houver nada relevante, devolva uma lista vazia (`[]`) — nunca omita a chave.
5. A resposta deve ser **apenas o objeto JSON** acima: sem markdown, sem comentários, sem texto
   explicativo fora do JSON.

A transcrição da aula será enviada na mensagem seguinte, com cada fala numerada e rotulada
"Aluno:" ou "Tutor:".
````

Create `prompts/analyze-tutor-taught-terms-v1.md`:

````markdown
# Prompt de análise — Termos apresentados pelo Tutor (v1)

Você é um assistente que analisa a transcrição diarizada de uma aula particular de inglês entre
um Aluno e um Tutor (plataforma Cambly).

Sua única tarefa é listar termos, palavras ou expressões que o **Tutor explicou ou ensinou
explicitamente** durante a aula — por exemplo, quando o Tutor para a conversa pra apresentar uma
palavra nova, corrigir o uso de um termo, ou sugerir uma forma alternativa de dizer algo.
Diferente da tarefa de "expressões do tutor" (que cobre uso natural na conversa, sem explicação),
aqui o Tutor **ensinou ativamente** o termo. Produza **apenas um objeto JSON**, sem nenhum texto
antes ou depois, seguindo exatamente este formato:

```json
{
  "tutor_taught_terms": [
    {"term": "...", "translation": "...", "context": "..."}
  ]
}
```

## Regras

1. `term` é o termo em inglês ensinado pelo Tutor; `translation` é a tradução pro português;
   `context` é uma nota curta em português sobre a situação em que o Tutor o apresentou.
2. Só inclua termos que o Tutor de fato explicou ou apresentou ativamente — não vocabulário que
   simplesmente apareceu na conversa sem nenhuma explicação do Tutor (isso é a tarefa de
   vocabulário).
3. Se não houver nada relevante, devolva uma lista vazia (`[]`) — nunca omita a chave.
4. A resposta deve ser **apenas o objeto JSON** acima: sem markdown, sem comentários, sem texto
   explicativo fora do JSON.

A transcrição da aula será enviada na mensagem seguinte, com cada fala numerada e rotulada
"Aluno:" ou "Tutor:".
````

Create `prompts/analyze-tutor-feedback-v1.md`:

````markdown
# Prompt de análise — Feedback do Tutor (v1)

Você é um assistente que analisa a transcrição diarizada de uma aula particular de inglês entre
um Aluno e um Tutor (plataforma Cambly).

A transcrição vem com cada fala numerada, no formato `[N] Aluno: ...` ou `[N] Tutor: ...` — N é o
índice da fala (começando em 0), na ordem em que ocorreram.

Sua única tarefa é identificar observações que o **Tutor** fez sobre o desempenho do Aluno durante
a aula — elogios, críticas construtivas, sugestões de prática, ou qualquer comentário do Tutor
sobre como o Aluno está indo. Produza **apenas um objeto JSON**, sem nenhum texto antes ou depois,
seguindo exatamente este formato:

```json
{
  "tutor_feedback": [
    {"utterance_index": 0, "feedback": "..."}
  ]
}
```

## Regras

1. `utterance_index` é o N exato da fala do **Tutor** onde o feedback foi dado — copie o número
   que aparece entre colchetes na transcrição, nunca invente um índice.
2. `feedback` é um resumo em português do que o Tutor disse sobre o desempenho do Aluno.
3. Não confunda com correções pontuais de gramática/vocabulário (isso é assunto de outra tarefa)
   — aqui o foco é observações mais gerais sobre fluência, confiança, progresso, etc.
4. Se não houver nenhum feedback desse tipo, devolva uma lista vazia (`[]`) — nunca omita a chave.
5. A resposta deve ser **apenas o objeto JSON** acima: sem markdown, sem comentários, sem texto
   explicativo fora do JSON.

A transcrição da aula será enviada na mensagem seguinte.
````

Create `prompts/analyze-tutor-corrections-v1.md`:

````markdown
# Prompt de análise — Correções dadas pelo Tutor (v1)

Você é um assistente que analisa a transcrição diarizada de uma aula particular de inglês entre
um Aluno e um Tutor (plataforma Cambly).

A transcrição vem com cada fala numerada, no formato `[N] Aluno: ...` ou `[N] Tutor: ...` — N é o
índice da fala (começando em 0), na ordem em que ocorreram.

Sua única tarefa é identificar correções que o **próprio Tutor deu ao vivo**, durante a conversa —
quando o Tutor repete a frase do Aluno de forma corrigida, ou aponta diretamente um erro. Isso é
diferente de uma correção derivada por análise automática: aqui você só registra o que o Tutor
realmente disse na aula. Produza **apenas um objeto JSON**, sem nenhum texto antes ou depois,
seguindo exatamente este formato:

```json
{
  "tutor_corrections": [
    {"utterance_index": 0, "tutor_said": "...", "note": "..."}
  ]
}
```

## Regras

1. `utterance_index` é o N exato da fala do **Aluno** que o Tutor corrigiu — copie o número que
   aparece entre colchetes na transcrição, nunca invente um índice.
2. `tutor_said` é o que o Tutor disse ao corrigir (a forma correta que ele forneceu); `note` é uma
   nota curta em português sobre o que estava errado na fala original do Aluno.
3. Só inclua correções que o Tutor deu de fato na conversa — não invente correções que a análise
   automática identificaria mas que o Tutor não mencionou (isso é a tarefa de correções).
4. Se o Tutor não corrigiu nada ao vivo, devolva uma lista vazia (`[]`) — nunca omita a chave.
5. A resposta deve ser **apenas o objeto JSON** acima: sem markdown, sem comentários, sem texto
   explicativo fora do JSON.

A transcrição da aula será enviada na mensagem seguinte.
````

Create `prompts/analyze-topics-v1.md`:

````markdown
# Prompt de análise — Tópicos da aula (v1)

Você é um assistente que analisa a transcrição diarizada de uma aula particular de inglês entre
um Aluno e um Tutor (plataforma Cambly).

Sua única tarefa é listar os principais tópicos/assuntos discutidos ao longo da aula. Produza
**apenas um objeto JSON**, sem nenhum texto antes ou depois, seguindo exatamente este formato:

```json
{
  "topics": ["...", "..."]
}
```

## Regras

1. Cada item é um tópico curto em português (2-5 palavras), ex.: "planos de viagem", "trabalho
   remoto", "receitas de família".
2. Liste só os assuntos que de fato tomaram um trecho relevante da conversa — não liste
   comentários passageiros de uma frase só.
3. Não use uma taxonomia fixa nem categorias pré-definidas — a lista é livre, específica da aula
   (taxonomia hierárquica é assunto de fase futura).
4. Não repita o mesmo tópico com palavras diferentes.
5. Se não for possível identificar nenhum tópico claro, devolva uma lista vazia (`[]`).
6. A resposta deve ser **apenas o objeto JSON** acima: sem markdown, sem comentários, sem texto
   explicativo fora do JSON.

A transcrição da aula será enviada na mensagem seguinte, com cada fala numerada e rotulada
"Aluno:" ou "Tutor:".
````

- [ ] **Step 6: Run the test and confirm it passes**

Run: `go test ./prompts/... -v`
Expected: PASS.

- [ ] **Step 7: `go vet` and commit**

Run: `go vet ./prompts/...`
Expected: no output.

```bash
git add prompts/
git commit -m "feat: replace Phase 0's single prompt with Phase 2's 7 per-task prompts"
```

---

## Task 3: `internal/analysis` — task-agnostic Provider + task framework

**Files:**
- Modify: `internal/analysis/analysis.go`
- Modify: `internal/analysis/openai_compatible.go`
- Modify: `internal/analysis/parsing.go`
- Modify: `internal/analysis/transcript.go`
- Create: `internal/analysis/task.go`
- Modify: `internal/analysis/parsing_test.go` (rewritten)
- Modify: `internal/analysis/transcript_test.go` (numbering)
- Create: `internal/analysis/openai_compatible_test.go`
- Create: `internal/analysis/task_test.go`

**Interfaces:**
- Consumes: nothing from outside the package (`internal/stt.Utterance`, already used by `transcript.go`).
- Produces (used by Task 4):
  - `type Provider interface { Name() string; Complete(ctx context.Context, systemPrompt, transcript string) (json.RawMessage, error) }`
  - `func NewDeepSeekProvider(apiKey string) (Provider, error)`
  - `type TaskDef interface { Name() string; Version() int; Prompt() string; Execute(ctx context.Context, provider Provider, transcript string, utteranceCount int) (resultJSON, raw json.RawMessage, err error) }`
  - `type task[T any] struct { name string; version int; prompt string; parse func(json.RawMessage, int) (T, error) }` (implementa `TaskDef`)
  - `type anchored interface { UtteranceIndex() int }`
  - `func filterAnchored[T anchored](items []T, utteranceCount int) (kept []T, discarded int)`
  - `func mustLoadPrompt(filename string) string`
  - `func unmarshalJSON(raw json.RawMessage, v any) error`
  - `func FormatTranscript(utterances []stt.Utterance, speakerRoles map[string]string) (string, error)` — signature unchanged, only the output format changes (now numbered).

- [ ] **Step 1: Rewrite `transcript_test.go` expecting numbering (failing)**

Replace the content of `internal/analysis/transcript_test.go`:

```go
// internal/analysis/transcript_test.go
package analysis

import (
	"testing"

	"assistente-idiomas/internal/stt"
)

func syntheticUtterances() []stt.Utterance {
	return []stt.Utterance{
		{Speaker: "speaker_0", Text: "Hi, how was your week?"},
		{Speaker: "speaker_1", Text: "It was good, I felt a lot of saudade for my hometown though."},
		{Speaker: "speaker_0", Text: "That's understandable."},
	}
}

func TestFormatTranscript(t *testing.T) {
	roles := map[string]string{"speaker_0": "tutor", "speaker_1": "aluno"}

	got, err := FormatTranscript(syntheticUtterances(), roles)
	if err != nil {
		t.Fatalf("FormatTranscript retornou erro: %v", err)
	}

	want := "[0] Tutor: Hi, how was your week?\n" +
		"[1] Aluno: It was good, I felt a lot of saudade for my hometown though.\n" +
		"[2] Tutor: That's understandable.\n"
	if got != want {
		t.Errorf("FormatTranscript = %q, esperava %q", got, want)
	}
}

func TestFormatTranscript_MissingRole(t *testing.T) {
	roles := map[string]string{"speaker_0": "tutor"}

	_, err := FormatTranscript(syntheticUtterances(), roles)
	if err == nil {
		t.Fatal("esperava erro para locutor sem papel mapeado, obteve nil")
	}
}

func TestFormatTranscript_InvalidRole(t *testing.T) {
	roles := map[string]string{"speaker_0": "tutor", "speaker_1": "narrator"}

	_, err := FormatTranscript(syntheticUtterances(), roles)
	if err == nil {
		t.Fatal("esperava erro para papel inválido, obteve nil")
	}
}

func TestSpeakerExamples(t *testing.T) {
	got := SpeakerExamples(syntheticUtterances(), 1)

	if len(got["speaker_0"]) != 1 || got["speaker_0"][0] != "Hi, how was your week?" {
		t.Errorf("speaker_0 examples = %+v, inesperado", got["speaker_0"])
	}
	if len(got["speaker_1"]) != 1 || got["speaker_1"][0] != "It was good, I felt a lot of saudade for my hometown though." {
		t.Errorf("speaker_1 examples = %+v, inesperado", got["speaker_1"])
	}
}

func TestSpeakerExamples_LimitsToN(t *testing.T) {
	got := SpeakerExamples(syntheticUtterances(), 1)
	if len(got["speaker_0"]) != 1 {
		t.Errorf("esperava no máximo 1 exemplo por locutor, obteve %d", len(got["speaker_0"]))
	}
}
```

- [ ] **Step 2: Run and confirm only the numbering test fails**

Run: `go test ./internal/analysis/... -run TestFormatTranscript -v`
Expected: `TestFormatTranscript` FAILs (string without the `[N]` prefixes); the other functions
still compile and pass.

- [ ] **Step 3: Update `FormatTranscript` in `transcript.go`**

In `internal/analysis/transcript.go`, replace the `Fprintf` line inside the loop:

```go
		fmt.Fprintf(&b, "[%d] %s: %s\n", i, label, u.Text)
```

(it was `fmt.Fprintf(&b, "%s: %s\n", label, u.Text)`; the loop also needs to become `for i, u :=
range utterances` instead of `for _, u := range utterances` — that's the only other change in the
function.)

- [ ] **Step 4: Run again and confirm it passes**

Run: `go test ./internal/analysis/... -run TestFormatTranscript -v`
Expected: PASS.

- [ ] **Step 5: Rewrite `analysis.go` (task-agnostic Provider)**

Replace the content of `internal/analysis/analysis.go`:

```go
// internal/analysis/analysis.go
package analysis

import (
	"context"
	"encoding/json"
)

// Provider is the single interface implemented by each candidate LLM
// analysis service (DeepSeek, and in future slices: Anthropic, OpenAI,
// Gemini, GLM, Qwen — see
// docs/superpowers/specs/2026-07-19-analysis-llm-v1-design.md).
// Task-agnostic: it knows nothing about Correction/VocabularyItem/etc, it
// just trades a system prompt + the transcript for a raw JSON response —
// each TaskDef (see task.go) is what knows how to interpret that JSON.
type Provider interface {
	Name() string
	Complete(ctx context.Context, systemPrompt, transcript string) (json.RawMessage, error)
}
```

- [ ] **Step 6: Rewrite `parsing.go`**

Replace the content of `internal/analysis/parsing.go`:

```go
// internal/analysis/parsing.go
package analysis

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// stripTrailingCodeFence removes a leftover closing code fence (```) at
// the end of the content — the only mess possible when the provider uses
// prefill (see openai_compatible.go); a harmless no-op for providers
// without prefill (JSON already comes clean).
func stripTrailingCodeFence(raw []byte) []byte {
	trimmed := bytes.TrimSpace(raw)
	trimmed = bytes.TrimSuffix(trimmed, []byte("```"))
	return bytes.TrimSpace(trimmed)
}

// unmarshalJSON unmarshals raw (already stripped of the HTTP envelope and
// code fence — see Provider.Complete) into v. Shared by all 7 tasks; it
// knows nothing about the schema of any specific task.
func unmarshalJSON(raw json.RawMessage, v any) error {
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("analysis: json inválido: %w", err)
	}
	return nil
}
```

- [ ] **Step 7: Write the new `parsing_test.go` (replacing the old one)**

Replace the content of `internal/analysis/parsing_test.go`:

```go
// internal/analysis/parsing_test.go
package analysis

import (
	"encoding/json"
	"testing"
)

func TestStripTrailingCodeFence(t *testing.T) {
	got := stripTrailingCodeFence([]byte("{\"a\":1}\n```"))
	if string(got) != `{"a":1}` {
		t.Errorf("stripTrailingCodeFence = %q, esperado {\"a\":1}", got)
	}
}

func TestStripTrailingCodeFence_NoFenceIsNoop(t *testing.T) {
	got := stripTrailingCodeFence([]byte(`{"a":1}`))
	if string(got) != `{"a":1}` {
		t.Errorf("stripTrailingCodeFence = %q, esperado inalterado", got)
	}
}

func TestUnmarshalJSON_Valid(t *testing.T) {
	var v struct {
		A int `json:"a"`
	}
	if err := unmarshalJSON(json.RawMessage(`{"a":1}`), &v); err != nil {
		t.Fatalf("unmarshalJSON erro inesperado: %v", err)
	}
	if v.A != 1 {
		t.Errorf("v.A = %d, esperado 1", v.A)
	}
}

func TestUnmarshalJSON_Invalid(t *testing.T) {
	var v struct{ A int }
	if err := unmarshalJSON(json.RawMessage("not json"), &v); err == nil {
		t.Fatal("esperava erro para JSON inválido, obteve nil")
	}
}
```

- [ ] **Step 8: Confirm the package still doesn't compile (expected at this point)**

Run: `go test ./internal/analysis/... -run 'TestStripTrailingCodeFence|TestUnmarshalJSON' -v`
Expected: FAILs to compile — `openai_compatible.go` still references `Result`/`analysisJSON`/
`parseAnalysisResponse`, removed in Steps 5-6. This is expected: `analysis.go`, `parsing.go`, and
`openai_compatible.go` are a single interdependent block (the task-agnostic `Provider` only makes
sense with all three consistent at the same time); the next step finishes the rewrite and brings
the package back to compiling.

- [ ] **Step 9: Rewrite `openai_compatible.go`**

Replace the content of `internal/analysis/openai_compatible.go`:

```go
// internal/analysis/openai_compatible.go
package analysis

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// openAICompatibleProvider implements Provider for any service that
// exposes a /chat/completions endpoint in the OpenAI format. Today it's
// only used by DeepSeek; in future slices (see
// docs/superpowers/specs/2026-07-19-analysis-llm-v1-design.md) it may gain
// constructors for OpenAI, GLM, and Qwen, reusing this same type.
type openAICompatibleProvider struct {
	name            string
	baseURL         string
	apiKey          string
	model           string
	supportsPrefill bool
	client          *http.Client
}

func newOpenAICompatibleProvider(name, baseURL, apiKey, model string, supportsPrefill bool) (*openAICompatibleProvider, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("analysis: chave de API vazia para %s", name)
	}
	return &openAICompatibleProvider{
		name:            name,
		baseURL:         baseURL,
		apiKey:          apiKey,
		model:           model,
		supportsPrefill: supportsPrefill,
		client:          &http.Client{Timeout: 5 * time.Minute},
	}, nil
}

// NewDeepSeekProvider creates a Provider for the DeepSeek API, model
// deepseek-v4-flash (the cheapest tier — see "Slicing strategy" in the
// design doc). Uses the beta base URL, required by the "Chat Prefix
// Completion" feature that backs the ```json prefill.
func NewDeepSeekProvider(apiKey string) (Provider, error) {
	return newOpenAICompatibleProvider("deepseek", "https://api.deepseek.com/beta", apiKey, "deepseek-v4-flash", true)
}

func (p *openAICompatibleProvider) Name() string { return p.name }

// Complete sends systemPrompt + transcript and returns the raw content
// (already stripped of the HTTP envelope and code fence) the model
// produced — each TaskDef (task.go) is what knows the expected schema of
// that content.
func (p *openAICompatibleProvider) Complete(ctx context.Context, systemPrompt, transcript string) (json.RawMessage, error) {
	if systemPrompt == "" {
		return nil, fmt.Errorf("analysis: prompt de sistema vazio para %s", p.name)
	}

	req, err := p.buildRequest(ctx, systemPrompt, transcript)
	if err != nil {
		return nil, fmt.Errorf("analysis: montar requisição %s: %w", p.name, err)
	}

	raw, err := p.do(req)
	if err != nil {
		return nil, fmt.Errorf("analysis: chamar %s: %w", p.name, err)
	}

	var envelope openAICompatibleEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("analysis: parsear envelope %s: %w", p.name, err)
	}
	if len(envelope.Choices) == 0 {
		return nil, fmt.Errorf("analysis: %s não retornou choices", p.name)
	}

	slog.Info("analysis: chamada concluída", "provedor", p.name,
		"prompt_tokens", envelope.Usage.PromptTokens, "completion_tokens", envelope.Usage.CompletionTokens)

	return stripTrailingCodeFence([]byte(envelope.Choices[0].Message.Content)), nil
}

func (p *openAICompatibleProvider) buildRequest(ctx context.Context, systemPrompt, transcript string) (*http.Request, error) {
	messages := []chatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: transcript},
	}

	reqBody := chatCompletionRequest{
		Model:    p.model,
		Messages: messages,
	}

	if p.supportsPrefill {
		// DeepSeek rejects the combination response_format=json_object +
		// prefix (400 error "response_format json_object should not be used
		// with prefix", confirmed in a real call) — the prefill alone
		// already forces the content to start as JSON, so response_format
		// is left out when there's a prefill.
		reqBody.Messages = append(reqBody.Messages, chatMessage{
			Role:    "assistant",
			Content: "```json\n",
			Prefix:  true,
		})
		reqBody.Stop = []string{"```"}
	} else {
		reqBody.ResponseFormat = &responseFormat{Type: "json_object"}
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

// do executes the request and returns the response body, with an error if
// the status isn't 2xx (the message includes status and body, for
// debugging).
func (p *openAICompatibleProvider) do(req *http.Request) ([]byte, error) {
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}
	return respBody, nil
}

type chatCompletionRequest struct {
	Model          string          `json:"model"`
	Messages       []chatMessage   `json:"messages"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
	Stop           []string        `json:"stop,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Prefix  bool   `json:"prefix,omitempty"`
}

type responseFormat struct {
	Type string `json:"type"`
}

type openAICompatibleEnvelope struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}
```

- [ ] **Step 10: Write `openai_compatible_test.go`**

```go
// internal/analysis/openai_compatible_test.go
package analysis

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAICompatibleProvider_Complete_SendsSystemPromptPerCall(t *testing.T) {
	var capturedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"ok\":true}"}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`))
	}))
	defer server.Close()

	p, err := newOpenAICompatibleProvider("fake", server.URL, "key", "fake-model", false)
	if err != nil {
		t.Fatalf("newOpenAICompatibleProvider erro: %v", err)
	}

	raw, err := p.Complete(context.Background(), "system prompt A", "transcript A")
	if err != nil {
		t.Fatalf("Complete erro: %v", err)
	}
	if string(raw) != `{"ok":true}` {
		t.Errorf("raw = %s, inesperado", raw)
	}

	messages, _ := capturedBody["messages"].([]any)
	if len(messages) < 1 {
		t.Fatal("esperava ao menos 1 mensagem no corpo")
	}
	first, _ := messages[0].(map[string]any)
	if first["role"] != "system" || first["content"] != "system prompt A" {
		t.Errorf("primeira mensagem = %+v, esperado role=system content=\"system prompt A\"", first)
	}

	if _, err := p.Complete(context.Background(), "system prompt B", "transcript B"); err != nil {
		t.Fatalf("segunda Complete erro: %v", err)
	}
	messages2, _ := capturedBody["messages"].([]any)
	first2, _ := messages2[0].(map[string]any)
	if first2["content"] != "system prompt B" {
		t.Errorf("segunda chamada content = %v, esperado \"system prompt B\"", first2["content"])
	}
}

func TestOpenAICompatibleProvider_Complete_EmptySystemPrompt(t *testing.T) {
	p, err := newOpenAICompatibleProvider("fake", "http://example.invalid", "key", "fake-model", false)
	if err != nil {
		t.Fatalf("newOpenAICompatibleProvider erro: %v", err)
	}
	if _, err := p.Complete(context.Background(), "", "transcript"); err == nil {
		t.Fatal("esperava erro para systemPrompt vazio, obteve nil")
	}
}
```

With that, `analysis.go`, `parsing.go`, and `openai_compatible.go` become consistent with each
other again — confirm by running:

Run: `go test ./internal/analysis/... -v`
Expected: PASS on all tests in the package (the package compiles again; `task.go` doesn't exist
yet, but nothing up to this point references it).

- [ ] **Step 11: Create `internal/analysis/task.go`**

```go
// internal/analysis/task.go
package analysis

import (
	"context"
	"encoding/json"
	"fmt"

	"assistente-idiomas/prompts"
)

// TaskDef is the common interface for the 7 analysis tasks — it lets them
// all be iterated over a single list (var Tasks, see Task 4 of this plan)
// even though each one has a different result type (generics don't allow a
// slice of task[T] with a variable T, hence this non-generic interface on
// top).
type TaskDef interface {
	Name() string   // e.g.: "analyze_corrections" — same value stored in prompts.name and analysis_results.task
	Version() int   // prompt version (manual bump in code when the .md content changes)
	Prompt() string // prompt content (embed.FS)

	// Execute calls provider.Complete, parses it, and (when the task is
	// anchored) discards items with an invalid utterance_index. Returns the
	// already-validated JSON (ready to store in
	// analysis_results.result_json) and the raw content returned by the
	// provider (ready to write to disk, raw_response_path). err != nil
	// doesn't stop the caller from writing raw to disk (same principle as
	// runTranscribe: the call already cost money).
	Execute(ctx context.Context, provider Provider, transcript string, utteranceCount int) (resultJSON json.RawMessage, raw json.RawMessage, err error)
}

type task[T any] struct {
	name    string
	version int
	prompt  string
	parse   func(raw json.RawMessage, utteranceCount int) (T, error)
}

func (t task[T]) Name() string   { return t.name }
func (t task[T]) Version() int   { return t.version }
func (t task[T]) Prompt() string { return t.prompt }

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
// TutorFeedbackItem, see Task 4) — a negative UtteranceIndex represents
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

// mustLoadPrompt reads a prompt embedded in prompts.FS (prompts/embed.go,
// Task 2) — panicking when it's missing is intentional: a missing prompt is
// a build/packaging error, not a runtime condition to handle gracefully
// (same spirit as a template.Must).
func mustLoadPrompt(filename string) string {
	b, err := prompts.FS.ReadFile(filename)
	if err != nil {
		panic(fmt.Sprintf("analysis: prompt %s não encontrado: %v", filename, err))
	}
	return string(b)
}
```

- [ ] **Step 12: Write `task_test.go`**

```go
// internal/analysis/task_test.go
package analysis

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type testAnchoredItem struct {
	idx int
}

func (i testAnchoredItem) UtteranceIndex() int { return i.idx }

func TestFilterAnchored_DropsOutOfRange(t *testing.T) {
	items := []testAnchoredItem{{idx: 0}, {idx: 5}, {idx: -1}, {idx: 2}}
	kept, discarded := filterAnchored(items, 3)
	if len(kept) != 2 || kept[0].idx != 0 || kept[1].idx != 2 {
		t.Errorf("kept = %+v, esperado índices 0 e 2", kept)
	}
	if discarded != 2 {
		t.Errorf("discarded = %d, esperado 2", discarded)
	}
}

func TestFilterAnchored_KeepsAllWhenValid(t *testing.T) {
	items := []testAnchoredItem{{idx: 0}, {idx: 1}}
	kept, discarded := filterAnchored(items, 2)
	if len(kept) != 2 {
		t.Errorf("kept = %+v, esperado os 2 itens", kept)
	}
	if discarded != 0 {
		t.Errorf("discarded = %d, esperado 0", discarded)
	}
}

func TestMustLoadPrompt_PanicsWhenMissing(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("esperava panic para prompt inexistente, não houve panic")
		}
	}()
	mustLoadPrompt("nao-existe-v99.md")
}

type fakeProvider struct {
	content json.RawMessage
	err     error
}

func (f fakeProvider) Name() string { return "fake" }
func (f fakeProvider) Complete(ctx context.Context, systemPrompt, transcript string) (json.RawMessage, error) {
	return f.content, f.err
}

func TestTaskExecute_ReturnsParsedResultAndRaw(t *testing.T) {
	parseCalls := 0
	tk := task[[]string]{
		name: "fake_task", version: 1, prompt: "system prompt",
		parse: func(raw json.RawMessage, utteranceCount int) ([]string, error) {
			parseCalls++
			var out []string
			if err := json.Unmarshal(raw, &out); err != nil {
				return nil, err
			}
			return out, nil
		},
	}
	provider := fakeProvider{content: json.RawMessage(`["a","b"]`)}
	resultJSON, raw, err := tk.Execute(context.Background(), provider, "transcript", 3)
	if err != nil {
		t.Fatalf("Execute erro inesperado: %v", err)
	}
	if string(raw) != `["a","b"]` {
		t.Errorf("raw = %s, inesperado", raw)
	}
	if string(resultJSON) != `["a","b"]` {
		t.Errorf("resultJSON = %s, inesperado", resultJSON)
	}
	if parseCalls != 1 {
		t.Errorf("parse chamado %d vezes, esperado 1", parseCalls)
	}
}

func TestTaskExecute_ProviderErrorPreservesRaw(t *testing.T) {
	tk := task[[]string]{
		name: "fake_task", version: 1, prompt: "p",
		parse: func(raw json.RawMessage, n int) ([]string, error) { return nil, nil },
	}
	provider := fakeProvider{content: json.RawMessage(`partial`), err: errors.New("boom")}
	resultJSON, raw, err := tk.Execute(context.Background(), provider, "t", 1)
	if err == nil {
		t.Fatal("esperava erro, obteve nil")
	}
	if resultJSON != nil {
		t.Errorf("resultJSON = %s, esperado nil em caso de erro", resultJSON)
	}
	if string(raw) != "partial" {
		t.Errorf("raw = %s, esperado preservado mesmo com erro", raw)
	}
}
```

- [ ] **Step 13: Run all tests in the package and confirm they pass**

Run: `go test ./internal/analysis/... -v`
Expected: PASS on all tests (the package now compiles end to end — `var Tasks` still doesn't
exist, but nothing in this package references it yet; that only arrives in Task 4).

- [ ] **Step 14: `go vet` and commit**

Run: `go vet ./internal/analysis/...`
Expected: no output.

```bash
git add internal/analysis/ prompts/
git commit -m "refactor: make analysis.Provider task-agnostic and introduce the TaskDef framework"
```

---

## Task 4: The 7 concrete tasks + `RegisterPrompts`

**Files:**
- Create: `internal/analysis/tasks_corrections.go` + `tasks_corrections_test.go`
- Create: `internal/analysis/tasks_vocabulary.go` + `tasks_vocabulary_test.go`
- Create: `internal/analysis/tasks_tutor_expressions.go` + `tasks_tutor_expressions_test.go`
- Create: `internal/analysis/tasks_tutor_taught_terms.go` + `tasks_tutor_taught_terms_test.go`
- Create: `internal/analysis/tasks_tutor_feedback.go` + `tasks_tutor_feedback_test.go`
- Create: `internal/analysis/tasks_tutor_corrections.go` + `tasks_tutor_corrections_test.go`
- Create: `internal/analysis/tasks_topics.go` + `tasks_topics_test.go`
- Create: `internal/analysis/tasks.go` (`var Tasks`)
- Create: `internal/analysis/prompts.go` (`RegisterPrompts`)
- Create: `internal/analysis/prompts_test.go`

**Interfaces:**
- Consumes: `TaskDef`/`task[T]`/`anchored`/`filterAnchored`/`mustLoadPrompt`/`unmarshalJSON` (Task
  3); `db.UpsertPrompt` (Task 1); the 7 `.md` files via `mustLoadPrompt` (Task 2).
- Produces (used by Task 5 and by `cmd/validate-analysis` in Task 6):
  - `var Tasks []TaskDef` with the 7 tasks.
  - `func RegisterPrompts(conn *sql.DB) error`
  - Item types: `Correction`, `VocabularyItem`, `Expression`, `TutorTaughtTerm`,
    `TutorFeedbackItem`, `TutorCorrection` (topics use `[]string`, with no dedicated struct).

- [ ] **Step 1: Write `tasks_corrections.go` + test**

```go
// internal/analysis/tasks_corrections.go
package analysis

import (
	"encoding/json"
	"log/slog"
)

// Correction is a correction to a student utterance, derived by the
// analysis (as opposed to TutorCorrection, given live by the Tutor
// themselves).
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

```go
// internal/analysis/tasks_corrections_test.go
package analysis

import (
	"encoding/json"
	"testing"
)

func TestParseCorrections_Valid(t *testing.T) {
	raw := json.RawMessage(`{"corrections":[{"utterance_index":1,"original":"I go yesterday","correction":"I went yesterday","explanation":"Passado simples irregular."}]}`)
	got, err := parseCorrections(raw, 3)
	if err != nil {
		t.Fatalf("parseCorrections erro inesperado: %v", err)
	}
	if len(got) != 1 || got[0].Original != "I go yesterday" || got[0].CorrectionTx != "I went yesterday" {
		t.Errorf("got = %+v, inesperado", got)
	}
}

func TestParseCorrections_DropsOutOfRangeIndex(t *testing.T) {
	raw := json.RawMessage(`{"corrections":[{"utterance_index":0,"original":"a","correction":"b","explanation":"c"},{"utterance_index":99,"original":"x","correction":"y","explanation":"z"}]}`)
	got, err := parseCorrections(raw, 1)
	if err != nil {
		t.Fatalf("parseCorrections erro inesperado: %v", err)
	}
	if len(got) != 1 || got[0].Original != "a" {
		t.Errorf("got = %+v, esperado só o item com índice válido", got)
	}
}

func TestParseCorrections_InvalidJSON(t *testing.T) {
	if _, err := parseCorrections(json.RawMessage("not json"), 3); err == nil {
		t.Fatal("esperava erro para JSON inválido, obteve nil")
	}
}

func TestNewCorrectionsTask_HasNameAndPrompt(t *testing.T) {
	tk := newCorrectionsTask()
	if tk.Name() != "analyze_corrections" {
		t.Errorf("Name() = %q, esperado analyze_corrections", tk.Name())
	}
	if tk.Prompt() == "" {
		t.Error("Prompt() vazio, esperado conteúdo carregado do .md")
	}
}
```

- [ ] **Step 2: Run and confirm it passes**

Run: `go test ./internal/analysis/... -run 'Corrections' -v`
Expected: PASS.

- [ ] **Step 3: Write `tasks_vocabulary.go` + test**

```go
// internal/analysis/tasks_vocabulary.go
package analysis

import "encoding/json"

// VocabularyItem is a new word or expression for the student to learn —
// includes PT/ES words used as a native-language fallback, never treated
// as an English mistake (see analyze-corrections-v1.md).
type VocabularyItem struct {
	Term        string `json:"term"`
	Translation string `json:"translation"`
}

func parseVocabulary(raw json.RawMessage, utteranceCount int) ([]VocabularyItem, error) {
	var parsed struct {
		Vocabulary []VocabularyItem `json:"vocabulary"`
	}
	if err := unmarshalJSON(raw, &parsed); err != nil {
		return nil, err
	}
	return parsed.Vocabulary, nil
}

func newVocabularyTask() TaskDef {
	return task[[]VocabularyItem]{name: "analyze_vocabulary", version: 1, prompt: mustLoadPrompt("analyze-vocabulary-v1.md"), parse: parseVocabulary}
}
```

```go
// internal/analysis/tasks_vocabulary_test.go
package analysis

import (
	"encoding/json"
	"testing"
)

func TestParseVocabulary_Valid(t *testing.T) {
	raw := json.RawMessage(`{"vocabulary":[{"term":"homesick","translation":"com saudade de casa"}]}`)
	got, err := parseVocabulary(raw, 3)
	if err != nil {
		t.Fatalf("parseVocabulary erro inesperado: %v", err)
	}
	if len(got) != 1 || got[0].Term != "homesick" || got[0].Translation != "com saudade de casa" {
		t.Errorf("got = %+v, inesperado", got)
	}
}

func TestParseVocabulary_EmptyList(t *testing.T) {
	got, err := parseVocabulary(json.RawMessage(`{"vocabulary":[]}`), 3)
	if err != nil {
		t.Fatalf("parseVocabulary erro inesperado: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got = %+v, esperado lista vazia", got)
	}
}

func TestParseVocabulary_InvalidJSON(t *testing.T) {
	if _, err := parseVocabulary(json.RawMessage("not json"), 3); err == nil {
		t.Fatal("esperava erro para JSON inválido, obteve nil")
	}
}
```

- [ ] **Step 4: Run and confirm it passes**

Run: `go test ./internal/analysis/... -run 'Vocabulary' -v`
Expected: PASS.

- [ ] **Step 5: Write `tasks_tutor_expressions.go` + test**

```go
// internal/analysis/tasks_tutor_expressions.go
package analysis

import "encoding/json"

// Expression is an expression the Tutor used naturally in the conversation
// that's worth the student reusing — distinct from TutorTaughtTerm (a term
// the Tutor explicitly explained/taught).
type Expression struct {
	Text string `json:"text"`
	Note string `json:"note"`
}

func parseTutorExpressions(raw json.RawMessage, utteranceCount int) ([]Expression, error) {
	var parsed struct {
		TutorExpressions []Expression `json:"tutor_expressions"`
	}
	if err := unmarshalJSON(raw, &parsed); err != nil {
		return nil, err
	}
	return parsed.TutorExpressions, nil
}

func newTutorExpressionsTask() TaskDef {
	return task[[]Expression]{name: "analyze_tutor_expressions", version: 1, prompt: mustLoadPrompt("analyze-tutor-expressions-v1.md"), parse: parseTutorExpressions}
}
```

```go
// internal/analysis/tasks_tutor_expressions_test.go
package analysis

import (
	"encoding/json"
	"testing"
)

func TestParseTutorExpressions_Valid(t *testing.T) {
	raw := json.RawMessage(`{"tutor_expressions":[{"text":"let's circle back to that","note":"retomar um assunto depois"}]}`)
	got, err := parseTutorExpressions(raw, 3)
	if err != nil {
		t.Fatalf("parseTutorExpressions erro inesperado: %v", err)
	}
	if len(got) != 1 || got[0].Text != "let's circle back to that" {
		t.Errorf("got = %+v, inesperado", got)
	}
}

func TestParseTutorExpressions_InvalidJSON(t *testing.T) {
	if _, err := parseTutorExpressions(json.RawMessage("not json"), 3); err == nil {
		t.Fatal("esperava erro para JSON inválido, obteve nil")
	}
}
```

- [ ] **Step 6: Run and confirm it passes**

Run: `go test ./internal/analysis/... -run 'TutorExpressions' -v`
Expected: PASS.

- [ ] **Step 7: Write `tasks_tutor_taught_terms.go` + test**

```go
// internal/analysis/tasks_tutor_taught_terms.go
package analysis

import "encoding/json"

// TutorTaughtTerm is a term/expression the Tutor explicitly explained or
// taught during the lesson (as opposed to Expression, which is just
// natural use in the conversation).
type TutorTaughtTerm struct {
	Term        string `json:"term"`
	Translation string `json:"translation"`
	Context     string `json:"context"`
}

func parseTutorTaughtTerms(raw json.RawMessage, utteranceCount int) ([]TutorTaughtTerm, error) {
	var parsed struct {
		TutorTaughtTerms []TutorTaughtTerm `json:"tutor_taught_terms"`
	}
	if err := unmarshalJSON(raw, &parsed); err != nil {
		return nil, err
	}
	return parsed.TutorTaughtTerms, nil
}

func newTutorTaughtTermsTask() TaskDef {
	return task[[]TutorTaughtTerm]{name: "analyze_tutor_taught_terms", version: 1, prompt: mustLoadPrompt("analyze-tutor-taught-terms-v1.md"), parse: parseTutorTaughtTerms}
}
```

```go
// internal/analysis/tasks_tutor_taught_terms_test.go
package analysis

import (
	"encoding/json"
	"testing"
)

func TestParseTutorTaughtTerms_Valid(t *testing.T) {
	raw := json.RawMessage(`{"tutor_taught_terms":[{"term":"to bring up","translation":"trazer à tona","context":"o tutor explicou ao introduzir um assunto novo"}]}`)
	got, err := parseTutorTaughtTerms(raw, 3)
	if err != nil {
		t.Fatalf("parseTutorTaughtTerms erro inesperado: %v", err)
	}
	if len(got) != 1 || got[0].Term != "to bring up" || got[0].Context == "" {
		t.Errorf("got = %+v, inesperado", got)
	}
}

func TestParseTutorTaughtTerms_InvalidJSON(t *testing.T) {
	if _, err := parseTutorTaughtTerms(json.RawMessage("not json"), 3); err == nil {
		t.Fatal("esperava erro para JSON inválido, obteve nil")
	}
}
```

- [ ] **Step 8: Run and confirm it passes**

Run: `go test ./internal/analysis/... -run 'TutorTaughtTerms' -v`
Expected: PASS.

- [ ] **Step 9: Write `tasks_tutor_feedback.go` + test**

```go
// internal/analysis/tasks_tutor_feedback.go
package analysis

import (
	"encoding/json"
	"log/slog"
)

// TutorFeedbackItem is an observation the Tutor made about the student's
// performance, anchored to the Tutor utterance where it was given.
type TutorFeedbackItem struct {
	UtteranceIdx int    `json:"utterance_index"`
	Feedback     string `json:"feedback"`
}

func (f TutorFeedbackItem) UtteranceIndex() int { return f.UtteranceIdx }

func parseTutorFeedback(raw json.RawMessage, utteranceCount int) ([]TutorFeedbackItem, error) {
	var parsed struct {
		TutorFeedback []TutorFeedbackItem `json:"tutor_feedback"`
	}
	if err := unmarshalJSON(raw, &parsed); err != nil {
		return nil, err
	}
	kept, discarded := filterAnchored(parsed.TutorFeedback, utteranceCount)
	if discarded > 0 {
		slog.Warn("analysis: itens descartados por utterance_index inválido", "tarefa", "analyze_tutor_feedback", "descartados", discarded)
	}
	return kept, nil
}

func newTutorFeedbackTask() TaskDef {
	return task[[]TutorFeedbackItem]{name: "analyze_tutor_feedback", version: 1, prompt: mustLoadPrompt("analyze-tutor-feedback-v1.md"), parse: parseTutorFeedback}
}
```

```go
// internal/analysis/tasks_tutor_feedback_test.go
package analysis

import (
	"encoding/json"
	"testing"
)

func TestParseTutorFeedback_Valid(t *testing.T) {
	raw := json.RawMessage(`{"tutor_feedback":[{"utterance_index":2,"feedback":"Fluência melhorou bastante nas últimas aulas."}]}`)
	got, err := parseTutorFeedback(raw, 3)
	if err != nil {
		t.Fatalf("parseTutorFeedback erro inesperado: %v", err)
	}
	if len(got) != 1 || got[0].Feedback == "" {
		t.Errorf("got = %+v, inesperado", got)
	}
}

func TestParseTutorFeedback_DropsOutOfRangeIndex(t *testing.T) {
	raw := json.RawMessage(`{"tutor_feedback":[{"utterance_index":50,"feedback":"x"}]}`)
	got, err := parseTutorFeedback(raw, 3)
	if err != nil {
		t.Fatalf("parseTutorFeedback erro inesperado: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got = %+v, esperado vazio (índice fora do range)", got)
	}
}

func TestParseTutorFeedback_InvalidJSON(t *testing.T) {
	if _, err := parseTutorFeedback(json.RawMessage("not json"), 3); err == nil {
		t.Fatal("esperava erro para JSON inválido, obteve nil")
	}
}
```

- [ ] **Step 10: Run and confirm it passes**

Run: `go test ./internal/analysis/... -run 'TutorFeedback' -v`
Expected: PASS.

- [ ] **Step 11: Write `tasks_tutor_corrections.go` + test**

```go
// internal/analysis/tasks_tutor_corrections.go
package analysis

import (
	"encoding/json"
	"log/slog"
)

// TutorCorrection is a correction the Tutor themselves gave the student
// during the lesson (live, in the conversation) — different from
// Correction (derived by the analysis), even though both may point to the
// same utterance.
type TutorCorrection struct {
	UtteranceIdx int    `json:"utterance_index"`
	TutorSaid    string `json:"tutor_said"`
	Note         string `json:"note"`
}

func (c TutorCorrection) UtteranceIndex() int { return c.UtteranceIdx }

func parseTutorCorrections(raw json.RawMessage, utteranceCount int) ([]TutorCorrection, error) {
	var parsed struct {
		TutorCorrections []TutorCorrection `json:"tutor_corrections"`
	}
	if err := unmarshalJSON(raw, &parsed); err != nil {
		return nil, err
	}
	kept, discarded := filterAnchored(parsed.TutorCorrections, utteranceCount)
	if discarded > 0 {
		slog.Warn("analysis: itens descartados por utterance_index inválido", "tarefa", "analyze_tutor_corrections", "descartados", discarded)
	}
	return kept, nil
}

func newTutorCorrectionsTask() TaskDef {
	return task[[]TutorCorrection]{name: "analyze_tutor_corrections", version: 1, prompt: mustLoadPrompt("analyze-tutor-corrections-v1.md"), parse: parseTutorCorrections}
}
```

```go
// internal/analysis/tasks_tutor_corrections_test.go
package analysis

import (
	"encoding/json"
	"testing"
)

func TestParseTutorCorrections_Valid(t *testing.T) {
	raw := json.RawMessage(`{"tutor_corrections":[{"utterance_index":1,"tutor_said":"I went there","note":"Aluno usou o tempo verbal errado"}]}`)
	got, err := parseTutorCorrections(raw, 3)
	if err != nil {
		t.Fatalf("parseTutorCorrections erro inesperado: %v", err)
	}
	if len(got) != 1 || got[0].TutorSaid != "I went there" {
		t.Errorf("got = %+v, inesperado", got)
	}
}

func TestParseTutorCorrections_DropsOutOfRangeIndex(t *testing.T) {
	raw := json.RawMessage(`{"tutor_corrections":[{"utterance_index":-1,"tutor_said":"x","note":"y"}]}`)
	got, err := parseTutorCorrections(raw, 3)
	if err != nil {
		t.Fatalf("parseTutorCorrections erro inesperado: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got = %+v, esperado vazio (índice negativo)", got)
	}
}

func TestParseTutorCorrections_InvalidJSON(t *testing.T) {
	if _, err := parseTutorCorrections(json.RawMessage("not json"), 3); err == nil {
		t.Fatal("esperava erro para JSON inválido, obteve nil")
	}
}
```

- [ ] **Step 12: Run and confirm it passes**

Run: `go test ./internal/analysis/... -run 'TutorCorrections' -v`
Expected: PASS.

- [ ] **Step 13: Write `tasks_topics.go` + test**

```go
// internal/analysis/tasks_topics.go
package analysis

import "encoding/json"

func parseTopics(raw json.RawMessage, utteranceCount int) ([]string, error) {
	var parsed struct {
		Topics []string `json:"topics"`
	}
	if err := unmarshalJSON(raw, &parsed); err != nil {
		return nil, err
	}
	return parsed.Topics, nil
}

func newTopicsTask() TaskDef {
	return task[[]string]{name: "analyze_topics", version: 1, prompt: mustLoadPrompt("analyze-topics-v1.md"), parse: parseTopics}
}
```

```go
// internal/analysis/tasks_topics_test.go
package analysis

import (
	"encoding/json"
	"testing"
)

func TestParseTopics_Valid(t *testing.T) {
	raw := json.RawMessage(`{"topics":["planos de viagem","trabalho remoto"]}`)
	got, err := parseTopics(raw, 3)
	if err != nil {
		t.Fatalf("parseTopics erro inesperado: %v", err)
	}
	if len(got) != 2 || got[0] != "planos de viagem" {
		t.Errorf("got = %+v, inesperado", got)
	}
}

func TestParseTopics_InvalidJSON(t *testing.T) {
	if _, err := parseTopics(json.RawMessage("not json"), 3); err == nil {
		t.Fatal("esperava erro para JSON inválido, obteve nil")
	}
}
```

- [ ] **Step 14: Run and confirm it passes**

Run: `go test ./internal/analysis/... -run 'Topics' -v`
Expected: PASS.

- [ ] **Step 15: Create `tasks.go` with `var Tasks`**

```go
// internal/analysis/tasks.go
package analysis

// Tasks lists the 7 Phase 2 analysis tasks — order doesn't matter for
// execution (they're independent of each other), only for human readability
// and for RegisterPrompts (prompts.go).
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

- [ ] **Step 16: Write `prompts_test.go` (failing — `RegisterPrompts` doesn't exist yet)**

```go
// internal/analysis/prompts_test.go
package analysis

import (
	"path/filepath"
	"testing"

	"assistente-idiomas/internal/db"
)

func TestRegisterPrompts_InsertsAllTasksAndIsIdempotent(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	if err := RegisterPrompts(conn); err != nil {
		t.Fatalf("RegisterPrompts() erro inesperado: %v", err)
	}
	if err := RegisterPrompts(conn); err != nil {
		t.Fatalf("segunda RegisterPrompts() erro inesperado: %v", err)
	}

	var count int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM prompts`).Scan(&count); err != nil {
		t.Fatalf("contar prompts falhou: %v", err)
	}
	if count != len(Tasks) {
		t.Errorf("count = %d, esperado %d (um por tarefa, sem duplicar na segunda chamada)", count, len(Tasks))
	}

	for _, tk := range Tasks {
		var version int
		err := conn.QueryRow(`SELECT version FROM prompts WHERE name = ?`, tk.Name()).Scan(&version)
		if err != nil {
			t.Errorf("prompt %q não encontrado: %v", tk.Name(), err)
			continue
		}
		if version != tk.Version() {
			t.Errorf("prompt %q: version = %d, esperado %d", tk.Name(), version, tk.Version())
		}
	}
}
```

- [ ] **Step 17: Run and confirm it fails (`RegisterPrompts` doesn't exist)**

Run: `go test ./internal/analysis/... -run TestRegisterPrompts -v`
Expected: FAIL — `undefined: RegisterPrompts`.

- [ ] **Step 18: Create `prompts.go` with `RegisterPrompts`**

```go
// internal/analysis/prompts.go
package analysis

import (
	"database/sql"
	"fmt"

	"assistente-idiomas/internal/db"
)

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

- [ ] **Step 19: Run again and confirm it passes**

Run: `go test ./internal/analysis/... -v`
Expected: PASS on all tests in the package (now with the 7 tasks + `RegisterPrompts`).

- [ ] **Step 20: `go build`, `go vet` for the whole module, and commit**

Run: `go build ./... && go vet ./...`
Expected: no errors (confirms that `internal/db` and `internal/analysis` remain compatible with
each other and with the rest of the module).

```bash
git add internal/analysis/
git commit -m "feat: implement the 7 analysis tasks and the prompt registry"
```

---

## Task 5: `main.go` — register the prompts at startup

**Files:**
- Modify: `main.go:1-32`

**Interfaces:**
- Consumes: `analysis.RegisterPrompts(conn *sql.DB) error` (Task 4).
- Produces: nothing consumed by another task in this plan.

- [ ] **Step 1: Add the import and the call**

In `main.go`, add the import (alphabetical order, alongside the other `internal/` ones):

```go
	"assistente-idiomas/internal/analysis"
```

And right after `defer conn.Close()` (line 32), before the `storageRoot := ...` block:

```go
	if err := analysis.RegisterPrompts(conn); err != nil {
		log.Fatalf("registrar prompts de análise: %v", err)
	}
```

A failure to register prompts is fatal (the same treatment a `db.Open` failure already gets
above) — there's no way for the Phase 2 queue (Story 2) to work without the prompts in the table,
and failing early is preferable to only discovering this when the first analysis job runs.

- [ ] **Step 2: Build and vet the whole module**

Run: `go build ./... && go vet ./...`
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add main.go
git commit -m "feat: register the analysis prompts on app startup"
```

---

## Task 6: `cmd/validate-analysis` — temporary manual-validation CLI

**Files:**
- Create: `cmd/validate-analysis/main.go`

**Interfaces:**
- Consumes: `db.Open`, `db.FindTranscriptByLessonID` (already existing); `analysis.Tasks`,
  `analysis.NewDeepSeekProvider`, `analysis.FormatTranscript`, `analysis.SpeakerExamples`,
  `analysis.TaskDef.Execute` (Task 4); `config.DBPath` (already existing).
- Produces: nothing consumed by app code — just files under
  `local/output/analysis-validation/<task>/{raw.json,result.json}`, used manually in Task 7.

- [ ] **Step 1: Create `cmd/validate-analysis/main.go`**

```go
// cmd/validate-analysis/main.go
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"assistente-idiomas/internal/analysis"
	"assistente-idiomas/internal/config"
	"assistente-idiomas/internal/db"
	"assistente-idiomas/internal/stt"
)

// validate-analysis runs the 7 analysis tasks (Phase 2, Story 1) against
// a real, already-transcribed lesson, to inspect quality and cost before
// wiring this into the Worker (Story 2). Temporary tool — see "Scope
// decisions" in
// docs/superpowers/specs/2026-07-30-fase2-historia-1-prompts-persistencia-design.md;
// removed after recording the findings in docs/notas-analise-llm.md (Task
// 7 of this plan), the same fate as the defunct cmd/spike (Phase 0).
func main() {
	if err := loadDotEnv(".env"); err != nil {
		log.Fatalf("carregar .env: %v", err)
	}

	defaultDBPath, err := config.DBPath()
	if err != nil {
		log.Fatalf("resolver caminho padrão do banco: %v", err)
	}

	dbPath := flag.String("db", defaultDBPath, "caminho do banco SQLite do app")
	lessonID := flag.Int64("lesson-id", 0, "id da lesson (já transcrita) a analisar")
	outDir := flag.String("out", "local/output/analysis-validation", "diretório de saída")
	flag.Parse()

	if *lessonID == 0 {
		log.Fatal("informe -lesson-id")
	}

	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		log.Fatal("DEEPSEEK_API_KEY não definida (defina no ambiente ou em .env)")
	}

	conn, err := db.Open(*dbPath)
	if err != nil {
		log.Fatalf("abrir banco %s: %v", *dbPath, err)
	}
	defer conn.Close()

	tr, err := db.FindTranscriptByLessonID(conn, *lessonID)
	if err != nil {
		log.Fatalf("buscar transcrição da lesson %d: %v", *lessonID, err)
	}
	if tr == nil {
		log.Fatalf("lesson %d não tem transcrição gravada ainda", *lessonID)
	}

	speakerRoles, err := confirmSpeakerRoles(tr.Utterances)
	if err != nil {
		log.Fatalf("confirmar papéis dos locutores: %v", err)
	}

	transcript, err := analysis.FormatTranscript(tr.Utterances, speakerRoles)
	if err != nil {
		log.Fatalf("formatar transcrição: %v", err)
	}

	provider, err := analysis.NewDeepSeekProvider(apiKey)
	if err != nil {
		log.Fatalf("criar provedor de análise: %v", err)
	}

	ctx := context.Background()
	hadFailure := false
	for _, tk := range analysis.Tasks {
		if !runTask(ctx, tk, provider, transcript, len(tr.Utterances), *outDir) {
			hadFailure = true
		}
	}
	if hadFailure {
		os.Exit(1)
	}
}

func runTask(ctx context.Context, tk analysis.TaskDef, provider analysis.Provider, transcript string, utteranceCount int, outDir string) bool {
	taskDir := filepath.Join(outDir, tk.Name())
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		slog.Error("criar diretório de saída", "tarefa", tk.Name(), "erro", err)
		return false
	}

	resultJSON, raw, err := tk.Execute(ctx, provider, transcript, utteranceCount)
	if len(raw) > 0 {
		if writeErr := os.WriteFile(filepath.Join(taskDir, "raw.json"), raw, 0o644); writeErr != nil {
			slog.Error("gravar raw.json", "tarefa", tk.Name(), "erro", writeErr)
		}
	}
	if err != nil {
		slog.Error("tarefa falhou", "tarefa", tk.Name(), "erro", err)
		return false
	}

	if err := os.WriteFile(filepath.Join(taskDir, "result.json"), resultJSON, 0o644); err != nil {
		slog.Error("gravar result.json", "tarefa", tk.Name(), "erro", err)
		return false
	}

	slog.Info("tarefa concluída", "tarefa", tk.Name())
	return true
}

// confirmSpeakerRoles shows up to 3 example utterances per speaker and
// asks the user, via stdin, which of the two is the student — the other
// becomes the Tutor (a Cambly lesson is always 1:1). Copied from the
// defunct cmd/spike (Phase 0), same behavior.
func confirmSpeakerRoles(utterances []stt.Utterance) (map[string]string, error) {
	examples := analysis.SpeakerExamples(utterances, 3)

	speakers := make([]string, 0, len(examples))
	for speaker := range examples {
		speakers = append(speakers, speaker)
	}
	sort.Strings(speakers)

	if len(speakers) != 2 {
		return nil, fmt.Errorf("esperava 2 locutores, encontrei %d: %v", len(speakers), speakers)
	}

	for _, speaker := range speakers {
		fmt.Printf("\n%s:\n", speaker)
		for _, example := range examples[speaker] {
			fmt.Printf("  - %s\n", example)
		}
	}

	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("\n%s é aluno ou tutor? [aluno/tutor] ", speakers[0])
	answer, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer != "aluno" && answer != "tutor" {
		return nil, fmt.Errorf("resposta inválida %q, esperava \"aluno\" ou \"tutor\"", answer)
	}

	other := "tutor"
	if answer == "tutor" {
		other = "aluno"
	}
	return map[string]string{speakers[0]: answer, speakers[1]: other}, nil
}

// loadDotEnv reads KEY=VALUE pairs from path and sets them as environment
// variables, without overwriting variables already set in the process. A
// missing file is not an error. Copied from the defunct cmd/spike (Phase
// 0).
func loadDotEnv(path string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if _, exists := os.LookupEnv(key); !exists {
			os.Setenv(key, value)
		}
	}
	return nil
}
```

- [ ] **Step 2: Build and vet**

Run: `go build ./... && go vet ./...`
Expected: no errors. No automated test for this CLI — same pattern as the defunct `cmd/spike`
(manual inspection tool, not production code).

- [ ] **Step 3: Commit**

```bash
git add cmd/validate-analysis/
git commit -m "feat: add temporary CLI for manual validation of the 7 analysis tasks"
```

---

## Task 7: Manual validation on a real lesson (run it yourself — do not delegate to a subagent)

> **This task cannot be run autonomously by an agent/subagent.** It requires a real DeepSeek
> credential, a real lesson already transcribed in your local database, and human judgment about
> the quality of each of the 7 outputs — exactly the kind of thing this plan cannot prescribe in
> advance. Run the steps below yourself (or in a session with access to your real machine/API
> key), outside the subagent-driven-development flow.

**Files:**
- Modify: `docs/notas-analise-llm.md`
- Modify: `docs/fase-2-analise-llm.md` (Story 1 checkboxes + a row in the progress table)
- Delete: `cmd/validate-analysis/`

- [ ] **Step 1: Find the id of an already-transcribed lesson**

Open the Library in the app, choose a lesson with status "pronta" (already transcribed) and
confirm the id by querying the database directly (the path is the same one `config.DBPath()`
resolves — by default, the app's data directory on your machine):

```bash
sqlite3 <path/to/your/app.db> "SELECT id, lesson_date, tutor FROM lessons WHERE id IN (SELECT lesson_id FROM transcripts);"
```

- [ ] **Step 2: Configure the DeepSeek credential**

Create (or confirm) a `.env` at the repository root (already gitignored) with:

```
DEEPSEEK_API_KEY=your-key-here
```

- [ ] **Step 3: Run the validation CLI**

```bash
go run ./cmd/validate-analysis -lesson-id=<id-from-step-1>
```

Answer the interactive prompt (`aluno` or `tutor`) when it shows the example utterances.

- [ ] **Step 4: Review the 7 outputs**

Open `local/output/analysis-validation/<task>/result.json` for each of the 7 tasks (the whole
directory is under `/local/`, gitignored). Check the `prompt_tokens`/`completion_tokens` of each
call in the terminal logs (`slog.Info("analysis: chamada concluída", ...)`).

- [ ] **Step 5: Record the findings in `docs/notas-analise-llm.md`**

Add a new section at the end of the file, in the same style as the existing sections (Phase 0's
DeepSeek flash/pro): observed quality per task (all 7), how many items each one produced, whether
any `utterance_index` was discarded (and whether that looked correct when reading the referenced
utterance), total real cost (sum of the tokens across the 7 calls, converted to USD using the
prices already recorded in Phase 0) compared to Phase 0's single measurement (~US$0.0014/lesson
for 1 call — 7 calls should cost more, quantify how much).

- [ ] **Step 6: Remove the temporary CLI**

```bash
git rm -r cmd/validate-analysis
git commit -m "chore: remove temporary analysis validation CLI after use"
```

- [ ] **Step 7: Mark Story 1 as done in `docs/fase-2-analise-llm.md`**

Mark the 5 Story 1 acceptance-criteria checkboxes (lines 45-61) as `[x]`, and add a row to the
"Progress log" table at the end of the file, in the same format as the rows in
`docs/fase-1-mvp.md`, summarizing what was done and citing this plan/spec.

```bash
git add docs/notas-analise-llm.md docs/fase-2-analise-llm.md
git commit -m "docs: record real validation and close Phase 2 Story 1"
```

---

## Final verification (run after all tasks, before considering the story closed)

```bash
go build ./...
go vet ./...
go test ./...
```

Expected: everything clean/green. After that, Phase 2 Story 1 is ready for Story 2 (wiring the 7
tasks to the `Worker`), which is a separate plan.
