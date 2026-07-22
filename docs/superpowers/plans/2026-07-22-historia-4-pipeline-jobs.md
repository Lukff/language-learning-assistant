# História 4 — Pipeline em Background (Fila de Jobs) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extração de áudio e transcrição rodam sozinhas em background depois que uma aula é
confirmada (História 3), via um worker único processando uma fila em tabela, sem bloquear o app.

**Architecture:** Novo pacote `internal/jobs` (Go puro, sem Wails) com um `Worker` que seleciona
o próximo job elegível (respeitando precedência `extract_audio → transcribe` e backoff de
retry), executa via `media.ExtractAudio`/`stt.Provider` injetados, e notifica transições via uma
interface `Notifier` — implementada em `services/` (que já importa Wails) emitindo eventos.
`storage_root` e o provedor de STT são resolvidos a cada job (não na criação do Worker), porque
o wizard de primeira execução só grava essa configuração depois que o app já iniciou.

**Tech Stack:** Go stdlib (`database/sql`, `context`, `log/slog`, `encoding/json`, `time`),
`modernc.org/sqlite` (já em uso via `internal/db`), `internal/media` e `internal/stt` da Fase 0
(reaproveitados como estão).

## Global Constraints

- `internal/` nunca importa Wails — camada fina (CLAUDE.md, `docs/decisoes-tecnologia.md`).
- Fila de jobs: tabela `jobs` + worker único, sem lib externa de job queue (decisão vigente em
  `docs/decisoes-tecnologia.md`).
- Idempotência real por artefato (não só por status); retry com `attempts` + backoff simples;
  jobs presos em `running` voltam a `pending` na abertura do app.
- Falha de transcrição nunca impede assistir ao vídeo — princípio de resiliência da Fase 1.
- SQL portável na camada de repositório (nada específico de driver).
- Paths de vídeo no banco são sempre relativos à `storage_root` — nunca absolutos.
- Commits em uma linha só, formato `tipo: descrição` (`feat:`, `fix:`, `test:`, ...).
- `go vet ./...` limpo antes de cada commit. `go build ./...` gera um erro pré-existente e não
  relacionado no pacote `build/ios` (scaffold do Wails) — para verificar o build do que importa
  aqui, use `go build ./internal/... ./services/... .` (sem `build/ios`).
- Sem migração de banco nesta história — o schema de `jobs` já tem todas as colunas necessárias.

---

### Task 1: `internal/db` — modelo e queries de `Job`

**Files:**
- Create: `internal/db/jobs.go`
- Test: `internal/db/jobs_test.go`

**Interfaces:**
- Produces: `type Job struct { ID, LessonID int64; Kind, Status, LastError, CreatedAt, UpdatedAt string; Attempts int }`;
  `func ListPendingJobs(conn *sql.DB) ([]Job, error)`; `func FindJob(conn *sql.DB, lessonID int64, kind string) (*Job, error)`;
  `func MarkJobRunning(conn *sql.DB, id int64) error`; `func MarkJobDone(conn *sql.DB, id int64) error`;
  `func MarkJobRetryOrError(conn *sql.DB, id int64, lastError string, maxAttempts int) (string, int, error)`;
  `func MarkJobBlocked(conn *sql.DB, id int64, reason string) error`;
  `func RequeueRunningJobs(conn *sql.DB) (int64, error)`.
  Status é sempre uma destas strings: `"pending"`, `"running"`, `"done"`, `"error"`.

- [ ] **Step 1: Escrever os testes (vão falhar por falta de implementação)**

```go
// internal/db/jobs_test.go
package db

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func mustInsertLessonForJobs(t *testing.T, conn *sql.DB, videoPath string) int64 {
	t.Helper()
	res, err := conn.Exec(
		`INSERT INTO lessons (lesson_date, tutor, video_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"2026-07-22", "Fulano", videoPath, "2026-07-22T09:00:00Z", "2026-07-22T09:00:00Z",
	)
	if err != nil {
		t.Fatalf("inserir lesson de fixture falhou: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("obter id da lesson de fixture falhou: %v", err)
	}
	return id
}

func mustInsertJob(t *testing.T, conn *sql.DB, lessonID int64, kind, status string, attempts int, createdAt, updatedAt string) int64 {
	t.Helper()
	res, err := conn.Exec(
		`INSERT INTO jobs (lesson_id, kind, status, attempts, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		lessonID, kind, status, attempts, createdAt, updatedAt,
	)
	if err != nil {
		t.Fatalf("inserir job de fixture falhou: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("obter id do job de fixture falhou: %v", err)
	}
	return id
}

func TestListPendingJobs_OrdersByCreatedAtAscending(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	mustInsertJob(t, conn, lessonID, "transcribe", "pending", 0, "2026-07-22T10:00:05Z", "2026-07-22T10:00:05Z")
	olderID := mustInsertJob(t, conn, lessonID, "extract_audio", "pending", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")

	jobs, err := ListPendingJobs(conn)
	if err != nil {
		t.Fatalf("ListPendingJobs() erro inesperado: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("ListPendingJobs() = %d jobs, esperado 2", len(jobs))
	}
	if jobs[0].ID != olderID {
		t.Errorf("jobs[0].ID = %d, esperado %d (mais antigo primeiro)", jobs[0].ID, olderID)
	}
}

func TestFindJob_NotFoundReturnsNilNil(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	job, err := FindJob(conn, lessonID, "extract_audio")
	if err != nil {
		t.Fatalf("FindJob() erro inesperado: %v", err)
	}
	if job != nil {
		t.Errorf("FindJob() = %+v, esperado nil", job)
	}
}

func TestMarkJobRunning_ClaimsPendingJobAndRejectsNonPending(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	id := mustInsertJob(t, conn, lessonID, "extract_audio", "pending", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")

	if err := MarkJobRunning(conn, id); err != nil {
		t.Fatalf("MarkJobRunning() erro inesperado: %v", err)
	}
	job, err := FindJob(conn, lessonID, "extract_audio")
	if err != nil {
		t.Fatalf("FindJob() erro inesperado: %v", err)
	}
	if job.Status != "running" {
		t.Errorf("job.Status = %q, esperado running", job.Status)
	}

	if err := MarkJobRunning(conn, id); err == nil {
		t.Error("MarkJobRunning() num job já running deveria falhar, veio nil")
	}
}

func TestMarkJobDone_SetsStatusDoneAndClearsError(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	id := mustInsertJob(t, conn, lessonID, "extract_audio", "running", 1, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")
	if _, err := conn.Exec(`UPDATE jobs SET last_error = 'erro anterior' WHERE id = ?`, id); err != nil {
		t.Fatalf("preparar last_error de fixture falhou: %v", err)
	}

	if err := MarkJobDone(conn, id); err != nil {
		t.Fatalf("MarkJobDone() erro inesperado: %v", err)
	}
	job, err := FindJob(conn, lessonID, "extract_audio")
	if err != nil {
		t.Fatalf("FindJob() erro inesperado: %v", err)
	}
	if job.Status != "done" || job.LastError != "" {
		t.Errorf("job = %+v, esperado status done e last_error vazio", job)
	}
}

func TestMarkJobRetryOrError_RetriesUntilMaxAttemptsThenTerminal(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	id := mustInsertJob(t, conn, lessonID, "extract_audio", "running", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")

	status, attempts, err := MarkJobRetryOrError(conn, id, "falha 1", 3)
	if err != nil {
		t.Fatalf("MarkJobRetryOrError() erro inesperado: %v", err)
	}
	if status != "pending" || attempts != 1 {
		t.Errorf("1ª falha: status=%q attempts=%d, esperado pending/1", status, attempts)
	}

	status, attempts, err = MarkJobRetryOrError(conn, id, "falha 2", 3)
	if err != nil {
		t.Fatalf("MarkJobRetryOrError() erro inesperado: %v", err)
	}
	if status != "pending" || attempts != 2 {
		t.Errorf("2ª falha: status=%q attempts=%d, esperado pending/2", status, attempts)
	}

	status, attempts, err = MarkJobRetryOrError(conn, id, "falha 3", 3)
	if err != nil {
		t.Fatalf("MarkJobRetryOrError() erro inesperado: %v", err)
	}
	if status != "error" || attempts != 3 {
		t.Errorf("3ª falha: status=%q attempts=%d, esperado error/3 (terminal)", status, attempts)
	}
}

func TestMarkJobBlocked_SetsErrorWithoutIncrementingAttempts(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	id := mustInsertJob(t, conn, lessonID, "transcribe", "pending", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")

	if err := MarkJobBlocked(conn, id, "depende de extract_audio que falhou"); err != nil {
		t.Fatalf("MarkJobBlocked() erro inesperado: %v", err)
	}
	job, err := FindJob(conn, lessonID, "transcribe")
	if err != nil {
		t.Fatalf("FindJob() erro inesperado: %v", err)
	}
	if job.Status != "error" || job.Attempts != 0 || job.LastError != "depende de extract_audio que falhou" {
		t.Errorf("job = %+v, esperado status=error attempts=0 last_error preenchido", job)
	}
}

func TestRequeueRunningJobs_MovesRunningBackToPendingWithoutIncrementingAttempts(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	stuckID := mustInsertJob(t, conn, lessonID, "extract_audio", "running", 1, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")
	doneID := mustInsertJob(t, conn, lessonID, "transcribe", "done", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")

	n, err := RequeueRunningJobs(conn)
	if err != nil {
		t.Fatalf("RequeueRunningJobs() erro inesperado: %v", err)
	}
	if n != 1 {
		t.Errorf("RequeueRunningJobs() = %d, esperado 1", n)
	}

	stuck, err := FindJob(conn, lessonID, "extract_audio")
	if err != nil {
		t.Fatalf("FindJob() erro inesperado: %v", err)
	}
	if stuck.ID != stuckID || stuck.Status != "pending" || stuck.Attempts != 1 {
		t.Errorf("job requeued = %+v, esperado status=pending attempts=1 (inalterado)", stuck)
	}

	done, err := FindJob(conn, lessonID, "transcribe")
	if err != nil {
		t.Fatalf("FindJob() erro inesperado: %v", err)
	}
	if done.ID != doneID || done.Status != "done" {
		t.Errorf("job done = %+v, não deveria ser afetado pelo requeue", done)
	}
}
```

- [ ] **Step 2: Rodar os testes e confirmar que falham (pacote não compila — funções não existem)**

Run: `go test ./internal/db/... -run TestListPendingJobs -v`
Expected: FAIL — `undefined: ListPendingJobs` (erro de compilação)

- [ ] **Step 3: Implementar `internal/db/jobs.go`**

```go
// internal/db/jobs.go
package db

import (
	"database/sql"
	"fmt"
	"time"
)

// Job é uma linha de jobs — ver a fila em tabela + worker único decidida em
// docs/decisoes-tecnologia.md e a História 4 em docs/fase-1-mvp.md. Status
// é sempre um de "pending", "running", "done", "error".
type Job struct {
	ID        int64
	LessonID  int64
	Kind      string
	Status    string
	Attempts  int
	LastError string
	CreatedAt string
	UpdatedAt string
}

// ListPendingJobs lista os jobs "pending", mais antigos primeiro — é a
// ordem de FIFO que internal/jobs.Worker usa pra escolher o próximo job a
// processar.
func ListPendingJobs(conn *sql.DB) ([]Job, error) {
	rows, err := conn.Query(
		`SELECT id, lesson_id, kind, status, attempts, COALESCE(last_error, ''), created_at, updated_at FROM jobs WHERE status = 'pending' ORDER BY created_at ASC, id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("listar jobs pendentes: %w", err)
	}
	defer rows.Close()

	var out []Job
	for rows.Next() {
		var j Job
		if err := rows.Scan(&j.ID, &j.LessonID, &j.Kind, &j.Status, &j.Attempts, &j.LastError, &j.CreatedAt, &j.UpdatedAt); err != nil {
			return nil, fmt.Errorf("ler job: %w", err)
		}
		out = append(out, j)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterar jobs pendentes: %w", err)
	}
	return out, nil
}

// FindJob busca o job de kind (ex.: "extract_audio") para lessonID.
// Retorna (nil, nil) se não houver — usado pelo Worker pra checar a
// precedência de transcribe sobre extract_audio.
func FindJob(conn *sql.DB, lessonID int64, kind string) (*Job, error) {
	var j Job
	err := conn.QueryRow(
		`SELECT id, lesson_id, kind, status, attempts, COALESCE(last_error, ''), created_at, updated_at FROM jobs WHERE lesson_id = ? AND kind = ?`,
		lessonID, kind,
	).Scan(&j.ID, &j.LessonID, &j.Kind, &j.Status, &j.Attempts, &j.LastError, &j.CreatedAt, &j.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("buscar job %s da lesson %d: %w", kind, lessonID, err)
	}
	return &j, nil
}

// MarkJobRunning reivindica um job pending, marcando status="running".
// Falha se o job não estiver mais pending — não deve acontecer com o
// worker único da Fase 1, mas evita corrida silenciosa se isso mudar.
func MarkJobRunning(conn *sql.DB, id int64) error {
	res, err := conn.Exec(
		`UPDATE jobs SET status = 'running', updated_at = ? WHERE id = ? AND status = 'pending'`,
		time.Now().UTC().Format(time.RFC3339), id,
	)
	if err != nil {
		return fmt.Errorf("marcar job %d como running: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("confirmar marcação de job %d como running: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("job %d não estava pending — não pode ser reivindicado", id)
	}
	return nil
}

// MarkJobDone marca um job como concluído com sucesso.
func MarkJobDone(conn *sql.DB, id int64) error {
	_, err := conn.Exec(
		`UPDATE jobs SET status = 'done', last_error = NULL, updated_at = ? WHERE id = ?`,
		time.Now().UTC().Format(time.RFC3339), id,
	)
	if err != nil {
		return fmt.Errorf("marcar job %d como done: %w", id, err)
	}
	return nil
}

// MarkJobRetryOrError registra a falha de execução de um job: incrementa
// attempts e grava lastError. Se o novo total de attempts ainda for menor
// que maxAttempts, o job volta a "pending" (o Worker retenta depois do
// backoff); senão vira "error" — terminal, só reprocessa manualmente.
// Retorna o novo status e o novo total de attempts.
func MarkJobRetryOrError(conn *sql.DB, id int64, lastError string, maxAttempts int) (string, int, error) {
	var attempts int
	if err := conn.QueryRow(`SELECT attempts FROM jobs WHERE id = ?`, id).Scan(&attempts); err != nil {
		return "", 0, fmt.Errorf("ler attempts do job %d: %w", id, err)
	}
	attempts++
	status := "pending"
	if attempts >= maxAttempts {
		status = "error"
	}
	_, err := conn.Exec(
		`UPDATE jobs SET status = ?, attempts = ?, last_error = ?, updated_at = ? WHERE id = ?`,
		status, attempts, lastError, time.Now().UTC().Format(time.RFC3339), id,
	)
	if err != nil {
		return "", 0, fmt.Errorf("registrar falha do job %d: %w", id, err)
	}
	return status, attempts, nil
}

// MarkJobBlocked marca um job como "error" sem executá-lo e sem
// incrementar attempts — usado quando a dependência dele (ex.:
// extract_audio de um transcribe) já falhou definitivamente, então rodar o
// job não faria sentido.
func MarkJobBlocked(conn *sql.DB, id int64, reason string) error {
	_, err := conn.Exec(
		`UPDATE jobs SET status = 'error', last_error = ?, updated_at = ? WHERE id = ?`,
		reason, time.Now().UTC().Format(time.RFC3339), id,
	)
	if err != nil {
		return fmt.Errorf("bloquear job %d: %w", id, err)
	}
	return nil
}

// RequeueRunningJobs volta todo job "running" pra "pending" — chamado uma
// vez na inicialização do Worker pra cobrir crash/kill no meio de um job.
// attempts não é incrementado: a interrupção não foi uma falha de
// execução. Retorna quantos jobs foram requeued.
func RequeueRunningJobs(conn *sql.DB) (int64, error) {
	res, err := conn.Exec(
		`UPDATE jobs SET status = 'pending', updated_at = ? WHERE status = 'running'`,
		time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return 0, fmt.Errorf("requeue de jobs running: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("confirmar requeue de jobs running: %w", err)
	}
	return n, nil
}
```

- [ ] **Step 4: Rodar os testes e confirmar que passam**

Run: `go test ./internal/db/... -v`
Expected: PASS em todos os testes de `jobs_test.go` e nos já existentes de `internal/db`.

- [ ] **Step 5: `go vet` e commit**

```bash
go vet ./internal/db/...
git add internal/db/jobs.go internal/db/jobs_test.go
git commit -m "feat: adiciona modelo e queries de Job para a fila de background"
```

---

### Task 2: `internal/db` — busca de lesson por id e persistência de transcript

**Files:**
- Modify: `internal/db/lessons.go` (adicionar `FindLessonByID` após `FindLessonByHash`)
- Modify: `internal/db/lessons_test.go` (adicionar teste)
- Create: `internal/db/transcripts.go`
- Test: `internal/db/transcripts_test.go`

**Interfaces:**
- Produces: `func FindLessonByID(conn *sql.DB, id int64) (*Lesson, error)`;
  `func HasTranscript(conn *sql.DB, lessonID int64) (bool, error)`;
  `func InsertTranscript(conn *sql.DB, lessonID int64, rawJSONPath string, utterancesJSON string) error`.
- Consumes: `type Lesson struct` de `internal/db/lessons.go` (já existe, tem `VideoPath` relativo).

- [ ] **Step 1: Escrever o teste de `FindLessonByID` (vai falhar por falta de implementação)**

Adicionar ao final de `internal/db/lessons_test.go`:

```go
func TestFindLessonByID_FindsExistingAndNilWhenMissing(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	res, err := conn.Exec(
		`INSERT INTO lessons (lesson_date, tutor, video_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"2026-07-15", "Sarah", "aula-01.mp4", "2026-07-15T10:00:00Z", "2026-07-15T10:00:00Z",
	)
	if err != nil {
		t.Fatalf("insert de fixture falhou: %v", err)
	}
	id, _ := res.LastInsertId()

	found, err := FindLessonByID(conn, id)
	if err != nil {
		t.Fatalf("FindLessonByID() erro inesperado: %v", err)
	}
	if found == nil || found.VideoPath != "aula-01.mp4" {
		t.Errorf("FindLessonByID() = %+v, esperado video_path aula-01.mp4", found)
	}

	missing, err := FindLessonByID(conn, id+999)
	if err != nil {
		t.Fatalf("FindLessonByID() erro inesperado: %v", err)
	}
	if missing != nil {
		t.Errorf("FindLessonByID() para id inexistente = %+v, esperado nil", missing)
	}
}
```

- [ ] **Step 2: Rodar e confirmar falha de compilação**

Run: `go test ./internal/db/... -run TestFindLessonByID -v`
Expected: FAIL — `undefined: FindLessonByID`

- [ ] **Step 3: Implementar `FindLessonByID`**

Adicionar em `internal/db/lessons.go`, logo após a função `FindLessonByHash`:

```go
// FindLessonByID busca a lesson por id. Retorna (nil, nil) se não houver.
func FindLessonByID(conn *sql.DB, id int64) (*Lesson, error) {
	var l Lesson
	err := conn.QueryRow(
		`SELECT id, lesson_date, tutor, video_path, COALESCE(video_hash, ''), COALESCE(file_size, 0), COALESCE(file_mtime, '') FROM lessons WHERE id = ?`,
		id,
	).Scan(&l.ID, &l.LessonDate, &l.Tutor, &l.VideoPath, &l.VideoHash, &l.FileSize, &l.FileMTime)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("buscar lesson por id: %w", err)
	}
	return &l, nil
}
```

- [ ] **Step 4: Rodar e confirmar que passa**

Run: `go test ./internal/db/... -run TestFindLessonByID -v`
Expected: PASS

- [ ] **Step 5: Escrever os testes de transcripts (vão falhar por falta de implementação)**

```go
// internal/db/transcripts_test.go
package db

import (
	"path/filepath"
	"testing"
)

func TestHasTranscript_FalseThenTrueAfterInsert(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	res, err := conn.Exec(
		`INSERT INTO lessons (lesson_date, tutor, video_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"2026-07-22", "Fulano", "aula.mp4", "2026-07-22T09:00:00Z", "2026-07-22T09:00:00Z",
	)
	if err != nil {
		t.Fatalf("inserir lesson de fixture falhou: %v", err)
	}
	lessonID, _ := res.LastInsertId()

	has, err := HasTranscript(conn, lessonID)
	if err != nil {
		t.Fatalf("HasTranscript() erro inesperado: %v", err)
	}
	if has {
		t.Error("HasTranscript() = true antes de inserir, esperado false")
	}

	if err := InsertTranscript(conn, lessonID, "aula.transcript.json", `[{"speaker":"speaker_0","text":"hi"}]`); err != nil {
		t.Fatalf("InsertTranscript() erro inesperado: %v", err)
	}

	has, err = HasTranscript(conn, lessonID)
	if err != nil {
		t.Fatalf("HasTranscript() erro inesperado: %v", err)
	}
	if !has {
		t.Error("HasTranscript() = false após inserir, esperado true")
	}
}

func TestInsertTranscript_PersistsRawPathAndUtterances(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	res, err := conn.Exec(
		`INSERT INTO lessons (lesson_date, tutor, video_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"2026-07-22", "Fulano", "aula.mp4", "2026-07-22T09:00:00Z", "2026-07-22T09:00:00Z",
	)
	if err != nil {
		t.Fatalf("inserir lesson de fixture falhou: %v", err)
	}
	lessonID, _ := res.LastInsertId()

	if err := InsertTranscript(conn, lessonID, "aula.transcript.json", `[{"speaker":"speaker_0","text":"hi"}]`); err != nil {
		t.Fatalf("InsertTranscript() erro inesperado: %v", err)
	}

	var rawPath, utterances string
	err = conn.QueryRow(`SELECT raw_json_path, utterances FROM transcripts WHERE lesson_id = ?`, lessonID).Scan(&rawPath, &utterances)
	if err != nil {
		t.Fatalf("select em transcripts falhou: %v", err)
	}
	if rawPath != "aula.transcript.json" {
		t.Errorf("raw_json_path = %q, esperado aula.transcript.json", rawPath)
	}
	if utterances != `[{"speaker":"speaker_0","text":"hi"}]` {
		t.Errorf("utterances = %q, não bate com o que foi inserido", utterances)
	}
}
```

- [ ] **Step 6: Rodar e confirmar falha de compilação**

Run: `go test ./internal/db/... -run TestHasTranscript -v`
Expected: FAIL — `undefined: HasTranscript`

- [ ] **Step 7: Implementar `internal/db/transcripts.go`**

```go
// internal/db/transcripts.go
package db

import (
	"database/sql"
	"fmt"
	"time"
)

// HasTranscript indica se já existe uma transcrição gravada para
// lessonID — usado por internal/jobs.Worker pra idempotência do job
// transcribe.
func HasTranscript(conn *sql.DB, lessonID int64) (bool, error) {
	var id int64
	err := conn.QueryRow(`SELECT id FROM transcripts WHERE lesson_id = ?`, lessonID).Scan(&id)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("buscar transcript da lesson %d: %w", lessonID, err)
	}
	return true, nil
}

// InsertTranscript grava a transcrição de uma lesson. rawJSONPath é
// relativo à storage_root (mesma convenção de lessons.video_path);
// utterancesJSON já vem serializado ([]stt.Utterance em JSON).
func InsertTranscript(conn *sql.DB, lessonID int64, rawJSONPath string, utterancesJSON string) error {
	_, err := conn.Exec(
		`INSERT INTO transcripts (lesson_id, raw_json_path, utterances, created_at) VALUES (?, ?, ?, ?)`,
		lessonID, rawJSONPath, utterancesJSON, time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("inserir transcript da lesson %d: %w", lessonID, err)
	}
	return nil
}
```

- [ ] **Step 8: Rodar todos os testes de `internal/db` e confirmar que passam**

Run: `go test ./internal/db/... -v`
Expected: PASS em todos.

- [ ] **Step 9: `go vet` e commit**

```bash
go vet ./internal/db/...
git add internal/db/lessons.go internal/db/lessons_test.go internal/db/transcripts.go internal/db/transcripts_test.go
git commit -m "feat: adiciona FindLessonByID e persistencia de transcript"
```

---

### Task 3: `internal/config` — diretório de cache de áudio

**Files:**
- Modify: `internal/config/paths.go` (adicionar `AudioCacheDir`)
- Modify: `internal/config/config_test.go` (adicionar teste)

**Interfaces:**
- Produces: `func AudioCacheDir() (string, error)` — resolve e cria `AppDataDir()/audio-cache`.

- [ ] **Step 1: Escrever o teste (vai falhar por falta de implementação)**

Adicionar ao final de `internal/config/config_test.go`:

```go
func TestAudioCacheDir_IsUnderAppDataDirAudioCacheSubdirAndCreated(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	appDir, err := AppDataDir()
	if err != nil {
		t.Fatalf("AppDataDir() erro inesperado: %v", err)
	}
	cacheDir, err := AudioCacheDir()
	if err != nil {
		t.Fatalf("AudioCacheDir() erro inesperado: %v", err)
	}
	want := filepath.Join(appDir, "audio-cache")
	if cacheDir != want {
		t.Errorf("AudioCacheDir() = %q, esperado %q", cacheDir, want)
	}
	info, err := os.Stat(cacheDir)
	if err != nil {
		t.Fatalf("diretório não foi criado: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("%q não é um diretório", cacheDir)
	}
}
```

- [ ] **Step 2: Rodar e confirmar falha de compilação**

Run: `go test ./internal/config/... -run TestAudioCacheDir -v`
Expected: FAIL — `undefined: AudioCacheDir`

- [ ] **Step 3: Implementar `AudioCacheDir` em `internal/config/paths.go`**

Adicionar ao final do arquivo (imports `fmt`, `os`, `path/filepath` já existem no arquivo):

```go
// AudioCacheDir resolve (criando se necessário) o diretório de cache de
// áudio intermediário (WAVs extraídos pra chamar a API de STT) dentro do
// AppDataDir — fora da pasta sincronizada, já que esses arquivos são
// descartáveis assim que a transcrição é salva (ver internal/jobs).
func AudioCacheDir() (string, error) {
	dir, err := AppDataDir()
	if err != nil {
		return "", err
	}
	cacheDir := filepath.Join(dir, "audio-cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", fmt.Errorf("criar diretório de cache de áudio: %w", err)
	}
	return cacheDir, nil
}
```

- [ ] **Step 4: Rodar todos os testes de `internal/config` e confirmar que passam**

Run: `go test ./internal/config/... -v`
Expected: PASS em todos.

- [ ] **Step 5: `go vet` e commit**

```bash
go vet ./internal/config/...
git add internal/config/paths.go internal/config/config_test.go
git commit -m "feat: adiciona AudioCacheDir para o cache de audio do worker"
```

---

### Task 4: `internal/jobs` — Worker: construção, seleção, precedência e backoff

**Files:**
- Create: `internal/jobs/worker.go`
- Test: `internal/jobs/worker_test.go`

**Interfaces:**
- Consumes: `db.Job`, `db.ListPendingJobs`, `db.FindJob`, `db.MarkJobRunning`, `db.MarkJobBlocked`,
  `db.MarkJobDone`, `db.MarkJobRetryOrError`, `db.RequeueRunningJobs`, `db.FindLessonByID`,
  `db.HasTranscript`, `db.InsertTranscript` (Tasks 1–2); `stt.Provider` e `stt.Result`/`stt.Utterance`
  (`internal/stt/stt.go`, já existente).
- Produces: `type MediaExtractorFunc func(ctx context.Context, videoPath, outputPath string) error`;
  `type StorageRootResolver func() (string, error)`; `type STTProviderFactory func() (stt.Provider, error)`;
  `type Notifier interface { JobChanged(JobEvent) }`; `type JobEvent struct { LessonID int64; Kind, Status, LastError string; Attempts int }`;
  `type Option func(*Worker)`; `func WithPollInterval(d time.Duration) Option`; `func WithLogger(l *slog.Logger) Option`;
  `func NewWorker(conn *sql.DB, storageRoot StorageRootResolver, audioCacheDir string, extractAudio MediaExtractorFunc, sttFactory STTProviderFactory, notifier Notifier, opts ...Option) *Worker`;
  método `(*Worker) Run(ctx context.Context) error`; método `(*Worker) Wake()`.
  Métodos não exportados usados por Tasks 5–6: `claimNextEligibleJob`, `eligibleForRetry` (função livre),
  `process`, `fail`, `runExtractAudio`, `runTranscribe`, `audioPathFor`, `rawJSONRelPath`.

- [ ] **Step 1: Escrever os testes de seleção/precedência (vão falhar — pacote não existe)**

```go
// internal/jobs/worker_test.go
package jobs

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"assistente-idiomas/internal/db"
	"assistente-idiomas/internal/stt"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() erro inesperado: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func insertLesson(t *testing.T, conn *sql.DB, videoPath string) int64 {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := conn.Exec(
		`INSERT INTO lessons (lesson_date, tutor, video_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"2026-07-22", "Fulano", videoPath, now, now,
	)
	if err != nil {
		t.Fatalf("inserir lesson de fixture falhou: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("obter id da lesson de fixture falhou: %v", err)
	}
	return id
}

func insertJob(t *testing.T, conn *sql.DB, lessonID int64, kind, status string, attempts int, createdAt, updatedAt string) int64 {
	t.Helper()
	res, err := conn.Exec(
		`INSERT INTO jobs (lesson_id, kind, status, attempts, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		lessonID, kind, status, attempts, createdAt, updatedAt,
	)
	if err != nil {
		t.Fatalf("inserir job de fixture falhou: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("obter id do job de fixture falhou: %v", err)
	}
	return id
}

func fakeExtractAudioAlwaysOK(ctx context.Context, videoPath, outputPath string) error {
	return os.WriteFile(outputPath, []byte("wav-fake"), 0o644)
}

type fakeSTTProvider struct {
	result *stt.Result
	err    error
	calls  int
}

func (f *fakeSTTProvider) Name() string { return "fake" }

func (f *fakeSTTProvider) Transcribe(ctx context.Context, audioPath string) (*stt.Result, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

func newTestWorker(t *testing.T, conn *sql.DB, opts ...Option) *Worker {
	t.Helper()
	storageRoot := t.TempDir()
	audioCacheDir := t.TempDir()
	return NewWorker(
		conn,
		func() (string, error) { return storageRoot, nil },
		audioCacheDir,
		fakeExtractAudioAlwaysOK,
		func() (stt.Provider, error) { return &fakeSTTProvider{result: &stt.Result{RawResponse: []byte(`{}`)}}, nil },
		noopNotifier{},
		opts...,
	)
}

func TestClaimNextEligibleJob_PicksOldestPendingFirstAndMarksRunning(t *testing.T) {
	conn := newTestDB(t)
	lessonA := insertLesson(t, conn, "aula-a.mp4")
	lessonB := insertLesson(t, conn, "aula-b.mp4")
	insertJob(t, conn, lessonB, "extract_audio", "pending", 0, "2026-07-22T10:00:05Z", "2026-07-22T10:00:05Z")
	olderID := insertJob(t, conn, lessonA, "extract_audio", "pending", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")

	w := newTestWorker(t, conn)

	job, err := w.claimNextEligibleJob()
	if err != nil {
		t.Fatalf("claimNextEligibleJob() erro inesperado: %v", err)
	}
	if job == nil || job.ID != olderID {
		t.Fatalf("claimNextEligibleJob() = %+v, esperado job %d (mais antigo)", job, olderID)
	}
	if job.Status != "running" {
		t.Errorf("job.Status = %q, esperado running", job.Status)
	}

	persisted, err := db.FindJob(conn, lessonA, "extract_audio")
	if err != nil {
		t.Fatalf("FindJob() erro inesperado: %v", err)
	}
	if persisted.Status != "running" {
		t.Errorf("status persistido = %q, esperado running", persisted.Status)
	}
}

func TestClaimNextEligibleJob_BlocksTranscribeWhenExtractAudioErrored(t *testing.T) {
	conn := newTestDB(t)
	lessonID := insertLesson(t, conn, "aula.mp4")
	now := "2026-07-22T10:00:00Z"
	insertJob(t, conn, lessonID, "extract_audio", "error", 3, now, now)
	transcribeID := insertJob(t, conn, lessonID, "transcribe", "pending", 0, now, now)

	sttCalled := false
	w := NewWorker(
		conn,
		func() (string, error) { return t.TempDir(), nil },
		t.TempDir(),
		fakeExtractAudioAlwaysOK,
		func() (stt.Provider, error) {
			sttCalled = true
			return &fakeSTTProvider{}, nil
		},
		noopNotifier{},
	)

	job, err := w.claimNextEligibleJob()
	if err != nil {
		t.Fatalf("claimNextEligibleJob() erro inesperado: %v", err)
	}
	if job != nil {
		t.Fatalf("claimNextEligibleJob() = %+v, esperado nil (nada elegível)", job)
	}
	if sttCalled {
		t.Error("sttFactory não deveria ser chamado — extract_audio já falhou")
	}

	got, err := db.FindJob(conn, lessonID, "transcribe")
	if err != nil {
		t.Fatalf("FindJob() erro inesperado: %v", err)
	}
	if got.ID != transcribeID || got.Status != "error" {
		t.Errorf("transcribe = %+v, esperado status=error", got)
	}
}
```

- [ ] **Step 2: Rodar e confirmar falha de compilação**

Run: `go test ./internal/jobs/... -v`
Expected: FAIL — pacote `internal/jobs` não compila (`NewWorker`, `noopNotifier` etc. indefinidos)

- [ ] **Step 3: Implementar `internal/jobs/worker.go`**

```go
// internal/jobs/worker.go
package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"assistente-idiomas/internal/db"
	"assistente-idiomas/internal/stt"
)

// maxAttempts é o total de tentativas (a primeira + os retries) antes de
// um job falho virar "error" terminal — ver docs/fase-1-mvp.md (História 4).
const maxAttempts = 3

// backoff mapeia attempts (já incrementado após uma falha) para o tempo
// mínimo de espera antes da próxima tentativa.
var backoff = map[int]time.Duration{
	1: 10 * time.Second,
	2: 60 * time.Second,
	3: 5 * time.Minute,
}

const defaultPollInterval = 5 * time.Second

// MediaExtractorFunc tem a mesma assinatura de media.ExtractAudio —
// permite injetar um fake nos testes sem depender de ffmpeg.
type MediaExtractorFunc func(ctx context.Context, videoPath, outputPath string) error

// StorageRootResolver resolve o path absoluto da pasta de armazenamento.
// Reavaliado a cada job (não guardado como valor fixo na criação do
// Worker) porque o wizard de primeira execução grava essa configuração
// depois que o app (e o Worker) já foram iniciados — ver o spec da
// História 4 em docs/superpowers/specs/.
type StorageRootResolver func() (string, error)

// STTProviderFactory constrói (ou retorna) o stt.Provider a usar. Mesma
// razão de StorageRootResolver: a credencial de STT só existe depois do
// wizard.
type STTProviderFactory func() (stt.Provider, error)

// Notifier é notificado a cada transição de status de job. A implementação
// real (que emite eventos Wails) mora em services/ — internal/jobs não
// importa Wails (camada fina).
type Notifier interface {
	JobChanged(JobEvent)
}

// JobEvent é o payload passado ao Notifier a cada transição.
type JobEvent struct {
	LessonID  int64
	Kind      string
	Status    string
	Attempts  int
	LastError string
}

type noopNotifier struct{}

func (noopNotifier) JobChanged(JobEvent) {}

// Worker processa a fila de jobs (tabela jobs) sequencialmente, um de cada
// vez — ver decisão "worker único" em docs/decisoes-tecnologia.md.
type Worker struct {
	conn          *sql.DB
	storageRoot   StorageRootResolver
	audioCacheDir string
	extractAudio  MediaExtractorFunc
	sttFactory    STTProviderFactory
	notifier      Notifier
	logger        *slog.Logger
	pollInterval  time.Duration
	wake          chan struct{}
}

// Option customiza um Worker na criação — usado nos testes pra encurtar o
// pollInterval.
type Option func(*Worker)

func WithPollInterval(d time.Duration) Option {
	return func(w *Worker) { w.pollInterval = d }
}

func WithLogger(l *slog.Logger) Option {
	return func(w *Worker) { w.logger = l }
}

func NewWorker(
	conn *sql.DB,
	storageRoot StorageRootResolver,
	audioCacheDir string,
	extractAudio MediaExtractorFunc,
	sttFactory STTProviderFactory,
	notifier Notifier,
	opts ...Option,
) *Worker {
	if notifier == nil {
		notifier = noopNotifier{}
	}
	w := &Worker{
		conn:          conn,
		storageRoot:   storageRoot,
		audioCacheDir: audioCacheDir,
		extractAudio:  extractAudio,
		sttFactory:    sttFactory,
		notifier:      notifier,
		logger:        slog.Default(),
		pollInterval:  defaultPollInterval,
		wake:          make(chan struct{}, 1),
	}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

// Wake sinaliza ao Worker que há um job novo pra olhar, sem esperar o
// próximo tick do poll de fallback. Não bloqueia — se já houver um sinal
// pendente, este é descartado (o worker já vai acordar).
func (w *Worker) Wake() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// Run bloqueia processando jobs até ctx ser cancelado. Ao iniciar, faz
// requeue de qualquer job preso em "running" (crash/kill anterior).
func (w *Worker) Run(ctx context.Context) error {
	if _, err := db.RequeueRunningJobs(w.conn); err != nil {
		return fmt.Errorf("jobs: requeue de jobs presos em running: %w", err)
	}
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()
	for {
		for {
			job, err := w.claimNextEligibleJob()
			if err != nil {
				w.logger.Error("jobs: erro ao selecionar próximo job", "erro", err)
				break
			}
			if job == nil {
				break
			}
			w.process(ctx, *job)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-w.wake:
		case <-ticker.C:
		}
	}
}

// claimNextEligibleJob escolhe o próximo job pending elegível (respeitando
// backoff e a precedência de transcribe sobre extract_audio) e o marca
// running. Retorna (nil, nil) se nada estiver elegível agora.
func (w *Worker) claimNextEligibleJob() (*db.Job, error) {
	pending, err := db.ListPendingJobs(w.conn)
	if err != nil {
		return nil, fmt.Errorf("listar jobs pendentes: %w", err)
	}
	now := time.Now().UTC()
	for _, j := range pending {
		if j.Kind == "transcribe" {
			sibling, err := db.FindJob(w.conn, j.LessonID, "extract_audio")
			if err != nil {
				return nil, fmt.Errorf("buscar job extract_audio da lesson %d: %w", j.LessonID, err)
			}
			if sibling == nil {
				continue
			}
			if sibling.Status == "error" {
				reason := fmt.Sprintf("depende de extract_audio que falhou: %s", sibling.LastError)
				if err := db.MarkJobBlocked(w.conn, j.ID, reason); err != nil {
					return nil, fmt.Errorf("bloquear job transcribe %d: %w", j.ID, err)
				}
				w.notifier.JobChanged(JobEvent{LessonID: j.LessonID, Kind: j.Kind, Status: "error", Attempts: j.Attempts, LastError: reason})
				continue
			}
			if sibling.Status != "done" {
				continue
			}
		}
		if !eligibleForRetry(j, now) {
			continue
		}
		if err := db.MarkJobRunning(w.conn, j.ID); err != nil {
			return nil, fmt.Errorf("reivindicar job %d: %w", j.ID, err)
		}
		claimed := j
		claimed.Status = "running"
		w.notifier.JobChanged(JobEvent{LessonID: claimed.LessonID, Kind: claimed.Kind, Status: "running", Attempts: claimed.Attempts})
		return &claimed, nil
	}
	return nil, nil
}

// eligibleForRetry indica se j já passou da janela de backoff da sua
// última tentativa (se attempts == 0, é a primeira tentativa: sempre
// elegível).
func eligibleForRetry(j db.Job, now time.Time) bool {
	if j.Attempts == 0 {
		return true
	}
	wait, ok := backoff[j.Attempts]
	if !ok {
		wait = backoff[maxAttempts]
	}
	updatedAt, err := time.Parse(time.RFC3339, j.UpdatedAt)
	if err != nil {
		return true
	}
	return now.After(updatedAt.Add(wait))
}

// process executa job (já marcado running) e registra o resultado.
func (w *Worker) process(ctx context.Context, job db.Job) {
	var err error
	switch job.Kind {
	case "extract_audio":
		err = w.runExtractAudio(ctx, job)
	case "transcribe":
		err = w.runTranscribe(ctx, job)
	default:
		err = fmt.Errorf("kind de job desconhecido: %s", job.Kind)
	}
	if err != nil {
		w.fail(job, err)
		return
	}
	if markErr := db.MarkJobDone(w.conn, job.ID); markErr != nil {
		w.logger.Error("jobs: erro ao marcar job como done", "job_id", job.ID, "erro", markErr)
		return
	}
	w.notifier.JobChanged(JobEvent{LessonID: job.LessonID, Kind: job.Kind, Status: "done", Attempts: job.Attempts})
}

// fail registra a falha de execução de job: incrementa attempts e decide
// entre retry (volta a pending) ou error terminal.
func (w *Worker) fail(job db.Job, cause error) {
	status, attempts, err := db.MarkJobRetryOrError(w.conn, job.ID, cause.Error(), maxAttempts)
	if err != nil {
		w.logger.Error("jobs: erro ao registrar falha do job", "job_id", job.ID, "erro", err)
		return
	}
	w.notifier.JobChanged(JobEvent{LessonID: job.LessonID, Kind: job.Kind, Status: status, Attempts: attempts, LastError: cause.Error()})
}

// runExtractAudio extrai o áudio do vídeo da lesson pro cache
// (audioCacheDir/<lessonID>.wav), pulando se o WAV já existir
// (idempotência).
func (w *Worker) runExtractAudio(ctx context.Context, job db.Job) error {
	lesson, err := db.FindLessonByID(w.conn, job.LessonID)
	if err != nil {
		return fmt.Errorf("buscar lesson %d: %w", job.LessonID, err)
	}
	if lesson == nil {
		return fmt.Errorf("lesson %d não encontrada", job.LessonID)
	}
	audioPath := w.audioPathFor(job.LessonID)
	if info, statErr := os.Stat(audioPath); statErr == nil && info.Size() > 0 {
		return nil
	}
	root, err := w.storageRoot()
	if err != nil {
		return fmt.Errorf("resolver storage_root: %w", err)
	}
	videoPath := filepath.Join(root, filepath.FromSlash(lesson.VideoPath))
	if err := w.extractAudio(ctx, videoPath, audioPath); err != nil {
		return fmt.Errorf("extrair áudio: %w", err)
	}
	return nil
}

// runTranscribe transcreve o áudio em cache da lesson via STT, gravando o
// JSON bruto junto do vídeo e a transcrição mapeada em transcripts. Pula
// se já existir uma transcrição pra essa lesson (idempotência).
func (w *Worker) runTranscribe(ctx context.Context, job db.Job) error {
	has, err := db.HasTranscript(w.conn, job.LessonID)
	if err != nil {
		return fmt.Errorf("verificar transcrição existente: %w", err)
	}
	if has {
		return nil
	}
	lesson, err := db.FindLessonByID(w.conn, job.LessonID)
	if err != nil {
		return fmt.Errorf("buscar lesson %d: %w", job.LessonID, err)
	}
	if lesson == nil {
		return fmt.Errorf("lesson %d não encontrada", job.LessonID)
	}
	provider, err := w.sttFactory()
	if err != nil {
		return fmt.Errorf("obter provedor de STT: %w", err)
	}
	audioPath := w.audioPathFor(job.LessonID)
	result, err := provider.Transcribe(ctx, audioPath)
	if err != nil {
		return fmt.Errorf("transcrever: %w", err)
	}
	root, err := w.storageRoot()
	if err != nil {
		return fmt.Errorf("resolver storage_root: %w", err)
	}
	rawRelPath := rawJSONRelPath(lesson.VideoPath)
	rawAbsPath := filepath.Join(root, filepath.FromSlash(rawRelPath))
	if err := os.WriteFile(rawAbsPath, result.RawResponse, 0o644); err != nil {
		return fmt.Errorf("gravar JSON bruto: %w", err)
	}
	utterancesJSON, err := json.Marshal(result.Utterances)
	if err != nil {
		return fmt.Errorf("serializar utterances: %w", err)
	}
	if err := db.InsertTranscript(w.conn, job.LessonID, rawRelPath, string(utterancesJSON)); err != nil {
		return fmt.Errorf("gravar transcript: %w", err)
	}
	if err := os.Remove(audioPath); err != nil && !os.IsNotExist(err) {
		w.logger.Warn("jobs: falha ao remover WAV do cache após transcrição", "path", audioPath, "erro", err)
	}
	return nil
}

func (w *Worker) audioPathFor(lessonID int64) string {
	return filepath.Join(w.audioCacheDir, strconv.FormatInt(lessonID, 10)+".wav")
}

// rawJSONRelPath calcula o path (relativo à storage_root, sempre com "/")
// do JSON bruto do provedor: mesmo diretório do vídeo, nome
// "<basename-sem-extensão>.transcript.json" — sem assumir nenhuma
// subpasta (consistente com a varredura da História 3).
func rawJSONRelPath(videoRelPath string) string {
	dir := path.Dir(videoRelPath)
	base := strings.TrimSuffix(path.Base(videoRelPath), path.Ext(videoRelPath))
	return path.Join(dir, base+".transcript.json")
}
```

- [ ] **Step 4: Rodar os testes e confirmar que passam**

Run: `go test ./internal/jobs/... -v`
Expected: PASS nos dois testes de `worker_test.go`.

- [ ] **Step 5: `go vet` e commit**

```bash
go vet ./internal/jobs/...
git add internal/jobs/worker.go internal/jobs/worker_test.go
git commit -m "feat: adiciona Worker com selecao e precedencia de jobs"
```

---

### Task 5: `internal/jobs` — idempotência e artefatos (extract_audio/transcribe)

**Files:**
- Modify: `internal/jobs/worker_test.go` (adicionar import `encoding/json` e 4 testes)

**Interfaces:**
- Consumes: tudo produzido na Task 4 (mesmo arquivo/pacote); `db.InsertTranscript`, `db.HasTranscript`
  (Task 2); `stt.Utterance` (`internal/stt/stt.go`).

- [ ] **Step 1: Adicionar o import `encoding/json` e os 4 testes (vão falhar — funções não usadas ainda por eles, mas o comportamento está incorreto/incompleto sem o teste)**

Editar o bloco de imports no topo de `internal/jobs/worker_test.go`, adicionando `"encoding/json"`:

```go
import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"assistente-idiomas/internal/db"
	"assistente-idiomas/internal/stt"
)
```

Adicionar ao final do arquivo:

```go
func TestRunExtractAudio_SkipsWhenWavAlreadyCached(t *testing.T) {
	conn := newTestDB(t)
	lessonID := insertLesson(t, conn, "aula.mp4")
	job := db.Job{ID: insertJob(t, conn, lessonID, "extract_audio", "running", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z"), LessonID: lessonID, Kind: "extract_audio"}

	audioCacheDir := t.TempDir()
	extractCalls := 0
	w := NewWorker(
		conn,
		func() (string, error) { return t.TempDir(), nil },
		audioCacheDir,
		func(ctx context.Context, videoPath, outputPath string) error {
			extractCalls++
			return nil
		},
		func() (stt.Provider, error) { return &fakeSTTProvider{}, nil },
		noopNotifier{},
	)

	if err := os.WriteFile(w.audioPathFor(lessonID), []byte("ja-existe"), 0o644); err != nil {
		t.Fatalf("preparar wav de fixture falhou: %v", err)
	}

	if err := w.runExtractAudio(context.Background(), job); err != nil {
		t.Fatalf("runExtractAudio() erro inesperado: %v", err)
	}
	if extractCalls != 0 {
		t.Errorf("extractAudio foi chamado %d vezes, esperado 0 (WAV já em cache)", extractCalls)
	}
}

func TestRunExtractAudio_CallsExtractorWhenNotCached(t *testing.T) {
	conn := newTestDB(t)
	storageRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(storageRoot, "aula.mp4"), []byte("video"), 0o644); err != nil {
		t.Fatalf("preparar vídeo de fixture falhou: %v", err)
	}
	lessonID := insertLesson(t, conn, "aula.mp4")
	job := db.Job{ID: insertJob(t, conn, lessonID, "extract_audio", "running", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z"), LessonID: lessonID, Kind: "extract_audio"}

	audioCacheDir := t.TempDir()
	var gotVideoPath, gotOutputPath string
	w := NewWorker(
		conn,
		func() (string, error) { return storageRoot, nil },
		audioCacheDir,
		func(ctx context.Context, videoPath, outputPath string) error {
			gotVideoPath, gotOutputPath = videoPath, outputPath
			return os.WriteFile(outputPath, []byte("wav"), 0o644)
		},
		func() (stt.Provider, error) { return &fakeSTTProvider{}, nil },
		noopNotifier{},
	)

	if err := w.runExtractAudio(context.Background(), job); err != nil {
		t.Fatalf("runExtractAudio() erro inesperado: %v", err)
	}
	if gotVideoPath != filepath.Join(storageRoot, "aula.mp4") {
		t.Errorf("videoPath = %q, esperado %q", gotVideoPath, filepath.Join(storageRoot, "aula.mp4"))
	}
	if gotOutputPath != w.audioPathFor(lessonID) {
		t.Errorf("outputPath = %q, esperado %q", gotOutputPath, w.audioPathFor(lessonID))
	}
	if _, err := os.Stat(w.audioPathFor(lessonID)); err != nil {
		t.Errorf("WAV não foi criado: %v", err)
	}
}

func TestRunTranscribe_SkipsWhenTranscriptAlreadyExists(t *testing.T) {
	conn := newTestDB(t)
	lessonID := insertLesson(t, conn, "aula.mp4")
	if err := db.InsertTranscript(conn, lessonID, "aula.transcript.json", "[]"); err != nil {
		t.Fatalf("InsertTranscript() de fixture falhou: %v", err)
	}
	job := db.Job{ID: insertJob(t, conn, lessonID, "transcribe", "running", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z"), LessonID: lessonID, Kind: "transcribe"}

	sttCalled := false
	w := NewWorker(
		conn,
		func() (string, error) { return t.TempDir(), nil },
		t.TempDir(),
		fakeExtractAudioAlwaysOK,
		func() (stt.Provider, error) {
			sttCalled = true
			return &fakeSTTProvider{}, nil
		},
		noopNotifier{},
	)

	if err := w.runTranscribe(context.Background(), job); err != nil {
		t.Fatalf("runTranscribe() erro inesperado: %v", err)
	}
	if sttCalled {
		t.Error("sttFactory não deveria ser chamado — transcript já existe")
	}
}

func TestRunTranscribe_WritesRawJSONInsertsTranscriptAndCleansCache(t *testing.T) {
	conn := newTestDB(t)
	storageRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(storageRoot, "aulas", "2026"), 0o755); err != nil {
		t.Fatalf("preparar subpasta de fixture falhou: %v", err)
	}
	videoRelPath := "aulas/2026/aula-01.mp4"
	lessonID := insertLesson(t, conn, videoRelPath)
	job := db.Job{ID: insertJob(t, conn, lessonID, "transcribe", "running", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z"), LessonID: lessonID, Kind: "transcribe"}

	audioCacheDir := t.TempDir()
	w := NewWorker(
		conn,
		func() (string, error) { return storageRoot, nil },
		audioCacheDir,
		fakeExtractAudioAlwaysOK,
		func() (stt.Provider, error) {
			return &fakeSTTProvider{result: &stt.Result{
				RawResponse: []byte(`{"raw":true}`),
				Utterances: []stt.Utterance{
					{Speaker: "speaker_0", Text: "hello", Start: 0, End: time.Second},
				},
			}}, nil
		},
		noopNotifier{},
	)
	if err := os.WriteFile(w.audioPathFor(lessonID), []byte("wav-em-cache"), 0o644); err != nil {
		t.Fatalf("preparar wav de fixture falhou: %v", err)
	}

	if err := w.runTranscribe(context.Background(), job); err != nil {
		t.Fatalf("runTranscribe() erro inesperado: %v", err)
	}

	rawAbsPath := filepath.Join(storageRoot, "aulas", "2026", "aula-01.transcript.json")
	rawContent, err := os.ReadFile(rawAbsPath)
	if err != nil {
		t.Fatalf("JSON bruto não foi gravado em %q: %v", rawAbsPath, err)
	}
	if string(rawContent) != `{"raw":true}` {
		t.Errorf("conteúdo do JSON bruto = %q, esperado {\"raw\":true}", rawContent)
	}

	has, err := db.HasTranscript(conn, lessonID)
	if err != nil {
		t.Fatalf("HasTranscript() erro inesperado: %v", err)
	}
	if !has {
		t.Error("HasTranscript() = false após runTranscribe, esperado true")
	}

	var rawPath, utterancesJSON string
	if err := conn.QueryRow(`SELECT raw_json_path, utterances FROM transcripts WHERE lesson_id = ?`, lessonID).Scan(&rawPath, &utterancesJSON); err != nil {
		t.Fatalf("select em transcripts falhou: %v", err)
	}
	if rawPath != "aulas/2026/aula-01.transcript.json" {
		t.Errorf("raw_json_path = %q, esperado aulas/2026/aula-01.transcript.json", rawPath)
	}
	var utterances []stt.Utterance
	if err := json.Unmarshal([]byte(utterancesJSON), &utterances); err != nil {
		t.Fatalf("utterances gravado não é JSON válido: %v", err)
	}
	if len(utterances) != 1 || utterances[0].Text != "hello" {
		t.Errorf("utterances = %+v, esperado 1 item com Text=hello", utterances)
	}

	if _, err := os.Stat(w.audioPathFor(lessonID)); !os.IsNotExist(err) {
		t.Errorf("WAV do cache deveria ter sido removido após sucesso, err=%v", err)
	}
}
```

- [ ] **Step 2: Rodar os 4 testes novos**

Run: `go test ./internal/jobs/... -run 'TestRunExtractAudio|TestRunTranscribe' -v`
Expected: PASS em todos — a implementação da Task 4 já cobre idempotência e escrita de artefatos,
nenhum código de produção novo é necessário aqui, só a cobertura de teste.

- [ ] **Step 3: Rodar a suíte inteira de `internal/jobs` e confirmar que nada quebrou**

Run: `go test ./internal/jobs/... -v`
Expected: PASS em todos os 6 testes (2 da Task 4 + 4 novos).

- [ ] **Step 4: `go vet` e commit**

```bash
go vet ./internal/jobs/...
git add internal/jobs/worker_test.go
git commit -m "test: cobre idempotencia e artefatos de extract_audio/transcribe"
```

---

### Task 6: `internal/jobs` — retry/backoff, requeue e loop `Run` completo

**Files:**
- Modify: `internal/jobs/worker_test.go` (adicionar import `errors` e 4 testes/1 tipo)

**Interfaces:**
- Consumes: tudo de Tasks 4–5 (mesmo pacote).
- Produces (só em teste): `type recordingNotifier struct { events *[]JobEvent }` com método
  `JobChanged(JobEvent)`, implementando `Notifier` pra inspecionar eventos emitidos.

- [ ] **Step 1: Adicionar o import `errors` e os testes**

Editar o bloco de imports no topo de `internal/jobs/worker_test.go`, adicionando `"errors"`:

```go
import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"assistente-idiomas/internal/db"
	"assistente-idiomas/internal/stt"
)
```

Adicionar ao final do arquivo:

```go
type recordingNotifier struct {
	events *[]JobEvent
}

func (r recordingNotifier) JobChanged(e JobEvent) {
	*r.events = append(*r.events, e)
}

func TestEligibleForRetry_RespectsBackoffWindow(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name     string
		attempts int
		updated  time.Time
		want     bool
	}{
		{"primeira tentativa sempre elegível", 0, now, true},
		{"1 falha, ainda dentro dos 10s", 1, now.Add(-5 * time.Second), false},
		{"1 falha, passou dos 10s", 1, now.Add(-11 * time.Second), true},
		{"2 falhas, ainda dentro de 60s", 2, now.Add(-30 * time.Second), false},
		{"2 falhas, passou de 60s", 2, now.Add(-61 * time.Second), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			j := db.Job{Attempts: tc.attempts, UpdatedAt: tc.updated.Format(time.RFC3339)}
			got := eligibleForRetry(j, now)
			if got != tc.want {
				t.Errorf("eligibleForRetry(attempts=%d, updated=%s) = %v, esperado %v", tc.attempts, tc.updated, got, tc.want)
			}
		})
	}
}

func TestFail_RetriesThenTerminatesAfterMaxAttempts(t *testing.T) {
	conn := newTestDB(t)
	lessonID := insertLesson(t, conn, "aula.mp4")
	job := db.Job{ID: insertJob(t, conn, lessonID, "extract_audio", "running", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z"), LessonID: lessonID, Kind: "extract_audio"}

	var events []JobEvent
	notifier := recordingNotifier{events: &events}
	w := NewWorker(
		conn,
		func() (string, error) { return t.TempDir(), nil },
		t.TempDir(),
		fakeExtractAudioAlwaysOK,
		func() (stt.Provider, error) { return &fakeSTTProvider{}, nil },
		notifier,
	)

	w.fail(job, errors.New("falha simulada"))
	w.fail(job, errors.New("falha simulada"))
	w.fail(job, errors.New("falha simulada"))

	got, err := db.FindJob(conn, lessonID, "extract_audio")
	if err != nil {
		t.Fatalf("FindJob() erro inesperado: %v", err)
	}
	if got.Status != "error" || got.Attempts != 3 {
		t.Errorf("job após 3 falhas = %+v, esperado status=error attempts=3", got)
	}
	if len(events) != 3 || events[2].Status != "error" {
		t.Errorf("eventos notificados = %+v, esperado 3 eventos terminando em error", events)
	}
}

func TestRun_RequeuesRunningJobsOnStart(t *testing.T) {
	conn := newTestDB(t)
	lessonID := insertLesson(t, conn, "aula.mp4")
	stuckID := insertJob(t, conn, lessonID, "extract_audio", "running", 1, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")

	w := newTestWorker(t, conn, WithPollInterval(10*time.Millisecond))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_ = w.Run(ctx)

	job, err := db.FindJob(conn, lessonID, "extract_audio")
	if err != nil {
		t.Fatalf("FindJob() erro inesperado: %v", err)
	}
	if job.ID != stuckID {
		t.Fatalf("job errado retornado por FindJob")
	}
	if job.Status == "running" {
		t.Errorf("job.Status = running, esperado que o requeue tivesse tirado do estado preso")
	}
}

func TestRun_ProcessesExtractAudioThenTranscribeEndToEnd(t *testing.T) {
	conn := newTestDB(t)
	storageRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(storageRoot, "aula.mp4"), []byte("video"), 0o644); err != nil {
		t.Fatalf("preparar vídeo de fixture falhou: %v", err)
	}
	lessonID := insertLesson(t, conn, "aula.mp4")
	now := time.Now().UTC().Format(time.RFC3339)
	insertJob(t, conn, lessonID, "extract_audio", "pending", 0, now, now)
	insertJob(t, conn, lessonID, "transcribe", "pending", 0, now, now)

	audioCacheDir := t.TempDir()
	w := NewWorker(
		conn,
		func() (string, error) { return storageRoot, nil },
		audioCacheDir,
		func(ctx context.Context, videoPath, outputPath string) error {
			return os.WriteFile(outputPath, []byte("wav"), 0o644)
		},
		func() (stt.Provider, error) {
			return &fakeSTTProvider{result: &stt.Result{RawResponse: []byte(`{}`), Utterances: []stt.Utterance{{Speaker: "speaker_0", Text: "oi"}}}}, nil
		},
		noopNotifier{},
		WithPollInterval(10*time.Millisecond),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = w.Run(ctx)

	extractJob, err := db.FindJob(conn, lessonID, "extract_audio")
	if err != nil {
		t.Fatalf("FindJob() erro inesperado: %v", err)
	}
	if extractJob.Status != "done" {
		t.Errorf("extract_audio.Status = %q, esperado done", extractJob.Status)
	}
	transcribeJob, err := db.FindJob(conn, lessonID, "transcribe")
	if err != nil {
		t.Fatalf("FindJob() erro inesperado: %v", err)
	}
	if transcribeJob.Status != "done" {
		t.Errorf("transcribe.Status = %q, esperado done", transcribeJob.Status)
	}
	has, err := db.HasTranscript(conn, lessonID)
	if err != nil {
		t.Fatalf("HasTranscript() erro inesperado: %v", err)
	}
	if !has {
		t.Error("HasTranscript() = false, esperado true após pipeline completo")
	}
}
```

- [ ] **Step 2: Rodar a suíte inteira de `internal/jobs` e confirmar que passa**

Run: `go test ./internal/jobs/... -v`
Expected: PASS em todos os testes (Tasks 4–6 combinadas).

- [ ] **Step 3: `go vet` e commit**

```bash
go vet ./internal/jobs/...
git add internal/jobs/worker_test.go
git commit -m "test: cobre retry/backoff, requeue e o loop Run do worker"
```

---

### Task 7: `services` — Notifier que emite eventos Wails

**Files:**
- Create: `services/jobs_notifier.go`

**Interfaces:**
- Consumes: `jobs.Notifier`, `jobs.JobEvent` (`internal/jobs/worker.go`, Task 4).
- Produces: `type WailsJobNotifier struct{}` com método `JobChanged(e jobs.JobEvent)`;
  `const JobUpdatedEvent = "job:updated"`.

Sem teste automatizado nesta task: `WailsJobNotifier.JobChanged` só faz sentido chamando
`application.Get().Event.Emit(...)`, que exige um app Wails já inicializado — o mesmo motivo
pelo qual `main.go` e os métodos de `SetupService` que usam `application.Get().Dialog` não têm
teste automatizado neste projeto. A verificação é `go build`/`go vet` (Step 2) mais a checagem
manual do evento chegando ao frontend, quando a Fila (História 7) existir para consumi-lo.

- [ ] **Step 1: Criar `services/jobs_notifier.go`**

```go
// services/jobs_notifier.go
package services

import (
	"assistente-idiomas/internal/jobs"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// JobUpdatedEvent é o nome do evento Wails emitido a cada transição de
// status de job — nenhuma tela consome isso ainda (fica pras Históras 5 e
// 7); esta história só monta o transporte.
const JobUpdatedEvent = "job:updated"

// WailsJobNotifier implementa jobs.Notifier emitindo eventos Wails — única
// peça do pipeline (História 4) que sabe que o Wails existe. internal/jobs
// em si não importa Wails (camada fina).
type WailsJobNotifier struct{}

func (WailsJobNotifier) JobChanged(e jobs.JobEvent) {
	application.Get().Event.Emit(JobUpdatedEvent, e)
}
```

- [ ] **Step 2: Verificar que compila**

Run: `go build ./services/... && go vet ./services/...`
Expected: sem erros.

- [ ] **Step 3: Commit**

```bash
git add services/jobs_notifier.go
git commit -m "feat: adiciona WailsJobNotifier para eventos de job"
```

---

### Task 8: `main.go` — inicia o worker em background

**Files:**
- Modify: `main.go`

**Interfaces:**
- Consumes: `jobs.NewWorker`, `jobs.StorageRootResolver`, `jobs.STTProviderFactory` (Task 4);
  `services.WailsJobNotifier` (Task 7); `config.Load`, `config.GetSTTAPIKey`, `config.AudioCacheDir`
  (`internal/config`, já existente + Task 3); `media.ExtractAudio` (`internal/media/media.go`, já
  existente); `stt.NewElevenLabsProvider` (`internal/stt/elevenlabs.go`, já existente).

Sem teste automatizado — é wiring de `main()`, no mesmo padrão do resto do arquivo (não testado
neste projeto). Verificação por build/vet (Step 2) e, se possível, abertura visual do app.

- [ ] **Step 1: Reescrever `main.go`**

```go
package main

import (
	"context"
	"database/sql"
	"embed"
	"log"

	"assistente-idiomas/internal/config"
	"assistente-idiomas/internal/db"
	"assistente-idiomas/internal/jobs"
	"assistente-idiomas/internal/media"
	"assistente-idiomas/internal/stt"
	"assistente-idiomas/services"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	dbPath, err := config.DBPath()
	if err != nil {
		log.Fatalf("resolver caminho do banco: %v", err)
	}
	conn, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("abrir banco de dados: %v", err)
	}
	defer conn.Close()

	startJobWorker(conn)

	app := application.New(application.Options{
		Name:        "Assistente de Idiomas",
		Description: "Arquivo e análise de aulas de inglês do Cambly",
		Services: []application.Service{
			application.NewService(services.NewSetupService()),
			application.NewService(services.NewImportService(conn)),
			application.NewService(services.NewLibraryService(conn)),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "Assistente de Idiomas",
		Width:            1200,
		Height:           760,
		BackgroundColour: application.NewRGB(20, 24, 31), // #14181F — colors.bg
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

// startJobWorker inicia o pipeline em background (História 4) numa
// goroutine. storage_root e a credencial de STT são resolvidos a cada job,
// não aqui — o wizard de primeira execução ainda não rodou neste ponto do
// startup, então resolvê-los agora falharia sempre na primeira sessão do
// app (ver docs/superpowers/specs/2026-07-22-historia-4-pipeline-jobs-design.md).
// Só o cache de áudio (que não depende do wizard) é resolvido aqui; se
// isso falhar, é um problema de disco/permissão e o worker não inicia.
func startJobWorker(conn *sql.DB) {
	audioCacheDir, err := config.AudioCacheDir()
	if err != nil {
		log.Printf("worker de jobs não iniciado: %v", err)
		return
	}
	storageRoot := func() (string, error) {
		cfg, err := config.Load()
		if err != nil {
			return "", err
		}
		return cfg.StorageRoot, nil
	}
	sttFactory := func() (stt.Provider, error) {
		apiKey, err := config.GetSTTAPIKey()
		if err != nil {
			return nil, err
		}
		return stt.NewElevenLabsProvider(apiKey)
	}
	worker := jobs.NewWorker(conn, storageRoot, audioCacheDir, media.ExtractAudio, sttFactory, services.WailsJobNotifier{})
	go func() {
		if err := worker.Run(context.Background()); err != nil {
			log.Printf("worker de jobs encerrado: %v", err)
		}
	}()
}
```

- [ ] **Step 2: Verificar que compila e passa `go vet`**

Run: `go build ./internal/... ./services/... . && go vet ./...`
Expected: sem erros (o erro conhecido e não relacionado de `go build ./...` no pacote
`build/ios` do scaffold do Wails não afeta esta verificação, que já exclui esse pacote).

- [ ] **Step 3: Rodar a suíte completa do projeto**

Run: `go test ./... -v`
Expected: PASS em todos os pacotes (`internal/db`, `internal/config`, `internal/jobs`,
`internal/media`, `internal/stt`, `services`, e os demais já existentes).

- [ ] **Step 4: Commit**

```bash
git add main.go
git commit -m "feat: inicia o worker de jobs em background no startup do app"
```

---

## Verificação manual pendente (fora do escopo de testes automatizados)

Depois da Task 8, com uma máquina que tenha display (Windows/Linux com GUI — ver risco 3 e a
pendência de verificação visual já registrada para as Histórias 1 e 3 em `docs/fase-1-mvp.md`):
rodar `wails3 dev`, completar o wizard, confirmar uma aula de teste e observar no log/backend que
os jobs `extract_audio`/`transcribe` avançam para `done` (ou `error` com uma API key inválida,
sem travar o app nem impedir assistir ao vídeo). Isso não é um passo desta plan — é a mesma
verificação visual pendente já anotada no `docs/fase-1-mvp.md`, a ser feita quando alguém abrir o
app numa máquina com display.
