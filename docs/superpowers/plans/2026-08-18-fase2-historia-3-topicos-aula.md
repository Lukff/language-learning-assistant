# Phase 2, Story 3 — Lesson Topics: implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bring `analyze_topics` to the app on demand, with editable topics (chips in Detail + global rename in Settings), prompt v2 with general granularity and reuse of existing topics.

**Architecture:** Topics become a `topics` entity (like teachers), `lesson_topics` now points via `topic_id` (migration 00006 with backfill). `lesson_topics` becomes the UI's source of truth; `analysis_results` only keeps "did it run?" + raw + model. `AnalysisService` gains Get/Analyze/ReprocessTopics (mirroring corrections), and a new `TopicsService` covers the entity's CRUD. The list of existing topics is appended to the content sent to the model (a pure helper), without changing the task interface.

**Tech Stack:** Go (stdlib + modernc.org/sqlite + goose + go-keyring, already present), Wails v3 (pinned), Svelte 5 (runes).

**Spec:** `docs/superpowers/specs/2026-08-18-historia-3-topicos-aula-design.md`

## Global Constraints

- Recent Go; stdlib preferred. SQLite driver `modernc.org/sqlite`; embedded `goose` migrations (`//go:embed migrations/*.sql`).
- **Code/identifiers in English; user-facing error messages, analysis text, and docs in PT-BR.**
- **Portable SQL** in the repository layer — the only driver dependency is the `isUniqueConstraintError` helper already existing in `internal/db/teachers.go`.
- **Frontend Svelte 5 with runes, always** (`$state`/`$derived`/`$effect`/`$props`; never legacy syntax).
- Wails v3 pinned version in `go.mod` — don't change the version.
- Commits on **one line**, semantic format (`type: description`).
- Synthetic test fixtures (no real lesson / real tutor name).
- `prompts/embed.go` uses `//go:embed *.md` — a new `.md` in `prompts/` is embedded automatically, with no change to the embed needed.

---

### Task 1: Migration `00006_topics.sql` + `ReplaceLessonTopics` by id + backfill

The migration changes `lesson_topics`'s schema (from `topic TEXT` to `topic_id INTEGER`), which breaks the current `ReplaceLessonTopics` (it inserts `topic`). This task does the migration and the signature change together so the `db` package stays green.

**Files:**
- Create: `internal/db/migrations/00006_topics.sql`
- Modify: `internal/db/analysis_results.go` (function `ReplaceLessonTopics`)
- Modify: `internal/db/analysis_results_test.go` (`TestReplaceLessonTopics_ReplacesEntirely` + novo helper `insertTopicFixture`)
- Modify: `internal/db/migration_backfill_test.go` (novo teste)

**Interfaces:**
- Produces: `func ReplaceLessonTopics(conn *sql.DB, lessonID int64, topicIDs []int64) error` (signature CHANGED — `[]int64`, not `[]string`); `topics` table and `lesson_topics(topic_id)`.

- [ ] **Step 1: Write the migration**

Create `internal/db/migrations/00006_topics.sql`:

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

- [ ] **Step 2: Write the backfill test (fails before the migration exists)**

Add to `internal/db/migration_backfill_test.go`:

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

	// Pre-migration-6 schema: lesson_topics(topic TEXT). Needs a teacher and
	// a lesson (lessons has pointed via teacher_id since migration 5).
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

- [ ] **Step 3: Run the backfill test to see it fail**

Run: `go test ./internal/db/ -run TestMigration00006 -v`
Expected: FAIL — `goose: no migrations to run` or an error about nonexistent version 6 (the migration doesn't exist yet).

- [ ] **Step 4: Change `ReplaceLessonTopics` to ids**

In `internal/db/analysis_results.go`, replace `ReplaceLessonTopics`'s implementation:

```go
// ReplaceLessonTopics deletes the existing links and inserts the new ones
// (by topic_id) — the list is always derived wholesale from the most recent
// analyze_topics result, never an incremental merge. INSERT OR IGNORE
// absorbs a duplicate topic that the caller might repeat.
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

- [ ] **Step 5: Update the existing `ReplaceLessonTopics` test**

In `internal/db/analysis_results_test.go`, add the `insertTopicFixture` helper (raw SQL, so this task doesn't depend on Task 2) and replace `TestReplaceLessonTopics_ReplacesEntirely`:

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

- [ ] **Step 6: Run the db package's tests**

Run: `go test ./internal/db/ -v`
Expected: PASS (includes `TestMigration00006` and `TestReplaceLessonTopics`).

- [ ] **Step 7: Commit**

```bash
git add internal/db/migrations/00006_topics.sql internal/db/analysis_results.go internal/db/analysis_results_test.go internal/db/migration_backfill_test.go
git commit -m "feat: topics + lesson_topics migration keyed by topic_id"
```

---

### Task 2: `internal/db/topics.go` repository + tests

**Files:**
- Create: `internal/db/topics.go`
- Create: `internal/db/topics_test.go`

**Interfaces:**
- Consumes: `execer` and `isUniqueConstraintError` (already exist in `teachers.go`); Task 1's schema.
- Produces: `Topic{ID int64, Name string}`, `ListTopics(conn) ([]Topic, error)`, `GetOrCreateTopicByName(conn, name) (int64, error)`, `RenameTopic(conn, id, newName) error`, `ListLessonTopics(conn, lessonID) ([]Topic, error)`, `AddLessonTopic(conn, lessonID, topicID) error`, `RemoveLessonTopic(conn, lessonID, topicID) error`.

- [ ] **Step 1: Write the test (fails)**

Create `internal/db/topics_test.go`:

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

- [ ] **Step 2: Run it to see it fail**

Run: `go test ./internal/db/ -run 'TestGetOrCreateTopicByName|TestListTopics|TestRenameTopic|TestAddAndRemoveLessonTopic' -v`
Expected: FAIL — `undefined: GetOrCreateTopicByName` (etc.).

- [ ] **Step 3: Implement `internal/db/topics.go`**

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

// Topic is a topics row — the entity that replaces the old
// lesson_topics.topic column (see Phase 2's Story 3). Name is unique:
// renaming is a single-row UPDATE, reflected across all lessons via JOIN.
type Topic struct {
	ID   int64
	Name string
}

// ListTopics lists the registered topics in alphabetical order — feeds the
// "Tópicos" panel in Settings and the prompt's reuse list.
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

// getOrCreateTopicByName resolves a (trimmed) name to a topic_id — reuses
// execer (teachers.go) so it can run standalone or inside a transaction.
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

// RenameTopic renames topic id — automatically reflected across all its
// lessons (JOIN). A collision (UNIQUE) becomes a readable error.
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

// ListLessonTopics returns lessonID's topics (JOIN topics), in
// alphabetical order.
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

// AddLessonTopic links topicID to lessonID (INSERT OR IGNORE — already
// linked isn't an error).
func AddLessonTopic(conn *sql.DB, lessonID, topicID int64) error {
	_, err := conn.Exec(`INSERT OR IGNORE INTO lesson_topics (lesson_id, topic_id) VALUES (?, ?)`, lessonID, topicID)
	if err != nil {
		return fmt.Errorf("vincular tópico %d à lesson %d: %w", topicID, lessonID, err)
	}
	return nil
}

// RemoveLessonTopic unlinks topicID from lessonID (doesn't delete the entity).
func RemoveLessonTopic(conn *sql.DB, lessonID, topicID int64) error {
	_, err := conn.Exec(`DELETE FROM lesson_topics WHERE lesson_id = ? AND topic_id = ?`, lessonID, topicID)
	if err != nil {
		return fmt.Errorf("desvincular tópico %d da lesson %d: %w", topicID, lessonID, err)
	}
	return nil
}
```

- [ ] **Step 4: Run the db package's tests**

Run: `go test ./internal/db/ -v`
Expected: PASS (Tasks 1 + 2 together).

- [ ] **Step 5: Commit**

```bash
git add internal/db/topics.go internal/db/topics_test.go
git commit -m "feat: topics repository (entity + per-lesson links)"
```

---

### Task 3: Selective deletion on speaker swap

**Files:**
- Modify: `internal/db/analysis_results.go` (rename `DeleteAnalysisResultsForLesson` → `DeleteSpeakerDependentAnalysisResults` and change the SQL)
- Modify: `services/library.go` (`SetStudentSpeaker`)
- Modify: `internal/db/analysis_results_test.go` (rename/rewrite the deletion test)

**Interfaces:**
- Consumes: `UpsertAnalysisResult`, `UpsertPrompt` (already exist); `insertLessonFixture` (a `db` package helper); `GetOrCreateTopicByName`/`AddLessonTopic` (Task 2).
- Produces: `func DeleteSpeakerDependentAnalysisResults(conn *sql.DB, lessonID int64) error`.

- [ ] **Step 1: Write the new test (fails due to a missing symbol)**

In `internal/db/analysis_results_test.go`, replace `TestDeleteAnalysisResultsForLesson_DeletesAllTasksForLessonOnly` with:

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

	// lessonA's lesson_topics must survive the speaker swap.
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

- [ ] **Step 2: Run it to see it fail**

Run: `go test ./internal/db/ -run TestDeleteSpeakerDependentAnalysisResults -v`
Expected: FAIL — `undefined: DeleteSpeakerDependentAnalysisResults`.

- [ ] **Step 3: Rename and change the function**

In `internal/db/analysis_results.go`, replace `DeleteAnalysisResultsForLesson` with:

```go
// DeleteSpeakerDependentAnalysisResults deletes the analyses that depend on
// who is the student/tutor in the lesson (everything except analyze_topics)
// — called when the student/tutor mapping changes
// (services.LibraryService.SetStudentSpeaker). analyze_topics doesn't
// mention Student/Tutor in its prompt and survives; lesson_topics also stays
// intact (the table is the chips' source of truth, independent of the
// analysis).
func DeleteSpeakerDependentAnalysisResults(conn *sql.DB, lessonID int64) error {
	if _, err := conn.Exec(`DELETE FROM analysis_results WHERE lesson_id = ? AND task != 'analyze_topics'`, lessonID); err != nil {
		return fmt.Errorf("apagar análises dependentes de falante da lesson %d: %w", lessonID, err)
	}
	return nil
}
```

- [ ] **Step 4: Update the caller**

In `services/library.go`, in the `SetStudentSpeaker` function (line ~179), change:

```go
		if err := db.DeleteAnalysisResultsForLesson(s.conn, lessonID); err != nil {
```

to:

```go
		if err := db.DeleteSpeakerDependentAnalysisResults(s.conn, lessonID); err != nil {
```

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/db/ ./services/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/db/analysis_results.go internal/db/analysis_results_test.go services/library.go
git commit -m "feat: speaker swap preserves topics (selective deletion)"
```

---

### Task 4: `internal/analysis` package — `NewTopicsTask` v2, `ParseTopicsResult`, `AppendExistingTopics` + prompt v2

**Files:**
- Create: `prompts/analyze-topics-v2.md`
- Modify: `internal/analysis/tasks_topics.go`
- Modify: `internal/analysis/tasks_topics_test.go`

**Interfaces:**
- Consumes: `mustLoadPrompt`, `task[T]`, `parseTopics` (already exist).
- Produces: `func NewTopicsTask() TaskDef` (version 2, prompt v2), `func ParseTopicsResult(resultJSON json.RawMessage) ([]string, error)`, `func AppendExistingTopics(transcript string, existing []string) string`.

- [ ] **Step 1: Write the prompt v2**

Create `prompts/analyze-topics-v2.md`:

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

- [ ] **Step 2: Write the tests (fail)**

In `internal/analysis/tasks_topics_test.go`, add:

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

- [ ] **Step 3: Run it to see it fail**

Run: `go test ./internal/analysis/ -run 'TestNewTopicsTask|TestParseTopicsResult|TestAppendExistingTopics' -v`
Expected: FAIL — `undefined: NewTopicsTask`, `undefined: ParseTopicsResult`, `undefined: AppendExistingTopics`.

- [ ] **Step 4: Implement**

In `internal/analysis/tasks_topics.go`, replace the content with:

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

// NewTopicsTask returns the topics task on prompt version 2
// (general granularity + reuse of existing topics).
func NewTopicsTask() TaskDef {
	return task[[]string]{name: "analyze_topics", version: 2, prompt: mustLoadPrompt("analyze-topics-v2.md"), parse: parseTopics}
}

// ParseTopicsResult decodes a persisted result_json (a JSON array of
// strings — resultJSON is json.Marshal([]string), no envelope) back into
// []string.
func ParseTopicsResult(resultJSON json.RawMessage) ([]string, error) {
	var out []string
	if err := json.Unmarshal(resultJSON, &out); err != nil {
		return nil, fmt.Errorf("analysis: desserializar resultado de analyze_topics: %w", err)
	}
	return out, nil
}

// AppendExistingTopics appends the list of already-existing topics to the
// transcript content, in a trailing block that the v2 prompt recognizes.
// Returns the transcript unchanged when there are no existing topics.
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

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/analysis/ -v`
Expected: PASS (includes the existing `TestParseTopics_*`, which keep passing — `parseTopics` didn't change).

- [ ] **Step 6: Commit**

```bash
git add prompts/analyze-topics-v2.md internal/analysis/tasks_topics.go internal/analysis/tasks_topics_test.go
git commit -m "feat: topics task v2 (granularity + reuse)"
```

---

### Task 5: `services/topics.go` — `Topic` + `TopicsService`

**Files:**
- Create: `services/topics.go`
- Create: `services/topics_test.go`

**Interfaces:**
- Consumes: `db.GetOrCreateTopicByName`, `db.AddLessonTopic`, `db.RemoveLessonTopic`, `db.ListTopics`, `db.RenameTopic` (Task 2); helpers `openTestDB`/`mustInsertLesson`/`testStorageRoot` (already exist).
- Produces: `type Topic struct { ID int64 `json:"id"`; Name string `json:"name"` }`, `type TopicsService struct{}`, `NewTopicsService(conn) *TopicsService`, `(*TopicsService) ListTopics() ([]Topic, error)`, `RenameTopic(id, newName) error`, `AddTopic(lessonID, name) (Topic, error)`, `RemoveTopic(lessonID, topicID) error`.

- [ ] **Step 1: Write the test (fails)**

Create `services/topics_test.go`:

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

- [ ] **Step 2: Run it to see it fail**

Run: `go test ./services/ -run 'TestTopicsService' -v`
Expected: FAIL — `undefined: NewTopicsService`.

- [ ] **Step 3: Implement `services/topics.go`**

```go
// services/topics.go
package services

import (
	"database/sql"
	"fmt"
	"strings"

	"assistente-idiomas/internal/db"
)

// Topic is a registered topic, in the format exposed to the frontend — an
// entity reused by TopicsService (management) and by AnalysisService
// (TopicsResult).
type Topic struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// TopicsService covers topic management as an entity (Story 3): listing
// for the Settings panel, renaming globally, and adding/removing a topic's
// link to a specific lesson (Detail's chips).
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

// AddTopic resolves the name to an entity (creating it if necessary) and
// links it to lessonID. Returns the resolved topic; the frontend re-fetches
// GetTopics afterward to reconcile canonical names.
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

// RemoveTopic unlinks topicID from lessonID (doesn't delete the entity).
func (s *TopicsService) RemoveTopic(lessonID, topicID int64) error {
	return db.RemoveLessonTopic(s.conn, lessonID, topicID)
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./services/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add services/topics.go services/topics_test.go
git commit -m "feat: TopicsService (topics managed as an entity)"
```

---

### Task 6: `services/analysis.go` — `TopicsResult` + Get/Analyze/ReprocessTopics

**Files:**
- Modify: `services/analysis.go`
- Modify: `services/analysis_test.go` (estender o fake + novos testes)

**Interfaces:**
- Consumes: `db.FindLessonByID`, `db.FindTranscriptByLessonID`, `db.FindAnalysisResult`, `db.ListTopics`, `db.ListLessonTopics`, `db.GetOrCreateTopicByName`, `db.ReplaceLessonTopics`, `db.UpsertPrompt`, `db.UpsertAnalysisResult`; `analysis.NewTopicsTask`/`ParseTopicsResult`/`AppendExistingTopics`/`FormatTranscript`; `analysisRawRelPath`/`writeRawResponse` (already existing in this file); `Topic` (Task 5).
- Produces: `type TopicsResult struct { Analyzed bool `json:"analyzed"`; Items []Topic `json:"items"` }`, `(*AnalysisService) GetTopics(lessonID int64) (TopicsResult, error)`, `AnalyzeTopics(...)`, `ReprocessTopics(...)`.

- [ ] **Step 1: Extend the fake provider to capture the input**

In `services/analysis_test.go`, add the `lastInput string` field to the `fakeAnalysisProvider` struct and record it in `Complete`:

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

- [ ] **Step 2: Write the tests (fail)**

Add to `services/analysis_test.go`:

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

**Note:** the new tests use `strings` and `fmt` — `services/analysis_test.go` already imports `fmt`; add `"strings"` to the import block.

- [ ] **Step 3: Run it to see it fail**

Run: `go test ./services/ -run 'TestAnalysisService_.*Topic' -v`
Expected: FAIL — `undefined: svc.GetTopics` (etc.) and nonexistent `TopicsResult`.

- [ ] **Step 4: Implement in `services/analysis.go`**

Add after the corrections block (reuses `analysisRawRelPath`/`writeRawResponse`/imports already present):

```go
const topicsTaskName = "analyze_topics"

// TopicsResult is analyze_topics's result exposed to the frontend. Items
// comes from lesson_topics (the editable source of truth); Analyzed
// indicates whether the task has already run (a row in analysis_results).
type TopicsResult struct {
	Analyzed bool    `json:"analyzed"`
	Items    []Topic `json:"items"`
}

// GetTopics returns the lesson's current topics (from lesson_topics)
// without calling the API — Analyzed == false if the task never ran.
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

- [ ] **Step 5: Run the tests**

Run: `go test ./services/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add services/analysis.go services/analysis_test.go
git commit -m "feat: on-demand topic analysis (Get/Analyze/Reprocess)"
```

---

### Task 7: Register `TopicsService` in `main.go`

**Files:**
- Modify: `main.go`

- [ ] **Step 1: Add the service**

In `main.go`, in the `Services` list, after `application.NewService(services.NewTeacherService(conn))`, add:

```go
			application.NewService(services.NewTopicsService(conn)),
```

- [ ] **Step 2: Compile**

Run: `go build ./...`
Expected: PASS (no errors).

- [ ] **Step 3: Commit**

```bash
git add main.go
git commit -m "feat: register TopicsService in the Wails app"
```

---

### Task 8: Bindings + frontend (chips in Detail + Tópicos panel in Settings)

**Files:**
- Modify: `frontend/src/lib/screens/LessonDetail.svelte`
- Modify: `frontend/src/lib/screens/Settings.svelte`
- (Auto-generated, don't edit by hand): `frontend/bindings/assistente-idiomas/services/topicsservice.ts`, `.../analysisservice.ts`, `.../models.ts`

- [ ] **Step 1: Regenerate bindings**

Run: `wails3 generate bindings -ts -i ./...`
Expected: generates `topicsservice.ts` (TopicsService), new methods in `analysisservice.ts` (GetTopics/AnalyzeTopics/ReprocessTopics), and `Topic`/`TopicsResult` in `models.ts`.

- [ ] **Step 2: LessonDetail — imports and state**

In `LessonDetail.svelte`, add the imports and the state. Change the models import line to include `TopicsResult`, and add the TopicsService import:

```ts
  import * as TopicsService from "../../../bindings/assistente-idiomas/services/topicsservice";
  import type { Lesson, Transcript, CorrectionsResult, TopicsResult } from "../../../bindings/assistente-idiomas/services/models";
```

And, alongside the other `$state`s, add:

```ts
  let topics: TopicsResult | null = $state(null);
  let analyzingTopics: boolean = $state(false);
  let topicsError: string = $state("");
  let newTopicName: string = $state("");
```

- [ ] **Step 3: LessonDetail — functions**

Add (near `fetchCorrectionsIfReady`/`analyzeCorrections`):

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

- [ ] **Step 4: LessonDetail — call `fetchTopicsIfReady`**

In `refreshAfterSpeakerChange` (after `await fetchCorrectionsIfReady()`) and in `onMount` (after `await fetchCorrectionsIfReady()`), add `await fetchTopicsIfReady();`.

- [ ] **Step 5: LessonDetail — chips markup**

Right after the `.header-row` block (after the header-row's `</div>`, before `<div class="grid">`), add:

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

Add to the `<style>` block:

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

- [ ] **Step 7: Settings — Tópicos panel**

In `Settings.svelte`, add the import and the state:

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

And the functions (mirroring `loadTeachers`/`renameTeacher`, with topic names):

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

Add `loadTopics()` to the `onMount`'s `Promise.all` and, after the "Professores" `</section>`, add the new section:

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

- [ ] **Step 8: Check the frontend**

Run: `pnpm run check`
Expected: PASS (svelte-check with no type errors).

- [ ] **Step 9: Commit**

```bash
git add frontend/src/lib/screens/LessonDetail.svelte frontend/src/lib/screens/Settings.svelte frontend/bindings/assistente-idiomas/
git commit -m "feat: topic chips in Detail and Topics panel in Settings"
```

---

### Task 9: Final verification + recording in the phase doc

**Files:**
- Modify: `docs/fase-2-analise-llm.md` (Progress log + acceptance checkboxes)

- [ ] **Step 1: Run the full Go suite**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 2: Run go vet**

Run: `go vet ./...`
Expected: no error output.

- [ ] **Step 3: Run the frontend and app build**

Run: `pnpm run build` (in `frontend/`) and then `wails3 build`
Expected: PASS on both.

- [ ] **Step 4: Update the phase doc**

In `docs/fase-2-analise-llm.md`, check off Story 3's acceptance criteria that are verifiable by code/test (all except the last one — the decision in `docs/notas-analise-llm.md` depends on observing a real lesson), and add a line to the "Progress log" in the same format as the previous ones:

```markdown
| 18/08/2026 | Story 3 implemented: topics become a `topics` entity (migration 00006 with backfill), `lesson_topics` by `topic_id` becomes the UI's source of truth; `AnalysisService` gains Get/Analyze/ReprocessTopics (on demand, idempotent, writes to `analysis_results` + `lesson_topics`); `TopicsService` covers add/remove per lesson and global rename; prompt v2 with general granularity + reuse of existing topics (appended to the message); chips in the Detail view + a "Topics" panel in Settings; switching speakers now preserves topics (selective deletion by speaker dependency) | `go test ./...`, `go vet ./...`, `pnpm run check`/`build` confirmed clean; manual verification on a real lesson (granularity, reuse, chip editing, global rename) and the decision in `docs/notas-analise-llm.md` are still pending — same pattern as previous stories |
```

- [ ] **Step 5: Commit**

```bash
git add docs/fase-2-analise-llm.md
git commit -m "docs: record implementation of Story 3 (lesson topics)"
```

---

## Manual validation (human, closes the story — NOT a task in this plan)

After this plan, the dev runs `wails3 dev` on a real lesson and observes:

1. "Analisar tópicos" generates chips with general granularity (not overly detailed).
2. On a second lesson about a similar subject, the model **reuses** the existing topic instead of creating a redundant variation.
3. Adding/removing chips works; renaming in Settings is reflected across all lessons.
4. Switching "quem é você" in Edit lesson preserves the topics (and discards corrections).

Record the decision (keep/refine/discard) in `docs/notas-analise-llm.md` and check off Story 3's last acceptance criterion — that's what closes the story.
