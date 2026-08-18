# Fase 2, História 3 — Tópicos da aula: plano de implementação

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Levar `analyze_topics` para o app sob demanda, com tópicos editáveis (chips no Detalhe + renomear global em Configurações), prompt v2 com granularidade geral e reaproveitamento dos tópicos existentes.

**Architecture:** Tópicos viram entidade `topics` (como professores), `lesson_topics` passa a apontar por `topic_id` (migration 00006 com backfill). `lesson_topics` vira a fonte da verdade da UI; `analysis_results` guarda só "rodou?" + raw + modelo. O `AnalysisService` ganha Get/Analyze/ReprocessTopics (espelhando correções), e um novo `TopicsService` cobre o CRUD de entidade. A lista de tópicos existentes é anexada ao conteúdo enviado ao modelo (helper puro), sem mudar a interface de task.

**Tech Stack:** Go (stdlib + modernc.org/sqlite + goose + go-keyring, já presentes), Wails v3 (pinada), Svelte 5 (runes).

**Spec:** `docs/superpowers/specs/2026-08-18-historia-3-topicos-aula-design.md`

## Global Constraints

- Go recente; stdlib preferida. Driver SQLite `modernc.org/sqlite`; migrations `goose` embutidas (`//go:embed migrations/*.sql`).
- **Código/identificadores em inglês; mensagens de erro ao usuário, textos de análise e docs em PT-BR.**
- **SQL portável** na camada de repositório — a única dependência de driver é o helper `isUniqueConstraintError` já existente em `internal/db/teachers.go`.
- **Frontend Svelte 5 com runes, sempre** (`$state`/`$derived`/`$effect`/`$props`; nunca sintaxe legada).
- Wails v3 versão pinada no `go.mod` — não alterar versão.
- Commits em **uma linha**, formato semântico (`tipo: descrição`).
- Fixtures de teste sintéticas (nada de aula real / nome de tutor real).
- `prompts/embed.go` usa `//go:embed *.md` — um novo `.md` em `prompts/` é embutido automaticamente, sem mexer no embed.

---

### Task 1: Migration `00006_topics.sql` + `ReplaceLessonTopics` por id + backfill

A migration muda o schema de `lesson_topics` (de `topic TEXT` para `topic_id INTEGER`), o que quebra o `ReplaceLessonTopics` atual (insere `topic`). Esta task faz a migration e a mudança de assinatura juntas para o pacote `db` ficar verde.

**Files:**
- Create: `internal/db/migrations/00006_topics.sql`
- Modify: `internal/db/analysis_results.go` (função `ReplaceLessonTopics`)
- Modify: `internal/db/analysis_results_test.go` (`TestReplaceLessonTopics_ReplacesEntirely` + novo helper `insertTopicFixture`)
- Modify: `internal/db/migration_backfill_test.go` (novo teste)

**Interfaces:**
- Produces: `func ReplaceLessonTopics(conn *sql.DB, lessonID int64, topicIDs []int64) error` (assinatura MUDADA — `[]int64`, não `[]string`); tabela `topics` e `lesson_topics(topic_id)`.

- [ ] **Step 1: Escrever a migration**

Criar `internal/db/migrations/00006_topics.sql`:

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

- [ ] **Step 2: Escrever o teste de backfill (falha antes da migration existir)**

Adicionar a `internal/db/migration_backfill_test.go`:

```go
func TestMigration00006_BackfillsTopicsFromExistingLessonTopics(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	conn, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("sql.Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	goose.SetBaseFS(migrationsFS)
	defer goose.SetBaseFS(nil)
	if err := goose.SetDialect("sqlite"); err != nil {
		t.Fatalf("SetDialect() erro inesperado: %v", err)
	}

	if err := goose.UpTo(conn, "migrations", 5); err != nil {
		t.Fatalf("UpTo(5) erro inesperado: %v", err)
	}

	// Schema pré-migration 6: lesson_topics(topic TEXT). Precisa de teacher e
	// lesson (lessons já aponta por teacher_id desde a migration 5).
	now := "2026-08-18T10:00:00Z"
	resT, err := conn.Exec(`INSERT INTO teachers (name, created_at, updated_at) VALUES ('Sarah M.', ?, ?)`, now, now)
	if err != nil {
		t.Fatalf("insert teacher legado falhou: %v", err)
	}
	teacherID, _ := resT.LastInsertId()
	resL, err := conn.Exec(
		`INSERT INTO lessons (lesson_date, teacher_id, video_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"2026-08-18", teacherID, "aulas/2026/a.mp4", now, now,
	)
	if err != nil {
		t.Fatalf("insert lesson legada falhou: %v", err)
	}
	lessonID, _ := resL.LastInsertId()
	for _, topic := range []string{"viagens", "trabalho remoto"} {
		if _, err := conn.Exec(`INSERT INTO lesson_topics (lesson_id, topic) VALUES (?, ?)`, lessonID, topic); err != nil {
			t.Fatalf("insert lesson_topics legado (%q) falhou: %v", topic, err)
		}
	}

	if err := goose.UpTo(conn, "migrations", 6); err != nil {
		t.Fatalf("UpTo(6) erro inesperado: %v", err)
	}

	var topicCount int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM topics`).Scan(&topicCount); err != nil {
		t.Fatalf("contar topics falhou: %v", err)
	}
	if topicCount != 2 {
		t.Errorf("topicCount = %d, esperado 2 (dedup por nome)", topicCount)
	}

	rows, err := conn.Query(`SELECT t.name FROM lesson_topics lt JOIN topics t ON t.id = lt.topic_id WHERE lt.lesson_id = ? ORDER BY t.name`, lessonID)
	if err != nil {
		t.Fatalf("query em lesson_topics pós-migration falhou: %v", err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatalf("scan de topic pós-migration falhou: %v", err)
		}
		names = append(names, n)
	}
	if len(names) != 2 || names[0] != "trabalho remoto" || names[1] != "viagens" {
		t.Errorf("names = %+v, esperado [trabalho remoto viagens]", names)
	}

	var topicCol int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('lesson_topics') WHERE name = 'topic'`).Scan(&topicCol); err != nil {
		t.Fatalf("pragma_table_info(lesson_topics) falhou: %v", err)
	}
	if topicCol != 0 {
		t.Errorf("coluna topic ainda existe em lesson_topics, esperado removida")
	}
}
```

- [ ] **Step 3: Rodar o teste de backfill para ver falhar**

Run: `go test ./internal/db/ -run TestMigration00006 -v`
Expected: FAIL — `goose: no migrations to run` ou erro de versão 6 inexistente (a migration ainda não existe).

- [ ] **Step 4: Mudar `ReplaceLessonTopics` para ids**

Em `internal/db/analysis_results.go`, substituir a implementação de `ReplaceLessonTopics`:

```go
// ReplaceLessonTopics apaga os vínculos existentes e insere os novos (por
// topic_id) — a lista é sempre derivada por inteiro do resultado mais
// recente de analyze_topics, nunca um merge incremental. INSERT OR IGNORE
// absorve um tópico duplicado que o chamador eventualmente repita.
func ReplaceLessonTopics(conn *sql.DB, lessonID int64, topicIDs []int64) error {
	tx, err := conn.Begin()
	if err != nil {
		return fmt.Errorf("iniciar transação de lesson_topics da lesson %d: %w", lessonID, err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM lesson_topics WHERE lesson_id = ?`, lessonID); err != nil {
		return fmt.Errorf("apagar lesson_topics antigos da lesson %d: %w", lessonID, err)
	}
	for _, topicID := range topicIDs {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO lesson_topics (lesson_id, topic_id) VALUES (?, ?)`, lessonID, topicID); err != nil {
			return fmt.Errorf("inserir tópico %d da lesson %d: %w", topicID, lessonID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commitar lesson_topics da lesson %d: %w", lessonID, err)
	}
	return nil
}
```

- [ ] **Step 5: Atualizar o teste existente de `ReplaceLessonTopics`**

Em `internal/db/analysis_results_test.go`, adicionar o helper `insertTopicFixture` (SQL cru, para esta task não depender da Task 2) e substituir `TestReplaceLessonTopics_ReplacesEntirely`:

```go
func insertTopicFixture(t *testing.T, conn *sql.DB, name string) int64 {
	t.Helper()
	res, err := conn.Exec(
		`INSERT INTO topics (name, created_at, updated_at) VALUES (?, ?, ?)`,
		name, "2026-08-18T10:00:00Z", "2026-08-18T10:00:00Z",
	)
	if err != nil {
		t.Fatalf("insert topic de fixture (%q) falhou: %v", name, err)
	}
	id, _ := res.LastInsertId()
	return id
}

func TestReplaceLessonTopics_ReplacesEntirely(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := insertLessonFixture(t, conn)
	viagens := insertTopicFixture(t, conn, "viagens")
	trabalho := insertTopicFixture(t, conn, "trabalho remoto")
	receitas := insertTopicFixture(t, conn, "receitas de família")

	if err := ReplaceLessonTopics(conn, lessonID, []int64{viagens, trabalho}); err != nil {
		t.Fatalf("ReplaceLessonTopics() erro inesperado: %v", err)
	}
	if err := ReplaceLessonTopics(conn, lessonID, []int64{receitas}); err != nil {
		t.Fatalf("segunda ReplaceLessonTopics() erro inesperado: %v", err)
	}

	rows, err := conn.Query(`SELECT t.name FROM lesson_topics lt JOIN topics t ON t.id = lt.topic_id WHERE lt.lesson_id = ? ORDER BY t.name`, lessonID)
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
		t.Errorf("topics = %+v, esperado apenas ["receitas de família"]", topics)
	}
}
```

- [ ] **Step 6: Rodar os testes do pacote db**

Run: `go test ./internal/db/ -v`
Expected: PASS (inclui `TestMigration00006` e `TestReplaceLessonTopics`).

- [ ] **Step 7: Commit**

```bash
git add internal/db/migrations/00006_topics.sql internal/db/analysis_results.go internal/db/analysis_results_test.go internal/db/migration_backfill_test.go
git commit -m "feat: migration topics + lesson_topics por topic_id"
```

---

### Task 2: Repositório `internal/db/topics.go` + testes

**Files:**
- Create: `internal/db/topics.go`
- Create: `internal/db/topics_test.go`

**Interfaces:**
- Consumes: `execer` e `isUniqueConstraintError` (já existem em `teachers.go`); schema da Task 1.
- Produces: `Topic{ID int64, Name string}`, `ListTopics(conn) ([]Topic, error)`, `GetOrCreateTopicByName(conn, name) (int64, error)`, `RenameTopic(conn, id, newName) error`, `ListLessonTopics(conn, lessonID) ([]Topic, error)`, `AddLessonTopic(conn, lessonID, topicID) error`, `RemoveLessonTopic(conn, lessonID, topicID) error`.

- [ ] **Step 1: Escrever o teste (falha)**

Criar `internal/db/topics_test.go`:

```go
package db

import (
	"path/filepath"
	"testing"
)

func TestGetOrCreateTopicByName_IdempotentAndTrims(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	id1, err := GetOrCreateTopicByName(conn, "viagens")
	if err != nil {
		t.Fatalf("GetOrCreateTopicByName() erro inesperado: %v", err)
	}
	id2, err := GetOrCreateTopicByName(conn, "  viagens  ")
	if err != nil {
		t.Fatalf("segunda GetOrCreateTopicByName() erro inesperado: %v", err)
	}
	if id1 != id2 {
		t.Errorf("id2 = %d, esperado igual a id1 = %d (mesmo nome após trim)", id2, id1)
	}
	var count int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM topics WHERE name = 'viagens'`).Scan(&count); err != nil {
		t.Fatalf("contar topics falhou: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, esperado 1", count)
	}
}

func TestListTopics_ReturnsDistinctSortedByName(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	for _, n := range []string{"viagens", "trabalho remoto", "receitas"} {
		if _, err := GetOrCreateTopicByName(conn, n); err != nil {
			t.Fatalf("GetOrCreateTopicByName(%q) erro inesperado: %v", n, err)
		}
	}
	topics, err := ListTopics(conn)
	if err != nil {
		t.Fatalf("ListTopics() erro inesperado: %v", err)
	}
	if len(topics) != 3 || topics[0].Name != "receitas" || topics[1].Name != "trabalho remoto" || topics[2].Name != "viagens" {
		t.Errorf("ListTopics() = %+v, esperado [receitas trabalho remoto viagens]", topics)
	}
}

func TestRenameTopic_ReflectsOnLinkedLessons(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := insertLessonFixture(t, conn)
	topicID, err := GetOrCreateTopicByName(conn, "viagens")
	if err != nil {
		t.Fatalf("GetOrCreateTopicByName() erro inesperado: %v", err)
	}
	if err := AddLessonTopic(conn, lessonID, topicID); err != nil {
		t.Fatalf("AddLessonTopic() erro inesperado: %v", err)
	}

	if err := RenameTopic(conn, topicID, "planos de viagem"); err != nil {
		t.Fatalf("RenameTopic() erro inesperado: %v", err)
	}

	topics, err := ListLessonTopics(conn, lessonID)
	if err != nil {
		t.Fatalf("ListLessonTopics() erro inesperado: %v", err)
	}
	if len(topics) != 1 || topics[0].Name != "planos de viagem" {
		t.Errorf("ListLessonTopics() = %+v, esperado [planos de viagem] (renome refletido via JOIN)", topics)
	}
}

func TestRenameTopic_CollidingNameReturnsFriendlyError(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	if _, err := GetOrCreateTopicByName(conn, "viagens"); err != nil {
		t.Fatalf("GetOrCreateTopicByName() erro inesperado: %v", err)
	}
	trabalho, err := GetOrCreateTopicByName(conn, "trabalho remoto")
	if err != nil {
		t.Fatalf("GetOrCreateTopicByName() erro inesperado: %v", err)
	}

	err = RenameTopic(conn, trabalho, "viagens")
	if err == nil {
		t.Fatal("RenameTopic() colidindo = nil, esperado erro amigável")
	}
	if err.Error() != "já existe um tópico com esse nome" {
		t.Errorf("RenameTopic() = %q, esperado \"já existe um tópico com esse nome\"", err.Error())
	}
}

func TestAddAndRemoveLessonTopic(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := insertLessonFixture(t, conn)
	topicID, err := GetOrCreateTopicByName(conn, "viagens")
	if err != nil {
		t.Fatalf("GetOrCreateTopicByName() erro inesperado: %v", err)
	}

	if err := AddLessonTopic(conn, lessonID, topicID); err != nil {
		t.Fatalf("AddLessonTopic() erro inesperado: %v", err)
	}
	if err := AddLessonTopic(conn, lessonID, topicID); err != nil {
		t.Fatalf("segunda AddLessonTopic() erro inesperado: %v", err)
	}
	var count int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM lesson_topics WHERE lesson_id = ?`, lessonID).Scan(&count); err != nil {
		t.Fatalf("contar lesson_topics falhou: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, esperado 1 (INSERT OR IGNORE)", count)
	}

	if err := RemoveLessonTopic(conn, lessonID, topicID); err != nil {
		t.Fatalf("RemoveLessonTopic() erro inesperado: %v", err)
	}
	var topicCount int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM topics WHERE id = ?`, topicID).Scan(&topicCount); err != nil {
		t.Fatalf("contar topics falhou: %v", err)
	}
	if topicCount != 1 {
		t.Errorf("topicCount = %d, esperado 1 (remover não apaga a entidade)", topicCount)
	}
}
```

- [ ] **Step 2: Rodar para ver falhar**

Run: `go test ./internal/db/ -run 'TestGetOrCreateTopicByName|TestListTopics|TestRenameTopic|TestAddAndRemoveLessonTopic' -v`
Expected: FAIL — `undefined: GetOrCreateTopicByName` (etc.).

- [ ] **Step 3: Implementar `internal/db/topics.go`**

```go
// internal/db/topics.go
package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Topic é uma linha de topics — a entidade que substitui a antiga coluna
// lesson_topics.topic (ver História 3 da Fase 2). Nome é único: renomear é
// um UPDATE de uma linha só, refletido em todas as aulas via JOIN.
type Topic struct {
	ID   int64
	Name string
}

// ListTopics lista os tópicos cadastrados em ordem alfabética — alimenta o
// painel "Tópicos" de Configurações e a lista de reaproveitamento do prompt.
func ListTopics(conn *sql.DB) ([]Topic, error) {
	rows, err := conn.Query(`SELECT id, name FROM topics ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("listar tópicos: %w", err)
	}
	defer rows.Close()

	out := make([]Topic, 0)
	for rows.Next() {
		var t Topic
		if err := rows.Scan(&t.ID, &t.Name); err != nil {
			return nil, fmt.Errorf("ler tópico: %w", err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterar tópicos: %w", err)
	}
	return out, nil
}

// getOrCreateTopicByName resolve um nome (aparado) para um topic_id —
// reusa execer (teachers.go) pra rodar solto ou dentro de uma transação.
func getOrCreateTopicByName(q execer, name string) (int64, error) {
	name = strings.TrimSpace(name)
	var id int64
	err := q.QueryRow(`SELECT id FROM topics WHERE name = ?`, name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("buscar tópico por nome: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	res, err := q.Exec(
		`INSERT INTO topics (name, created_at, updated_at) VALUES (?, ?, ?)`,
		name, now, now,
	)
	if err != nil {
		return 0, fmt.Errorf("criar tópico: %w", err)
	}
	id, err = res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("obter id do tópico criado: %w", err)
	}
	return id, nil
}

func GetOrCreateTopicByName(conn *sql.DB, name string) (int64, error) {
	return getOrCreateTopicByName(conn, name)
}

// RenameTopic renomeia o tópico id — reflete em todas as aulas dele
// automaticamente (JOIN). Colisão (UNIQUE) vira um erro legível.
func RenameTopic(conn *sql.DB, id int64, newName string) error {
	newName = strings.TrimSpace(newName)
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := conn.Exec(`UPDATE topics SET name = ?, updated_at = ? WHERE id = ?`, newName, now, id)
	if err != nil {
		if isUniqueConstraintError(err) {
			return fmt.Errorf("já existe um tópico com esse nome")
		}
		return fmt.Errorf("renomear tópico: %w", err)
	}
	return nil
}

// ListLessonTopics devolve os tópicos de lessonID (JOIN topics), em ordem
// alfabética.
func ListLessonTopics(conn *sql.DB, lessonID int64) ([]Topic, error) {
	rows, err := conn.Query(
		`SELECT t.id, t.name FROM lesson_topics lt JOIN topics t ON t.id = lt.topic_id WHERE lt.lesson_id = ? ORDER BY t.name ASC`,
		lessonID,
	)
	if err != nil {
		return nil, fmt.Errorf("listar tópicos da lesson %d: %w", lessonID, err)
	}
	defer rows.Close()

	out := make([]Topic, 0)
	for rows.Next() {
		var t Topic
		if err := rows.Scan(&t.ID, &t.Name); err != nil {
			return nil, fmt.Errorf("ler tópico da lesson: %w", err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterar tópicos da lesson: %w", err)
	}
	return out, nil
}

// AddLessonTopic vincula topicID a lessonID (INSERT OR IGNORE — já vinculado
// não é erro).
func AddLessonTopic(conn *sql.DB, lessonID, topicID int64) error {
	_, err := conn.Exec(`INSERT OR IGNORE INTO lesson_topics (lesson_id, topic_id) VALUES (?, ?)`, lessonID, topicID)
	if err != nil {
		return fmt.Errorf("vincular tópico %d à lesson %d: %w", topicID, lessonID, err)
	}
	return nil
}

// RemoveLessonTopic desvincula topicID de lessonID (não apaga a entidade).
func RemoveLessonTopic(conn *sql.DB, lessonID, topicID int64) error {
	_, err := conn.Exec(`DELETE FROM lesson_topics WHERE lesson_id = ? AND topic_id = ?`, lessonID, topicID)
	if err != nil {
		return fmt.Errorf("desvincular tópico %d da lesson %d: %w", topicID, lessonID, err)
	}
	return nil
}
```

- [ ] **Step 4: Rodar os testes do pacote db**

Run: `go test ./internal/db/ -v`
Expected: PASS (Task 1 + Task 2 juntas).

- [ ] **Step 5: Commit**

```bash
git add internal/db/topics.go internal/db/topics_test.go
git commit -m "feat: repositório de tópicos (entidade + vínculos por aula)"
```

---

### Task 3: Deleção seletiva na troca de falante

**Files:**
- Modify: `internal/db/analysis_results.go` (renomear `DeleteAnalysisResultsForLesson` → `DeleteSpeakerDependentAnalysisResults` e mudar SQL)
- Modify: `services/library.go` (`SetStudentSpeaker`)
- Modify: `internal/db/analysis_results_test.go` (renomear/reescrever o teste de deleção)

**Interfaces:**
- Consumes: `UpsertAnalysisResult`, `UpsertPrompt` (existem); `insertLessonFixture` (helper do pacote db); `GetOrCreateTopicByName`/`AddLessonTopic` (Task 2).
- Produces: `func DeleteSpeakerDependentAnalysisResults(conn *sql.DB, lessonID int64) error`.

- [ ] **Step 1: Escrever o teste novo (falha por símbolo ausente)**

Em `internal/db/analysis_results_test.go`, substituir `TestDeleteAnalysisResultsForLesson_DeletesAllTasksForLessonOnly` por:

```go
func TestDeleteSpeakerDependentAnalysisResults_PreservesTopicsAndOtherLessons(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonA := insertLessonFixture(t, conn)
	lessonB := insertLessonFixture(t, conn)
	promptID, err := UpsertPrompt(conn, "analyze_corrections", 1, "conteúdo")
	if err != nil {
		t.Fatalf("UpsertPrompt() erro inesperado: %v", err)
	}
	if err := UpsertAnalysisResult(conn, lessonA, "analyze_corrections", promptID, "deepseek", "[]", "a.json"); err != nil {
		t.Fatalf("UpsertAnalysisResult() corrections lessonA erro: %v", err)
	}
	if err := UpsertAnalysisResult(conn, lessonA, "analyze_topics", promptID, "deepseek", "[]", "a.topics.json"); err != nil {
		t.Fatalf("UpsertAnalysisResult() topics lessonA erro: %v", err)
	}
	if err := UpsertAnalysisResult(conn, lessonB, "analyze_corrections", promptID, "deepseek", "[]", "b.json"); err != nil {
		t.Fatalf("UpsertAnalysisResult() lessonB erro: %v", err)
	}

	// lesson_topics de lessonA deve sobreviver à troca de falante.
	topicID, err := GetOrCreateTopicByName(conn, "viagens")
	if err != nil {
		t.Fatalf("GetOrCreateTopicByName() erro inesperado: %v", err)
	}
	if err := AddLessonTopic(conn, lessonA, topicID); err != nil {
		t.Fatalf("AddLessonTopic() erro inesperado: %v", err)
	}

	if err := DeleteSpeakerDependentAnalysisResults(conn, lessonA); err != nil {
		t.Fatalf("DeleteSpeakerDependentAnalysisResults() erro inesperado: %v", err)
	}

	var topicsCount, correctionsCount int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM analysis_results WHERE lesson_id = ? AND task = 'analyze_topics'`, lessonA).Scan(&topicsCount); err != nil {
		t.Fatalf("contar topics de lessonA falhou: %v", err)
	}
	if err := conn.QueryRow(`SELECT COUNT(*) FROM analysis_results WHERE lesson_id = ? AND task = 'analyze_corrections'`, lessonA).Scan(&correctionsCount); err != nil {
		t.Fatalf("contar corrections de lessonA falhou: %v", err)
	}
	if topicsCount != 1 {
		t.Errorf("topicsCount = %d, esperado 1 (preservado)", topicsCount)
	}
	if correctionsCount != 0 {
		t.Errorf("correctionsCount = %d, esperado 0 (apagado)", correctionsCount)
	}

	var lessonTopicsCount int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM lesson_topics WHERE lesson_id = ?`, lessonA).Scan(&lessonTopicsCount); err != nil {
		t.Fatalf("contar lesson_topics de lessonA falhou: %v", err)
	}
	if lessonTopicsCount != 1 {
		t.Errorf("lessonTopicsCount = %d, esperado 1 (preservado)", lessonTopicsCount)
	}

	var lessonBCount int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM analysis_results WHERE lesson_id = ?`, lessonB).Scan(&lessonBCount); err != nil {
		t.Fatalf("contar analysis_results de lessonB falhou: %v", err)
	}
	if lessonBCount != 1 {
		t.Errorf("lessonBCount = %d, esperado 1 (não afetada)", lessonBCount)
	}
}
```

- [ ] **Step 2: Rodar para ver falhar**

Run: `go test ./internal/db/ -run TestDeleteSpeakerDependentAnalysisResults -v`
Expected: FAIL — `undefined: DeleteSpeakerDependentAnalysisResults`.

- [ ] **Step 3: Renomear e mudar a função**

Em `internal/db/analysis_results.go`, substituir `DeleteAnalysisResultsForLesson` por:

```go
// DeleteSpeakerDependentAnalysisResults apaga as análises que dependem de
// quem é aluno/tutor na aula (tudo exceto analyze_topics) — chamada quando o
// mapeamento aluno/tutor muda (services.LibraryService.SetStudentSpeaker).
// analyze_topics não menciona Aluno/Tutor no prompt e sobrevive; lesson_topics
// também fica intacto (a tabela é a fonte da verdade dos chips, independente
// da análise).
func DeleteSpeakerDependentAnalysisResults(conn *sql.DB, lessonID int64) error {
	if _, err := conn.Exec(`DELETE FROM analysis_results WHERE lesson_id = ? AND task != 'analyze_topics'`, lessonID); err != nil {
		return fmt.Errorf("apagar análises dependentes de falante da lesson %d: %w", lessonID, err)
	}
	return nil
}
```

- [ ] **Step 4: Atualizar o chamador**

Em `services/library.go`, na função `SetStudentSpeaker` (linha ~179), trocar:

```go
		if err := db.DeleteAnalysisResultsForLesson(s.conn, lessonID); err != nil {
```

por:

```go
		if err := db.DeleteSpeakerDependentAnalysisResults(s.conn, lessonID); err != nil {
```

- [ ] **Step 5: Rodar os testes**

Run: `go test ./internal/db/ ./services/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/db/analysis_results.go internal/db/analysis_results_test.go services/library.go
git commit -m "feat: troca de falante preserva tópicos (deleção seletiva)"
```

---

### Task 4: Pacote `internal/analysis` — `NewTopicsTask` v2, `ParseTopicsResult`, `AppendExistingTopics` + prompt v2

**Files:**
- Create: `prompts/analyze-topics-v2.md`
- Modify: `internal/analysis/tasks_topics.go`
- Modify: `internal/analysis/tasks_topics_test.go`

**Interfaces:**
- Consumes: `mustLoadPrompt`, `task[T]`, `parseTopics` (existem).
- Produces: `func NewTopicsTask() TaskDef` (version 2, prompt v2), `func ParseTopicsResult(resultJSON json.RawMessage) ([]string, error)`, `func AppendExistingTopics(transcript string, existing []string) string`.

- [ ] **Step 1: Escrever o prompt v2**

Criar `prompts/analyze-topics-v2.md`:

```markdown
# Prompt de análise — Tópicos da aula (v2)

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
2. Mantenha os tópicos **gerais**, no nível de uma etiqueta de busca: prefira "viagens" a "visto de
   turista para os EUA". Não detalhe além disso — a granularidade ideal será calibrada depois com
   exemplos.
3. Liste só os assuntos que de fato tomaram um trecho relevante da conversa — não liste comentários
   passageiros de uma frase só.
4. Não use uma taxonomia fixa nem categorias pré-definidas — a lista é livre, específica da aula.
5. **Reaproveitamento:** se a mensagem seguinte incluir uma seção "Tópicos já utilizados em outras
   aulas", reutilize um tópico dessa lista quando ele se aplicar a esta aula, em vez de criar uma
   variação redundante do mesmo assunto.
6. Não repita o mesmo tópico com palavras diferentes.
7. Se não for possível identificar nenhum tópico claro, devolva uma lista vazia (`[]`).
8. A resposta deve ser **apenas o objeto JSON** acima: sem markdown, sem comentários, sem texto
   explicativo fora do JSON.

A transcrição da aula será enviada na mensagem seguinte, com cada fala numerada e rotulada
"Aluno:" ou "Tutor:".
```

- [ ] **Step 2: Escrever os testes (falham)**

Em `internal/analysis/tasks_topics_test.go`, adicionar:

```go
func TestNewTopicsTask_HasNameVersionAndPrompt(t *testing.T) {
	tk := NewTopicsTask()
	if tk.Name() != "analyze_topics" {
		t.Errorf("Name() = %q, esperado analyze_topics", tk.Name())
	}
	if tk.Version() != 2 {
		t.Errorf("Version() = %d, esperado 2", tk.Version())
	}
	if tk.Prompt() == "" {
		t.Error("Prompt() vazio, esperado conteúdo do v2")
	}
}

func TestParseTopicsResult_Valid(t *testing.T) {
	resultJSON := json.RawMessage(`["viagens","trabalho remoto"]`)
	got, err := ParseTopicsResult(resultJSON)
	if err != nil {
		t.Fatalf("ParseTopicsResult erro inesperado: %v", err)
	}
	if len(got) != 2 || got[0] != "viagens" || got[1] != "trabalho remoto" {
		t.Errorf("got = %+v, inesperado", got)
	}
}

func TestParseTopicsResult_InvalidJSON(t *testing.T) {
	if _, err := ParseTopicsResult(json.RawMessage("not json")); err == nil {
		t.Fatal("esperava erro para JSON inválido, obteve nil")
	}
}

func TestAppendExistingTopics_EmptyLeavesTranscriptUnchanged(t *testing.T) {
	got := AppendExistingTopics("linha", nil)
	if got != "linha" {
		t.Errorf("got = %q, esperado transcript inalterado", got)
	}
}

func TestAppendExistingTopics_AppendsBlock(t *testing.T) {
	got := AppendExistingTopics("linha", []string{"viagens", "trabalho remoto"})
	want := `linha

Tópicos já utilizados em outras aulas (reutilize quando fizer sentido):
- viagens
- trabalho remoto
`
	if got != want {
		t.Errorf("got = %q, esperado %q", got, want)
	}
}
```

- [ ] **Step 3: Rodar para ver falhar**

Run: `go test ./internal/analysis/ -run 'TestNewTopicsTask|TestParseTopicsResult|TestAppendExistingTopics' -v`
Expected: FAIL — `undefined: NewTopicsTask`, `undefined: ParseTopicsResult`, `undefined: AppendExistingTopics`.

- [ ] **Step 4: Implementar**

Em `internal/analysis/tasks_topics.go`, substituir o conteúdo por:

```go
// internal/analysis/tasks_topics.go
package analysis

import (
	"encoding/json"
	"fmt"
	"strings"
)

func parseTopics(raw json.RawMessage, utteranceCount int) ([]string, error) {
	var parsed struct {
		Topics []string `json:"topics"`
	}
	if err := unmarshalJSON(raw, &parsed); err != nil {
		return nil, err
	}
	return parsed.Topics, nil
}

// NewTopicsTask devolve a tarefa de tópicos na versão 2 do prompt
// (granularidade geral + reaproveitamento de tópicos existentes).
func NewTopicsTask() TaskDef {
	return task[[]string]{name: "analyze_topics", version: 2, prompt: mustLoadPrompt("analyze-topics-v2.md"), parse: parseTopics}
}

// ParseTopicsResult decodifica um result_json persistido (array JSON de
// strings — resultJSON é json.Marshal([]string), sem envelope) de volta em
// []string.
func ParseTopicsResult(resultJSON json.RawMessage) ([]string, error) {
	var out []string
	if err := json.Unmarshal(resultJSON, &out); err != nil {
		return nil, fmt.Errorf("analysis: desserializar resultado de analyze_topics: %w", err)
	}
	return out, nil
}

// AppendExistingTopics anexa a lista de tópicos já existentes ao conteúdo da
// transcrição, num bloco final que o prompt v2 reconhece. Devolve transcript
// inalterado quando não há tópicos existentes.
func AppendExistingTopics(transcript string, existing []string) string {
	if len(existing) == 0 {
		return transcript
	}
	var b strings.Builder
	b.WriteString(transcript)
	b.WriteString("\n\nTópicos já utilizados em outras aulas (reutilize quando fizer sentido):\n")
	for _, t := range existing {
		b.WriteString("- ")
		b.WriteString(t)
		b.WriteString("\n")
	}
	return b.String()
}
```

- [ ] **Step 5: Rodar os testes**

Run: `go test ./internal/analysis/ -v`
Expected: PASS (inclui os `TestParseTopics_*` existentes, que continuam passando — `parseTopics` não mudou).

- [ ] **Step 6: Commit**

```bash
git add prompts/analyze-topics-v2.md internal/analysis/tasks_topics.go internal/analysis/tasks_topics_test.go
git commit -m "feat: tarefa de tópicos v2 (granularidade + reaproveitamento)"
```

---

### Task 5: `services/topics.go` — `Topic` + `TopicsService`

**Files:**
- Create: `services/topics.go`
- Create: `services/topics_test.go`

**Interfaces:**
- Consumes: `db.GetOrCreateTopicByName`, `db.AddLessonTopic`, `db.RemoveLessonTopic`, `db.ListTopics`, `db.RenameTopic` (Task 2); helpers `openTestDB`/`mustInsertLesson`/`testStorageRoot` (existem).
- Produces: `type Topic struct { ID int64 `json:"id"`; Name string `json:"name"` }`, `type TopicsService struct{}`, `NewTopicsService(conn) *TopicsService`, `(*TopicsService) ListTopics() ([]Topic, error)`, `RenameTopic(id, newName) error`, `AddTopic(lessonID, name) (Topic, error)`, `RemoveTopic(lessonID, topicID) error`.

- [ ] **Step 1: Escrever o teste (falha)**

Criar `services/topics_test.go`:

```go
package services

import (
	"path/filepath"
	"testing"

	"assistente-idiomas/internal/db"
)

func TestTopicsService_ListTopics(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	svc := NewTopicsService(conn)
	if _, err := svc.AddTopic(0, "viagens"); err != nil {
		t.Fatalf("AddTopic() erro inesperado: %v", err)
	}
	topics, err := svc.ListTopics()
	if err != nil {
		t.Fatalf("ListTopics() erro inesperado: %v", err)
	}
	if len(topics) != 1 || topics[0].Name != "viagens" {
		t.Errorf("ListTopics() = %+v, esperado [viagens]", topics)
	}
}

func TestTopicsService_AddTopic_ReusesEntity(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLesson(t, conn, "2026-08-18", "Sarah M.", "aula.mp4")
	svc := NewTopicsService(conn)

	t1, err := svc.AddTopic(lessonID, "viagens")
	if err != nil {
		t.Fatalf("AddTopic() erro inesperado: %v", err)
	}
	t2, err := svc.AddTopic(lessonID, "  viagens  ")
	if err != nil {
		t.Fatalf("segunda AddTopic() erro inesperado: %v", err)
	}
	if t1.ID != t2.ID {
		t.Errorf("t2.ID = %d, esperado igual a t1.ID = %d (mesma entidade)", t2.ID, t1.ID)
	}
	topics, err := db.ListLessonTopics(conn, lessonID)
	if err != nil {
		t.Fatalf("ListLessonTopics() erro inesperado: %v", err)
	}
	if len(topics) != 1 {
		t.Errorf("len(topics) = %d, esperado 1 (INSERT OR IGNORE)", len(topics))
	}
}

func TestTopicsService_RemoveTopic_UnlinksOnly(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLesson(t, conn, "2026-08-18", "Sarah M.", "aula.mp4")
	svc := NewTopicsService(conn)

	topic, err := svc.AddTopic(lessonID, "viagens")
	if err != nil {
		t.Fatalf("AddTopic() erro inesperado: %v", err)
	}
	if err := svc.RemoveTopic(lessonID, topic.ID); err != nil {
		t.Fatalf("RemoveTopic() erro inesperado: %v", err)
	}
	topics, err := db.ListLessonTopics(conn, lessonID)
	if err != nil {
		t.Fatalf("ListLessonTopics() erro inesperado: %v", err)
	}
	if len(topics) != 0 {
		t.Errorf("len(topics) = %d, esperado 0 (desvinculado)", len(topics))
	}
	all, err := db.ListTopics(conn)
	if err != nil {
		t.Fatalf("ListTopics() erro inesperado: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("len(all) = %d, esperado 1 (entidade não apagada)", len(all))
	}
}

func TestTopicsService_RenameTopic_ReflectsGlobally(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	svc := NewTopicsService(conn)
	topic, err := svc.AddTopic(0, "viagens")
	if err != nil {
		t.Fatalf("AddTopic() erro inesperado: %v", err)
	}
	if err := svc.RenameTopic(topic.ID, "planos de viagem"); err != nil {
		t.Fatalf("RenameTopic() erro inesperado: %v", err)
	}
	topics, err := svc.ListTopics()
	if err != nil {
		t.Fatalf("ListTopics() erro inesperado: %v", err)
	}
	if len(topics) != 1 || topics[0].Name != "planos de viagem" {
		t.Errorf("ListTopics() = %+v, esperado [planos de viagem]", topics)
	}
}
```

- [ ] **Step 2: Rodar para ver falhar**

Run: `go test ./services/ -run 'TestTopicsService' -v`
Expected: FAIL — `undefined: NewTopicsService`.

- [ ] **Step 3: Implementar `services/topics.go`**

```go
// services/topics.go
package services

import (
	"database/sql"
	"fmt"
	"strings"

	"assistente-idiomas/internal/db"
)

// Topic é um tópico cadastrado, no formato exposto ao frontend — entidade
// reutilizada por TopicsService (gestão) e por AnalysisService (TopicsResult).
type Topic struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// TopicsService cobre a gestão de tópicos como entidade (História 3): listar
// pro painel de Configurações, renomear globalmente, e adicionar/remover o
// vínculo de um tópico a uma aula específica (chips do Detalhe).
type TopicsService struct {
	conn *sql.DB
}

func NewTopicsService(conn *sql.DB) *TopicsService {
	return &TopicsService{conn: conn}
}

func (s *TopicsService) ListTopics() ([]Topic, error) {
	rows, err := db.ListTopics(s.conn)
	if err != nil {
		return nil, err
	}
	out := make([]Topic, 0, len(rows))
	for _, r := range rows {
		out = append(out, Topic{ID: r.ID, Name: r.Name})
	}
	return out, nil
}

func (s *TopicsService) RenameTopic(id int64, newName string) error {
	return db.RenameTopic(s.conn, id, newName)
}

// AddTopic resolve o nome para uma entidade (criando se necessário) e vincula
// à lessonID. Devolve o tópico resolvido; o frontend re-busca GetTopics depois
// para reconciliar nomes canônicos.
func (s *TopicsService) AddTopic(lessonID int64, name string) (Topic, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Topic{}, fmt.Errorf("tópico não pode ser vazio")
	}
	id, err := db.GetOrCreateTopicByName(s.conn, name)
	if err != nil {
		return Topic{}, err
	}
	if err := db.AddLessonTopic(s.conn, lessonID, id); err != nil {
		return Topic{}, err
	}
	return Topic{ID: id, Name: name}, nil
}

// RemoveTopic desvincula topicID de lessonID (não apaga a entidade).
func (s *TopicsService) RemoveTopic(lessonID, topicID int64) error {
	return db.RemoveLessonTopic(s.conn, lessonID, topicID)
}
```

- [ ] **Step 4: Rodar os testes**

Run: `go test ./services/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add services/topics.go services/topics_test.go
git commit -m "feat: TopicsService (gestão de tópicos como entidade)"
```

---

### Task 6: `services/analysis.go` — `TopicsResult` + Get/Analyze/ReprocessTopics

**Files:**
- Modify: `services/analysis.go`
- Modify: `services/analysis_test.go` (estender o fake + novos testes)

**Interfaces:**
- Consumes: `db.FindLessonByID`, `db.FindTranscriptByLessonID`, `db.FindAnalysisResult`, `db.ListTopics`, `db.ListLessonTopics`, `db.GetOrCreateTopicByName`, `db.ReplaceLessonTopics`, `db.UpsertPrompt`, `db.UpsertAnalysisResult`; `analysis.NewTopicsTask`/`ParseTopicsResult`/`AppendExistingTopics`/`FormatTranscript`; `analysisRawRelPath`/`writeRawResponse` (existentes neste arquivo); `Topic` (Task 5).
- Produces: `type TopicsResult struct { Analyzed bool `json:"analyzed"`; Items []Topic `json:"items"` }`, `(*AnalysisService) GetTopics(lessonID int64) (TopicsResult, error)`, `AnalyzeTopics(...)`, `ReprocessTopics(...)`.

- [ ] **Step 1: Estender o fake provider para capturar o input**

Em `services/analysis_test.go`, adicionar o campo `lastInput string` ao struct `fakeAnalysisProvider` e gravá-lo em `Complete`:

```go
type fakeAnalysisProvider struct {
	model     string
	raw       json.RawMessage
	err       error
	calls     int
	lastInput string
}

func (p *fakeAnalysisProvider) Complete(ctx context.Context, systemPrompt, transcript string) (json.RawMessage, error) {
	p.calls++
	p.lastInput = transcript
	return p.raw, p.err
}
```

- [ ] **Step 2: Escrever os testes (falham)**

Adicionar a `services/analysis_test.go`:

```go
func TestAnalysisService_GetTopics_NotAnalyzedYet(t *testing.T) {
	conn := openTestDB(t)
	lessonID := mustInsertLesson(t, conn, "2026-08-01", "Sarah M.", "aula.mp4")

	svc := NewAnalysisService(conn, testStorageRoot(t), nil)
	got, err := svc.GetTopics(lessonID)
	if err != nil {
		t.Fatalf("GetTopics() erro inesperado: %v", err)
	}
	if got.Analyzed {
		t.Error("GetTopics().Analyzed = true, esperado false")
	}
	if len(got.Items) != 0 {
		t.Errorf("GetTopics().Items = %+v, esperado vazio", got.Items)
	}
}

func TestAnalysisService_AnalyzeTopics_PersistsBothAndAppendsExisting(t *testing.T) {
	conn := openTestDB(t)
	lessonID := mustInsertLesson(t, conn, "2026-08-01", "Sarah M.", "aulas/2026/aula.mp4")
	if err := db.SetStudentSpeaker(conn, lessonID, "speaker_0"); err != nil {
		t.Fatalf("SetStudentSpeaker() de fixture falhou: %v", err)
	}
	insertTranscriptFixture(t, conn, lessonID, "aulas/2026/aula.transcript.json", []map[string]any{
		{"Speaker": "speaker_0", "Text": "I want to travel", "Start": 0, "End": 2000000000},
		{"Speaker": "speaker_1", "Text": "Where to?", "Start": 2000000000, "End": 4000000000},
	})
	if _, err := db.GetOrCreateTopicByName(conn, "viagens"); err != nil {
		t.Fatalf("GetOrCreateTopicByName() de fixture falhou: %v", err)
	}

	fake := &fakeAnalysisProvider{
		model: "deepseek-v4-flash",
		raw:   json.RawMessage(`{"topics":["viagens","trabalho remoto"]}`),
	}
	svc := NewAnalysisService(conn, testStorageRoot(t), func() (analysis.Provider, error) { return fake, nil })

	got, err := svc.AnalyzeTopics(lessonID)
	if err != nil {
		t.Fatalf("AnalyzeTopics() erro inesperado: %v", err)
	}
	if !got.Analyzed || len(got.Items) != 2 {
		t.Fatalf("AnalyzeTopics() = %+v, esperado Analyzed=true e 2 itens", got)
	}
	if fake.calls != 1 {
		t.Errorf("provider chamado %d vezes, esperado 1", fake.calls)
	}
	if !strings.Contains(fake.lastInput, "Tópicos já utilizados") || !strings.Contains(fake.lastInput, "viagens") {
		t.Errorf("lastInput não contém a lista de reaproveitamento: %q", fake.lastInput)
	}

	topics, err := db.ListLessonTopics(conn, lessonID)
	if err != nil {
		t.Fatalf("ListLessonTopics() erro inesperado: %v", err)
	}
	if len(topics) != 2 {
		t.Errorf("lesson_topics = %+v, esperado 2 vínculos persistidos", topics)
	}
	persisted, err := db.FindAnalysisResult(conn, lessonID, "analyze_topics")
	if err != nil {
		t.Fatalf("FindAnalysisResult() erro inesperado: %v", err)
	}
	if persisted == nil {
		t.Fatal("FindAnalysisResult() = nil, esperado persistido")
	}
}

func TestAnalysisService_AnalyzeTopics_IsIdempotent(t *testing.T) {
	conn := openTestDB(t)
	lessonID := mustInsertLesson(t, conn, "2026-08-01", "Sarah M.", "aula.mp4")
	if err := db.SetStudentSpeaker(conn, lessonID, "speaker_0"); err != nil {
		t.Fatalf("SetStudentSpeaker() falhou: %v", err)
	}
	insertTranscriptFixture(t, conn, lessonID, "aula.transcript.json", []map[string]any{
		{"Speaker": "speaker_0", "Text": "Hello", "Start": 0, "End": 1000000000},
	})

	fake := &fakeAnalysisProvider{model: "deepseek-v4-flash", raw: json.RawMessage(`{"topics":["viagens"]}`)}
	svc := NewAnalysisService(conn, testStorageRoot(t), func() (analysis.Provider, error) { return fake, nil })

	if _, err := svc.AnalyzeTopics(lessonID); err != nil {
		t.Fatalf("primeira AnalyzeTopics() erro: %v", err)
	}
	got, err := svc.AnalyzeTopics(lessonID)
	if err != nil {
		t.Fatalf("segunda AnalyzeTopics() erro: %v", err)
	}
	if fake.calls != 1 {
		t.Errorf("provider chamado %d vezes, esperado 1 (idempotente)", fake.calls)
	}
	if !got.Analyzed || len(got.Items) != 1 {
		t.Errorf("segunda AnalyzeTopics() = %+v, esperado resultado persistido", got)
	}
}

func TestAnalysisService_ReprocessTopics_Overwrites(t *testing.T) {
	conn := openTestDB(t)
	lessonID := mustInsertLesson(t, conn, "2026-08-01", "Sarah M.", "aula.mp4")
	if err := db.SetStudentSpeaker(conn, lessonID, "speaker_0"); err != nil {
		t.Fatalf("SetStudentSpeaker() falhou: %v", err)
	}
	insertTranscriptFixture(t, conn, lessonID, "aula.transcript.json", []map[string]any{
		{"Speaker": "speaker_0", "Text": "Hello", "Start": 0, "End": 1000000000},
	})

	fake := &fakeAnalysisProvider{model: "deepseek-v4-flash", raw: json.RawMessage(`{"topics":["viagens"]}`)}
	svc := NewAnalysisService(conn, testStorageRoot(t), func() (analysis.Provider, error) { return fake, nil })

	if _, err := svc.AnalyzeTopics(lessonID); err != nil {
		t.Fatalf("AnalyzeTopics() erro: %v", err)
	}
	fake.raw = json.RawMessage(`{"topics":["trabalho remoto"]}`)
	got, err := svc.ReprocessTopics(lessonID)
	if err != nil {
		t.Fatalf("ReprocessTopics() erro: %v", err)
	}
	if fake.calls != 2 {
		t.Errorf("provider chamado %d vezes, esperado 2", fake.calls)
	}
	topics, err := db.ListLessonTopics(conn, lessonID)
	if err != nil {
		t.Fatalf("ListLessonTopics() erro: %v", err)
	}
	if len(topics) != 1 || topics[0].Name != "trabalho remoto" {
		t.Errorf("lesson_topics = %+v, esperado [trabalho remoto] (substituído)", topics)
	}
	_ = got
}

func TestAnalysisService_AnalyzeTopics_RequiresStudentSpeakerChosen(t *testing.T) {
	conn := openTestDB(t)
	lessonID := mustInsertLesson(t, conn, "2026-08-01", "Sarah M.", "aula.mp4")
	insertTranscriptFixture(t, conn, lessonID, "aula.transcript.json", []map[string]any{
		{"Speaker": "speaker_0", "Text": "Hello", "Start": 0, "End": 1000000000},
	})

	svc := NewAnalysisService(conn, testStorageRoot(t), func() (analysis.Provider, error) {
		t.Fatal("providerFactory não deveria ser chamado sem student_speaker_label")
		return nil, nil
	})
	if _, err := svc.AnalyzeTopics(lessonID); err == nil {
		t.Fatal("AnalyzeTopics() esperava erro sem student_speaker_label, veio nil")
	}
}

func TestAnalysisService_AnalyzeTopics_ProviderErrorDoesNotPersist(t *testing.T) {
	conn := openTestDB(t)
	lessonID := mustInsertLesson(t, conn, "2026-08-01", "Sarah M.", "aula.mp4")
	if err := db.SetStudentSpeaker(conn, lessonID, "speaker_0"); err != nil {
		t.Fatalf("SetStudentSpeaker() falhou: %v", err)
	}
	insertTranscriptFixture(t, conn, lessonID, "aula.transcript.json", []map[string]any{
		{"Speaker": "speaker_0", "Text": "Hello", "Start": 0, "End": 1000000000},
	})

	fake := &fakeAnalysisProvider{err: fmt.Errorf("erro de rede simulado")}
	svc := NewAnalysisService(conn, testStorageRoot(t), func() (analysis.Provider, error) { return fake, nil })

	if _, err := svc.AnalyzeTopics(lessonID); err == nil {
		t.Fatal("AnalyzeTopics() esperava erro do provider, veio nil")
	}
	persisted, err := db.FindAnalysisResult(conn, lessonID, "analyze_topics")
	if err != nil {
		t.Fatalf("FindAnalysisResult() erro inesperado: %v", err)
	}
	if persisted != nil {
		t.Errorf("FindAnalysisResult() = %+v, esperado nil (não persistir em falha)", persisted)
	}
	topics, err := db.ListLessonTopics(conn, lessonID)
	if err != nil {
		t.Fatalf("ListLessonTopics() erro inesperado: %v", err)
	}
	if len(topics) != 0 {
		t.Errorf("lesson_topics = %+v, esperado vazio (não persistir em falha)", topics)
	}
}
```

**Nota:** os novos testes usam `strings` e `fmt` — `services/analysis_test.go` já importa `fmt`; adicionar `"strings"` ao bloco de imports.

- [ ] **Step 3: Rodar para ver falhar**

Run: `go test ./services/ -run 'TestAnalysisService_.*Topic' -v`
Expected: FAIL — `undefined: svc.GetTopics` (etc.) e `TopicsResult` inexistente.

- [ ] **Step 4: Implementar em `services/analysis.go`**

Adicionar após o bloco de correções (reaproveita `analysisRawRelPath`/`writeRawResponse`/imports já presentes):

```go
const topicsTaskName = "analyze_topics"

// TopicsResult é o resultado de analyze_topics exposto ao frontend. Items
// vem de lesson_topics (fonte da verdade, editável); Analyzed indica se a
// tarefa já rodou (linha em analysis_results).
type TopicsResult struct {
	Analyzed bool    `json:"analyzed"`
	Items    []Topic `json:"items"`
}

// GetTopics devolve os tópicos atuais da lesson (de lesson_topics) sem chamar
// a API — Analyzed == false se a tarefa nunca rodou.
func (s *AnalysisService) GetTopics(lessonID int64) (TopicsResult, error) {
	return s.currentTopics(lessonID)
}

func (s *AnalysisService) AnalyzeTopics(lessonID int64) (TopicsResult, error) {
	return s.runTopics(lessonID, false)
}

func (s *AnalysisService) ReprocessTopics(lessonID int64) (TopicsResult, error) {
	return s.runTopics(lessonID, true)
}

func (s *AnalysisService) currentTopics(lessonID int64) (TopicsResult, error) {
	items, err := db.ListLessonTopics(s.conn, lessonID)
	if err != nil {
		return TopicsResult{}, fmt.Errorf("buscar tópicos da lesson %d: %w", lessonID, err)
	}
	result, err := db.FindAnalysisResult(s.conn, lessonID, topicsTaskName)
	if err != nil {
		return TopicsResult{}, fmt.Errorf("buscar análise de tópicos da lesson %d: %w", lessonID, err)
	}
	return TopicsResult{Analyzed: result != nil, Items: toTopics(items)}, nil
}

func toTopics(items []db.Topic) []Topic {
	out := make([]Topic, 0, len(items))
	for _, it := range items {
		out = append(out, Topic{ID: it.ID, Name: it.Name})
	}
	return out
}

func (s *AnalysisService) runTopics(lessonID int64, overwrite bool) (TopicsResult, error) {
	lesson, err := db.FindLessonByID(s.conn, lessonID)
	if err != nil {
		return TopicsResult{}, fmt.Errorf("buscar lesson %d: %w", lessonID, err)
	}
	if lesson == nil {
		return TopicsResult{}, fmt.Errorf("aula %d não encontrada", lessonID)
	}
	if lesson.StudentSpeakerLabel == nil {
		return TopicsResult{}, fmt.Errorf("escolha quem é você na aula antes de analisar tópicos")
	}

	transcript, err := db.FindTranscriptByLessonID(s.conn, lessonID)
	if err != nil {
		return TopicsResult{}, fmt.Errorf("buscar transcrição da lesson %d: %w", lessonID, err)
	}
	if transcript == nil {
		return TopicsResult{}, fmt.Errorf("aula %d ainda não tem transcrição", lessonID)
	}

	if !overwrite {
		existing, err := db.FindAnalysisResult(s.conn, lessonID, topicsTaskName)
		if err != nil {
			return TopicsResult{}, fmt.Errorf("buscar análise de tópicos da lesson %d: %w", lessonID, err)
		}
		if existing != nil {
			return s.currentTopics(lessonID)
		}
	}

	speakerRoles := make(map[string]string, len(transcript.Utterances))
	for _, u := range transcript.Utterances {
		if u.Speaker == *lesson.StudentSpeakerLabel {
			speakerRoles[u.Speaker] = "aluno"
		} else {
			speakerRoles[u.Speaker] = "tutor"
		}
	}
	formatted, err := analysis.FormatTranscript(transcript.Utterances, speakerRoles)
	if err != nil {
		return TopicsResult{}, fmt.Errorf("formatar transcrição da lesson %d: %w", lessonID, err)
	}

	existingTopics, err := db.ListTopics(s.conn)
	if err != nil {
		return TopicsResult{}, fmt.Errorf("listar tópicos existentes: %w", err)
	}
	names := make([]string, 0, len(existingTopics))
	for _, t := range existingTopics {
		names = append(names, t.Name)
	}
	input := analysis.AppendExistingTopics(formatted, names)

	provider, err := s.providerFactory()
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return TopicsResult{}, fmt.Errorf("configure a credencial do provedor de análise em Configurações")
		}
		return TopicsResult{}, fmt.Errorf("obter provedor de análise: %w", err)
	}

	task := analysis.NewTopicsTask()
	resultJSON, raw, err := task.Execute(context.Background(), provider, input, len(transcript.Utterances))
	if raw != nil {
		if writeErr := s.writeRawResponse(lesson.VideoPath, task.Name(), raw); writeErr != nil {
			slog.Warn("analysis: falha ao gravar resposta bruta em disco", "lesson_id", lessonID, "task", task.Name(), "erro", writeErr)
		}
	}
	if err != nil {
		return TopicsResult{}, fmt.Errorf("analisar tópicos da lesson %d: %w", lessonID, err)
	}

	topics, err := analysis.ParseTopicsResult(resultJSON)
	if err != nil {
		return TopicsResult{}, fmt.Errorf("desserializar tópicos: %w", err)
	}
	ids := make([]int64, 0, len(topics))
	for _, name := range topics {
		id, err := db.GetOrCreateTopicByName(s.conn, name)
		if err != nil {
			return TopicsResult{}, fmt.Errorf("registrar tópico %q: %w", name, err)
		}
		ids = append(ids, id)
	}
	if err := db.ReplaceLessonTopics(s.conn, lessonID, ids); err != nil {
		return TopicsResult{}, fmt.Errorf("gravar tópicos da lesson %d: %w", lessonID, err)
	}

	promptID, err := db.UpsertPrompt(s.conn, task.Name(), task.Version(), task.Prompt())
	if err != nil {
		return TopicsResult{}, fmt.Errorf("registrar prompt %s: %w", task.Name(), err)
	}
	rawRelPath := analysisRawRelPath(lesson.VideoPath, task.Name())
	if err := db.UpsertAnalysisResult(s.conn, lessonID, task.Name(), promptID, provider.Model(), string(resultJSON), rawRelPath); err != nil {
		return TopicsResult{}, fmt.Errorf("gravar resultado da análise: %w", err)
	}

	return s.currentTopics(lessonID)
}
```

- [ ] **Step 5: Rodar os testes**

Run: `go test ./services/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add services/analysis.go services/analysis_test.go
git commit -m "feat: análise de tópicos sob demanda (Get/Analyze/Reprocess)"
```

---

### Task 7: Registrar `TopicsService` no `main.go`

**Files:**
- Modify: `main.go`

- [ ] **Step 1: Adicionar o serviço**

Em `main.go`, na lista `Services`, após `application.NewService(services.NewTeacherService(conn))`, adicionar:

```go
			application.NewService(services.NewTopicsService(conn)),
```

- [ ] **Step 2: Compilar**

Run: `go build ./...`
Expected: PASS (sem erros).

- [ ] **Step 3: Commit**

```bash
git add main.go
git commit -m "feat: registra TopicsService no app Wails"
```

---

### Task 8: Bindings + frontend (chips no Detalhe + painel Tópicos em Configurações)

**Files:**
- Modify: `frontend/src/lib/screens/LessonDetail.svelte`
- Modify: `frontend/src/lib/screens/Settings.svelte`
- (Gerados automaticamente, não editar à mão): `frontend/bindings/assistente-idiomas/services/topicsservice.ts`, `.../analysisservice.ts`, `.../models.ts`

- [ ] **Step 1: Regenerar bindings**

Run: `wails3 generate bindings -ts -i ./...`
Expected: gera `topicsservice.ts` (TopicsService), novos métodos em `analysisservice.ts` (GetTopics/AnalyzeTopics/ReprocessTopics) e `Topic`/`TopicsResult` em `models.ts`.

- [ ] **Step 2: LessonDetail — imports e estado**

Em `LessonDetail.svelte`, adicionar os imports e o estado. Trocar a linha de import de models para incluir `TopicsResult`, e adicionar o import do TopicsService:

```ts
  import * as TopicsService from "../../../bindings/assistente-idiomas/services/topicsservice";
  import type { Lesson, Transcript, CorrectionsResult, TopicsResult } from "../../../bindings/assistente-idiomas/services/models";
```

E, junto dos outros `$state`, adicionar:

```ts
  let topics: TopicsResult | null = $state(null);
  let analyzingTopics: boolean = $state(false);
  let topicsError: string = $state("");
  let newTopicName: string = $state("");
```

- [ ] **Step 3: LessonDetail — funções**

Adicionar (perto de `fetchCorrectionsIfReady`/`analyzeCorrections`):

```ts
  async function fetchTopicsIfReady() {
    if (!lesson || lesson.status !== "pronta") {
      topics = null;
      return;
    }
    try {
      topics = await AnalysisService.GetTopics(lessonId);
    } catch {
      topics = null;
    }
  }

  async function analyzeTopics() {
    if ((topics?.items?.length ?? 0) > 0) {
      const confirmed = confirm("Isso substitui os tópicos atuais e gera uma nova chamada à API. Continuar?");
      if (!confirmed) return;
    }
    topicsError = "";
    analyzingTopics = true;
    try {
      topics = await AnalysisService.AnalyzeTopics(lessonId);
    } catch (e) {
      topicsError = String(e);
    } finally {
      analyzingTopics = false;
    }
  }

  async function reprocessTopics() {
    const confirmed = confirm("Isso substitui os tópicos atuais e gera uma nova chamada à API. Continuar?");
    if (!confirmed) return;
    topicsError = "";
    analyzingTopics = true;
    try {
      topics = await AnalysisService.ReprocessTopics(lessonId);
    } catch (e) {
      topicsError = String(e);
    } finally {
      analyzingTopics = false;
    }
  }

  async function addTopic() {
    const name = newTopicName.trim();
    if (!name) return;
    topicsError = "";
    try {
      await TopicsService.AddTopic(lessonId, name);
      newTopicName = "";
      topics = await AnalysisService.GetTopics(lessonId);
    } catch (e) {
      topicsError = String(e);
    }
  }

  async function removeTopic(topicId: number) {
    topicsError = "";
    try {
      await TopicsService.RemoveTopic(lessonId, topicId);
      topics = await AnalysisService.GetTopics(lessonId);
    } catch (e) {
      topicsError = String(e);
    }
  }
```

- [ ] **Step 4: LessonDetail — chamar `fetchTopicsIfReady`**

Em `refreshAfterSpeakerChange` (após `await fetchCorrectionsIfReady()`) e em `onMount` (após `await fetchCorrectionsIfReady()`), adicionar `await fetchTopicsIfReady();`.

- [ ] **Step 5: LessonDetail — markup dos chips**

Logo após o bloco `.header-row` (após o `</div>` do header-row, antes do `<div class="grid">`), adicionar:

```svelte
    <div class="topics-row">
      {#each topics?.items ?? [] as topic (topic.id)}
        <span class="chip" style="background: {colors.surface2}; border: 1px solid {colors.line};">
          {topic.name}
          <button class="chip-remove" onclick={() => removeTopic(topic.id)} style="color: {colors.mut};">✕</button>
        </span>
      {/each}
      <input
        class="topic-input"
        bind:value={newTopicName}
        placeholder="+ adicionar tópico"
        onkeydown={(e) => { if (e.key === "Enter") addTopic(); }}
        style="border: 1px solid {colors.line}; background: transparent; color: {colors.text};"
      />
      {#if lesson.studentSpeakerLabel}
        {#if topics?.analyzed}
          <button onclick={reprocessTopics} disabled={analyzingTopics}>
            {analyzingTopics ? "Reprocessando…" : "Reprocessar tópicos"}
          </button>
        {:else}
          <button onclick={analyzeTopics} disabled={analyzingTopics}>
            {analyzingTopics ? "Analisando…" : "Analisar tópicos"}
          </button>
        {/if}
      {/if}
    </div>
    {#if topics?.analyzed && (topics.items?.length ?? 0) === 0}
      <p class="hint" style="color: {colors.mut};">sem tópicos identificados</p>
    {/if}
    {#if topicsError}
      <p class="error" style="color: {colors.red};">{topicsError}</p>
    {/if}
```

- [ ] **Step 6: LessonDetail — CSS**

Adicionar ao bloco `<style>`:

```css
  .topics-row {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    flex-wrap: wrap;
    margin-bottom: 1.25rem;
  }
  .chip {
    display: inline-flex;
    align-items: center;
    gap: 0.35rem;
    padding: 0.2rem 0.6rem;
    border-radius: 999px;
    font-size: 0.8rem;
  }
  .chip-remove {
    background: none;
    border: none;
    cursor: pointer;
    font-size: 0.75rem;
    padding: 0;
  }
  .topic-input {
    padding: 0.35rem 0.6rem;
    border-radius: 999px;
    font-size: 0.8rem;
  }
```

- [ ] **Step 7: Settings — painel Tópicos**

Em `Settings.svelte`, adicionar o import e o estado:

```ts
  import * as TopicsService from "../../../bindings/assistente-idiomas/services/topicsservice";
  import type { Teacher, Topic } from "../../../bindings/assistente-idiomas/services/models";
```

```ts
  let topics: Topic[] = $state([]);
  let topicsError: string = $state("");
  let topicRenameDrafts: Record<number, string> = $state({});
  let renamingTopicId: number | null = $state(null);
  let topicRenameErrors: Record<number, string> = $state({});
```

E as funções (espelhando `loadTeachers`/`renameTeacher`, com nomes de tópico):

```ts
  async function loadTopics(justRenamedId?: number) {
    const previousTopics = topics;
    const previousDrafts = topicRenameDrafts;
    const fresh = (await TopicsService.ListTopics()) ?? [];
    const nextDrafts: Record<number, string> = {};
    for (const tp of fresh) {
      const existingDraft = previousDrafts[tp.id];
      const oldTopic = previousTopics.find((p) => p.id === tp.id);
      const isDirty = existingDraft !== undefined && oldTopic !== undefined && existingDraft !== oldTopic.name;
      nextDrafts[tp.id] = isDirty && tp.id !== justRenamedId ? existingDraft : tp.name;
    }
    topics = fresh;
    topicRenameDrafts = nextDrafts;
  }

  async function renameTopic(id: number) {
    topicRenameErrors = { ...topicRenameErrors, [id]: "" };
    renamingTopicId = id;
    try {
      await TopicsService.RenameTopic(id, topicRenameDrafts[id]);
      await loadTopics(id);
    } catch (e) {
      topicRenameErrors = { ...topicRenameErrors, [id]: String(e) };
    } finally {
      renamingTopicId = null;
    }
  }
```

Adicionar `loadTopics()` ao `Promise.all` do `onMount` e, após a `</section>` de "Professores", adicionar a nova seção:

```svelte
    <section class="card" style="background: {colors.surface}; border: 1px solid {colors.line};">
      <h2 style="font-family: {fonts.display};">Tópicos</h2>
      {#if topicsError}
        <p class="error" style="color: {colors.red};">{topicsError}</p>
      {/if}
      {#if topics.length === 0}
        <p class="hint" style="color: {colors.mut};">Nenhum tópico cadastrado ainda.</p>
      {:else}
        <ul class="teacher-list">
          {#each topics as topic (topic.id)}
            <li>
              <input
                type="text"
                bind:value={topicRenameDrafts[topic.id]}
                style="border: 1px solid {colors.line}; background: transparent; color: {colors.text};"
              />
              <button
                onclick={() => renameTopic(topic.id)}
                disabled={renamingTopicId === topic.id || !topicRenameDrafts[topic.id] || topicRenameDrafts[topic.id] === topic.name}
              >
                {renamingTopicId === topic.id ? "Renomeando…" : "Renomear"}
              </button>
              {#if topicRenameErrors[topic.id]}
                <p class="error" style="color: {colors.red};">{topicRenameErrors[topic.id]}</p>
              {/if}
            </li>
          {/each}
        </ul>
      {/if}
    </section>
```

- [ ] **Step 8: Checar frontend**

Run: `pnpm run check`
Expected: PASS (svelte-check sem erros de tipo).

- [ ] **Step 9: Commit**

```bash
git add frontend/src/lib/screens/LessonDetail.svelte frontend/src/lib/screens/Settings.svelte frontend/bindings/assistente-idiomas/
git commit -m "feat: chips de tópicos no Detalhe e painel Tópicos em Configurações"
```

---

### Task 9: Verificação final + registro no doc da fase

**Files:**
- Modify: `docs/fase-2-analise-llm.md` (Registro de progresso + checkboxes de aceite)

- [ ] **Step 1: Rodar a suíte Go completa**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 2: Rodar go vet**

Run: `go vet ./...`
Expected: sem saída de erro.

- [ ] **Step 3: Rodar build do frontend e do app**

Run: `pnpm run build` (em `frontend/`) e depois `wails3 build`
Expected: PASS nos dois.

- [ ] **Step 4: Atualizar o doc da fase**

Em `docs/fase-2-analise-llm.md`, marcar os critérios de aceite da História 3 que são verificáveis por código/teste (todos, exceto o último — a decisão em `docs/notas-analise-llm.md` depende de observação em aula real), e adicionar uma linha no "Registro de progresso" no mesmo formato das anteriores:

```markdown
| 18/08/2026 | História 3 implementada: tópicos viram entidade `topics` (migration 00006 com backfill), `lesson_topics` por `topic_id` vira a fonte da verdade da UI; `AnalysisService` ganha Get/Analyze/ReprocessTopics (sob demanda, idempotente, grava em `analysis_results` + `lesson_topics`); `TopicsService` cobre adicionar/remover por aula e renomear global; prompt v2 com granularidade geral + reaproveitamento dos tópicos existentes (anexados à mensagem); chips no Detalhe + painel "Tópicos" em Configurações; troca de falante passa a preservar tópicos (deleção seletiva por dependência de falante) | `go test ./...`, `go vet ./...`, `pnpm run check`/`build` confirmados limpos; verificação manual em aula real (granularidade, reaproveitamento, edição de chips, renome global) e a decisão em `docs/notas-analise-llm.md` seguem pendentes — mesmo padrão das histórias anteriores |
```

- [ ] **Step 5: Commit**

```bash
git add docs/fase-2-analise-llm.md
git commit -m "docs: registra implementação da História 3 (tópicos da aula)"
```

---

## Validação manual (humano, fecha a história — NÃO é tarefa deste plano)

Depois deste plano, o dev roda `wails3 dev` numa aula real e observa:

1. "Analisar tópicos" gera chips com granularidade geral (não detalhada demais).
2. Numa segunda aula sobre assunto parecido, o modelo **reutiliza** o tópico existente em vez de criar uma variação redundante.
3. Adicionar/remover chips funciona; renomear em Configurações reflete em todas as aulas.
4. Trocar "quem é você" em Editar aula preserva os tópicos (e descarta correções).

Registrar a decisão (manter/refinar/descartar) em `docs/notas-analise-llm.md` e marcar o último critério de aceite da História 3 — é isso que fecha a história.
