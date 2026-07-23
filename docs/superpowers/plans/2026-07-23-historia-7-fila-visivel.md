# História 7 — Fila visível — Plano de implementação

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Tela de Fila mostrando aulas com pipeline ativo/em erro (uma linha por aula, etapa +
estado, `last_error` legível, botão Reprocessar) e badge de contagem de jobs ativos na sidebar,
ambos atualizados ao vivo pelo evento Wails `job:updated` (já emitido desde a História 4, sem
consumidor até agora).

**Architecture:** Query nova em `internal/db` deriva, por aula, qual job (extract_audio ou
transcribe) está "ativo" agora ou em erro, com prioridade que respeita o bloqueio de dependência
entre os dois jobs. `services.QueueService` expõe isso traduzido pro frontend. No frontend, um
store reativo único (`jobsStore.svelte.ts`, runes do Svelte 5) busca a lista uma vez e refaz a
busca a cada `job:updated`; `Queue.svelte` e o badge da `Sidebar.svelte` só leem esse store,
sem inscrição duplicada no evento.

**Tech Stack:** Go (stdlib `database/sql`, `sort`), SQLite via `modernc.org/sqlite` (já
configurado), Wails v3 (`application.Service`, evento `job:updated` já existente), Svelte 5
(runes), `@wailsio/runtime` (`Events.On`).

## Global Constraints

- Frontend sempre Svelte 5 com runes (`$state`, `$derived`, `$props`) — nunca sintaxe legada
  Svelte 3/4 (`CLAUDE.md`).
- Código e identificadores em inglês; texto voltado ao usuário e mensagens de erro em PT-BR
  (`CLAUDE.md`).
- `internal/` nunca importa Wails — só `services/` pode (princípio da camada fina,
  `CLAUDE.md`).
- SQL na camada de repositório (`internal/db`) portável entre drivers — nada específico de
  `modernc.org/sqlite` (`CLAUDE.md`).
- Mensagens de commit: uma linha só, formato semântico (`tipo: descrição`) (`CLAUDE.md`).
- Sem coluna de progresso percentual — jobs só têm status `pending/running/done/error`, sem
  dado de progresso incremental (ver spec).

Spec completa: `docs/superpowers/specs/2026-07-23-historia-7-fila-visivel-design.md`.

---

### Task 1: `internal/db/queue.go` — query da fila

**Files:**
- Create: `internal/db/queue.go`
- Test: `internal/db/queue_test.go`

**Interfaces:**
- Consumes: nada de tasks anteriores. Reaproveita `mustInsertLessonForJobs` e `mustInsertJob`
  já definidos em `internal/db/jobs_test.go` (mesmo pacote `db`, mesmo padrão usado por
  `internal/db/lesson_status_test.go`).
- Produces: `type QueueEntry struct { LessonID int64; LessonDate string; Tutor string; Kind
  string; Status string; Attempts int; LastError string; UpdatedAt string }` e `func
  ListQueueEntries(conn *sql.DB) ([]QueueEntry, error)` — usados pela Task 2.

- [ ] **Step 1: Escrever os testes (vão falhar — `ListQueueEntries` ainda não existe)**

Criar `internal/db/queue_test.go`:

```go
// internal/db/queue_test.go
package db

import (
	"path/filepath"
	"testing"
)

func TestListQueueEntries_ExtractAudioRunningTakesPriorityOverTranscribePending(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	mustInsertJob(t, conn, lessonID, "extract_audio", "running", 0, "2026-07-23T10:00:00Z", "2026-07-23T10:00:00Z")
	mustInsertJob(t, conn, lessonID, "transcribe", "pending", 0, "2026-07-23T10:00:00Z", "2026-07-23T10:00:00Z")

	entries, err := ListQueueEntries(conn)
	if err != nil {
		t.Fatalf("ListQueueEntries() erro inesperado: %v", err)
	}
	if len(entries) != 1 || entries[0].Kind != "extract_audio" || entries[0].Status != "running" {
		t.Errorf("ListQueueEntries() = %+v, esperado 1 entrada extract_audio/running (transcribe ainda bloqueado, mesmo pending no banco)", entries)
	}
}

func TestListQueueEntries_ExtractAudioErrorIsRootCause(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	mustInsertJob(t, conn, lessonID, "extract_audio", "error", 3, "2026-07-23T10:00:00Z", "2026-07-23T10:00:00Z")
	if _, err := conn.Exec(`UPDATE jobs SET last_error = ? WHERE lesson_id = ? AND kind = ?`, "ffmpeg não encontrado", lessonID, "extract_audio"); err != nil {
		t.Fatalf("preparar last_error de fixture falhou: %v", err)
	}
	mustInsertJob(t, conn, lessonID, "transcribe", "error", 0, "2026-07-23T10:00:00Z", "2026-07-23T10:00:00Z")
	if _, err := conn.Exec(`UPDATE jobs SET last_error = ? WHERE lesson_id = ? AND kind = ?`, "depende de extract_audio que falhou: ffmpeg não encontrado", lessonID, "transcribe"); err != nil {
		t.Fatalf("preparar last_error de fixture falhou: %v", err)
	}

	entries, err := ListQueueEntries(conn)
	if err != nil {
		t.Fatalf("ListQueueEntries() erro inesperado: %v", err)
	}
	if len(entries) != 1 || entries[0].Kind != "extract_audio" || entries[0].Status != "error" || entries[0].LastError != "ffmpeg não encontrado" {
		t.Errorf("ListQueueEntries() = %+v, esperado extract_audio/error com a mensagem de causa raiz", entries)
	}
}

func TestListQueueEntries_TranscribeErrorWhenExtractDone(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	mustInsertJob(t, conn, lessonID, "extract_audio", "done", 0, "2026-07-23T10:00:00Z", "2026-07-23T10:00:00Z")
	mustInsertJob(t, conn, lessonID, "transcribe", "error", 2, "2026-07-23T10:05:00Z", "2026-07-23T10:05:00Z")
	if _, err := conn.Exec(`UPDATE jobs SET last_error = ? WHERE lesson_id = ? AND kind = ?`, "falha real de STT", lessonID, "transcribe"); err != nil {
		t.Fatalf("preparar last_error de fixture falhou: %v", err)
	}

	entries, err := ListQueueEntries(conn)
	if err != nil {
		t.Fatalf("ListQueueEntries() erro inesperado: %v", err)
	}
	if len(entries) != 1 || entries[0].Kind != "transcribe" || entries[0].Status != "error" || entries[0].LastError != "falha real de STT" {
		t.Errorf("ListQueueEntries() = %+v, esperado transcribe/error", entries)
	}
}

func TestListQueueEntries_TranscribePendingWhenExtractDone(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	mustInsertJob(t, conn, lessonID, "extract_audio", "done", 0, "2026-07-23T10:00:00Z", "2026-07-23T10:00:00Z")
	mustInsertJob(t, conn, lessonID, "transcribe", "pending", 0, "2026-07-23T10:05:00Z", "2026-07-23T10:05:00Z")

	entries, err := ListQueueEntries(conn)
	if err != nil {
		t.Fatalf("ListQueueEntries() erro inesperado: %v", err)
	}
	if len(entries) != 1 || entries[0].Kind != "transcribe" || entries[0].Status != "pending" {
		t.Errorf("ListQueueEntries() = %+v, esperado transcribe/pending (extract_audio já done, transcribe genuinamente elegível)", entries)
	}
}

func TestListQueueEntries_ExcludesReadyLessons(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	mustInsertJob(t, conn, lessonID, "extract_audio", "done", 0, "2026-07-23T10:00:00Z", "2026-07-23T10:00:00Z")
	mustInsertJob(t, conn, lessonID, "transcribe", "done", 0, "2026-07-23T10:00:00Z", "2026-07-23T10:00:00Z")

	entries, err := ListQueueEntries(conn)
	if err != nil {
		t.Fatalf("ListQueueEntries() erro inesperado: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("ListQueueEntries() = %+v, esperado vazio (aula pronta não entra na fila)", entries)
	}
}

func TestListQueueEntries_OrdersErrorFirstThenByUpdatedAt(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	older := mustInsertLessonForJobs(t, conn, "older.mp4")
	mustInsertJob(t, conn, older, "extract_audio", "running", 0, "2026-07-23T09:00:00Z", "2026-07-23T09:00:00Z")
	mustInsertJob(t, conn, older, "transcribe", "pending", 0, "2026-07-23T09:00:00Z", "2026-07-23T09:00:00Z")

	newer := mustInsertLessonForJobs(t, conn, "newer.mp4")
	mustInsertJob(t, conn, newer, "extract_audio", "pending", 0, "2026-07-23T11:00:00Z", "2026-07-23T11:00:00Z")
	mustInsertJob(t, conn, newer, "transcribe", "pending", 0, "2026-07-23T11:00:00Z", "2026-07-23T11:00:00Z")

	withError := mustInsertLessonForJobs(t, conn, "error.mp4")
	mustInsertJob(t, conn, withError, "extract_audio", "error", 3, "2026-07-23T10:00:00Z", "2026-07-23T10:00:00Z")
	mustInsertJob(t, conn, withError, "transcribe", "error", 0, "2026-07-23T10:00:00Z", "2026-07-23T10:00:00Z")

	entries, err := ListQueueEntries(conn)
	if err != nil {
		t.Fatalf("ListQueueEntries() erro inesperado: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("ListQueueEntries() = %+v, esperado 3 entradas", entries)
	}
	if entries[0].LessonID != withError {
		t.Errorf("ListQueueEntries()[0].LessonID = %d, esperado a aula com erro primeiro", entries[0].LessonID)
	}
	if entries[1].LessonID != older || entries[2].LessonID != newer {
		t.Errorf("ListQueueEntries()[1:] = %+v, esperado older antes de newer (FIFO por updated_at)", entries[1:])
	}
}
```

- [ ] **Step 2: Rodar os testes e confirmar que falham por `ListQueueEntries`/`QueueEntry` não existirem**

Run: `go test ./internal/db/... -run TestListQueueEntries -v`
Expected: FAIL — `undefined: ListQueueEntries` (erro de compilação do pacote)

- [ ] **Step 3: Implementar `internal/db/queue.go`**

```go
// internal/db/queue.go
package db

import (
	"database/sql"
	"fmt"
	"sort"
)

// QueueEntry é uma aula com pipeline ativo (pending/running) ou em erro,
// no formato que a Fila (História 7) precisa: qual job está "atual" agora,
// não só o status colapsado que LessonWithStatus usa pra Biblioteca.
type QueueEntry struct {
	LessonID   int64
	LessonDate string
	Tutor      string
	Kind       string // "extract_audio" ou "transcribe"
	Status     string // "pending", "running" ou "error"
	Attempts   int
	LastError  string
	UpdatedAt  string
}

// ListQueueEntries lista as aulas com pipeline ativo ou em erro, uma linha
// por aula (nunca duas), com o job "atual" de cada uma. Aulas prontas
// (transcribe done) não entram na lista — isso já é visível na Biblioteca.
//
// Prioridade pra decidir o job atual (extract_audio checado antes de
// transcribe): o job transcribe fica com status "pending" no banco durante
// todo o tempo em que está bloqueado esperando extract_audio terminar — o
// Worker só pula ele em memória (claimNextEligibleJob em
// internal/jobs/worker.go), sem mudar esse status. Checar transcribe antes
// de extract_audio mostraria "Transcrição — aguardando" pra uma aula que na
// verdade ainda está extraindo áudio.
//
// Ordenação: erro primeiro (precisa de ação do usuário), depois por
// UpdatedAt do job atual, mais antigo primeiro (mesma ordem FIFO que o
// Worker usa em ListPendingJobs).
func ListQueueEntries(conn *sql.DB) ([]QueueEntry, error) {
	rows, err := conn.Query(`
		SELECT
			l.id, l.lesson_date, l.tutor,
			COALESCE(ea.status, ''), COALESCE(ea.attempts, 0), COALESCE(ea.last_error, ''), COALESCE(ea.updated_at, ''),
			COALESCE(tr.status, ''), COALESCE(tr.attempts, 0), COALESCE(tr.last_error, ''), COALESCE(tr.updated_at, '')
		FROM lessons l
		LEFT JOIN jobs ea ON ea.lesson_id = l.id AND ea.kind = 'extract_audio'
		LEFT JOIN jobs tr ON tr.lesson_id = l.id AND tr.kind = 'transcribe'
	`)
	if err != nil {
		return nil, fmt.Errorf("listar aulas com jobs pra fila: %w", err)
	}
	defer rows.Close()

	out := make([]QueueEntry, 0)
	for rows.Next() {
		var lessonID int64
		var lessonDate, tutor string
		var extractStatus, extractError, extractUpdatedAt string
		var extractAttempts int
		var transcribeStatus, transcribeError, transcribeUpdatedAt string
		var transcribeAttempts int
		if err := rows.Scan(
			&lessonID, &lessonDate, &tutor,
			&extractStatus, &extractAttempts, &extractError, &extractUpdatedAt,
			&transcribeStatus, &transcribeAttempts, &transcribeError, &transcribeUpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("ler linha da fila: %w", err)
		}

		entry := QueueEntry{LessonID: lessonID, LessonDate: lessonDate, Tutor: tutor}
		switch {
		case extractStatus == "error":
			entry.Kind, entry.Status = "extract_audio", "error"
			entry.Attempts, entry.LastError, entry.UpdatedAt = extractAttempts, extractError, extractUpdatedAt
		case extractStatus == "pending" || extractStatus == "running":
			entry.Kind, entry.Status = "extract_audio", extractStatus
			entry.Attempts, entry.LastError, entry.UpdatedAt = extractAttempts, extractError, extractUpdatedAt
		case transcribeStatus == "error":
			entry.Kind, entry.Status = "transcribe", "error"
			entry.Attempts, entry.LastError, entry.UpdatedAt = transcribeAttempts, transcribeError, transcribeUpdatedAt
		case transcribeStatus == "pending" || transcribeStatus == "running":
			entry.Kind, entry.Status = "transcribe", transcribeStatus
			entry.Attempts, entry.LastError, entry.UpdatedAt = transcribeAttempts, transcribeError, transcribeUpdatedAt
		default:
			// transcribe done (ou nenhum job — não deve acontecer, os dois
			// jobs são sempre criados juntos na confirmação de import):
			// aula pronta, não entra na fila.
			continue
		}
		out = append(out, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterar fila: %w", err)
	}

	sortQueueEntries(out)
	return out, nil
}

// sortQueueEntries ordena in-place: status "error" primeiro, depois por
// UpdatedAt ascendente (FIFO) — ver regra de ordenação no comentário de
// ListQueueEntries.
func sortQueueEntries(entries []QueueEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		iErr, jErr := entries[i].Status == "error", entries[j].Status == "error"
		if iErr != jErr {
			return iErr
		}
		return entries[i].UpdatedAt < entries[j].UpdatedAt
	})
}
```

- [ ] **Step 4: Rodar os testes e confirmar que passam**

Run: `go test ./internal/db/... -run TestListQueueEntries -v`
Expected: PASS em todos os 6 testes

- [ ] **Step 5: `go vet` e commit**

```bash
go vet ./internal/db/...
git add internal/db/queue.go internal/db/queue_test.go
git commit -m "feat: adiciona ListQueueEntries pra fila de jobs ativos/em erro"
```

---

### Task 2: `services/queue.go` — QueueService

**Files:**
- Create: `services/queue.go`
- Test: `services/queue_test.go`

**Interfaces:**
- Consumes: `db.QueueEntry`, `db.ListQueueEntries(conn *sql.DB) ([]db.QueueEntry, error)`
  (Task 1); `db.ResetErrorJobsForLesson(conn *sql.DB, lessonID int64) (int64, error)` (já existe,
  usado por `LibraryService.RetryLesson`); helpers de teste `mustInsertLesson` e
  `mustInsertJobWithStatus` já definidos em `services/library_test.go` (mesmo pacote
  `services`).
- Produces: `type QueueItem struct { LessonID int64 \`json:"lessonId"\`; LessonDate string
  \`json:"lessonDate"\`; Tutor string \`json:"tutor"\`; Stage string \`json:"stage"\`; Status
  string \`json:"status"\`; Attempts int \`json:"attempts"\`; LastError string
  \`json:"lastError"\` }`, `func NewQueueService(conn *sql.DB) *QueueService`, `func
  (s *QueueService) ListQueue() ([]QueueItem, error)`, `func (s *QueueService)
  RetryLesson(lessonID int64) error` — usados pela Task 3 (registro em `main.go`) e pelo
  frontend via bindings geradas.

- [ ] **Step 1: Escrever os testes (vão falhar — `QueueService` ainda não existe)**

Criar `services/queue_test.go`:

```go
// services/queue_test.go
package services

import (
	"path/filepath"
	"testing"

	"assistente-idiomas/internal/db"
)

func TestQueueService_ListQueue_TranslatesStageAndStatus(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLesson(t, conn, "2026-07-23", "Sarah M.", "aula.mp4")
	mustInsertJobWithStatus(t, conn, lessonID, "extract_audio", "done", "")
	mustInsertJobWithStatus(t, conn, lessonID, "transcribe", "running", "")

	svc := NewQueueService(conn)
	items, err := svc.ListQueue()
	if err != nil {
		t.Fatalf("ListQueue() erro inesperado: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("ListQueue() = %+v, esperado 1 item", items)
	}
	if items[0].Stage != "Transcrição" || items[0].Status != "processando" {
		t.Errorf("ListQueue()[0] = %+v, esperado Stage=Transcrição Status=processando", items[0])
	}
	if items[0].LessonDate != "2026-07-23" || items[0].Tutor != "Sarah M." {
		t.Errorf("ListQueue()[0] = %+v, esperado data/tutor da fixture", items[0])
	}
}

func TestQueueService_ListQueue_ErrorStatusAndMessage(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLesson(t, conn, "2026-07-23", "Sarah M.", "aula.mp4")
	mustInsertJobWithStatus(t, conn, lessonID, "extract_audio", "error", "ffmpeg não encontrado")
	mustInsertJobWithStatus(t, conn, lessonID, "transcribe", "error", "depende de extract_audio que falhou")

	svc := NewQueueService(conn)
	items, err := svc.ListQueue()
	if err != nil {
		t.Fatalf("ListQueue() erro inesperado: %v", err)
	}
	if len(items) != 1 || items[0].Stage != "Extração de áudio" || items[0].Status != "erro" || items[0].LastError != "ffmpeg não encontrado" {
		t.Errorf("ListQueue()[0] = %+v, esperado Stage=Extração de áudio Status=erro com a causa raiz", items[0])
	}
}

func TestQueueService_ListQueue_ExcludesReadyLessons(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLesson(t, conn, "2026-07-23", "Sarah M.", "aula.mp4")
	mustInsertJobWithStatus(t, conn, lessonID, "extract_audio", "done", "")
	mustInsertJobWithStatus(t, conn, lessonID, "transcribe", "done", "")

	svc := NewQueueService(conn)
	items, err := svc.ListQueue()
	if err != nil {
		t.Fatalf("ListQueue() erro inesperado: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("ListQueue() = %+v, esperado vazio (aula pronta)", items)
	}
}

func TestQueueService_RetryLesson_ResetsErrorJobsToPending(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLesson(t, conn, "2026-07-23", "Sarah M.", "aula.mp4")
	mustInsertJobWithStatus(t, conn, lessonID, "extract_audio", "error", "ffmpeg não encontrado")
	mustInsertJobWithStatus(t, conn, lessonID, "transcribe", "error", "depende de extract_audio que falhou")

	svc := NewQueueService(conn)
	if err := svc.RetryLesson(lessonID); err != nil {
		t.Fatalf("RetryLesson() erro inesperado: %v", err)
	}

	items, err := svc.ListQueue()
	if err != nil {
		t.Fatalf("ListQueue() erro inesperado: %v", err)
	}
	if len(items) != 1 || items[0].Status != "aguardando" {
		t.Errorf("ListQueue() após RetryLesson = %+v, esperado status=aguardando (jobs voltaram a pending)", items)
	}
}
```

- [ ] **Step 2: Rodar os testes e confirmar que falham por `QueueService` não existir**

Run: `go test ./services/... -run TestQueueService -v`
Expected: FAIL — `undefined: NewQueueService` (erro de compilação do pacote)

- [ ] **Step 3: Implementar `services/queue.go`**

```go
// services/queue.go
package services

import (
	"database/sql"

	"assistente-idiomas/internal/db"
)

// QueueService expõe a fila de processamento (aulas com pipeline ativo ou
// em erro) pra tela de Fila (História 7).
type QueueService struct {
	conn *sql.DB
}

func NewQueueService(conn *sql.DB) *QueueService {
	return &QueueService{conn: conn}
}

// stageLabel traduz o kind do job pra um rótulo de etapa em PT-BR, exibido
// na Fila.
var stageLabel = map[string]string{
	"extract_audio": "Extração de áudio",
	"transcribe":    "Transcrição",
}

// statusLabel traduz o status bruto do job pro vocabulário já usado na
// Biblioteca (Library.svelte: STATUS_LABEL) — "pending"/"running" viram
// "aguardando"/"processando", "error" vira "erro".
var statusLabel = map[string]string{
	"pending": "aguardando",
	"running": "processando",
	"error":   "erro",
}

// QueueItem é uma entrada da fila, no formato exposto ao frontend.
type QueueItem struct {
	LessonID   int64  `json:"lessonId"`
	LessonDate string `json:"lessonDate"`
	Tutor      string `json:"tutor"`
	Stage      string `json:"stage"`
	Status     string `json:"status"`
	Attempts   int    `json:"attempts"`
	LastError  string `json:"lastError"`
}

// ListQueue lista as aulas com pipeline ativo ou em erro, uma por linha,
// erro primeiro depois FIFO — ver db.ListQueueEntries.
func (s *QueueService) ListQueue() ([]QueueItem, error) {
	entries, err := db.ListQueueEntries(s.conn)
	if err != nil {
		return nil, err
	}
	out := make([]QueueItem, 0, len(entries))
	for _, e := range entries {
		out = append(out, QueueItem{
			LessonID:   e.LessonID,
			LessonDate: e.LessonDate,
			Tutor:      e.Tutor,
			Stage:      stageLabel[e.Kind],
			Status:     statusLabel[e.Status],
			Attempts:   e.Attempts,
			LastError:  e.LastError,
		})
	}
	return out, nil
}

// RetryLesson reseta os jobs com erro da lesson pra "pending" — mesma
// primitiva de dados que LibraryService.RetryLesson usa (db package,
// nenhum serviço depende do outro). Não é erro se a lesson não tiver
// nenhum job em erro no momento.
func (s *QueueService) RetryLesson(lessonID int64) error {
	_, err := db.ResetErrorJobsForLesson(s.conn, lessonID)
	return err
}
```

- [ ] **Step 4: Rodar os testes e confirmar que passam**

Run: `go test ./services/... -run TestQueueService -v`
Expected: PASS em todos os 4 testes

- [ ] **Step 5: `go vet` e commit**

```bash
go vet ./services/...
git add services/queue.go services/queue_test.go
git commit -m "feat: adiciona QueueService pra listar e reprocessar a fila"
```

---

### Task 3: Registrar `QueueService` em `main.go` e regenerar bindings

**Files:**
- Modify: `main.go:46-50`

**Interfaces:**
- Consumes: `services.NewQueueService(conn *sql.DB) *services.QueueService` (Task 2).
- Produces: `application.Service` registrado, bindings TS geradas em
  `frontend/bindings/assistente-idiomas/services/queueservice.ts` (função `ListQueue`,
  `RetryLesson`) e `QueueItem` adicionado a
  `frontend/bindings/assistente-idiomas/services/models.ts` — consumidos pela Task 4.

- [ ] **Step 1: Registrar o serviço em `main.go`**

Editar o bloco `Services` em `main.go` (linhas 46-50):

De:
```go
		Services: []application.Service{
			application.NewService(services.NewSetupService()),
			application.NewService(services.NewImportService(conn)),
			application.NewService(services.NewLibraryService(conn)),
		},
```

Para:
```go
		Services: []application.Service{
			application.NewService(services.NewSetupService()),
			application.NewService(services.NewImportService(conn)),
			application.NewService(services.NewLibraryService(conn)),
			application.NewService(services.NewQueueService(conn)),
		},
```

- [ ] **Step 2: Confirmar que o app compila**

`go build ./...` inclui `build/ios`, um stub de scaffold do Wails que **já falha na `main`, sem
nenhuma mudança desta história** (`function main is undeclared in the main package` —
pré-existente, fora de escopo). Por isso o build de verificação é escopado nos pacotes que
importam: raiz (`main.go`), `internal/...`, `services/...`.

Run: `go build ./internal/... ./services/... .`
Expected: sem output (build limpo)

- [ ] **Step 3: Regenerar as bindings TypeScript**

Run (na raiz do projeto): `wails3 generate bindings -ts -i ./...`
Expected: comando termina sem erro; `frontend/bindings/assistente-idiomas/services/queueservice.ts`
é criado ou atualizado.

- [ ] **Step 4: Verificar o conteúdo gerado**

Run: `grep -E "export function (ListQueue|RetryLesson)" frontend/bindings/assistente-idiomas/services/queueservice.ts`
Expected: duas linhas, uma pra cada função

Run: `grep -A8 "interface QueueItem" frontend/bindings/assistente-idiomas/services/models.ts`
Expected: interface com os campos `lessonId`, `lessonDate`, `tutor`, `stage`, `status`,
`attempts`, `lastError`

- [ ] **Step 5: Rodar toda a suíte Go e `go vet`**

Run: `go test ./... && go vet ./...`
Expected: `ok` em todos os pacotes, `go vet` sem output

- [ ] **Step 6: Commit**

`frontend/bindings` está no `.gitignore` (gerado localmente por `wails3 generate bindings`, não
versionado — mesmo tratamento de `frontend/dist`/`frontend/node_modules`), então só `main.go`
entra no commit:

```bash
git add main.go
git commit -m "feat: registra QueueService no app"
```

---

### Task 4: `frontend/src/lib/jobsStore.svelte.ts` — store reativo compartilhado

**Files:**
- Create: `frontend/src/lib/jobsStore.svelte.ts`

**Interfaces:**
- Consumes: `QueueService.ListQueue(): $CancellablePromise<QueueItem[] | null>` e `QueueItem`
  (Task 3, bindings geradas); `Events.On(eventName: string, callback: (ev) => void): () => void`
  de `@wailsio/runtime` (já usado como transporte desde a História 4, evento
  `"job:updated"` — ver `services/jobs_notifier.go:13`).
- Produces: `export const jobsStore: { items: QueueItem[]; activeCount: number }` (getters
  reativos) e `export function initJobsStore(): void` — consumidos pelas Tasks 5 e 6.

- [ ] **Step 1: Criar o arquivo**

```ts
// frontend/src/lib/jobsStore.svelte.ts
import { Events } from "@wailsio/runtime";
import * as QueueService from "../../bindings/assistente-idiomas/services/queueservice";
import type { QueueItem } from "../../bindings/assistente-idiomas/services/models";

let items: QueueItem[] = $state([]);
let initialized = false;

// jobsStore é o único ponto de leitura do estado da fila no frontend —
// Queue.svelte e o badge da Sidebar.svelte leem daqui, sem cada um se
// inscrever separadamente em "job:updated" (ver
// docs/superpowers/specs/2026-07-23-historia-7-fila-visivel-design.md).
// Getters (não uma exportação direta de `items`) porque `export let` não
// propaga reatividade entre módulos no Svelte 5 — funções/objetos com
// getter são o padrão recomendado pra estado compartilhado em .svelte.ts.
export const jobsStore = {
  get items() {
    return items;
  },
  get activeCount() {
    return items.filter((i) => i.status !== "erro").length;
  },
};

async function refetch() {
  items = (await QueueService.ListQueue()) ?? [];
}

// initJobsStore busca a fila uma vez e assina "job:updated" pra refazer a
// busca a cada transição de status de job. Chamado uma única vez em
// App.svelte — chamadas repetidas são no-op (evita inscrições duplicadas
// no evento).
export function initJobsStore() {
  if (initialized) return;
  initialized = true;
  refetch();
  Events.On("job:updated", refetch);
}
```

- [ ] **Step 2: Checar tipos**

Run (dentro de `frontend/`): `corepack pnpm run check`
Expected: `0 ERRORS 0 WARNINGS` (o arquivo ainda não é importado por nenhum componente, então
`svelte-check` só valida sintaxe/tipos do próprio módulo)

- [ ] **Step 3: Commit**

```bash
git add frontend/src/lib/jobsStore.svelte.ts
git commit -m "feat: adiciona store reativo da fila de jobs"
```

---

### Task 5: `frontend/src/lib/screens/Queue.svelte` — lista da Fila

**Files:**
- Modify: `frontend/src/lib/screens/Queue.svelte` (reescrita completa — hoje é um placeholder
  estático de 18 linhas)

**Interfaces:**
- Consumes: `jobsStore.items: QueueItem[]` (Task 4); `QueueService.RetryLesson(lessonId:
  number): $CancellablePromise<void>` (Task 3, bindings).
- Produces: nada consumido por outra task.

- [ ] **Step 1: Reescrever o arquivo**

```svelte
<script lang="ts">
  import { colors, fonts } from "../theme";
  import * as QueueService from "../../../bindings/assistente-idiomas/services/queueservice";
  import { jobsStore } from "../jobsStore.svelte";

  let retryingId: number | null = $state(null);
  let error: string = $state("");

  // Mesmo formato de Library.svelte (lessonDate é "AAAA-MM-DD" ou
  // "AAAA-MM-DDTHH:MM", sem fuso — não é um timestamp com "Z").
  function formatLessonDateTime(value: string): string {
    const [datePart, timePart] = value.split("T");
    const [year, month, day] = datePart.split("-");
    const formattedDate = `${day}/${month}/${year}`;
    return timePart ? `${formattedDate} ${timePart}` : formattedDate;
  }

  async function retry(lessonId: number) {
    error = "";
    retryingId = lessonId;
    try {
      await QueueService.RetryLesson(lessonId);
    } catch (e) {
      error = String(e);
    } finally {
      retryingId = null;
    }
  }
</script>

<div class="screen" style="font-family: {fonts.body}; color: {colors.text};">
  <h1 style="font-family: {fonts.display};">Fila</h1>

  {#if error}
    <p class="error" style="color: {colors.red};">{error}</p>
  {/if}

  {#if jobsStore.items.length === 0}
    <p style="color: {colors.mut};">Nada na fila no momento.</p>
  {:else}
    <ul>
      {#each jobsStore.items as item (item.lessonId)}
        <li style="background: {colors.surface}; border: 1px solid {colors.line};">
          <div class="main">
            <span class="date" style="color: {colors.text};">{formatLessonDateTime(item.lessonDate)}</span>
            <span class="tutor" style="color: {colors.mut};">{item.tutor} · {item.stage}</span>
          </div>
          {#if item.status === "erro"}
            <div class="status-block">
              <span class="badge" style="color: {colors.red}; background: rgba(224,108,108,.1);">erro</span>
              <span class="error-message" style="color: {colors.mut};">{item.lastError}</span>
              <button onclick={() => retry(item.lessonId)} disabled={retryingId === item.lessonId}>
                {retryingId === item.lessonId ? "Reprocessando…" : "Reprocessar"}
              </button>
            </div>
          {:else}
            <span class="badge" style="color: {colors.blue}; background: rgba(110,168,254,.1);">
              {item.status}
            </span>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}
</div>

<style>
  .screen {
    padding: 2rem;
    max-width: 64rem;
    margin: 0 auto;
    width: 100%;
  }
  h1 {
    font-size: 1.4rem;
    margin: 0 0 1rem;
  }
  .error {
    font-size: 0.85rem;
    margin: 0 0 1rem;
  }
  ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }
  li {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
    border-radius: 0.75rem;
    padding: 0.75rem 1.25rem;
  }
  .main {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
    min-width: 0;
  }
  .date {
    font-size: 0.85rem;
  }
  .tutor {
    font-size: 0.8rem;
  }
  .status-block {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    flex-shrink: 0;
  }
  .error-message {
    font-size: 0.75rem;
    max-width: 16rem;
  }
  .badge {
    font-size: 0.75rem;
    padding: 0.25rem 0.6rem;
    border-radius: 999px;
    flex-shrink: 0;
  }
  button {
    padding: 0.5rem 1rem;
    border-radius: 0.5rem;
    cursor: pointer;
  }
</style>
```

- [ ] **Step 2: Checar tipos**

Run (dentro de `frontend/`): `corepack pnpm run check`
Expected: `0 ERRORS 0 WARNINGS`

- [ ] **Step 3: Commit**

```bash
git add frontend/src/lib/screens/Queue.svelte
git commit -m "feat: lista a fila de jobs ativos/em erro com reprocessar"
```

---

### Task 6: Badge na `Sidebar.svelte` e inicialização do store em `App.svelte`

**Files:**
- Modify: `frontend/src/lib/Sidebar.svelte`
- Modify: `frontend/src/App.svelte:1-38`

**Interfaces:**
- Consumes: `jobsStore.activeCount: number`, `initJobsStore(): void` (Task 4).
- Produces: nada consumido por outra task.

- [ ] **Step 1: Adicionar o badge em `Sidebar.svelte`**

De:
```svelte
<script lang="ts">
  import { colors, fonts } from "./theme";

  type Screen = "library" | "progress" | "queue";

  let { active, onNavigate }: { active: Screen; onNavigate: (screen: Screen) => void } = $props();

  const NAV: { key: Screen; label: string; icon: string }[] = [
    { key: "library", label: "Biblioteca", icon: "▤" },
    { key: "progress", label: "Progresso", icon: "◔" },
    { key: "queue", label: "Fila", icon: "≡" },
  ];
</script>
```

Para:
```svelte
<script lang="ts">
  import { colors, fonts } from "./theme";
  import { jobsStore } from "./jobsStore.svelte";

  type Screen = "library" | "progress" | "queue";

  let { active, onNavigate }: { active: Screen; onNavigate: (screen: Screen) => void } = $props();

  const NAV: { key: Screen; label: string; icon: string }[] = [
    { key: "library", label: "Biblioteca", icon: "▤" },
    { key: "progress", label: "Progresso", icon: "◔" },
    { key: "queue", label: "Fila", icon: "≡" },
  ];
</script>
```

De:
```svelte
      <span class="icon">{item.icon}</span>
      {item.label}
    </button>
```

Para:
```svelte
      <span class="icon">{item.icon}</span>
      {item.label}
      {#if item.key === "queue" && jobsStore.activeCount > 0}
        <span class="badge" style="background: {colors.blue}; color: {colors.bg};">
          {jobsStore.activeCount}
        </span>
      {/if}
    </button>
```

Adicionar ao `<style>` (depois da regra `.icon`):
```css
  .badge {
    margin-left: auto;
    font-size: 0.7rem;
    font-weight: 600;
    padding: 0.1rem 0.45rem;
    border-radius: 999px;
    flex-shrink: 0;
  }
```

- [ ] **Step 2: Inicializar o store em `App.svelte`**

De:
```svelte
  import Sidebar from "./lib/Sidebar.svelte";
  import Header from "./lib/Header.svelte";
  import Library from "./lib/screens/Library.svelte";
  import LessonDetail from "./lib/screens/LessonDetail.svelte";
  import Progress from "./lib/screens/Progress.svelte";
  import Queue from "./lib/screens/Queue.svelte";
  import SetupWizard from "./lib/SetupWizard.svelte";
  import { colors, fonts } from "./lib/theme";
  import * as SetupService from "../bindings/assistente-idiomas/services/setupservice";
```

Para:
```svelte
  import Sidebar from "./lib/Sidebar.svelte";
  import Header from "./lib/Header.svelte";
  import Library from "./lib/screens/Library.svelte";
  import LessonDetail from "./lib/screens/LessonDetail.svelte";
  import Progress from "./lib/screens/Progress.svelte";
  import Queue from "./lib/screens/Queue.svelte";
  import SetupWizard from "./lib/SetupWizard.svelte";
  import { colors, fonts } from "./lib/theme";
  import { initJobsStore } from "./lib/jobsStore.svelte";
  import * as SetupService from "../bindings/assistente-idiomas/services/setupservice";
```

De:
```svelte
  onMount(async () => {
    try {
      firstRun = await SetupService.IsFirstRun();
    } finally {
      checkingFirstRun = false;
    }
  });
```

Para:
```svelte
  onMount(async () => {
    initJobsStore();
    try {
      firstRun = await SetupService.IsFirstRun();
    } finally {
      checkingFirstRun = false;
    }
  });
```

- [ ] **Step 3: Checar tipos**

Run (dentro de `frontend/`): `corepack pnpm run check`
Expected: `0 ERRORS 0 WARNINGS`

- [ ] **Step 4: Commit**

```bash
git add frontend/src/lib/Sidebar.svelte frontend/src/App.svelte
git commit -m "feat: badge de jobs ativos na sidebar e inicializa o store da fila"
```

---

### Task 7: Verificação final e fechamento da história

**Files:**
- Modify: `docs/fase-1-mvp.md:171-176` (critérios de aceite da História 7)
- Modify: `docs/fase-1-mvp.md` (tabela de "Registro de progresso", nova linha ao final)

**Interfaces:** nenhuma — task de verificação e documentação, não produz símbolos.

- [ ] **Step 1: Suíte Go completa**

`go build ./...` inclui `build/ios` (stub de scaffold do Wails, quebrado mesmo na `main` sem
relação com esta história — ver Task 3 Step 2), por isso o build fica escopado.

Run: `go build ./internal/... ./services/... . && go vet ./... && go test ./...`
Expected: build limpo, vet sem output, `ok` em todos os pacotes

- [ ] **Step 2: Suíte frontend completa**

Run (dentro de `frontend/`):
```bash
corepack pnpm run check
corepack pnpm run build
```
Expected: `check` com `0 ERRORS 0 WARNINGS`; `build` termina sem erro (gera `frontend/dist`)

- [ ] **Step 3: Build do binário do app**

Run (na raiz do projeto): `wails3 build`
Expected: binário gerado sem erro (mesma verificação que as Histórias 4/5/6 já fizeram nesse
ambiente sem display — abertura de janela real continua pendente em Windows/Linux, mesmo padrão
das histórias anteriores)

- [ ] **Step 4: Marcar os critérios de aceite da História 7 em `docs/fase-1-mvp.md`**

De:
```markdown
### Critérios de aceite
- [ ] Tela de Fila com jobs, estado, progresso e `last_error` legível; ação de reprocessar em erros.
- [ ] Badge na sidebar com contagem de jobs ativos, atualizada por eventos.
```

Para:
```markdown
### Critérios de aceite
- [x] Tela de Fila com jobs, estado, progresso e `last_error` legível; ação de reprocessar em erros.
- [x] Badge na sidebar com contagem de jobs ativos, atualizada por eventos.
```

- [ ] **Step 5: Adicionar linha na tabela de "Registro de progresso"**

Adicionar, depois da linha de 23/07/2026 da História 6 (última linha da tabela):
```markdown
| 23/07/2026 | História 7 implementada: `internal/db.ListQueueEntries` deriva, por aula, qual job (extract_audio ou transcribe) está ativo agora ou em erro — checando extract_audio antes de transcribe, já que o job transcribe fica com status "pending" no banco o tempo todo em que está bloqueado esperando extract_audio (o Worker só pula ele em memória); `QueueService` traduz pro frontend (Stage/Status em PT-BR), reaproveitando `db.ResetErrorJobsForLesson` pro Reprocessar; `jobsStore.svelte.ts` é o primeiro consumidor real do evento `job:updated` (transporte pronto desde a História 4) — busca a fila uma vez e refaz a busca a cada evento, compartilhado entre `Queue.svelte` e o badge numérico da Sidebar (só pending+running, erro fica de fora do número) | Verificação visual (janela real) do badge atualizando ao vivo durante um processamento real e da lista da Fila mudando junto continua pendente em Windows/Linux, mesmo padrão das histórias anteriores |
```

- [ ] **Step 6: Commit final**

```bash
git add docs/fase-1-mvp.md
git commit -m "docs: marca História 7 concluída e registra progresso"
```
