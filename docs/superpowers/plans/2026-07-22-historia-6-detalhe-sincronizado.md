# História 6 — Detalhe da aula: vídeo + transcrição sincronizada — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** O Detalhe da aula mostra o vídeo (já servido com range requests desde a História 5) ao
lado da transcrição rolável, com clique-na-fala pulando o vídeo, highlight da fala corrente
acompanhando o playback, e um toggle simples pra marcar qual speaker é o aluno — persistido por
aula.

**Architecture:** Backend Go adiciona uma coluna (`lessons.student_speaker_label`) e três novas
operações (`FindTranscriptByLessonID`, `FindLessonWithStatusByID`, `SetStudentSpeaker`) expostas
via `LibraryService`. O frontend Svelte 5 reescreve `LessonDetail.svelte`: sincronização por
evento `timeupdate` do `<video>` + estado derivado (sem RAF, sem WebVTT), grade 2 colunas
(protótipo `docs/prototipo-app-aulas.jsx`), painel sem abas (Análise é Fase 2).

**Tech Stack:** Go (`database/sql`, `encoding/json`), SQLite via `modernc.org/sqlite` + `goose`,
Svelte 5 (runes), Wails v3 bindings geradas via `wails3 generate bindings -ts -i ./...`.

## Global Constraints

- Camada fina: nenhum import de Wails em `internal/` (só em `services/`, `main.go`).
- SQL portável na camada de repositório (`internal/db`) — nada específico de driver.
- Código/identificadores em inglês; mensagens de erro e textos de UI em PT-BR.
- Nenhuma sintaxe legada do Svelte (sempre `$state`/`$derived`/`$effect`/`$props`).
- Sem análise LLM na UI, sem highlight/clique por palavra, sem tela de Fila — fora de escopo
  desta história (ver spec).
- Sem atualização "ao vivo" do painel enquanto aberto: depois de clicar "Reprocessar" no
  Detalhe, o painel mostra "processando" uma vez (refetch imediato de `GetLesson`) — acompanhar
  o job até concluir e ver a transcrição aparecer sozinha é fora de escopo (isso é o evento
  `job:updated` da História 7, ainda sem consumidor).
- Spec de referência: `docs/superpowers/specs/2026-07-22-historia-6-detalhe-sincronizado-design.md`.

---

### Task 1: Migration + coluna `student_speaker_label` + `SetStudentSpeaker` (internal/db)

**Files:**
- Create: `internal/db/migrations/00003_student_speaker.sql`
- Modify: `internal/db/lessons.go` (struct `Lesson`, const `lessonColumns`, `scanLessonRow`; novo `SetStudentSpeaker`)
- Test: `internal/db/lessons_test.go`

**Interfaces:**
- Produces: `db.Lesson.StudentSpeakerLabel *string` (novo campo); `db.SetStudentSpeaker(conn *sql.DB, lessonID int64, speakerLabel string) error`.

- [ ] **Step 1: Criar a migration**

`internal/db/migrations/00003_student_speaker.sql`:
```sql
-- +goose Up
ALTER TABLE lessons ADD COLUMN student_speaker_label TEXT;

-- +goose Down
ALTER TABLE lessons DROP COLUMN student_speaker_label;
```

- [ ] **Step 2: Escrever o teste que falha (round-trip de `SetStudentSpeaker` + leitura via `FindLessonByID`)**

Adicionar ao final de `internal/db/lessons_test.go`:
```go
func TestSetStudentSpeaker_RoundTripsThroughFindLessonByID(t *testing.T) {
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

	before, err := FindLessonByID(conn, id)
	if err != nil {
		t.Fatalf("FindLessonByID() erro inesperado: %v", err)
	}
	if before.StudentSpeakerLabel != nil {
		t.Errorf("StudentSpeakerLabel = %v, esperado nil antes de SetStudentSpeaker", *before.StudentSpeakerLabel)
	}

	if err := SetStudentSpeaker(conn, id, "speaker_1"); err != nil {
		t.Fatalf("SetStudentSpeaker() erro inesperado: %v", err)
	}

	after, err := FindLessonByID(conn, id)
	if err != nil {
		t.Fatalf("FindLessonByID() erro inesperado: %v", err)
	}
	if after.StudentSpeakerLabel == nil || *after.StudentSpeakerLabel != "speaker_1" {
		t.Errorf("StudentSpeakerLabel = %v, esperado speaker_1", after.StudentSpeakerLabel)
	}
}
```

- [ ] **Step 3: Rodar o teste e confirmar que falha**

Run: `go test ./internal/db/... -run TestSetStudentSpeaker_RoundTripsThroughFindLessonByID -v`
Expected: FAIL — `undefined: SetStudentSpeaker` (compile error) e/ou `StudentSpeakerLabel` não existe em `Lesson`.

- [ ] **Step 4: Implementar — coluna no struct, `lessonColumns`, `scanLessonRow`, `SetStudentSpeaker`**

Em `internal/db/lessons.go`, substituir o struct `Lesson` (linhas 15-24):
```go
type Lesson struct {
	ID                  int64
	LessonDate          string
	Tutor               string
	VideoPath           string
	VideoHash           string
	FileSize            int64
	FileMTime           string
	DurationSeconds     *int64
	StudentSpeakerLabel *string
}
```

Substituir `lessonColumns` (linha 29):
```go
const lessonColumns = `id, lesson_date, tutor, video_path, COALESCE(video_hash, ''), COALESCE(file_size, 0), COALESCE(file_mtime, ''), duration_seconds, student_speaker_label`
```

Substituir `scanLessonRow` (linhas 34-49):
```go
func scanLessonRow(row *sql.Row) (*Lesson, error) {
	var l Lesson
	var duration sql.NullInt64
	var studentSpeaker sql.NullString
	err := row.Scan(&l.ID, &l.LessonDate, &l.Tutor, &l.VideoPath, &l.VideoHash, &l.FileSize, &l.FileMTime, &duration, &studentSpeaker)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if duration.Valid {
		d := duration.Int64
		l.DurationSeconds = &d
	}
	if studentSpeaker.Valid {
		s := studentSpeaker.String
		l.StudentSpeakerLabel = &s
	}
	return &l, nil
}
```

Adicionar logo depois de `SetLessonDuration` (após a linha 112):
```go

// SetStudentSpeaker grava qual speaker bruto (ex.: "speaker_0") é o aluno
// nesta lesson — escolha feita pelo toggle do Detalhe (História 6).
// Sobrescreve qualquer valor anterior, permitindo o usuário corrigir.
func SetStudentSpeaker(conn *sql.DB, lessonID int64, speakerLabel string) error {
	_, err := conn.Exec(
		`UPDATE lessons SET student_speaker_label = ?, updated_at = ? WHERE id = ?`,
		speakerLabel, time.Now().UTC().Format(time.RFC3339), lessonID,
	)
	if err != nil {
		return fmt.Errorf("gravar student_speaker_label da lesson %d: %w", lessonID, err)
	}
	return nil
}
```

- [ ] **Step 5: Rodar o teste e confirmar que passa**

Run: `go test ./internal/db/... -run TestSetStudentSpeaker_RoundTripsThroughFindLessonByID -v`
Expected: PASS

- [ ] **Step 6: Rodar toda a suíte de `internal/db` (garantir que `lessonColumns`/`scanLessonRow` não quebrou nada existente)**

Run: `go test ./internal/db/... -v`
Expected: PASS em todos os testes (incluindo `TestFindLessonByID_FindsExistingAndNilWhenMissing`, `TestSetLessonDuration_UpdatesDurationSeconds`, etc.)

- [ ] **Step 7: Commit**

```bash
git add internal/db/migrations/00003_student_speaker.sql internal/db/lessons.go internal/db/lessons_test.go
git commit -m "feat: adiciona student_speaker_label em lessons"
```

---

### Task 2: `FindTranscriptByLessonID` (internal/db)

**Files:**
- Modify: `internal/db/transcripts.go`
- Test: `internal/db/transcripts_test.go`

**Interfaces:**
- Consumes: `stt.Utterance{ Speaker, Text string; Start, End time.Duration; Words []stt.Word }` (`internal/stt/stt.go:25-30`).
- Produces: `db.Transcript{ LessonID int64; RawJSONPath string; Utterances []stt.Utterance }`; `db.FindTranscriptByLessonID(conn *sql.DB, lessonID int64) (*db.Transcript, error)` — retorna `(nil, nil)` se não houver transcrição.

- [ ] **Step 1: Escrever o teste que falha**

Adicionar ao final de `internal/db/transcripts_test.go` (adicionar `"encoding/json"` e `"assistente-idiomas/internal/stt"` aos imports):
```go
func TestFindTranscriptByLessonID_NilWhenMissing(t *testing.T) {
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

	tr, err := FindTranscriptByLessonID(conn, lessonID)
	if err != nil {
		t.Fatalf("FindTranscriptByLessonID() erro inesperado: %v", err)
	}
	if tr != nil {
		t.Errorf("FindTranscriptByLessonID() = %+v, esperado nil sem transcrição", tr)
	}
}

func TestFindTranscriptByLessonID_UnmarshalsUtterances(t *testing.T) {
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

	want := []stt.Utterance{
		{Speaker: "speaker_0", Text: "Hello", Start: 0, End: 2 * time.Second},
		{Speaker: "speaker_1", Text: "Hi there", Start: 2 * time.Second, End: 5 * time.Second},
	}
	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("json.Marshal() de fixture falhou: %v", err)
	}
	if err := InsertTranscript(conn, lessonID, "aula.transcript.json", string(raw)); err != nil {
		t.Fatalf("InsertTranscript() erro inesperado: %v", err)
	}

	tr, err := FindTranscriptByLessonID(conn, lessonID)
	if err != nil {
		t.Fatalf("FindTranscriptByLessonID() erro inesperado: %v", err)
	}
	if tr == nil {
		t.Fatal("FindTranscriptByLessonID() = nil, esperado transcript encontrado")
	}
	if tr.RawJSONPath != "aula.transcript.json" {
		t.Errorf("RawJSONPath = %q, esperado aula.transcript.json", tr.RawJSONPath)
	}
	if len(tr.Utterances) != 2 {
		t.Fatalf("Utterances = %+v, esperado 2 falas", tr.Utterances)
	}
	if tr.Utterances[0].Speaker != "speaker_0" || tr.Utterances[0].End != 2*time.Second {
		t.Errorf("Utterances[0] = %+v, não bate com a fixture", tr.Utterances[0])
	}
	if tr.Utterances[1].Text != "Hi there" || tr.Utterances[1].Start != 2*time.Second {
		t.Errorf("Utterances[1] = %+v, não bate com a fixture", tr.Utterances[1])
	}
}
```

- [ ] **Step 2: Rodar os testes e confirmar que falham**

Run: `go test ./internal/db/... -run TestFindTranscriptByLessonID -v`
Expected: FAIL — `undefined: FindTranscriptByLessonID`

- [ ] **Step 3: Implementar `FindTranscriptByLessonID`**

Substituir todo o conteúdo de `internal/db/transcripts.go`:
```go
package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"assistente-idiomas/internal/stt"
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

// Transcript é a transcrição completa de uma lesson, já desserializada.
type Transcript struct {
	LessonID    int64
	RawJSONPath string
	Utterances  []stt.Utterance
}

// FindTranscriptByLessonID busca a transcrição de uma lesson. Retorna
// (nil, nil) se ainda não houver transcrição gravada — estado normal
// enquanto o job transcribe está pendente/rodando ou falhou (ver
// LibraryService.GetLesson/GetTranscript, História 6), não um erro.
func FindTranscriptByLessonID(conn *sql.DB, lessonID int64) (*Transcript, error) {
	var rawJSONPath, utterancesJSON string
	err := conn.QueryRow(
		`SELECT raw_json_path, utterances FROM transcripts WHERE lesson_id = ?`, lessonID,
	).Scan(&rawJSONPath, &utterancesJSON)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("buscar transcript da lesson %d: %w", lessonID, err)
	}
	var utterances []stt.Utterance
	if err := json.Unmarshal([]byte(utterancesJSON), &utterances); err != nil {
		return nil, fmt.Errorf("desserializar utterances da lesson %d: %w", lessonID, err)
	}
	return &Transcript{LessonID: lessonID, RawJSONPath: rawJSONPath, Utterances: utterances}, nil
}
```

- [ ] **Step 4: Rodar os testes e confirmar que passam**

Run: `go test ./internal/db/... -run TestFindTranscriptByLessonID -v`
Expected: PASS (os dois testes)

- [ ] **Step 5: Rodar toda a suíte de `internal/db`**

Run: `go test ./internal/db/... -v`
Expected: PASS em todos os testes

- [ ] **Step 6: Commit**

```bash
git add internal/db/transcripts.go internal/db/transcripts_test.go
git commit -m "feat: adiciona leitura de transcript por lesson id"
```

---

### Task 3: `FindLessonWithStatusByID` + coluna nova em `ListLessonsWithStatus` (internal/db)

Refatora `lesson_status.go` pra compartilhar a lista de colunas/JOIN entre `ListLessonsWithStatus`
(várias linhas) e a nova `FindLessonWithStatusByID` (uma linha), evitando duas queries que podem
divergir. Também é aqui que `student_speaker_label` passa a ser lido junto com o status.

**Files:**
- Modify: `internal/db/lesson_status.go` (reescreve por completo)
- Test: `internal/db/lesson_status_test.go`

**Interfaces:**
- Consumes: `db.Lesson.StudentSpeakerLabel *string` (Task 1); `deriveStatus` (já existente, sem mudanças de assinatura).
- Produces: `db.FindLessonWithStatusByID(conn *sql.DB, id int64) (*db.LessonWithStatus, error)` — retorna `(nil, nil)` se a lesson não existir. `db.LessonWithStatus.StudentSpeakerLabel *string` (promovido de `Lesson`, já populado).

- [ ] **Step 1: Escrever o teste que falha**

Adicionar ao final de `internal/db/lesson_status_test.go`:
```go
func TestFindLessonWithStatusByID_FindsExistingWithStatusAndNilWhenMissing(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	mustInsertJob(t, conn, lessonID, "extract_audio", "done", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")
	mustInsertJob(t, conn, lessonID, "transcribe", "done", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")
	if err := SetStudentSpeaker(conn, lessonID, "speaker_0"); err != nil {
		t.Fatalf("SetStudentSpeaker() erro inesperado: %v", err)
	}

	found, err := FindLessonWithStatusByID(conn, lessonID)
	if err != nil {
		t.Fatalf("FindLessonWithStatusByID() erro inesperado: %v", err)
	}
	if found == nil || found.Status != "pronta" {
		t.Fatalf("FindLessonWithStatusByID() = %+v, esperado status=pronta", found)
	}
	if found.StudentSpeakerLabel == nil || *found.StudentSpeakerLabel != "speaker_0" {
		t.Errorf("StudentSpeakerLabel = %v, esperado speaker_0", found.StudentSpeakerLabel)
	}

	missing, err := FindLessonWithStatusByID(conn, lessonID+999)
	if err != nil {
		t.Fatalf("FindLessonWithStatusByID() erro inesperado: %v", err)
	}
	if missing != nil {
		t.Errorf("FindLessonWithStatusByID() para id inexistente = %+v, esperado nil", missing)
	}
}
```

- [ ] **Step 2: Rodar o teste e confirmar que falha**

Run: `go test ./internal/db/... -run TestFindLessonWithStatusByID -v`
Expected: FAIL — `undefined: FindLessonWithStatusByID`

- [ ] **Step 3: Implementar — reescrever `internal/db/lesson_status.go` por completo**

```go
// internal/db/lesson_status.go
package db

import (
	"database/sql"
	"fmt"
)

// LessonFilter filtra ListLessonsWithStatus — todos os campos são opcionais
// (string vazia = sem filtro), usado pelo filtro por tutor/período da
// Biblioteca (História 5).
type LessonFilter struct {
	Tutor    string
	DateFrom string // AAAA-MM-DD, inclusive
	DateTo   string // AAAA-MM-DD, inclusive
}

// LessonWithStatus é uma lesson com o status derivado dos jobs
// extract_audio/transcribe. Status é sempre um de "processando", "pronta",
// "erro"; ErrorMessage só é preenchido quando Status == "erro" — ver as
// regras de derivação em deriveStatus.
type LessonWithStatus struct {
	Lesson
	Status       string
	ErrorMessage string
}

// lessonWithStatusColumns e lessonWithStatusFromJoin são compartilhados por
// ListLessonsWithStatus (várias linhas) e FindLessonWithStatusByID (uma
// linha, História 6) — mesma lista de colunas/JOIN, pra não divergirem.
const lessonWithStatusColumns = `
		l.id, l.lesson_date, l.tutor, l.video_path,
		COALESCE(l.video_hash, ''), COALESCE(l.file_size, 0), COALESCE(l.file_mtime, ''),
		l.duration_seconds, l.student_speaker_label,
		COALESCE(ea.status, ''), COALESCE(ea.last_error, ''),
		COALESCE(tr.status, ''), COALESCE(tr.last_error, '')`

const lessonWithStatusFromJoin = `
	FROM lessons l
	LEFT JOIN jobs ea ON ea.lesson_id = l.id AND ea.kind = 'extract_audio'
	LEFT JOIN jobs tr ON tr.lesson_id = l.id AND tr.kind = 'transcribe'`

// rowScanner é satisfeito tanto por *sql.Row (uma linha) quanto por *sql.Rows
// (várias linhas) — permite compartilhar o scan entre
// ListLessonsWithStatus e FindLessonWithStatusByID.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanLessonWithStatusRow(s rowScanner) (LessonWithStatus, error) {
	var lws LessonWithStatus
	var duration sql.NullInt64
	var studentSpeaker sql.NullString
	var extractStatus, extractError, transcribeStatus, transcribeError string
	err := s.Scan(
		&lws.ID, &lws.LessonDate, &lws.Tutor, &lws.VideoPath,
		&lws.VideoHash, &lws.FileSize, &lws.FileMTime,
		&duration, &studentSpeaker,
		&extractStatus, &extractError,
		&transcribeStatus, &transcribeError,
	)
	if err != nil {
		return LessonWithStatus{}, err
	}
	if duration.Valid {
		d := duration.Int64
		lws.DurationSeconds = &d
	}
	if studentSpeaker.Valid {
		sp := studentSpeaker.String
		lws.StudentSpeakerLabel = &sp
	}
	lws.Status, lws.ErrorMessage = deriveStatus(extractStatus, extractError, transcribeStatus, transcribeError)
	return lws, nil
}

// ListLessonsWithStatus lista as lessons confirmadas com o status derivado
// dos jobs, mais recentes primeiro, aplicando filter (campos vazios são
// ignorados). O filtro de data compara só a parte AAAA-MM-DD de
// lesson_date (que pode ter horário, formato de <input type="datetime-local">),
// pra incluir aulas com horário registrado no dia inteiro do intervalo.
func ListLessonsWithStatus(conn *sql.DB, filter LessonFilter) ([]LessonWithStatus, error) {
	query := `SELECT` + lessonWithStatusColumns + lessonWithStatusFromJoin + ` WHERE 1=1`
	var args []any
	if filter.Tutor != "" {
		query += ` AND l.tutor = ?`
		args = append(args, filter.Tutor)
	}
	if filter.DateFrom != "" {
		query += ` AND substr(l.lesson_date, 1, 10) >= ?`
		args = append(args, filter.DateFrom)
	}
	if filter.DateTo != "" {
		query += ` AND substr(l.lesson_date, 1, 10) <= ?`
		args = append(args, filter.DateTo)
	}
	query += ` ORDER BY l.lesson_date DESC, l.id DESC`

	rows, err := conn.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("listar lessons com status: %w", err)
	}
	defer rows.Close()

	out := make([]LessonWithStatus, 0)
	for rows.Next() {
		lws, err := scanLessonWithStatusRow(rows)
		if err != nil {
			return nil, fmt.Errorf("ler lesson com status: %w", err)
		}
		out = append(out, lws)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterar lessons com status: %w", err)
	}
	return out, nil
}

// FindLessonWithStatusByID busca uma lesson por id já com o status
// derivado dos jobs (mesmas regras de ListLessonsWithStatus) — usada pelo
// Detalhe (História 6), que agora abre em qualquer status, não só
// "pronta" (ver services.LibraryService.GetLesson). Retorna (nil, nil) se
// a lesson não existir.
func FindLessonWithStatusByID(conn *sql.DB, id int64) (*LessonWithStatus, error) {
	row := conn.QueryRow(`SELECT`+lessonWithStatusColumns+lessonWithStatusFromJoin+` WHERE l.id = ?`, id)
	lws, err := scanLessonWithStatusRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("buscar lesson com status por id %d: %w", id, err)
	}
	return &lws, nil
}

// deriveStatus aplica as regras de status da Biblioteca (História 5): erro
// do extract_audio é a causa raiz e tem prioridade sobre o erro do
// transcribe (que fica bloqueado quando o extract_audio dele falha — ver
// claimNextEligibleJob em internal/jobs/worker.go); "pronta" exige o
// transcribe concluído, não só o extract_audio.
func deriveStatus(extractStatus, extractError, transcribeStatus, transcribeError string) (status string, message string) {
	if extractStatus == "error" {
		return "erro", extractError
	}
	if transcribeStatus == "error" {
		return "erro", transcribeError
	}
	if transcribeStatus == "done" {
		return "pronta", ""
	}
	return "processando", ""
}
```

- [ ] **Step 4: Rodar o teste novo e confirmar que passa**

Run: `go test ./internal/db/... -run TestFindLessonWithStatusByID -v`
Expected: PASS

- [ ] **Step 5: Rodar toda a suíte de `internal/db` (garantir que a refatoração não quebrou `ListLessonsWithStatus`)**

Run: `go test ./internal/db/... -v`
Expected: PASS em todos os testes, incluindo os 5 testes existentes de `lesson_status_test.go`
(`TestListLessonsWithStatus_ProcessandoWhenNoJobsDone`,
`TestListLessonsWithStatus_ProntaWhenTranscribeDone`,
`TestListLessonsWithStatus_ErroComMensagemDaCausaRaiz`,
`TestListLessonsWithStatus_ErroQuandoSoTranscribeFalhou`,
`TestListLessonsWithStatus_FiltraPorTutorEPeriodo`).

- [ ] **Step 6: Commit**

```bash
git add internal/db/lesson_status.go internal/db/lesson_status_test.go
git commit -m "feat: adiciona busca de lesson por id com status derivado"
```

---

### Task 4: `services/library.go` — `GetLesson` com status, `GetTranscript`, `SetStudentSpeaker`

**Files:**
- Modify: `services/library.go`
- Test: `services/library_test.go`

**Interfaces:**
- Consumes: `db.FindLessonWithStatusByID` (Task 3), `db.FindTranscriptByLessonID` (Task 2), `db.SetStudentSpeaker` (Task 1).
- Produces (usados pelo frontend via bindings, Task 5):
  - `services.Lesson` ganha `StudentSpeakerLabel *string \`json:"studentSpeakerLabel"\``.
  - `services.Transcript{ Utterances []Utterance \`json:"utterances"\` }`.
  - `services.Utterance{ Speaker string \`json:"speaker"\`; Text string \`json:"text"\`; StartSeconds float64 \`json:"startSeconds"\`; EndSeconds float64 \`json:"endSeconds"\` }`.
  - `(s *LibraryService) GetLesson(id int64) (Lesson, error)` — agora com `Status`/`ErrorMessage`/`StudentSpeakerLabel` preenchidos.
  - `(s *LibraryService) GetTranscript(lessonID int64) (Transcript, error)` — erro se não houver transcrição.
  - `(s *LibraryService) SetStudentSpeaker(lessonID int64, speakerLabel string) error`.

- [ ] **Step 1: Escrever os testes que falham**

Adicionar ao final de `services/library_test.go`:
```go
func TestLibraryService_GetLesson_IncludesStatusAndStudentSpeaker(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLesson(t, conn, "2026-07-20", "Sarah M.", "aula.mp4")
	mustInsertJobWithStatus(t, conn, lessonID, "extract_audio", "done", "")
	mustInsertJobWithStatus(t, conn, lessonID, "transcribe", "running", "")

	svc := NewLibraryService(conn)
	lesson, err := svc.GetLesson(lessonID)
	if err != nil {
		t.Fatalf("GetLesson() erro inesperado: %v", err)
	}
	if lesson.Status != "processando" {
		t.Errorf("GetLesson().Status = %q, esperado processando (transcribe ainda rodando)", lesson.Status)
	}
	if lesson.StudentSpeakerLabel != nil {
		t.Errorf("GetLesson().StudentSpeakerLabel = %v, esperado nil antes do toggle", lesson.StudentSpeakerLabel)
	}

	if err := svc.SetStudentSpeaker(lessonID, "speaker_0"); err != nil {
		t.Fatalf("SetStudentSpeaker() erro inesperado: %v", err)
	}
	lesson, err = svc.GetLesson(lessonID)
	if err != nil {
		t.Fatalf("GetLesson() erro inesperado: %v", err)
	}
	if lesson.StudentSpeakerLabel == nil || *lesson.StudentSpeakerLabel != "speaker_0" {
		t.Errorf("GetLesson().StudentSpeakerLabel = %v, esperado speaker_0", lesson.StudentSpeakerLabel)
	}
}

func TestLibraryService_GetTranscript_ReturnsUtterancesInSeconds(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLesson(t, conn, "2026-07-20", "Sarah M.", "aula.mp4")
	mustInsertJobWithStatus(t, conn, lessonID, "extract_audio", "done", "")
	mustInsertJobWithStatus(t, conn, lessonID, "transcribe", "done", "")
	utterancesJSON := `[{"Speaker":"speaker_0","Text":"Hello","Start":0,"End":2000000000},{"Speaker":"speaker_1","Text":"Hi","Start":2000000000,"End":3500000000}]`
	if err := db.InsertTranscript(conn, lessonID, "aula.transcript.json", utterancesJSON); err != nil {
		t.Fatalf("InsertTranscript() erro inesperado: %v", err)
	}

	svc := NewLibraryService(conn)
	tr, err := svc.GetTranscript(lessonID)
	if err != nil {
		t.Fatalf("GetTranscript() erro inesperado: %v", err)
	}
	if len(tr.Utterances) != 2 {
		t.Fatalf("GetTranscript().Utterances = %+v, esperado 2 falas", tr.Utterances)
	}
	if tr.Utterances[0].Speaker != "speaker_0" || tr.Utterances[0].StartSeconds != 0 || tr.Utterances[0].EndSeconds != 2 {
		t.Errorf("Utterances[0] = %+v, esperado speaker_0 0s-2s", tr.Utterances[0])
	}
	if tr.Utterances[1].StartSeconds != 2 || tr.Utterances[1].EndSeconds != 3.5 {
		t.Errorf("Utterances[1] = %+v, esperado 2s-3.5s", tr.Utterances[1])
	}
}

func TestLibraryService_GetTranscript_ErrorsWhenNoTranscriptYet(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLesson(t, conn, "2026-07-20", "Sarah M.", "aula.mp4")
	mustInsertJobWithStatus(t, conn, lessonID, "extract_audio", "done", "")
	mustInsertJobWithStatus(t, conn, lessonID, "transcribe", "running", "")

	svc := NewLibraryService(conn)
	if _, err := svc.GetTranscript(lessonID); err == nil {
		t.Error("GetTranscript() sem transcrição esperava erro, veio nil")
	}
}
```

- [ ] **Step 2: Rodar os testes e confirmar que falham**

Run: `go test ./services/... -run "TestLibraryService_GetLesson_IncludesStatusAndStudentSpeaker|TestLibraryService_GetTranscript" -v`
Expected: FAIL — `undefined: svc.SetStudentSpeaker` / `undefined: svc.GetTranscript` (compile error)

- [ ] **Step 3: Implementar — reescrever `services/library.go` por completo**

```go
package services

import (
	"database/sql"
	"fmt"

	"assistente-idiomas/internal/db"
)

// LibraryService expõe as aulas já confirmadas para a Biblioteca —
// listagem com status derivado dos jobs e duração (História 5), filtro por
// tutor/período, reprocessamento de aulas com erro, busca de uma aula pro
// Detalhe e sua transcrição sincronizada (História 6).
type LibraryService struct {
	conn *sql.DB
}

func NewLibraryService(conn *sql.DB) *LibraryService {
	return &LibraryService{conn: conn}
}

// Lesson é uma aula confirmada, no formato exposto ao frontend. Status é
// sempre um de "processando", "pronta", "erro" (ver db.LessonWithStatus);
// ErrorMessage só é preenchido quando Status == "erro". DurationSeconds é
// nil até o probe de duração (melhor esforço, na confirmação da
// importação) ter sucesso. StudentSpeakerLabel é nil até o usuário marcar
// quem é o aluno no toggle do Detalhe (História 6).
type Lesson struct {
	ID                  int64   `json:"id"`
	LessonDate          string  `json:"lessonDate"`
	Tutor               string  `json:"tutor"`
	VideoPath           string  `json:"videoPath"`
	DurationSeconds     *int64  `json:"durationSeconds"`
	Status              string  `json:"status"`
	ErrorMessage        string  `json:"errorMessage"`
	StudentSpeakerLabel *string `json:"studentSpeakerLabel"`
}

// LessonFilter filtra ListLessons — campos vazios são ignorados (sem
// filtro naquele critério).
type LessonFilter struct {
	Tutor    string `json:"tutor"`
	DateFrom string `json:"dateFrom"`
	DateTo   string `json:"dateTo"`
}

// Transcript é a transcrição de uma lesson, no formato exposto ao Detalhe
// (História 6).
type Transcript struct {
	Utterances []Utterance `json:"utterances"`
}

// Utterance é uma fala da transcrição. Timestamps em segundos — mesma
// unidade de HTMLVideoElement.currentTime no frontend, convertida aqui na
// borda do serviço (o banco guarda time.Duration).
type Utterance struct {
	Speaker      string  `json:"speaker"`
	Text         string  `json:"text"`
	StartSeconds float64 `json:"startSeconds"`
	EndSeconds   float64 `json:"endSeconds"`
}

// ListLessons lista as aulas confirmadas com status/duração, mais recentes
// primeiro, aplicando filter.
func (s *LibraryService) ListLessons(filter LessonFilter) ([]Lesson, error) {
	rows, err := db.ListLessonsWithStatus(s.conn, db.LessonFilter{
		Tutor:    filter.Tutor,
		DateFrom: filter.DateFrom,
		DateTo:   filter.DateTo,
	})
	if err != nil {
		return nil, err
	}
	out := make([]Lesson, 0, len(rows))
	for _, r := range rows {
		out = append(out, Lesson{
			ID:                  r.ID,
			LessonDate:          r.LessonDate,
			Tutor:               r.Tutor,
			VideoPath:           r.VideoPath,
			DurationSeconds:     r.DurationSeconds,
			Status:              r.Status,
			ErrorMessage:        r.ErrorMessage,
			StudentSpeakerLabel: r.StudentSpeakerLabel,
		})
	}
	return out, nil
}

// ListTutors lista os tutores distintos já registrados, pro dropdown de
// filtro da Biblioteca.
func (s *LibraryService) ListTutors() ([]string, error) {
	return db.ListTutors(s.conn)
}

// RetryLesson reseta os jobs com erro da lesson pra "pending" — o worker de
// jobs (internal/jobs) retoma o pipeline sozinho no próximo poll (~5s), sem
// precisar acordá-lo explicitamente (mesma decisão da História 4). Não é
// erro se a lesson não tiver nenhum job em erro no momento.
func (s *LibraryService) RetryLesson(lessonID int64) error {
	_, err := db.ResetErrorJobsForLesson(s.conn, lessonID)
	return err
}

// GetLesson busca uma aula por id, com status/erro derivados dos jobs, pro
// Detalhe (História 6) — que agora abre em qualquer status: "processando"
// e "erro" mostram o vídeo sem transcrição (ver LessonDetail.svelte),
// "pronta" habilita GetTranscript.
func (s *LibraryService) GetLesson(id int64) (Lesson, error) {
	lws, err := db.FindLessonWithStatusByID(s.conn, id)
	if err != nil {
		return Lesson{}, err
	}
	if lws == nil {
		return Lesson{}, fmt.Errorf("aula %d não encontrada", id)
	}
	return Lesson{
		ID:                  lws.ID,
		LessonDate:          lws.LessonDate,
		Tutor:               lws.Tutor,
		VideoPath:           lws.VideoPath,
		DurationSeconds:     lws.DurationSeconds,
		Status:              lws.Status,
		ErrorMessage:        lws.ErrorMessage,
		StudentSpeakerLabel: lws.StudentSpeakerLabel,
	}, nil
}

// GetTranscript busca a transcrição de uma lesson pro Detalhe (História 6).
// Só deve ser chamado quando GetLesson já retornou Status == "pronta" — o
// Detalhe não chama isso pra aulas processando/erro, que mostram o status
// no lugar do painel de transcrição.
func (s *LibraryService) GetTranscript(lessonID int64) (Transcript, error) {
	t, err := db.FindTranscriptByLessonID(s.conn, lessonID)
	if err != nil {
		return Transcript{}, err
	}
	if t == nil {
		return Transcript{}, fmt.Errorf("aula %d ainda não tem transcrição", lessonID)
	}
	out := Transcript{Utterances: make([]Utterance, 0, len(t.Utterances))}
	for _, u := range t.Utterances {
		out.Utterances = append(out.Utterances, Utterance{
			Speaker:      u.Speaker,
			Text:         u.Text,
			StartSeconds: u.Start.Seconds(),
			EndSeconds:   u.End.Seconds(),
		})
	}
	return out, nil
}

// SetStudentSpeaker grava qual speaker bruto (ex.: "speaker_0") é o aluno
// nesta lesson — toggle do Detalhe (História 6).
func (s *LibraryService) SetStudentSpeaker(lessonID int64, speakerLabel string) error {
	return db.SetStudentSpeaker(s.conn, lessonID, speakerLabel)
}
```

- [ ] **Step 4: Rodar os testes novos e confirmar que passam**

Run: `go test ./services/... -run "TestLibraryService_GetLesson_IncludesStatusAndStudentSpeaker|TestLibraryService_GetTranscript" -v`
Expected: PASS

- [ ] **Step 5: Rodar toda a suíte de `services` (garantir que `GetLesson`/`ListLessons` existentes não quebraram)**

Run: `go test ./services/... -v`
Expected: PASS em todos os testes, incluindo `TestLibraryService_GetLesson_FindsExistingAndErrorsWhenMissing`,
`TestLibraryService_ListLessons_ReturnsConfirmedLessons`, etc.

- [ ] **Step 6: `go vet` no módulo inteiro**

Run: `go vet ./...`
Expected: sem saída (limpo)

- [ ] **Step 7: Commit**

```bash
git add services/library.go services/library_test.go
git commit -m "feat: expoe status/transcricao/toggle de speaker no LibraryService"
```

---

### Task 5: Regenerar bindings TS do Wails

**Files:**
- Modify (gerado, não editar manualmente): `frontend/bindings/assistente-idiomas/services/models.ts`, `frontend/bindings/assistente-idiomas/services/libraryservice.ts`

**Interfaces:**
- Consumes: `LibraryService.GetLesson/GetTranscript/SetStudentSpeaker` (Task 4).
- Produces: `models.ts` exporta `Lesson.studentSpeakerLabel: string | null`, `Transcript`, `Utterance`; `libraryservice.ts` exporta `GetTranscript(lessonID: number): $CancellablePromise<$models.Transcript>` e `SetStudentSpeaker(lessonID: number, speakerLabel: string): $CancellablePromise<void>`.

- [ ] **Step 1: Rodar o gerador de bindings**

Run: `wails3 generate bindings -ts -i ./...`
Expected: comando termina sem erro (exit code 0); arquivos em `frontend/bindings/assistente-idiomas/services/` são reescritos.

- [ ] **Step 2: Conferir que `models.ts` ganhou os tipos esperados**

Run: `grep -n "studentSpeakerLabel\|interface Transcript\|interface Utterance" frontend/bindings/assistente-idiomas/services/models.ts`
Expected: 3 linhas de saída — `"studentSpeakerLabel"` dentro de `Lesson`, `export interface Transcript`, `export interface Utterance`.

- [ ] **Step 3: Conferir que `libraryservice.ts` ganhou as duas funções novas**

Run: `grep -n "export function GetTranscript\|export function SetStudentSpeaker" frontend/bindings/assistente-idiomas/services/libraryservice.ts`
Expected: 2 linhas de saída.

- [ ] **Step 4: Commit**

```bash
git add frontend/bindings/
git commit -m "chore: regenera bindings do LibraryService (transcricao e toggle de speaker)"
```

---

### Task 6: `Library.svelte` — Detalhe abre em qualquer status

**Files:**
- Modify: `frontend/src/lib/screens/Library.svelte:122-126,189-200`

**Interfaces:**
- Consumes: `Lesson.status` (bindings, Task 5).
- Produces: `openLesson` sempre chama `onOpenLesson(lesson.id)`, independente do status.

- [ ] **Step 1: Editar `openLesson` (linhas 122-126)**

Trocar:
```js
  function openLesson(lesson: Lesson) {
    if (lesson.status === "pronta") {
      onOpenLesson(lesson.id);
    }
  }
```
Por:
```js
  function openLesson(lesson: Lesson) {
    onOpenLesson(lesson.id);
  }
```

- [ ] **Step 2: Editar o botão da linha da aula (linhas 189-200) — sempre clicável**

Trocar:
```svelte
              <button
                class="lesson-main"
                onclick={() => openLesson(lesson)}
                style="cursor: {lesson.status === 'pronta' ? 'pointer' : 'default'}; opacity: {lesson.status === 'pronta' ? 1 : 0.7};"
              >
```
Por:
```svelte
              <button class="lesson-main" onclick={() => openLesson(lesson)}>
```

- [ ] **Step 3: Verificar tipos/compilação do frontend**

Run: `cd frontend && npm run check`
Expected: sem erros novos (mesmo baseline de antes da mudança)

- [ ] **Step 4: Commit**

```bash
git add frontend/src/lib/screens/Library.svelte
git commit -m "feat: Biblioteca abre o Detalhe em qualquer status da aula"
```

---

### Task 7: `LessonDetail.svelte` — grade com vídeo + transcrição sincronizada

Reescreve o stub da História 5 (cabeçalho + vídeo) para o layout completo: grade 2 colunas,
sincronização por `timeupdate`, toggle de speaker, auto-scroll, estados de painel
(processando/erro/pronta).

**Files:**
- Modify: `frontend/src/lib/screens/LessonDetail.svelte` (reescreve por completo)

**Interfaces:**
- Consumes: `LibraryService.GetLesson`, `LibraryService.GetTranscript`, `LibraryService.SetStudentSpeaker`, `LibraryService.RetryLesson` (bindings, Task 5); `colors`/`fonts` de `../theme`.
- Produces: componente `LessonDetail` com props `{ lessonId: number; onBack: () => void }` (inalteradas).

- [ ] **Step 1: Reescrever `frontend/src/lib/screens/LessonDetail.svelte` por completo**

```svelte
<script lang="ts">
  import { onMount } from "svelte";
  import { colors, fonts } from "../theme";
  import * as LibraryService from "../../../bindings/assistente-idiomas/services/libraryservice";
  import type { Lesson, Transcript } from "../../../bindings/assistente-idiomas/services/models";

  let { lessonId, onBack }: { lessonId: number; onBack: () => void } = $props();

  let lesson: Lesson | null = $state(null);
  let loading: boolean = $state(true);
  let lessonError: string = $state("");

  let transcript: Transcript | null = $state(null);
  let loadingTranscript: boolean = $state(false);

  let retrying: boolean = $state(false);

  let videoEl: HTMLVideoElement | undefined = $state();
  let currentTime: number = $state(0);
  let rowRefs: (HTMLElement | null)[] = [];

  function formatLessonDateTime(value: string): string {
    const [datePart, timePart] = value.split("T");
    const [year, month, day] = datePart.split("-");
    const formattedDate = `${day}/${month}/${year}`;
    return timePart ? `${formattedDate} ${timePart}` : formattedDate;
  }

  function formatDuration(seconds: number | null): string {
    if (seconds == null) return "";
    const totalMinutes = Math.round(seconds / 60);
    const hours = Math.floor(totalMinutes / 60);
    const minutes = totalMinutes % 60;
    return hours > 0 ? `${hours}h ${minutes}min` : `${minutes}min`;
  }

  // Ordem de primeira fala — determinística, não depende de ordem de mapa.
  // "Speaker A"/"Speaker B" (ou C, D... em diarização com ruído) são os
  // rótulos neutros exibidos antes do usuário escolher quem é o aluno.
  const speakerOrder = $derived.by(() => {
    const seen: string[] = [];
    for (const u of transcript?.utterances ?? []) {
      if (!seen.includes(u.speaker)) seen.push(u.speaker);
    }
    return seen;
  });

  function neutralLabel(speaker: string): string {
    const idx = speakerOrder.indexOf(speaker);
    return `Speaker ${String.fromCharCode(65 + (idx < 0 ? 0 : idx))}`;
  }

  type Role = "aluno" | "tutor" | "neutro";

  function roleFor(speaker: string): Role {
    if (!lesson?.studentSpeakerLabel) return "neutro";
    return speaker === lesson.studentSpeakerLabel ? "aluno" : "tutor";
  }

  function labelFor(speaker: string, role: Role): string {
    if (role === "aluno") return "Você";
    if (role === "tutor") return "Tutor";
    return neutralLabel(speaker);
  }

  // Última utterance cujo start já passou — busca linear, poucas centenas
  // de falas por aula, custo irrelevante a cada tick de timeupdate.
  const currentIndex = $derived.by(() => {
    const utterances = transcript?.utterances ?? [];
    let idx = -1;
    for (let i = 0; i < utterances.length; i++) {
      if (utterances[i].startSeconds <= currentTime) idx = i;
      else break;
    }
    return idx;
  });

  $effect(() => {
    const el = rowRefs[currentIndex];
    el?.scrollIntoView({ block: "nearest" });
  });

  function onTimeUpdate() {
    if (videoEl) currentTime = videoEl.currentTime;
  }

  // Só ajusta a posição (seek) — não força play nem pause, pra não
  // surpreender quem só quer conferir o timestamp.
  function seekTo(startSeconds: number) {
    if (videoEl) videoEl.currentTime = startSeconds;
  }

  async function fetchTranscriptIfReady() {
    if (!lesson || lesson.status !== "pronta") {
      transcript = null;
      return;
    }
    loadingTranscript = true;
    try {
      transcript = await LibraryService.GetTranscript(lessonId);
    } catch {
      transcript = null;
    } finally {
      loadingTranscript = false;
    }
  }

  async function chooseStudentSpeaker(speaker: string) {
    if (!lesson) return;
    const previous = lesson.studentSpeakerLabel;
    lesson = { ...lesson, studentSpeakerLabel: speaker };
    try {
      await LibraryService.SetStudentSpeaker(lessonId, speaker);
    } catch (e) {
      lesson = { ...lesson, studentSpeakerLabel: previous };
      lessonError = String(e);
    }
  }

  async function retry() {
    retrying = true;
    try {
      await LibraryService.RetryLesson(lessonId);
      lesson = await LibraryService.GetLesson(lessonId);
      await fetchTranscriptIfReady();
    } catch (e) {
      lessonError = String(e);
    } finally {
      retrying = false;
    }
  }

  onMount(async () => {
    try {
      lesson = await LibraryService.GetLesson(lessonId);
      await fetchTranscriptIfReady();
    } catch (e) {
      lessonError = String(e);
    } finally {
      loading = false;
    }
  });
</script>

<div class="screen" style="font-family: {fonts.body}; color: {colors.text};">
  <button class="back" onclick={onBack} style="color: {colors.mut};">← Biblioteca</button>

  {#if loading}
    <p style="color: {colors.mut};">Carregando…</p>
  {:else if lessonError}
    <p class="error" style="color: {colors.red};">{lessonError}</p>
  {:else if lesson}
    <div class="header-row">
      <h1 style="font-family: {fonts.display};">{formatLessonDateTime(lesson.lessonDate)}</h1>
      <span class="meta" style="color: {colors.mut}; font-family: {fonts.mono};"
        >{lesson.tutor}{formatDuration(lesson.durationSeconds) ? ` · ${formatDuration(lesson.durationSeconds)}` : ""}</span
      >
    </div>

    <div class="grid">
      <div>
        <!-- eslint-disable-next-line svelte/no-navigation-without-resolve -->
        <video
          bind:this={videoEl}
          ontimeupdate={onTimeUpdate}
          controls
          src={`/media/lesson/${lesson.id}`}
          style="border: 1px solid {colors.line};"
        >
          <track kind="captions" />
        </video>
        <p class="hint" style="color: {colors.mut};">
          Clique em qualquer fala ao lado para pular o vídeo até aquele momento.
        </p>
      </div>

      <div class="panel" style="background: {colors.surface}; border: 1px solid {colors.line};">
        {#if lesson.status === "processando"}
          <p class="panel-message" style="color: {colors.mut};">Transcrição em processamento…</p>
        {:else if lesson.status === "erro"}
          <p class="panel-message" style="color: {colors.red};">{lesson.errorMessage}</p>
          <button onclick={retry} disabled={retrying}>
            {retrying ? "Reprocessando…" : "Reprocessar"}
          </button>
        {:else if loadingTranscript}
          <p class="panel-message" style="color: {colors.mut};">Carregando transcrição…</p>
        {:else if !transcript || transcript.utterances.length === 0}
          <p class="panel-message" style="color: {colors.mut};">Transcrição em processamento…</p>
        {:else}
          <div class="speaker-toggle">
            {#each speakerOrder as speaker, i (speaker)}
              <button
                class:active={lesson.studentSpeakerLabel === speaker}
                onclick={() => chooseStudentSpeaker(speaker)}
              >
                {`Speaker ${String.fromCharCode(65 + i)} é você`}
              </button>
            {/each}
          </div>

          <div class="transcript">
            {#each transcript.utterances as utterance, i (i)}
              {@const role = roleFor(utterance.speaker)}
              <button
                bind:this={rowRefs[i]}
                class="row"
                onclick={() => seekTo(utterance.startSeconds)}
                style="background: {i === currentIndex ? colors.surface2 : 'transparent'}; border-left: 3px solid {role === 'aluno' ? colors.blue : 'transparent'};"
              >
                <span
                  class="speaker-label"
                  style="color: {role === 'aluno' ? colors.blue : role === 'tutor' ? colors.green : colors.mut}; font-family: {fonts.body};"
                >
                  {labelFor(utterance.speaker, role)}
                </span>
                <p class="text" style="color: {colors.text};">{utterance.text}</p>
              </button>
            {/each}
          </div>
        {/if}
      </div>
    </div>
  {/if}
</div>

<style>
  .screen {
    padding: 2rem;
    max-width: 72rem;
    margin: 0 auto;
    width: 100%;
  }
  .back {
    background: none;
    border: none;
    cursor: pointer;
    font-size: 0.85rem;
    margin-bottom: 1rem;
    padding: 0;
  }
  .header-row {
    display: flex;
    align-items: baseline;
    gap: 0.75rem;
    flex-wrap: wrap;
    margin-bottom: 1.25rem;
  }
  h1 {
    font-size: 1.4rem;
    margin: 0;
  }
  .meta {
    font-size: 0.85rem;
  }
  .grid {
    display: grid;
    grid-template-columns: 1fr;
    gap: 1.25rem;
  }
  @media (min-width: 960px) {
    .grid {
      grid-template-columns: 1fr 1fr;
    }
  }
  video {
    width: 100%;
    border-radius: 0.75rem;
    background: black;
    aspect-ratio: 16 / 9;
  }
  .hint {
    font-size: 0.75rem;
    margin-top: 0.75rem;
    line-height: 1.5;
  }
  .panel {
    border-radius: 0.75rem;
    padding: 1rem;
    max-height: 26rem;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
  }
  .panel-message {
    font-size: 0.9rem;
  }
  .speaker-toggle {
    display: flex;
    gap: 0.5rem;
    flex-wrap: wrap;
  }
  .speaker-toggle button {
    font-size: 0.75rem;
    padding: 0.35rem 0.7rem;
    border-radius: 999px;
    cursor: pointer;
  }
  .speaker-toggle button.active {
    font-weight: 600;
  }
  .transcript {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
  }
  .row {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 0.15rem;
    text-align: left;
    background: none;
    border: none;
    border-radius: 0.5rem;
    padding: 0.5rem 0.75rem;
    cursor: pointer;
    width: 100%;
  }
  .speaker-label {
    font-size: 0.7rem;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    font-weight: 600;
  }
  .text {
    font-size: 0.9rem;
    line-height: 1.5;
    margin: 0;
  }
  .error {
    font-size: 0.85rem;
  }
</style>
```

- [ ] **Step 2: Verificar tipos/compilação do frontend**

Run: `cd frontend && npm run check`
Expected: sem erros de tipo (a interface `Transcript`/`Utterance` já existe nos bindings desde a Task 5)

- [ ] **Step 3: Build de produção do frontend**

Run: `cd frontend && npm run build`
Expected: build termina sem erro

- [ ] **Step 4: Commit**

```bash
git add frontend/src/lib/screens/LessonDetail.svelte
git commit -m "feat: transcricao sincronizada no Detalhe da aula"
```

---

### Task 8: Verificação final, atualização do progresso e commit de fechamento

**Files:**
- Modify: `docs/fase-1-mvp.md` (marca critérios da História 6, adiciona linha na tabela de progresso)

- [ ] **Step 1: Rodar toda a suíte Go**

Run: `go test ./... -v`
Expected: PASS em todos os pacotes

- [ ] **Step 2: `go vet` no módulo inteiro**

Run: `go vet ./...`
Expected: sem saída

- [ ] **Step 3: Build do binário Go (garante que main.go/services compilam juntos)**

Run: `wails3 build`
Expected: termina sem erro, binário gerado

- [ ] **Step 4: Atualizar `docs/fase-1-mvp.md` — marcar os critérios da História 6 (linhas 154-159) como feitos, com a mesma ressalva de verificação visual pendente usada nas histórias anteriores**

Editar os 5 checkboxes de "### Critérios de aceite" da História 6 (linhas 155-159) de `- [ ]` para
`- [x]`, e adicionar ao final de cada um (seguindo o padrão das Histórias 3/4/5) uma nota entre
parênteses quando fizer sentido, por exemplo no critério do seek:
```
- [x] Vídeo local servido ao `<video>` via asset handler com range requests; seek funciona (risco 1 resolvido) — endpoint reaproveitado da História 5 sem mudanças.
```
Aplicar o mesmo padrão (marcar `[x]`, comentário curto) aos outros 4 critérios (transcrição
rolável, clique pula vídeo, highlight acompanha playback, aula sem transcrição reproduz vídeo).

- [ ] **Step 5: Adicionar linha na "Registro de progresso" (final do arquivo, depois da linha da História 5)**

Adicionar uma nova linha à tabela markdown, seguindo o formato das anteriores, resumindo: o que
foi implementado (grade vídeo+transcrição, sync por `timeupdate`, toggle de speaker persistido
em `student_speaker_label`, Biblioteca liberada pra abrir Detalhe em qualquer status, painel
sem abas) e a mesma ressalva recorrente de verificação visual (clique-pula-vídeo,
highlight-acompanha-playback, toggle) ainda pendente em janela real Windows/Linux.

- [ ] **Step 6: Commit de fechamento**

```bash
git add docs/fase-1-mvp.md
git commit -m "docs: marca Historia 6 concluida e registra progresso"
```

---

## Self-Review

**Cobertura do spec:** migração+coluna (Task 1), leitura de transcript (Task 2),
status/toggle no Detalhe (Task 3-4), bindings (Task 5), gating da Biblioteca (Task 6), layout
2 colunas/sync/toggle/estados de painel sem abas (Task 7), atualização de progresso (Task 8).
Todos os critérios de aceite da História 6 (`fase-1-mvp.md:154-159`) e todas as decisões da spec
(`docs/superpowers/specs/2026-07-22-historia-6-detalhe-sincronizado-design.md`) têm uma task
correspondente.

**Placeholders:** nenhum "TBD"/"implementar depois" — todo passo tem código completo ou comando
exato com saída esperada.

**Consistência de tipos:** `Transcript`/`Utterance` (Go, Task 4) → `Transcript`/`Utterance` (TS
gerado, Task 5) → `Transcript` importado em `LessonDetail.svelte` (Task 7), mesmos nomes de campo
(`utterances`, `speaker`, `text`, `startSeconds`, `endSeconds`) em todas as camadas.
`StudentSpeakerLabel`/`studentSpeakerLabel` consistente entre `db.Lesson` (Task 1),
`services.Lesson` (Task 4) e `Lesson.studentSpeakerLabel` no Svelte (Task 7).
