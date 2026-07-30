# Fase 2, História 1 — Prompts por tarefa e persistência da análise: Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Quebrar a análise via LLM em 7 tarefas independentes (um prompt/schema por tarefa), tornar
`internal/analysis.Provider` agnóstico de tarefa, e persistir os resultados em duas tabelas novas
(`analysis_results`, `lesson_topics`) — sem ainda ligar nada ao `Worker`/fila (História 2) ou à UI
(Histórias 3-5).

**Architecture:** `prompts/` vira também um pacote Go mínimo (`embed.FS`) para os 7 `.md` novos;
`internal/analysis` ganha uma abstração `TaskDef`/`task[T]` genérica por cima de um `Provider`
agnóstico (`Complete(ctx, systemPrompt, transcript) (json.RawMessage, error)`); `internal/db` ganha
o repositório de `analysis_results`/`lesson_topics` e o registro idempotente de prompts; um CLI
temporário (`cmd/validate-analysis`, removido ao final) valida as 7 tarefas contra uma aula real.

**Tech Stack:** Go stdlib (`embed`, `database/sql`, `encoding/json`, `log/slog`), `modernc.org/sqlite`
(driver já em uso), `goose` (migrations já em uso). Nenhuma dependência nova.

## Global Constraints

- Pacotes em `internal/` não importam Wails — camada fina (`CLAUDE.md`).
- SQL portável na camada de repositório — nada específico de driver (`CLAUDE.md`).
- Código e identificadores em inglês; mensagens de erro voltadas ao usuário, documentação e
  conteúdo de prompt em PT-BR (`CLAUDE.md`).
- Nenhum dado real de aula (transcrição, JSON de provedor) entra no repositório — `local/` e
  `.env` já estão no `.gitignore` (`CLAUDE.md`, seção Privacidade).
- Palavra em PT/ES na fala do aluno é recurso ao idioma nativo (candidata a vocabulário), nunca
  erro de inglês (`CLAUDE.md`).
- Reprocessar continua ação explícita — nenhum job/tarefa roda de novo sozinho ao mudar de prompt
  (`docs/fase-2-analise-llm.md`).
- Commits em uma linha só, formato semântico (`tipo: descrição`) (`CLAUDE.md`).
- `go vet ./...` limpo antes de qualquer commit que toque `.go` (`CLAUDE.md`).

---

## Task 1: Persistência — migration + repositório `analysis_results`/`lesson_topics`

**Files:**
- Create: `internal/db/migrations/00004_analysis_results.sql`
- Create: `internal/db/analysis_results.go`
- Create: `internal/db/analysis_results_test.go`

**Interfaces:**
- Consumes: nada (só a infraestrutura já existente de `internal/db`: `Open`, migrations via
  `goose`, tabela `lessons` já existente para as fixtures de teste).
- Produces (usado pela Task 4 e por `cmd/validate-analysis` na Task 6):
  - `func UpsertPrompt(conn *sql.DB, name string, version int, content string) (int64, error)`
  - `func UpsertAnalysisResult(conn *sql.DB, lessonID int64, task string, promptID int64, model, resultJSON, rawResponsePath string) error`
  - `type AnalysisResult struct { LessonID int64; Task string; PromptID int64; Model string; ResultJSON string; RawResponsePath string }`
  - `func FindAnalysisResult(conn *sql.DB, lessonID int64, task string) (*AnalysisResult, error)`
  - `func ReplaceLessonTopics(conn *sql.DB, lessonID int64, topics []string) error`

- [ ] **Step 1: Escrever a migration**

Crie `internal/db/migrations/00004_analysis_results.sql`:

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

`internal/db/db.go` já embute `migrations/*.sql` (`//go:embed migrations/*.sql`) e já roda
`goose.Up` em `Open()` — nenhuma mudança de código é necessária para essa migration ser aplicada.

- [ ] **Step 2: Escrever o teste falho de `UpsertPrompt`**

Crie `internal/db/analysis_results_test.go`:

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

- [ ] **Step 3: Rodar os testes para confirmar que falham (funções ainda não existem)**

Run: `go test ./internal/db/... -run 'TestUpsertPrompt|TestUpsertAnalysisResult|TestFindAnalysisResult|TestReplaceLessonTopics' -v`
Expected: FAIL — `undefined: UpsertPrompt` (e as demais funções).

- [ ] **Step 4: Implementar `internal/db/analysis_results.go`**

```go
// internal/db/analysis_results.go
package db

import (
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

// UpsertPrompt insere (name, version, content) na tabela prompts se ainda
// não existir. Se (name, version) já existir com content diferente, é
// sinal de versão esquecida no código (convenção "-vN" no nome do arquivo
// em prompts/); loga um aviso e mantém o conteúdo já gravado — não
// sobrescreve, porque analysis_results já pode referenciar esse prompt_id.
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

// UpsertAnalysisResult grava (ou substitui, se já existir) o resultado de
// task para lessonID — reprocessar (História 2) sobrescreve a linha
// existente.
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

// AnalysisResult é o resultado persistido de uma tarefa de análise para uma lesson.
type AnalysisResult struct {
	LessonID        int64
	Task            string
	PromptID        int64
	Model           string
	ResultJSON      string
	RawResponsePath string
}

// FindAnalysisResult retorna (nil, nil) se a tarefa ainda não rodou pra
// essa lesson — estado normal enquanto o job correspondente (História 2)
// está pending/running/error, não um erro.
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

// ReplaceLessonTopics apaga os tópicos existentes de lessonID e insere os
// novos — a lista é sempre derivada por inteiro do resultado mais recente
// de analyze_topics, nunca um merge incremental. INSERT OR IGNORE absorve
// um tópico duplicado que o próprio modelo eventualmente repita na mesma
// resposta, sem falhar a transação inteira por causa do UNIQUE(lesson_id, topic).
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

- [ ] **Step 5: Rodar os testes de novo e confirmar que passam**

Run: `go test ./internal/db/... -v`
Expected: PASS em todos os testes do pacote, incluindo os 5 novos.

- [ ] **Step 6: `go vet` e commit**

Run: `go vet ./internal/db/...`
Expected: sem saída (limpo).

```bash
git add internal/db/migrations/00004_analysis_results.sql internal/db/analysis_results.go internal/db/analysis_results_test.go
git commit -m "feat: adiciona persistência de analysis_results e lesson_topics"
```

---

## Task 2: Pacote `prompts/` + 7 arquivos de prompt

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
- Consumes: nada.
- Produces (usado pela Task 4): `var prompts.FS embed.FS` — pacote Go
  `assistente-idiomas/prompts`, com `FS.ReadFile("analyze-corrections-v1.md")` etc. devolvendo o
  conteúdo de cada arquivo.

- [ ] **Step 1: Escrever o teste falho do embed**

Crie `prompts/embed_test.go`:

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

- [ ] **Step 2: Rodar o teste e confirmar que falha (pacote/arquivos ainda não existem)**

Run: `go test ./prompts/... -v`
Expected: FAIL — `no Go files in prompts` ou similar.

- [ ] **Step 3: Criar `prompts/embed.go`**

```go
// prompts/embed.go
package prompts

import "embed"

//go:embed *.md
var FS embed.FS
```

- [ ] **Step 4: Remover o prompt único da Fase 0**

```bash
git rm prompts/analyze-v1.md
```

(Sem chamador desde a remoção do `cmd/spike` ao fechar a Fase 0 — substituído pelos 7 abaixo.)

- [ ] **Step 5: Criar os 7 arquivos de prompt**

Crie `prompts/analyze-corrections-v1.md`:

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

Crie `prompts/analyze-vocabulary-v1.md`:

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

Crie `prompts/analyze-tutor-expressions-v1.md`:

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

Crie `prompts/analyze-tutor-taught-terms-v1.md`:

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

Crie `prompts/analyze-tutor-feedback-v1.md`:

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

Crie `prompts/analyze-tutor-corrections-v1.md`:

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

Crie `prompts/analyze-topics-v1.md`:

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

- [ ] **Step 6: Rodar o teste e confirmar que passa**

Run: `go test ./prompts/... -v`
Expected: PASS.

- [ ] **Step 7: `go vet` e commit**

Run: `go vet ./prompts/...`
Expected: sem saída.

```bash
git add prompts/
git commit -m "feat: substitui o prompt único da Fase 0 pelos 7 prompts por tarefa da Fase 2"
```

---

## Task 3: `internal/analysis` — Provider agnóstico + framework de tarefas

**Files:**
- Modify: `internal/analysis/analysis.go`
- Modify: `internal/analysis/openai_compatible.go`
- Modify: `internal/analysis/parsing.go`
- Modify: `internal/analysis/transcript.go`
- Create: `internal/analysis/task.go`
- Modify: `internal/analysis/parsing_test.go` (reescrito)
- Modify: `internal/analysis/transcript_test.go` (numeração)
- Create: `internal/analysis/openai_compatible_test.go`
- Create: `internal/analysis/task_test.go`

**Interfaces:**
- Consumes: nada de fora do pacote (`internal/stt.Utterance`, já usado por `transcript.go`).
- Produces (usado pela Task 4):
  - `type Provider interface { Name() string; Complete(ctx context.Context, systemPrompt, transcript string) (json.RawMessage, error) }`
  - `func NewDeepSeekProvider(apiKey string) (Provider, error)`
  - `type TaskDef interface { Name() string; Version() int; Prompt() string; Execute(ctx context.Context, provider Provider, transcript string, utteranceCount int) (resultJSON, raw json.RawMessage, err error) }`
  - `type task[T any] struct { name string; version int; prompt string; parse func(json.RawMessage, int) (T, error) }` (implementa `TaskDef`)
  - `type anchored interface { UtteranceIndex() int }`
  - `func filterAnchored[T anchored](items []T, utteranceCount int) (kept []T, discarded int)`
  - `func mustLoadPrompt(filename string) string`
  - `func unmarshalJSON(raw json.RawMessage, v any) error`
  - `func FormatTranscript(utterances []stt.Utterance, speakerRoles map[string]string) (string, error)` — inalterada na assinatura, muda só o formato da saída (agora numerada).

- [ ] **Step 1: Reescrever `transcript_test.go` esperando numeração (falho)**

Substitua o conteúdo de `internal/analysis/transcript_test.go`:

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

- [ ] **Step 2: Rodar e confirmar que falha só no teste de numeração**

Run: `go test ./internal/analysis/... -run TestFormatTranscript -v`
Expected: `TestFormatTranscript` FAIL (string sem os prefixos `[N]`); as demais funções ainda
compilam e passam.

- [ ] **Step 3: Atualizar `FormatTranscript` em `transcript.go`**

Em `internal/analysis/transcript.go`, troque a linha do `Fprintf` dentro do loop:

```go
		fmt.Fprintf(&b, "[%d] %s: %s\n", i, label, u.Text)
```

(era `fmt.Fprintf(&b, "%s: %s\n", label, u.Text)`; o loop já precisa virar `for i, u := range
utterances` em vez de `for _, u := range utterances` — é a única outra mudança na função.)

- [ ] **Step 4: Rodar de novo e confirmar que passa**

Run: `go test ./internal/analysis/... -run TestFormatTranscript -v`
Expected: PASS.

- [ ] **Step 5: Reescrever `analysis.go` (Provider agnóstico)**

Substitua o conteúdo de `internal/analysis/analysis.go`:

```go
// internal/analysis/analysis.go
package analysis

import (
	"context"
	"encoding/json"
)

// Provider é a interface única implementada por cada serviço de análise LLM
// candidato (DeepSeek, e em fatias futuras: Anthropic, OpenAI, Gemini, GLM,
// Qwen — ver docs/superpowers/specs/2026-07-19-analysis-llm-v1-design.md).
// Agnóstica de tarefa: não conhece Correction/VocabularyItem/etc, só troca
// um prompt de sistema + a transcrição por um JSON de resposta bruto — cada
// TaskDef (ver task.go) é quem sabe interpretar esse JSON.
type Provider interface {
	Name() string
	Complete(ctx context.Context, systemPrompt, transcript string) (json.RawMessage, error)
}
```

- [ ] **Step 6: Reescrever `parsing.go`**

Substitua o conteúdo de `internal/analysis/parsing.go`:

```go
// internal/analysis/parsing.go
package analysis

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// stripTrailingCodeFence remove um fechamento de code fence (```) sobrando
// no final do conteúdo — a única sujeira possível quando o provedor usa
// prefill (ver openai_compatible.go); no-op inofensivo pra provedores sem
// prefill (JSON já vem puro).
func stripTrailingCodeFence(raw []byte) []byte {
	trimmed := bytes.TrimSpace(raw)
	trimmed = bytes.TrimSuffix(trimmed, []byte("```"))
	return bytes.TrimSpace(trimmed)
}

// unmarshalJSON desserializa raw (já sem envelope HTTP nem code fence — ver
// Provider.Complete) em v. Compartilhado pelas 7 tarefas; não sabe nada
// sobre o schema de nenhuma tarefa específica.
func unmarshalJSON(raw json.RawMessage, v any) error {
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("analysis: json inválido: %w", err)
	}
	return nil
}
```

- [ ] **Step 7: Escrever `parsing_test.go` novo (substituindo o antigo)**

Substitua o conteúdo de `internal/analysis/parsing_test.go`:

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

- [ ] **Step 8: Confirmar que o pacote ainda não compila (esperado neste ponto)**

Run: `go test ./internal/analysis/... -run 'TestStripTrailingCodeFence|TestUnmarshalJSON' -v`
Expected: FAIL ao compilar — `openai_compatible.go` ainda referencia `Result`/`analysisJSON`/
`parseAnalysisResponse`, removidos nos Steps 5-6. Isso é esperado: `analysis.go`, `parsing.go` e
`openai_compatible.go` são um único bloco interdependente (o `Provider` agnóstico só faz sentido
com os três consistentes ao mesmo tempo); o próximo step termina a reescrita e volta o pacote a
compilar.

- [ ] **Step 9: Reescrever `openai_compatible.go`**

Substitua o conteúdo de `internal/analysis/openai_compatible.go`:

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

// openAICompatibleProvider implementa Provider para qualquer serviço que
// exponha um endpoint /chat/completions no formato OpenAI. Hoje só é usado
// por DeepSeek; em fatias futuras (ver
// docs/superpowers/specs/2026-07-19-analysis-llm-v1-design.md) pode ganhar
// construtores para OpenAI, GLM e Qwen, reaproveitando este mesmo tipo.
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

// NewDeepSeekProvider cria um Provider pra API do DeepSeek, modelo
// deepseek-v4-flash (tier mais barato — ver "Estratégia de fatias" no design
// doc). Usa o base URL beta, exigido pelo recurso de "Chat Prefix
// Completion" que sustenta o prefill de ```json.
func NewDeepSeekProvider(apiKey string) (Provider, error) {
	return newOpenAICompatibleProvider("deepseek", "https://api.deepseek.com/beta", apiKey, "deepseek-v4-flash", true)
}

func (p *openAICompatibleProvider) Name() string { return p.name }

// Complete envia systemPrompt + transcript e devolve o conteúdo bruto (já
// sem envelope HTTP nem code fence) que o modelo produziu — cada TaskDef
// (task.go) é quem sabe o schema esperado desse conteúdo.
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
		// DeepSeek rejeita a combinação response_format=json_object + prefix
		// (erro 400 "response_format json_object should not be used with
		// prefix", confirmado numa chamada real) — o prefill por si só já
		// força o conteúdo a começar como JSON, então response_format fica
		// de fora quando há prefill.
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

// do executa a requisição e retorna o corpo da resposta, com erro se o
// status não for 2xx (mensagem inclui status e corpo, para depuração).
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

- [ ] **Step 10: Escrever `openai_compatible_test.go`**

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

Com isso, `analysis.go`, `parsing.go` e `openai_compatible.go` voltam a ser consistentes entre si
— confirme rodando:

Run: `go test ./internal/analysis/... -v`
Expected: PASS em todos os testes do pacote (o pacote volta a compilar; `task.go` ainda não
existe, mas nada até aqui o referencia).

- [ ] **Step 11: Criar `internal/analysis/task.go`**

```go
// internal/analysis/task.go
package analysis

import (
	"context"
	"encoding/json"
	"fmt"

	"assistente-idiomas/prompts"
)

// TaskDef é a interface comum das 7 tarefas de análise — permite iterar
// todas numa lista única (var Tasks, ver Task 4 deste plano) apesar de cada
// uma ter um tipo de resultado diferente (generics não permitem slice de
// task[T] com T variável, daí essa interface não-genérica por cima).
type TaskDef interface {
	Name() string   // ex.: "analyze_corrections" — mesmo valor gravado em prompts.name e analysis_results.task
	Version() int   // versão do prompt (bump manual no código quando o .md mudar de conteúdo)
	Prompt() string // conteúdo do prompt (embed.FS)

	// Execute chama provider.Complete, faz o parse e (quando a tarefa for
	// ancorada) descarta itens com utterance_index inválido. Devolve o JSON
	// já validado (pronto pra gravar em analysis_results.result_json) e o
	// conteúdo bruto devolvido pelo provedor (pronto pra gravar em disco,
	// raw_response_path). err != nil não impede o chamador de gravar raw em
	// disco (mesmo princípio de runTranscribe: a chamada já custou dinheiro).
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

// anchored é implementada pelos tipos de item cujo parse referencia uma
// fala específica da transcrição (Correction, TutorCorrection,
// TutorFeedbackItem, ver Task 4) — um UtteranceIndex negativo representa
// "ausente no JSON do modelo", tratado igual a um índice fora do range.
type anchored interface {
	UtteranceIndex() int
}

// filterAnchored descarta (retornando também a contagem descartada, pra
// log) itens cujo UtteranceIndex não caia em [0, utteranceCount).
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

// mustLoadPrompt lê um prompt embutido em prompts.FS (prompts/embed.go,
// Task 2) — panic em caso de ausência é intencional: um prompt faltando é
// erro de build/empacotamento, não uma condição de runtime a tratar
// graciosamente (mesmo espírito de um template.Must).
func mustLoadPrompt(filename string) string {
	b, err := prompts.FS.ReadFile(filename)
	if err != nil {
		panic(fmt.Sprintf("analysis: prompt %s não encontrado: %v", filename, err))
	}
	return string(b)
}
```

- [ ] **Step 12: Escrever `task_test.go`**

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

- [ ] **Step 13: Rodar todos os testes do pacote e confirmar que passam**

Run: `go test ./internal/analysis/... -v`
Expected: PASS em todos os testes (o pacote agora compila de ponta a ponta — `var Tasks` ainda
não existe, mas nada neste pacote o referencia ainda; isso só chega na Task 4).

- [ ] **Step 14: `go vet` e commit**

Run: `go vet ./internal/analysis/...`
Expected: sem saída.

```bash
git add internal/analysis/ prompts/
git commit -m "refactor: torna analysis.Provider agnóstico de tarefa e introduz o framework TaskDef"
```

---

## Task 4: As 7 tarefas concretas + `RegisterPrompts`

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
  3); `db.UpsertPrompt` (Task 1); os 7 arquivos `.md` via `mustLoadPrompt` (Task 2).
- Produces (usado pela Task 5 e pelo `cmd/validate-analysis` da Task 6):
  - `var Tasks []TaskDef` com as 7 tarefas.
  - `func RegisterPrompts(conn *sql.DB) error`
  - Tipos de item: `Correction`, `VocabularyItem`, `Expression`, `TutorTaughtTerm`,
    `TutorFeedbackItem`, `TutorCorrection` (tópicos usam `[]string`, sem struct própria).

- [ ] **Step 1: Escrever `tasks_corrections.go` + teste**

```go
// internal/analysis/tasks_corrections.go
package analysis

import (
	"encoding/json"
	"log/slog"
)

// Correction é uma correção de uma fala do Aluno, derivada pela análise
// (ao contrário de TutorCorrection, dada ao vivo pelo próprio Tutor).
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

- [ ] **Step 2: Rodar e confirmar que passa**

Run: `go test ./internal/analysis/... -run 'Corrections' -v`
Expected: PASS.

- [ ] **Step 3: Escrever `tasks_vocabulary.go` + teste**

```go
// internal/analysis/tasks_vocabulary.go
package analysis

import "encoding/json"

// VocabularyItem é uma palavra ou expressão nova pro Aluno aprender —
// inclui palavras em PT/ES usadas como recurso ao idioma nativo, nunca
// tratadas como erro de inglês (ver analyze-corrections-v1.md).
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

- [ ] **Step 4: Rodar e confirmar que passa**

Run: `go test ./internal/analysis/... -run 'Vocabulary' -v`
Expected: PASS.

- [ ] **Step 5: Escrever `tasks_tutor_expressions.go` + teste**

```go
// internal/analysis/tasks_tutor_expressions.go
package analysis

import "encoding/json"

// Expression é uma expressão que o Tutor usou naturalmente na conversa e
// que vale a pena o Aluno reutilizar — distinta de TutorTaughtTerm (termo
// que o Tutor explicou/ensinou explicitamente).
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

- [ ] **Step 6: Rodar e confirmar que passa**

Run: `go test ./internal/analysis/... -run 'TutorExpressions' -v`
Expected: PASS.

- [ ] **Step 7: Escrever `tasks_tutor_taught_terms.go` + teste**

```go
// internal/analysis/tasks_tutor_taught_terms.go
package analysis

import "encoding/json"

// TutorTaughtTerm é um termo/expressão que o Tutor explicou ou ensinou
// explicitamente durante a aula (ao contrário de Expression, que é só uso
// natural na conversa).
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

- [ ] **Step 8: Rodar e confirmar que passa**

Run: `go test ./internal/analysis/... -run 'TutorTaughtTerms' -v`
Expected: PASS.

- [ ] **Step 9: Escrever `tasks_tutor_feedback.go` + teste**

```go
// internal/analysis/tasks_tutor_feedback.go
package analysis

import (
	"encoding/json"
	"log/slog"
)

// TutorFeedbackItem é uma observação do Tutor sobre o desempenho do Aluno,
// ancorada na fala do Tutor em que foi dada.
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

- [ ] **Step 10: Rodar e confirmar que passa**

Run: `go test ./internal/analysis/... -run 'TutorFeedback' -v`
Expected: PASS.

- [ ] **Step 11: Escrever `tasks_tutor_corrections.go` + teste**

```go
// internal/analysis/tasks_tutor_corrections.go
package analysis

import (
	"encoding/json"
	"log/slog"
)

// TutorCorrection é uma correção que o próprio Tutor deu ao Aluno durante a
// aula (ao vivo, na conversa) — diferente de Correction (derivada pela
// análise), embora ambas possam apontar pra mesma fala.
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

- [ ] **Step 12: Rodar e confirmar que passa**

Run: `go test ./internal/analysis/... -run 'TutorCorrections' -v`
Expected: PASS.

- [ ] **Step 13: Escrever `tasks_topics.go` + teste**

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

- [ ] **Step 14: Rodar e confirmar que passa**

Run: `go test ./internal/analysis/... -run 'Topics' -v`
Expected: PASS.

- [ ] **Step 15: Criar `tasks.go` com `var Tasks`**

```go
// internal/analysis/tasks.go
package analysis

// Tasks lista as 7 tarefas de análise da Fase 2 — a ordem não importa pra
// execução (independentes entre si), só pra leitura humana e pra
// RegisterPrompts (prompts.go).
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

- [ ] **Step 16: Escrever `prompts_test.go` (falho — `RegisterPrompts` ainda não existe)**

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

- [ ] **Step 17: Rodar e confirmar que falha (`RegisterPrompts` não existe)**

Run: `go test ./internal/analysis/... -run TestRegisterPrompts -v`
Expected: FAIL — `undefined: RegisterPrompts`.

- [ ] **Step 18: Criar `prompts.go` com `RegisterPrompts`**

```go
// internal/analysis/prompts.go
package analysis

import (
	"database/sql"
	"fmt"

	"assistente-idiomas/internal/db"
)

// RegisterPrompts grava (nome, versão, conteúdo) de cada TaskDef em Tasks
// na tabela prompts, se ainda não existir — idempotente entre reinícios do
// app. Chamado uma vez em main.go, logo após db.Open.
func RegisterPrompts(conn *sql.DB) error {
	for _, t := range Tasks {
		if _, err := db.UpsertPrompt(conn, t.Name(), t.Version(), t.Prompt()); err != nil {
			return fmt.Errorf("analysis: registrar prompt %s: %w", t.Name(), err)
		}
	}
	return nil
}
```

- [ ] **Step 19: Rodar de novo e confirmar que passa**

Run: `go test ./internal/analysis/... -v`
Expected: PASS em todos os testes do pacote (agora com as 7 tarefas + `RegisterPrompts`).

- [ ] **Step 20: `go build`, `go vet` de todo o módulo e commit**

Run: `go build ./... && go vet ./...`
Expected: sem erros (confirma que `internal/db` e `internal/analysis` continuam compatíveis entre
si e com o resto do módulo).

```bash
git add internal/analysis/
git commit -m "feat: implementa as 7 tarefas de análise e o registro de prompts"
```

---

## Task 5: `main.go` — registrar os prompts na inicialização

**Files:**
- Modify: `main.go:1-32`

**Interfaces:**
- Consumes: `analysis.RegisterPrompts(conn *sql.DB) error` (Task 4).
- Produces: nada consumido por outra task deste plano.

- [ ] **Step 1: Adicionar o import e a chamada**

Em `main.go`, adicione o import (ordem alfabética, junto aos demais `internal/`):

```go
	"assistente-idiomas/internal/analysis"
```

E logo após `defer conn.Close()` (linha 32), antes do bloco `storageRoot := ...`:

```go
	if err := analysis.RegisterPrompts(conn); err != nil {
		log.Fatalf("registrar prompts de análise: %v", err)
	}
```

Falha ao registrar prompts é fatal (mesmo tratamento que uma falha de `db.Open` já recebe acima)
— não há como a fila da Fase 2 (História 2) funcionar sem os prompts na tabela, e falhar cedo é
preferível a descobrir isso só quando o primeiro job de análise rodar.

- [ ] **Step 2: Build e vet do módulo inteiro**

Run: `go build ./... && go vet ./...`
Expected: sem erros.

- [ ] **Step 3: Commit**

```bash
git add main.go
git commit -m "feat: registra os prompts de análise na inicialização do app"
```

---

## Task 6: `cmd/validate-analysis` — CLI temporário de validação manual

**Files:**
- Create: `cmd/validate-analysis/main.go`

**Interfaces:**
- Consumes: `db.Open`, `db.FindTranscriptByLessonID` (já existentes); `analysis.Tasks`,
  `analysis.NewDeepSeekProvider`, `analysis.FormatTranscript`, `analysis.SpeakerExamples`,
  `analysis.TaskDef.Execute` (Task 4); `config.DBPath` (já existente).
- Produces: nada consumido por código do app — só arquivos em
  `local/output/analysis-validation/<task>/{raw.json,result.json}`, usados manualmente na Task 7.

- [ ] **Step 1: Criar `cmd/validate-analysis/main.go`**

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

// validate-analysis roda as 7 tarefas de análise (Fase 2, História 1)
// contra uma aula real já transcrita, para inspecionar qualidade e custo
// antes de ligar isso ao Worker (História 2). Ferramenta temporária — ver
// "Decisões de escopo" em
// docs/superpowers/specs/2026-07-30-fase2-historia-1-prompts-persistencia-design.md;
// removida depois de registrar os achados em docs/notas-analise-llm.md
// (Task 7 deste plano), mesmo destino do extinto cmd/spike (Fase 0).
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

// confirmSpeakerRoles mostra até 3 falas de exemplo por locutor e pergunta
// ao usuário, via stdin, qual dos dois é o Aluno — o outro vira Tutor (aula
// do Cambly é sempre 1:1). Copiado do extinto cmd/spike (Fase 0), mesmo
// comportamento.
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

// loadDotEnv lê pares CHAVE=VALOR de path e os define como variáveis de
// ambiente, sem sobrescrever variáveis já definidas no processo. Arquivo
// ausente não é erro. Copiado do extinto cmd/spike (Fase 0).
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

- [ ] **Step 2: Build e vet**

Run: `go build ./... && go vet ./...`
Expected: sem erros. Sem teste automatizado para este CLI — mesmo padrão do extinto `cmd/spike`
(ferramenta de inspeção manual, não código de produção).

- [ ] **Step 3: Commit**

```bash
git add cmd/validate-analysis/
git commit -m "feat: adiciona CLI temporário de validação manual das 7 tarefas de análise"
```

---

## Task 7: Validação manual numa aula real (executar por conta própria — não delegar a um subagent)

> **Esta task não pode ser executada de forma autônoma por um agente/subagent.** Ela exige uma
> credencial real da DeepSeek, uma aula real já transcrita no seu banco local, e julgamento humano
> sobre a qualidade de cada uma das 7 saídas — exatamente o tipo de coisa que este plano não pode
> prescrever de antemão. Rode os passos abaixo você mesmo (ou em uma sessão com acesso à sua
> máquina/API key reais), fora do fluxo de subagent-driven-development.

**Files:**
- Modify: `docs/notas-analise-llm.md`
- Modify: `docs/fase-2-analise-llm.md` (checkboxes da História 1 + linha na tabela de progresso)
- Delete: `cmd/validate-analysis/`

- [ ] **Step 1: Descobrir o id de uma lesson já transcrita**

Abra a Biblioteca no app, escolha uma aula com status "pronta" (já transcrita) e confirme o id
consultando diretamente o banco (o path é o mesmo que `config.DBPath()` resolve — por padrão, o
diretório de dados do app na sua máquina):

```bash
sqlite3 <path/do/seu/app.db> "SELECT id, lesson_date, tutor FROM lessons WHERE id IN (SELECT lesson_id FROM transcripts);"
```

- [ ] **Step 2: Configurar a credencial da DeepSeek**

Crie (ou confirme) um `.env` na raiz do repositório (já ignorado pelo git) com:

```
DEEPSEEK_API_KEY=sua-chave-aqui
```

- [ ] **Step 3: Rodar o CLI de validação**

```bash
go run ./cmd/validate-analysis -lesson-id=<id-do-step-1>
```

Responda o prompt interativo (`aluno` ou `tutor`) quando ele mostrar as falas de exemplo.

- [ ] **Step 4: Revisar as 7 saídas**

Abra `local/output/analysis-validation/<task>/result.json` para cada uma das 7 tarefas (o
diretório inteiro está sob `/local/`, ignorado pelo git). Confira nos logs do terminal
(`slog.Info("analysis: chamada concluída", ...)`) os `prompt_tokens`/`completion_tokens` de cada
chamada.

- [ ] **Step 5: Registrar os achados em `docs/notas-analise-llm.md`**

Adicione uma seção nova ao final do arquivo, no mesmo estilo das seções existentes (DeepSeek
flash/pro da Fase 0): qualidade observada por tarefa (as 7), quantos itens cada uma trouxe, se
algum `utterance_index` foi descartado (e se isso pareceu correto ao ler a fala apontada), custo
real total (soma dos tokens das 7 chamadas, convertido a USD pelos preços já registrados na Fase
0) comparado à medição única da Fase 0 (~US$ 0,0014/aula p/ 1 chamada — 7 chamadas devem custar
mais, quantificar quanto).

- [ ] **Step 6: Remover o CLI temporário**

```bash
git rm -r cmd/validate-analysis
git commit -m "chore: remove o CLI temporário de validação de análise após uso"
```

- [ ] **Step 7: Marcar a História 1 como concluída em `docs/fase-2-analise-llm.md`**

Marque os 5 checkboxes de critérios de aceite da História 1 (linhas 45-61) como `[x]`, e adicione
uma linha na tabela "Registro de progresso" no final do arquivo, no mesmo formato das linhas de
`docs/fase-1-mvp.md`, resumindo o que foi feito e citando este plano/spec.

```bash
git add docs/notas-analise-llm.md docs/fase-2-analise-llm.md
git commit -m "docs: registra validação real e fecha a História 1 da Fase 2"
```

---

## Verificação final (rodar depois de todas as tasks, antes de considerar a história fechada)

```bash
go build ./...
go vet ./...
go test ./...
```

Expected: tudo limpo/verde. Depois disso, a História 1 da Fase 2 está pronta para a História 2
(ligar as 7 tarefas ao `Worker`), que é um plano separado.
