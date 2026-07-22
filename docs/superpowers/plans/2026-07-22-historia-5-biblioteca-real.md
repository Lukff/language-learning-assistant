# História 5 — Biblioteca real — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A Biblioteca lista aulas reais com status derivado do pipeline de jobs (processando/pronta/erro), duração, filtro por tutor/período e reprocessamento de erros; clicar numa aula pronta abre um Detalhe mínimo que já reproduz o vídeo local (resolvendo o risco técnico 1 do projeto).

**Architecture:** Status e duração nunca são colunas gravadas por um processo separado — status é sempre derivado, na leitura, dos dois jobs (`extract_audio`/`transcribe`) de cada lesson; duração é calculada via `ffprobe` em melhor esforço no momento da confirmação da importação, dissociada do pipeline de transcrição (resiliência: nunca bloqueia a confirmação). O vídeo local é servido ao webview por um `application.Middleware` do Wails v3 que intercepta `GET /media/lesson/{id}` e delega pro `http.ServeFile` da stdlib (que já trata `Range` requests), caindo no handler padrão do Wails (embedded assets em produção, proxy pro Vite em `wails3 dev`) pra qualquer outro path.

**Tech Stack:** Go (stdlib `net/http`, `os/exec` pro ffprobe), SQLite via `modernc.org/sqlite`, Svelte 5 (runes) + TypeScript, Wails v3 (`application.Middleware`, bindings geradas via `wails3 generate bindings -ts -i`).

## Global Constraints

- `internal/` nunca importa Wails (camada fina) — `application.Middleware`/`application.Service` só aparecem em `services/` e `main.go`.
- SQL portável na camada de repositório — nada específico de driver.
- Falha de transcrição/análise (e, nesta história, falha de cálculo de duração) nunca impede assistir ao vídeo — princípio de resiliência do `CLAUDE.md`.
- Código e identificadores em inglês; mensagens de erro voltadas ao usuário e UI em PT-BR.
- Svelte 5 com runes sempre (`$state`, `$props`, nunca `export let`/`$:`).
- Mensagens de commit: uma linha só, formato semântico (`feat:`, `fix:`, `test:`, ...).
- Bindings do frontend são regeneradas com **`wails3 generate bindings -ts -i ./...`** (as flags `-ts -i` são obrigatórias neste projeto — sem elas o gerador produz classes `.js` em vez das interfaces `.ts` que o frontend consome; confirmado experimentalmente antes deste plano). `frontend/bindings/` é gitignored — a regeneração não aparece em `git status`, mas precisa rodar antes de qualquer alteração no frontend que dependa de tipos/métodos novos.

---

### Task 1: `internal/media.Duration` — duração do vídeo via ffprobe

**Files:**
- Modify: `internal/media/media.go`
- Test: `internal/media/media_test.go`

**Interfaces:**
- Produces: `func Duration(ctx context.Context, videoPath string) (time.Duration, error)` — usado pela Task 5 (`services/import.go`).

- [ ] **Step 1: Escrever o teste que falha**

Adicionar ao final de `internal/media/media_test.go`:

```go
func TestDuration_FfprobeNotInPath(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	_, err := Duration(context.Background(), "input.mp4")
	if err == nil {
		t.Fatal("esperava erro quando ffprobe não está no PATH, obteve nil")
	}
}
```

- [ ] **Step 2: Rodar o teste e confirmar que falha**

Run: `go test ./internal/media/... -run TestDuration_FfprobeNotInPath -v`
Expected: FAIL — `Duration` ainda não existe (erro de compilação `undefined: Duration`).

- [ ] **Step 3: Implementar `Duration`**

Em `internal/media/media.go`, adicionar (mantendo o `package media` e o `ExtractAudio` já existentes) os imports `strconv` e `strings` e `time`, e a função:

```go
// Duration lê a duração do vídeo via ffprobe (companion do ffmpeg, mesma
// dependência externa já assumida por ExtractAudio) — usado pra gravar
// lessons.duration_seconds na confirmação da importação (História 5). É
// metadado intrínseco do vídeo, não produto do pipeline de transcrição:
// deve funcionar mesmo que extract_audio/transcribe nunca rodem.
func Duration(ctx context.Context, videoPath string) (time.Duration, error) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		return 0, fmt.Errorf("media: ffprobe não encontrado no PATH: %w", err)
	}

	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "csv=p=0",
		videoPath,
	)
	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("media: ffprobe falhou: %w", err)
	}
	seconds, err := strconv.ParseFloat(strings.TrimSpace(string(output)), 64)
	if err != nil {
		return 0, fmt.Errorf("media: duração inválida na saída do ffprobe: %w", err)
	}
	return time.Duration(seconds * float64(time.Second)), nil
}
```

O arquivo completo de imports fica:

```go
import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)
```

- [ ] **Step 4: Rodar o teste e confirmar que passa**

Run: `go test ./internal/media/... -v`
Expected: PASS em `TestDuration_FfprobeNotInPath` e `TestExtractAudio_FfmpegNotInPath` (já existente).

- [ ] **Step 5: Commit**

```bash
git add internal/media/media.go internal/media/media_test.go
git commit -m "feat: adiciona internal/media.Duration via ffprobe"
```

---

### Task 2: `internal/db` — coluna de duração na `Lesson`, `SetLessonDuration`, `ListTutors`

**Files:**
- Modify: `internal/db/lessons.go`
- Modify: `internal/db/lessons_test.go`

**Interfaces:**
- Consumes: nada de tasks anteriores.
- Produces: `Lesson.DurationSeconds *int64` (novo campo); `func SetLessonDuration(conn *sql.DB, lessonID int64, seconds int64) error`; `func ListTutors(conn *sql.DB) ([]string, error)`. `FindLessonByPath`/`FindLessonByHash`/`FindLessonByID` mantêm as mesmas assinaturas, mas o `*Lesson` retornado agora carrega `DurationSeconds`. `ListLessons` (a versão sem status, da História 3) **fica intocada nesta task** — `services/library.go:30` ainda a chama, e removê-la aqui deixaria o repositório sem compilar até a Task 6 rodar. `ListLessonsWithStatus` (Task 3) é a substituta; a remoção de `ListLessons` e dos dois testes que a cobrem (`TestListLessons_ReturnsAllOrderedByDateDesc`, `TestListLessons_EmptyReturnsEmptyNotNilError`) acontece **na Task 6**, no mesmo commit que reescreve `services/library.go` pra parar de chamá-la — assim o repositório nunca fica num estado intermediário sem compilar. (Nota de execução: esta correção de sequenciamento foi feita depois que a revisão da Task 2 pegou o build quebrado — a Task 2 originalmente removia `ListLessons` cedo demais.)

Este task reescreve `internal/db/lessons.go` por completo (extrai um scanner comum pras três buscas, que hoje repetem a mesma lista de colunas) — arquivo pequeno, mais claro reescrever do que remendar.

- [ ] **Step 1: Escrever os testes que falham**

Em `internal/db/lessons_test.go`, **não mexer** em `TestListLessons_ReturnsAllOrderedByDateDesc` nem `TestListLessons_EmptyReturnsEmptyNotNilError` — ficam como estão, a remoção é só na Task 6. No teste `TestFindLessonByID_FindsExistingAndNilWhenMissing` já existente, adicionar a verificação de duração nula por padrão, trocando o bloco:

```go
	found, err := FindLessonByID(conn, id)
	if err != nil {
		t.Fatalf("FindLessonByID() erro inesperado: %v", err)
	}
	if found == nil || found.VideoPath != "aula-01.mp4" {
		t.Errorf("FindLessonByID() = %+v, esperado video_path aula-01.mp4", found)
	}
```

por:

```go
	found, err := FindLessonByID(conn, id)
	if err != nil {
		t.Fatalf("FindLessonByID() erro inesperado: %v", err)
	}
	if found == nil || found.VideoPath != "aula-01.mp4" {
		t.Errorf("FindLessonByID() = %+v, esperado video_path aula-01.mp4", found)
	}
	if found.DurationSeconds != nil {
		t.Errorf("DurationSeconds = %v, esperado nil antes de SetLessonDuration", *found.DurationSeconds)
	}
```

Adicionar ao final do arquivo:

```go
func TestSetLessonDuration_UpdatesDurationSeconds(t *testing.T) {
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

	if err := SetLessonDuration(conn, id, 1860); err != nil {
		t.Fatalf("SetLessonDuration() erro inesperado: %v", err)
	}

	lesson, err := FindLessonByID(conn, id)
	if err != nil {
		t.Fatalf("FindLessonByID() erro inesperado: %v", err)
	}
	if lesson.DurationSeconds == nil || *lesson.DurationSeconds != 1860 {
		t.Errorf("DurationSeconds = %v, esperado 1860", lesson.DurationSeconds)
	}
}

func TestListTutors_ReturnsDistinctSortedTutors(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	insert := `INSERT INTO lessons (lesson_date, tutor, video_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`
	for _, row := range []struct{ date, tutor, path string }{
		{"2026-07-10", "Sarah M.", "a.mp4"},
		{"2026-07-11", "Sarah M.", "b.mp4"},
		{"2026-07-12", "James K.", "c.mp4"},
	} {
		if _, err := conn.Exec(insert, row.date, row.tutor, row.path, row.date+"T10:00:00Z", row.date+"T10:00:00Z"); err != nil {
			t.Fatalf("insert de fixture falhou: %v", err)
		}
	}

	tutors, err := ListTutors(conn)
	if err != nil {
		t.Fatalf("ListTutors() erro inesperado: %v", err)
	}
	if len(tutors) != 2 || tutors[0] != "James K." || tutors[1] != "Sarah M." {
		t.Errorf("ListTutors() = %+v, esperado [James K. Sarah M.] (distintos, ordem alfabética)", tutors)
	}
}
```

- [ ] **Step 2: Rodar os testes e confirmar que falham**

Run: `go test ./internal/db/... -run 'TestFindLessonByID_FindsExistingAndNilWhenMissing|TestSetLessonDuration_UpdatesDurationSeconds|TestListTutors_ReturnsDistinctSortedTutors' -v`
Expected: FAIL — `DurationSeconds` não existe em `Lesson`, `SetLessonDuration`/`ListTutors` não existem (erro de compilação).

- [ ] **Step 3: Reescrever `internal/db/lessons.go`**

Conteúdo completo do arquivo:

```go
package db

import (
	"database/sql"
	"fmt"
	"time"
)

// Lesson é uma linha de lessons. Além dos dados visíveis ao usuário
// (LessonDate, Tutor, DurationSeconds), carrega a identidade (path, hash) e
// o stat-cache (tamanho/mtime) usados pela varredura da História 3 para
// decidir se o conteúdo precisa ser rehasheado. DurationSeconds é nil até a
// História 5 gravá-lo (best-effort, via ffprobe, na confirmação da
// importação) — nunca bloqueia nada por ser nil.
type Lesson struct {
	ID              int64
	LessonDate      string
	Tutor           string
	VideoPath       string
	VideoHash       string
	FileSize        int64
	FileMTime       string
	DurationSeconds *int64
}

// lessonColumns é a lista de colunas (nesta ordem) que scanLessonRow espera
// — compartilhada por FindLessonByPath/ByHash/ByID pra manter as três
// consultas idênticas na forma como leem duration_seconds nullable.
const lessonColumns = `id, lesson_date, tutor, video_path, COALESCE(video_hash, ''), COALESCE(file_size, 0), COALESCE(file_mtime, ''), duration_seconds`

// scanLessonRow faz o scan de uma linha selecionada com lessonColumns.
// Retorna (nil, nil) se a linha não existir (sql.ErrNoRows) — path/hash/id
// não encontrado é o caso comum, não um erro, pros chamadores.
func scanLessonRow(row *sql.Row) (*Lesson, error) {
	var l Lesson
	var duration sql.NullInt64
	err := row.Scan(&l.ID, &l.LessonDate, &l.Tutor, &l.VideoPath, &l.VideoHash, &l.FileSize, &l.FileMTime, &duration)
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
	return &l, nil
}

// FindLessonByPath busca a lesson cujo video_path é exatamente path. Retorna
// (nil, nil) se não houver nenhuma — path já registrado é o caso comum, não
// um erro.
func FindLessonByPath(conn *sql.DB, path string) (*Lesson, error) {
	row := conn.QueryRow(`SELECT `+lessonColumns+` FROM lessons WHERE video_path = ?`, path)
	l, err := scanLessonRow(row)
	if err != nil {
		return nil, fmt.Errorf("buscar lesson por path: %w", err)
	}
	return l, nil
}

// FindLessonByHash busca a lesson cujo video_hash é exatamente hash. Retorna
// (nil, nil) se não houver nenhuma.
func FindLessonByHash(conn *sql.DB, hash string) (*Lesson, error) {
	row := conn.QueryRow(`SELECT `+lessonColumns+` FROM lessons WHERE video_hash = ?`, hash)
	l, err := scanLessonRow(row)
	if err != nil {
		return nil, fmt.Errorf("buscar lesson por hash: %w", err)
	}
	return l, nil
}

// FindLessonByID busca a lesson por id. Retorna (nil, nil) se não houver.
func FindLessonByID(conn *sql.DB, id int64) (*Lesson, error) {
	row := conn.QueryRow(`SELECT `+lessonColumns+` FROM lessons WHERE id = ?`, id)
	l, err := scanLessonRow(row)
	if err != nil {
		return nil, fmt.Errorf("buscar lesson por id: %w", err)
	}
	return l, nil
}

// UpdateLessonPath atualiza video_path/file_size/file_mtime de uma lesson já
// registrada — usado quando a varredura encontra o mesmo hash num path
// diferente (o arquivo só foi movido/renomeado, não é uma aula nova).
// file_mtime é o mtime do arquivo no disco; updated_at (a marca de quando a
// linha do banco mudou) é sempre "agora", nunca o mtime do arquivo.
func UpdateLessonPath(conn *sql.DB, lessonID int64, path string, size int64, fileMTime string) error {
	_, err := conn.Exec(
		`UPDATE lessons SET video_path = ?, file_size = ?, file_mtime = ?, updated_at = ? WHERE id = ?`,
		path, size, fileMTime, time.Now().UTC().Format(time.RFC3339), lessonID,
	)
	if err != nil {
		return fmt.Errorf("atualizar path da lesson: %w", err)
	}
	return nil
}

// ListLessons lista todas as lessons registradas, mais recentes primeiro
// por data da aula — usado pela Biblioteca da História 3 (sem status
// derivado dos jobs; isso é ListLessonsWithStatus, da História 5). Fica
// nesta task só até a Task 6 trocar o chamador em services/library.go por
// ListLessonsWithStatus e remover esta função (mantém o repositório
// compilando entre as duas tasks).
func ListLessons(conn *sql.DB) ([]Lesson, error) {
	rows, err := conn.Query(`SELECT ` + lessonColumns + ` FROM lessons ORDER BY lesson_date DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("listar lessons: %w", err)
	}
	defer rows.Close()

	var out []Lesson
	for rows.Next() {
		var l Lesson
		var duration sql.NullInt64
		if err := rows.Scan(&l.ID, &l.LessonDate, &l.Tutor, &l.VideoPath, &l.VideoHash, &l.FileSize, &l.FileMTime, &duration); err != nil {
			return nil, fmt.Errorf("ler lesson: %w", err)
		}
		if duration.Valid {
			d := duration.Int64
			l.DurationSeconds = &d
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterar lessons: %w", err)
	}
	return out, nil
}

// SetLessonDuration grava a duração do vídeo (calculada via ffprobe na
// confirmação da importação, best-effort — ver ImportService.ConfirmImport)
// — só é chamado quando o probe teve sucesso.
func SetLessonDuration(conn *sql.DB, lessonID int64, seconds int64) error {
	_, err := conn.Exec(
		`UPDATE lessons SET duration_seconds = ?, updated_at = ? WHERE id = ?`,
		seconds, time.Now().UTC().Format(time.RFC3339), lessonID,
	)
	if err != nil {
		return fmt.Errorf("gravar duração da lesson %d: %w", lessonID, err)
	}
	return nil
}

// ListTutors lista os tutores distintos já registrados em lessons, em ordem
// alfabética — alimenta o dropdown de filtro da Biblioteca (História 5).
func ListTutors(conn *sql.DB) ([]string, error) {
	rows, err := conn.Query(`SELECT DISTINCT tutor FROM lessons ORDER BY tutor ASC`)
	if err != nil {
		return nil, fmt.Errorf("listar tutores: %w", err)
	}
	defer rows.Close()

	out := make([]string, 0)
	for rows.Next() {
		var tutor string
		if err := rows.Scan(&tutor); err != nil {
			return nil, fmt.Errorf("ler tutor: %w", err)
		}
		out = append(out, tutor)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterar tutores: %w", err)
	}
	return out, nil
}
```

- [ ] **Step 4: Rodar os testes do pacote e confirmar que passam**

Run: `go test ./internal/db/... -v`
Expected: PASS em todos — inclusive `TestFindLessonByPathAndByHash_FindExistingRow`, `TestUpdateLessonPath_ChangesPathSizeAndMTime`, `TestLessons_VideoHashUniqueIndexRejectsDuplicate`, `TestListLessons_ReturnsAllOrderedByDateDesc`, `TestListLessons_EmptyReturnsEmptyNotNilError` (já existentes, não devem quebrar com o refactor — `ListLessons` continua existindo nesta task).

Também confirmar que o repositório inteiro ainda compila (não só `internal/db`), já que `services/library.go` ainda chama `db.ListLessons`:

Run: `go build ./internal/... ./services/... .`
Expected: sem erro.

- [ ] **Step 5: Commit**

```bash
git add internal/db/lessons.go internal/db/lessons_test.go
git commit -m "feat: adiciona duration_seconds e ListTutors em internal/db"
```

---

### Task 3: `internal/db` — status derivado dos jobs (`ListLessonsWithStatus`)

**Files:**
- Create: `internal/db/lesson_status.go`
- Create: `internal/db/lesson_status_test.go`

**Interfaces:**
- Consumes: `Lesson` (Task 2, embutido em `LessonWithStatus`); helpers de teste `mustInsertLessonForJobs`/`mustInsertJob` já existentes em `internal/db/jobs_test.go` (mesmo pacote `db`, reaproveitados sem redefinir).
- Produces: `type LessonFilter struct { Tutor, DateFrom, DateTo string }`; `type LessonWithStatus struct { Lesson; Status, ErrorMessage string }`; `func ListLessonsWithStatus(conn *sql.DB, filter LessonFilter) ([]LessonWithStatus, error)` — usado pela Task 6 (`services/library.go`).

- [ ] **Step 1: Escrever os testes que falham**

Criar `internal/db/lesson_status_test.go`:

```go
package db

import (
	"path/filepath"
	"testing"
)

func TestListLessonsWithStatus_ProcessandoWhenNoJobsDone(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	mustInsertJob(t, conn, lessonID, "extract_audio", "running", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")
	mustInsertJob(t, conn, lessonID, "transcribe", "pending", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")

	lessons, err := ListLessonsWithStatus(conn, LessonFilter{})
	if err != nil {
		t.Fatalf("ListLessonsWithStatus() erro inesperado: %v", err)
	}
	if len(lessons) != 1 || lessons[0].Status != "processando" {
		t.Errorf("ListLessonsWithStatus() = %+v, esperado status=processando", lessons)
	}
}

func TestListLessonsWithStatus_ProntaWhenTranscribeDone(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	mustInsertJob(t, conn, lessonID, "extract_audio", "done", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")
	mustInsertJob(t, conn, lessonID, "transcribe", "done", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")

	lessons, err := ListLessonsWithStatus(conn, LessonFilter{})
	if err != nil {
		t.Fatalf("ListLessonsWithStatus() erro inesperado: %v", err)
	}
	if len(lessons) != 1 || lessons[0].Status != "pronta" {
		t.Errorf("ListLessonsWithStatus() = %+v, esperado status=pronta", lessons)
	}
}

func TestListLessonsWithStatus_ErroComMensagemDaCausaRaiz(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	mustInsertJob(t, conn, lessonID, "extract_audio", "error", 3, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")
	if _, err := conn.Exec(`UPDATE jobs SET last_error = ? WHERE lesson_id = ? AND kind = ?`, "ffmpeg não encontrado", lessonID, "extract_audio"); err != nil {
		t.Fatalf("preparar last_error de fixture falhou: %v", err)
	}
	mustInsertJob(t, conn, lessonID, "transcribe", "error", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")
	if _, err := conn.Exec(`UPDATE jobs SET last_error = ? WHERE lesson_id = ? AND kind = ?`, "depende de extract_audio que falhou: ffmpeg não encontrado", lessonID, "transcribe"); err != nil {
		t.Fatalf("preparar last_error de fixture falhou: %v", err)
	}

	lessons, err := ListLessonsWithStatus(conn, LessonFilter{})
	if err != nil {
		t.Fatalf("ListLessonsWithStatus() erro inesperado: %v", err)
	}
	if len(lessons) != 1 || lessons[0].Status != "erro" || lessons[0].ErrorMessage != "ffmpeg não encontrado" {
		t.Errorf("ListLessonsWithStatus() = %+v, esperado status=erro com a mensagem do extract_audio (causa raiz, não a do transcribe bloqueado)", lessons)
	}
}

func TestListLessonsWithStatus_ErroQuandoSoTranscribeFalhou(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	mustInsertJob(t, conn, lessonID, "extract_audio", "done", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")
	mustInsertJob(t, conn, lessonID, "transcribe", "error", 3, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")
	if _, err := conn.Exec(`UPDATE jobs SET last_error = ? WHERE lesson_id = ? AND kind = ?`, "falha real de STT", lessonID, "transcribe"); err != nil {
		t.Fatalf("preparar last_error de fixture falhou: %v", err)
	}

	lessons, err := ListLessonsWithStatus(conn, LessonFilter{})
	if err != nil {
		t.Fatalf("ListLessonsWithStatus() erro inesperado: %v", err)
	}
	if len(lessons) != 1 || lessons[0].Status != "erro" || lessons[0].ErrorMessage != "falha real de STT" {
		t.Errorf("ListLessonsWithStatus() = %+v, esperado status=erro com a mensagem do transcribe", lessons)
	}
}

func TestListLessonsWithStatus_FiltraPorTutorEPeriodo(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	sarah := mustInsertLessonForJobs(t, conn, "sarah.mp4")
	if _, err := conn.Exec(`UPDATE lessons SET tutor = ?, lesson_date = ? WHERE id = ?`, "Sarah M.", "2026-07-10", sarah); err != nil {
		t.Fatalf("ajustar fixture sarah falhou: %v", err)
	}
	mustInsertJob(t, conn, sarah, "extract_audio", "done", 0, "2026-07-10T10:00:00Z", "2026-07-10T10:00:00Z")
	mustInsertJob(t, conn, sarah, "transcribe", "done", 0, "2026-07-10T10:00:00Z", "2026-07-10T10:00:00Z")

	james := mustInsertLessonForJobs(t, conn, "james.mp4")
	if _, err := conn.Exec(`UPDATE lessons SET tutor = ?, lesson_date = ? WHERE id = ?`, "James K.", "2026-07-20T14:00", james); err != nil {
		t.Fatalf("ajustar fixture james falhou: %v", err)
	}
	mustInsertJob(t, conn, james, "extract_audio", "done", 0, "2026-07-20T10:00:00Z", "2026-07-20T10:00:00Z")
	mustInsertJob(t, conn, james, "transcribe", "done", 0, "2026-07-20T10:00:00Z", "2026-07-20T10:00:00Z")

	byTutor, err := ListLessonsWithStatus(conn, LessonFilter{Tutor: "James K."})
	if err != nil {
		t.Fatalf("ListLessonsWithStatus(Tutor) erro inesperado: %v", err)
	}
	if len(byTutor) != 1 || byTutor[0].Tutor != "James K." {
		t.Errorf("ListLessonsWithStatus(Tutor=James K.) = %+v, esperado só a aula de James K.", byTutor)
	}

	byDate, err := ListLessonsWithStatus(conn, LessonFilter{DateFrom: "2026-07-15", DateTo: "2026-07-31"})
	if err != nil {
		t.Fatalf("ListLessonsWithStatus(DateFrom/DateTo) erro inesperado: %v", err)
	}
	if len(byDate) != 1 || byDate[0].Tutor != "James K." {
		t.Errorf("ListLessonsWithStatus(2026-07-15..2026-07-31) = %+v, esperado só a aula de 20/07 (inclui horário, filtra só pela data)", byDate)
	}
}
```

- [ ] **Step 2: Rodar os testes e confirmar que falham**

Run: `go test ./internal/db/... -run TestListLessonsWithStatus -v`
Expected: FAIL — `LessonFilter`/`ListLessonsWithStatus` não existem (erro de compilação).

- [ ] **Step 3: Criar `internal/db/lesson_status.go`**

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

// ListLessonsWithStatus lista as lessons confirmadas com o status derivado
// dos jobs, mais recentes primeiro, aplicando filter (campos vazios são
// ignorados). O filtro de data compara só a parte AAAA-MM-DD de
// lesson_date (que pode ter horário, formato de <input type="datetime-local">),
// pra incluir aulas com horário registrado no dia inteiro do intervalo.
func ListLessonsWithStatus(conn *sql.DB, filter LessonFilter) ([]LessonWithStatus, error) {
	query := `
		SELECT
			l.id, l.lesson_date, l.tutor, l.video_path,
			COALESCE(l.video_hash, ''), COALESCE(l.file_size, 0), COALESCE(l.file_mtime, ''),
			l.duration_seconds,
			COALESCE(ea.status, ''), COALESCE(ea.last_error, ''),
			COALESCE(tr.status, ''), COALESCE(tr.last_error, '')
		FROM lessons l
		LEFT JOIN jobs ea ON ea.lesson_id = l.id AND ea.kind = 'extract_audio'
		LEFT JOIN jobs tr ON tr.lesson_id = l.id AND tr.kind = 'transcribe'
		WHERE 1=1`
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
		var lws LessonWithStatus
		var duration sql.NullInt64
		var extractStatus, extractError, transcribeStatus, transcribeError string
		if err := rows.Scan(
			&lws.ID, &lws.LessonDate, &lws.Tutor, &lws.VideoPath,
			&lws.VideoHash, &lws.FileSize, &lws.FileMTime,
			&duration,
			&extractStatus, &extractError,
			&transcribeStatus, &transcribeError,
		); err != nil {
			return nil, fmt.Errorf("ler lesson com status: %w", err)
		}
		if duration.Valid {
			d := duration.Int64
			lws.DurationSeconds = &d
		}
		lws.Status, lws.ErrorMessage = deriveStatus(extractStatus, extractError, transcribeStatus, transcribeError)
		out = append(out, lws)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterar lessons com status: %w", err)
	}
	return out, nil
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

- [ ] **Step 4: Rodar os testes e confirmar que passam**

Run: `go test ./internal/db/... -v`
Expected: PASS em todos, incluindo os já existentes.

- [ ] **Step 5: Commit**

```bash
git add internal/db/lesson_status.go internal/db/lesson_status_test.go
git commit -m "feat: adiciona status derivado dos jobs em internal/db"
```

---

### Task 4: `internal/db/jobs.go` — `ResetErrorJobsForLesson`

**Files:**
- Modify: `internal/db/jobs.go`
- Modify: `internal/db/jobs_test.go`

**Interfaces:**
- Produces: `func ResetErrorJobsForLesson(conn *sql.DB, lessonID int64) (int64, error)` — usado pela Task 6 (`services/library.go`, `RetryLesson`).

- [ ] **Step 1: Escrever o teste que falha**

Adicionar ao final de `internal/db/jobs_test.go`:

```go
func TestResetErrorJobsForLesson_ResetsOnlyErrorJobsOfThatLesson(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	failing := mustInsertLessonForJobs(t, conn, "falhou.mp4")
	extractID := mustInsertJob(t, conn, failing, "extract_audio", "error", 3, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")
	transcribeID := mustInsertJob(t, conn, failing, "transcribe", "error", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")
	if _, err := conn.Exec(`UPDATE jobs SET last_error = 'falhou' WHERE id IN (?, ?)`, extractID, transcribeID); err != nil {
		t.Fatalf("preparar last_error de fixture falhou: %v", err)
	}

	other := mustInsertLessonForJobs(t, conn, "outra.mp4")
	otherDoneID := mustInsertJob(t, conn, other, "extract_audio", "done", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")

	n, err := ResetErrorJobsForLesson(conn, failing)
	if err != nil {
		t.Fatalf("ResetErrorJobsForLesson() erro inesperado: %v", err)
	}
	if n != 2 {
		t.Errorf("ResetErrorJobsForLesson() = %d, esperado 2 jobs resetados", n)
	}

	extract, err := FindJob(conn, failing, "extract_audio")
	if err != nil {
		t.Fatalf("FindJob() erro inesperado: %v", err)
	}
	if extract.Status != "pending" || extract.Attempts != 0 || extract.LastError != "" {
		t.Errorf("extract_audio após reset = %+v, esperado status=pending attempts=0 last_error vazio", extract)
	}

	transcribe, err := FindJob(conn, failing, "transcribe")
	if err != nil {
		t.Fatalf("FindJob() erro inesperado: %v", err)
	}
	if transcribe.Status != "pending" || transcribe.Attempts != 0 || transcribe.LastError != "" {
		t.Errorf("transcribe após reset = %+v, esperado status=pending attempts=0 last_error vazio", transcribe)
	}

	otherJob, err := FindJob(conn, other, "extract_audio")
	if err != nil {
		t.Fatalf("FindJob() erro inesperado: %v", err)
	}
	if otherJob.ID != otherDoneID || otherJob.Status != "done" {
		t.Errorf("job de outra lesson = %+v, não deveria ser afetado pelo reset", otherJob)
	}
}

func TestResetErrorJobsForLesson_NoErrorJobsReturnsZeroNoError(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	mustInsertJob(t, conn, lessonID, "extract_audio", "done", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")

	n, err := ResetErrorJobsForLesson(conn, lessonID)
	if err != nil {
		t.Fatalf("ResetErrorJobsForLesson() erro inesperado: %v", err)
	}
	if n != 0 {
		t.Errorf("ResetErrorJobsForLesson() = %d, esperado 0 (nenhum job em erro)", n)
	}
}
```

- [ ] **Step 2: Rodar os testes e confirmar que falham**

Run: `go test ./internal/db/... -run TestResetErrorJobsForLesson -v`
Expected: FAIL — `ResetErrorJobsForLesson` não existe (erro de compilação).

- [ ] **Step 3: Implementar `ResetErrorJobsForLesson`**

Adicionar ao final de `internal/db/jobs.go`:

```go
// ResetErrorJobsForLesson reseta todos os jobs em "error" da lesson pra
// "pending" (attempts=0, last_error=NULL) — usado pelo botão "Reprocessar"
// da Biblioteca (História 5). Reseta os dois jobs de uma vez de propósito:
// quando extract_audio falha em definitivo, o worker já marca transcribe
// como "error" também (bloqueado por dependência — ver claimNextEligibleJob
// em internal/jobs/worker.go), e resetar só o extract_audio deixaria o
// transcribe preso em erro pra sempre. Retorna quantos jobs foram
// resetados (0 não é erro — a lesson pode não ter nenhum job em erro).
func ResetErrorJobsForLesson(conn *sql.DB, lessonID int64) (int64, error) {
	res, err := conn.Exec(
		`UPDATE jobs SET status = 'pending', attempts = 0, last_error = NULL, updated_at = ? WHERE lesson_id = ? AND status = 'error'`,
		time.Now().UTC().Format(time.RFC3339), lessonID,
	)
	if err != nil {
		return 0, fmt.Errorf("resetar jobs com erro da lesson %d: %w", lessonID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("confirmar reset de jobs da lesson %d: %w", lessonID, err)
	}
	return n, nil
}
```

- [ ] **Step 4: Rodar os testes e confirmar que passam**

Run: `go test ./internal/db/... -v`
Expected: PASS em todos.

- [ ] **Step 5: Commit**

```bash
git add internal/db/jobs.go internal/db/jobs_test.go
git commit -m "feat: adiciona ResetErrorJobsForLesson pro reprocessamento da Biblioteca"
```

---

### Task 5: `services/import.go` — duração best-effort na confirmação

**Files:**
- Modify: `services/import.go`
- Modify: `services/import_test.go`

**Interfaces:**
- Consumes: `media.Duration` (Task 1), `db.FindLessonByID`/`db.SetLessonDuration` (Task 2).
- Produces: nenhuma assinatura pública nova — `ImportService.ConfirmImport` mantém `(id int64, lessonDate string, tutor string) error`.

- [ ] **Step 1: Escrever o teste que falha**

Adicionar ao final de `services/import_test.go`:

```go
func TestImportService_ConfirmImport_SucceedsEvenWhenDurationProbeFails(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	storageRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(storageRoot, "aula.mp4"), []byte("nao-e-um-video-de-verdade"), 0o644); err != nil {
		t.Fatalf("preparar vídeo de fixture falhou: %v", err)
	}
	if err := config.Save(&config.AppConfig{StorageRoot: storageRoot}); err != nil {
		t.Fatalf("config.Save() falhou: %v", err)
	}

	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	svc := NewImportService(conn)
	if _, err := svc.ScanFolder(); err != nil {
		t.Fatalf("ScanFolder() erro inesperado: %v", err)
	}
	pending, err := svc.ListPendingImports()
	if err != nil || len(pending) != 1 {
		t.Fatalf("setup: ListPendingImports() = %+v, %v", pending, err)
	}

	if err := svc.ConfirmImport(pending[0].ID, "2026-07-22", "Sarah M."); err != nil {
		t.Fatalf("ConfirmImport() com vídeo inválido não deveria falhar (duração é melhor esforço): %v", err)
	}

	lesson, err := db.FindLessonByPath(conn, "aula.mp4")
	if err != nil {
		t.Fatalf("FindLessonByPath() erro inesperado: %v", err)
	}
	if lesson == nil {
		t.Fatal("lesson não foi confirmada")
	}
	if lesson.DurationSeconds != nil {
		t.Errorf("DurationSeconds = %v, esperado nil (fixture não é um vídeo real, ffprobe deveria falhar ou estar ausente)", *lesson.DurationSeconds)
	}
}
```

- [ ] **Step 2: Rodar o teste e confirmar que passa mesmo sem a mudança**

Run: `go test ./services/... -run TestImportService_ConfirmImport_SucceedsEvenWhenDurationProbeFails -v`
Expected: PASS — este teste específico já passa sem nenhuma mudança de produção, porque `ConfirmImport` hoje simplesmente não grava duração nenhuma (fica sempre nil). O teste serve pra travar o comportamento **depois** que a Step 3 adicionar a chamada ao probe: ele deve continuar passando mesmo com o probe rodando (e falhando, por causa do conteúdo fake). Confirmar isso agora estabelece a baseline antes da mudança.

- [ ] **Step 3: Implementar a chamada best-effort em `ConfirmImport`**

Reescrever `services/import.go` (só a parte de imports, `ConfirmImport` e a nova função — `dbRepo` e o resto do arquivo ficam iguais):

```go
package services

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"path/filepath"

	"assistente-idiomas/internal/config"
	"assistente-idiomas/internal/db"
	"assistente-idiomas/internal/importer"
	"assistente-idiomas/internal/media"
)
```

Trocar o corpo de `ConfirmImport`:

```go
// ConfirmImport grava o candidato id como lesson real (lessonDate no
// formato AAAA-MM-DD, tutor livre) e cria os jobs de processamento. Depois
// de confirmar, tenta calcular a duração do vídeo (melhor esforço — ver
// setDurationBestEffort).
func (s *ImportService) ConfirmImport(id int64, lessonDate string, tutor string) error {
	if lessonDate == "" {
		return fmt.Errorf("data da aula não pode ser vazia")
	}
	if tutor == "" {
		return fmt.Errorf("tutor não pode ser vazio")
	}
	lessonID, err := db.ConfirmPendingImport(s.conn, id, lessonDate, tutor)
	if err != nil {
		return err
	}
	s.setDurationBestEffort(lessonID)
	return nil
}

// setDurationBestEffort calcula a duração do vídeo recém-confirmado via
// ffprobe e grava em lessons.duration_seconds. Duração é metadado
// intrínseco do vídeo, não produto do pipeline de transcrição — deve ficar
// disponível mesmo que o pipeline falhe (princípio de resiliência,
// CLAUDE.md). Por isso qualquer falha aqui (ffprobe ausente, arquivo
// inválido, etc.) é só logada: nunca propagada como erro de ConfirmImport,
// que já confirmou a lesson com sucesso.
func (s *ImportService) setDurationBestEffort(lessonID int64) {
	lesson, err := db.FindLessonByID(s.conn, lessonID)
	if err != nil || lesson == nil {
		return
	}
	cfg, err := config.Load()
	if err != nil {
		return
	}
	videoPath := filepath.Join(cfg.StorageRoot, filepath.FromSlash(lesson.VideoPath))
	dur, err := media.Duration(context.Background(), videoPath)
	if err != nil {
		slog.Warn("importer: não foi possível calcular a duração do vídeo", "lesson_id", lessonID, "erro", err)
		return
	}
	if err := db.SetLessonDuration(s.conn, lessonID, int64(dur.Seconds())); err != nil {
		slog.Warn("importer: não foi possível gravar a duração do vídeo", "lesson_id", lessonID, "erro", err)
	}
}
```

O resto do arquivo (`dbRepo` e seus métodos) fica inalterado.

- [ ] **Step 4: Rodar os testes do pacote e confirmar que passam**

Run: `go test ./services/... -v`
Expected: PASS em todos, incluindo os três testes já existentes de `ImportService` e o novo.

- [ ] **Step 5: Commit**

```bash
git add services/import.go services/import_test.go
git commit -m "feat: calcula duracao do video em melhor esforco na confirmacao da importacao"
```

---

### Task 6: `services/library.go` — status/duração/filtro/reprocessar/Detalhe + bindings

**Files:**
- Modify: `services/library.go`
- Modify: `services/library_test.go`
- Modify: `internal/db/lessons.go` — remover a função `ListLessons` (deixada de propósito na Task 2; este é o task que troca seu único chamador, `services/library.go`, por `ListLessonsWithStatus` — a remoção acontece no mesmo commit pra nunca deixar o repositório sem compilar entre uma coisa e outra).
- Modify: `internal/db/lessons_test.go` — remover `TestListLessons_ReturnsAllOrderedByDateDesc` e `TestListLessons_EmptyReturnsEmptyNotNilError` (cobrem a função que este task remove).
- Regenerate: `frontend/bindings/` (via `wails3 generate bindings -ts -i ./...`, não versionado)

**Interfaces:**
- Consumes: `db.ListLessonsWithStatus`/`db.LessonFilter` (Task 3), `db.ListTutors` (Task 2), `db.ResetErrorJobsForLesson` (Task 4), `db.FindLessonByID` (Task 2).
- Produces (consumido pela Task 7/8 do frontend, e pelo `main.go` na Task 7 do backend):
  - `type Lesson struct { ID int64; LessonDate string; Tutor string; VideoPath string; DurationSeconds *int64; Status string; ErrorMessage string }` (JSON: `id`, `lessonDate`, `tutor`, `videoPath`, `durationSeconds`, `status`, `errorMessage`).
  - `type LessonFilter struct { Tutor, DateFrom, DateTo string }` (JSON: `tutor`, `dateFrom`, `dateTo`).
  - `func (s *LibraryService) ListLessons(filter LessonFilter) ([]Lesson, error)` — **assinatura muda** (antes não tinha parâmetro).
  - `func (s *LibraryService) ListTutors() ([]string, error)`.
  - `func (s *LibraryService) RetryLesson(lessonID int64) error`.
  - `func (s *LibraryService) GetLesson(id int64) (Lesson, error)`.
  - Depois de `wails3 generate bindings -ts -i ./...`, os tipos TS gerados (confirmados experimentalmente) são:
    - `export interface Lesson { "id": number; "lessonDate": string; "tutor": string; "videoPath": string; "durationSeconds": number | null; "status": string; "errorMessage": string; }`
    - `export interface LessonFilter { "tutor": string; "dateFrom": string; "dateTo": string; }`
    - `ListLessons(filter: $models.LessonFilter): $CancellablePromise<$models.Lesson[] | null>`
    - `ListTutors(): $CancellablePromise<string[] | null>`
    - `RetryLesson(lessonID: number): $CancellablePromise<void>`
    - `GetLesson(id: number): $CancellablePromise<$models.Lesson>` (rejeita a promise se não encontrado — sem `| null`).

- [ ] **Step 1: Escrever os testes que falham**

Reescrever `services/library_test.go` por completo:

```go
package services

import (
	"database/sql"
	"path/filepath"
	"testing"

	"assistente-idiomas/internal/db"
)

func mustInsertLesson(t *testing.T, conn *sql.DB, date, tutor, videoPath string) int64 {
	t.Helper()
	res, err := conn.Exec(
		`INSERT INTO lessons (lesson_date, tutor, video_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		date, tutor, videoPath, "2026-07-22T09:00:00Z", "2026-07-22T09:00:00Z",
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

func mustInsertJobWithStatus(t *testing.T, conn *sql.DB, lessonID int64, kind, status, lastError string) {
	t.Helper()
	_, err := conn.Exec(
		`INSERT INTO jobs (lesson_id, kind, status, last_error, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		lessonID, kind, status, lastError, "2026-07-22T09:00:00Z", "2026-07-22T09:00:00Z",
	)
	if err != nil {
		t.Fatalf("inserir job de fixture falhou: %v", err)
	}
}

func TestLibraryService_ListLessons_ReturnsConfirmedLessons(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLesson(t, conn, "2026-07-20", "Sarah M.", "aula.mp4")
	mustInsertJobWithStatus(t, conn, lessonID, "extract_audio", "done", "")
	mustInsertJobWithStatus(t, conn, lessonID, "transcribe", "done", "")

	svc := NewLibraryService(conn)
	lessons, err := svc.ListLessons(LessonFilter{})
	if err != nil {
		t.Fatalf("ListLessons() erro inesperado: %v", err)
	}
	if len(lessons) != 1 {
		t.Fatalf("ListLessons() = %+v, esperado 1 aula", lessons)
	}
	if lessons[0].LessonDate != "2026-07-20" || lessons[0].Tutor != "Sarah M." || lessons[0].VideoPath != "aula.mp4" {
		t.Errorf("ListLessons()[0] = %+v, esperado data/tutor/path da fixture", lessons[0])
	}
	if lessons[0].Status != "pronta" {
		t.Errorf("ListLessons()[0].Status = %q, esperado pronta (extract_audio e transcribe done)", lessons[0].Status)
	}
}

func TestLibraryService_ListLessons_EmptyReturnsEmptySlice(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	svc := NewLibraryService(conn)
	lessons, err := svc.ListLessons(LessonFilter{})
	if err != nil {
		t.Fatalf("ListLessons() erro inesperado: %v", err)
	}
	if len(lessons) != 0 {
		t.Errorf("ListLessons() = %+v, esperado vazio", lessons)
	}
}

func TestLibraryService_ListLessons_FiltersByTutor(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	l1 := mustInsertLesson(t, conn, "2026-07-20", "Sarah M.", "a.mp4")
	mustInsertJobWithStatus(t, conn, l1, "extract_audio", "done", "")
	mustInsertJobWithStatus(t, conn, l1, "transcribe", "done", "")
	l2 := mustInsertLesson(t, conn, "2026-07-21", "James K.", "b.mp4")
	mustInsertJobWithStatus(t, conn, l2, "extract_audio", "done", "")
	mustInsertJobWithStatus(t, conn, l2, "transcribe", "done", "")

	svc := NewLibraryService(conn)
	lessons, err := svc.ListLessons(LessonFilter{Tutor: "James K."})
	if err != nil {
		t.Fatalf("ListLessons() erro inesperado: %v", err)
	}
	if len(lessons) != 1 || lessons[0].Tutor != "James K." {
		t.Errorf("ListLessons(Tutor=James K.) = %+v, esperado só a aula de James K.", lessons)
	}
}

func TestLibraryService_ListLessons_ErrorStatusAndMessageFromExtractAudio(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLesson(t, conn, "2026-07-20", "Sarah M.", "aula.mp4")
	mustInsertJobWithStatus(t, conn, lessonID, "extract_audio", "error", "ffmpeg não encontrado")
	mustInsertJobWithStatus(t, conn, lessonID, "transcribe", "error", "depende de extract_audio que falhou: ffmpeg não encontrado")

	svc := NewLibraryService(conn)
	lessons, err := svc.ListLessons(LessonFilter{})
	if err != nil {
		t.Fatalf("ListLessons() erro inesperado: %v", err)
	}
	if len(lessons) != 1 || lessons[0].Status != "erro" || lessons[0].ErrorMessage != "ffmpeg não encontrado" {
		t.Errorf("ListLessons()[0] = %+v, esperado status=erro com a mensagem do extract_audio (causa raiz)", lessons[0])
	}
}

func TestLibraryService_ListTutors_ReturnsDistinctTutors(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	mustInsertLesson(t, conn, "2026-07-20", "Sarah M.", "a.mp4")
	mustInsertLesson(t, conn, "2026-07-21", "Sarah M.", "b.mp4")
	mustInsertLesson(t, conn, "2026-07-22", "James K.", "c.mp4")

	svc := NewLibraryService(conn)
	tutors, err := svc.ListTutors()
	if err != nil {
		t.Fatalf("ListTutors() erro inesperado: %v", err)
	}
	if len(tutors) != 2 {
		t.Fatalf("ListTutors() = %+v, esperado 2 tutores distintos", tutors)
	}
}

func TestLibraryService_RetryLesson_ResetsErrorJobsToPending(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLesson(t, conn, "2026-07-20", "Sarah M.", "aula.mp4")
	mustInsertJobWithStatus(t, conn, lessonID, "extract_audio", "error", "ffmpeg não encontrado")
	mustInsertJobWithStatus(t, conn, lessonID, "transcribe", "error", "depende de extract_audio que falhou")

	svc := NewLibraryService(conn)
	if err := svc.RetryLesson(lessonID); err != nil {
		t.Fatalf("RetryLesson() erro inesperado: %v", err)
	}

	lessons, err := svc.ListLessons(LessonFilter{})
	if err != nil {
		t.Fatalf("ListLessons() erro inesperado: %v", err)
	}
	if len(lessons) != 1 || lessons[0].Status != "processando" {
		t.Errorf("ListLessons()[0] após RetryLesson = %+v, esperado status=processando (jobs voltaram a pending)", lessons[0])
	}
}

func TestLibraryService_GetLesson_FindsExistingAndErrorsWhenMissing(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLesson(t, conn, "2026-07-20", "Sarah M.", "aula.mp4")

	svc := NewLibraryService(conn)
	lesson, err := svc.GetLesson(lessonID)
	if err != nil {
		t.Fatalf("GetLesson() erro inesperado: %v", err)
	}
	if lesson.Tutor != "Sarah M." || lesson.VideoPath != "aula.mp4" {
		t.Errorf("GetLesson() = %+v, esperado tutor/path da fixture", lesson)
	}

	if _, err := svc.GetLesson(lessonID + 999); err == nil {
		t.Error("GetLesson() com id inexistente esperava erro, veio nil")
	}
}
```

- [ ] **Step 2: Rodar os testes e confirmar que falham**

Run: `go test ./services/... -run TestLibraryService -v`
Expected: FAIL — `ListLessons` ainda espera zero argumentos, `ListTutors`/`RetryLesson`/`GetLesson`/`LessonFilter` não existem (erro de compilação).

- [ ] **Step 3: Reescrever `services/library.go`**

```go
package services

import (
	"database/sql"
	"fmt"

	"assistente-idiomas/internal/db"
)

// LibraryService expõe as aulas já confirmadas para a Biblioteca —
// listagem com status derivado dos jobs e duração (História 5), filtro por
// tutor/período, reprocessamento de aulas com erro e busca de uma aula pro
// Detalhe.
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
// importação) ter sucesso.
type Lesson struct {
	ID              int64  `json:"id"`
	LessonDate      string `json:"lessonDate"`
	Tutor           string `json:"tutor"`
	VideoPath       string `json:"videoPath"`
	DurationSeconds *int64 `json:"durationSeconds"`
	Status          string `json:"status"`
	ErrorMessage    string `json:"errorMessage"`
}

// LessonFilter filtra ListLessons — campos vazios são ignorados (sem
// filtro naquele critério).
type LessonFilter struct {
	Tutor    string `json:"tutor"`
	DateFrom string `json:"dateFrom"`
	DateTo   string `json:"dateTo"`
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
			ID:              r.ID,
			LessonDate:      r.LessonDate,
			Tutor:           r.Tutor,
			VideoPath:       r.VideoPath,
			DurationSeconds: r.DurationSeconds,
			Status:          r.Status,
			ErrorMessage:    r.ErrorMessage,
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

// GetLesson busca uma aula confirmada por id, pro Detalhe (História 5/6).
// Diferente de ListLessons, não calcula Status/ErrorMessage — o Detalhe só
// é aberto a partir de uma aula já "pronta" na Biblioteca (ver
// Library.svelte), então recalcular o status aqui seria trabalho sem uso.
func (s *LibraryService) GetLesson(id int64) (Lesson, error) {
	lesson, err := db.FindLessonByID(s.conn, id)
	if err != nil {
		return Lesson{}, err
	}
	if lesson == nil {
		return Lesson{}, fmt.Errorf("aula %d não encontrada", id)
	}
	return Lesson{
		ID:              lesson.ID,
		LessonDate:      lesson.LessonDate,
		Tutor:           lesson.Tutor,
		VideoPath:       lesson.VideoPath,
		DurationSeconds: lesson.DurationSeconds,
	}, nil
}
```

- [ ] **Step 3b: Remover `ListLessons` de `internal/db`**

Este método (da História 3) fica sem chamador depois da Step 3 acima — `services/library.go` agora chama `db.ListLessonsWithStatus`, não mais `db.ListLessons`. Removê-lo neste mesmo commit (não antes: a Task 2 deixou `ListLessons` intocada de propósito, pra nunca deixar o repositório sem compilar entre as duas tasks).

Em `internal/db/lessons.go`, remover a função `ListLessons` inteira (a que lista todas as lessons ordenadas por `lesson_date DESC, id DESC`, sem status).

Em `internal/db/lessons_test.go`, remover `TestListLessons_ReturnsAllOrderedByDateDesc` e `TestListLessons_EmptyReturnsEmptyNotNilError` (cobrem a função removida).

- [ ] **Step 4: Rodar os testes e confirmar que passam**

Run: `go test ./internal/db/... ./services/... -v`
Expected: PASS em todos — nenhum teste deve referenciar `db.ListLessons` depois desta step.

Run: `go build ./internal/... ./services/... .`
Expected: sem erro (confirma que remover `ListLessons` não deixou nenhum outro chamador esquecido em nenhum pacote).

- [ ] **Step 5: Regenerar os bindings do frontend**

Run: `wails3 generate bindings -ts -i ./...`
Expected: saída `INFO Processed: ... Services, ... Methods, ... Models ...` sem erro. Conferir com `cat frontend/bindings/assistente-idiomas/services/models.ts` e `cat frontend/bindings/assistente-idiomas/services/libraryservice.ts` que os tipos batem com os listados em **Interfaces** acima (nomes de campos, `| null` onde esperado).

- [ ] **Step 6: Commit**

```bash
git add services/library.go services/library_test.go internal/db/lessons.go internal/db/lessons_test.go
git commit -m "feat: status/duracao/filtro/reprocessar/detalhe na LibraryService"
```

(`frontend/bindings/` é gitignored — não entra no commit; é regenerado localmente por quem builda o app.)

---

### Task 7: Vídeo local ao webview — `VideoAssetMiddleware` + wiring em `main.go`

**Files:**
- Create: `services/video_asset.go`
- Create: `services/video_asset_test.go`
- Modify: `main.go`

**Interfaces:**
- Consumes: `db.FindLessonByID` (Task 2), `jobs.StorageRootResolver` (já existente em `internal/jobs/worker.go`), `mustInsertLesson(t, conn, date, tutor, videoPath) int64` (helper de teste definido em `services/library_test.go` pela Task 6, reaproveitado aqui sem redefinir — mesmo pacote `services`).
- Produces: `func VideoAssetMiddleware(conn *sql.DB, storageRoot jobs.StorageRootResolver) application.Middleware` — servido em `GET /media/lesson/{id}`, usado pela Task 8 (frontend) como `src` do `<video>`.

- [ ] **Step 1: Escrever os testes que falham**

Criar `services/video_asset_test.go`:

```go
package services

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"assistente-idiomas/internal/db"
)

func TestVideoAssetMiddleware_ServesVideoWithRangeSupport(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	storageRoot := t.TempDir()
	content := []byte("conteudo-de-video-fake-para-teste-de-range")
	if err := os.WriteFile(filepath.Join(storageRoot, "aula.mp4"), content, 0o644); err != nil {
		t.Fatalf("preparar vídeo de fixture falhou: %v", err)
	}
	lessonID := mustInsertLesson(t, conn, "2026-07-20", "Sarah M.", "aula.mp4")

	handler := VideoAssetMiddleware(conn, func() (string, error) { return storageRoot, nil })(http.NotFoundHandler())

	req := httptest.NewRequest(http.MethodGet, "/media/lesson/"+strconv.FormatInt(lessonID, 10), nil)
	req.Header.Set("Range", "bytes=0-4")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, esperado 206 Partial Content", rec.Code)
	}
	if rec.Body.String() != string(content[:5]) {
		t.Errorf("body = %q, esperado os 5 primeiros bytes do vídeo", rec.Body.String())
	}
}

func TestVideoAssetMiddleware_UnknownLessonReturns404(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	handler := VideoAssetMiddleware(conn, func() (string, error) { return t.TempDir(), nil })(http.NotFoundHandler())

	req := httptest.NewRequest(http.MethodGet, "/media/lesson/999", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, esperado 404", rec.Code)
	}
}

func TestVideoAssetMiddleware_OtherPathsDelegateToNext(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { nextCalled = true })
	handler := VideoAssetMiddleware(conn, func() (string, error) { return t.TempDir(), nil })(next)

	req := httptest.NewRequest(http.MethodGet, "/wails/runtime.js", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !nextCalled {
		t.Error("path fora de /media/lesson/ deveria ser delegado a next, mas next não foi chamado")
	}
}
```

- [ ] **Step 2: Rodar os testes e confirmar que falham**

Run: `go test ./services/... -run TestVideoAssetMiddleware -v`
Expected: FAIL — `VideoAssetMiddleware` não existe (erro de compilação).

- [ ] **Step 3: Criar `services/video_asset.go`**

```go
// services/video_asset.go
package services

import (
	"database/sql"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"assistente-idiomas/internal/db"
	"assistente-idiomas/internal/jobs"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// videoAssetPrefix é o path base do endpoint que serve o .mp4 de uma
// lesson ao webview — resolve o risco técnico 1 do fase-1-mvp.md (vídeo
// local com suporte a range requests) via http.ServeFile da stdlib, que já
// trata Range de graça. Ver
// docs/superpowers/specs/2026-07-22-historia-5-biblioteca-real-design.md.
const videoAssetPrefix = "/media/lesson/"

// VideoAssetMiddleware serve GET /media/lesson/{id} com o vídeo da lesson
// id; qualquer outro path é delegado a next (o AssetServer padrão do
// Wails — embedded em produção, proxy pro dev server em `wails3 dev`,
// confirmado lendo internal/assetserver/build_dev.go da dependência: o
// webview sempre fala com o servidor Go, que só faz proxy pro Vite
// internamente quando FRONTEND_DEVSERVER_URL está setado).
// storageRoot é reavaliado a cada requisição, não uma vez só na criação do
// middleware — mesma razão de internal/jobs.Worker: storage_root só existe
// depois do wizard de primeira execução, que roda depois do app já estar
// de pé.
func VideoAssetMiddleware(conn *sql.DB, storageRoot jobs.StorageRootResolver) application.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			idStr, ok := strings.CutPrefix(r.URL.Path, videoAssetPrefix)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			id, err := strconv.ParseInt(idStr, 10, 64)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			lesson, err := db.FindLessonByID(conn, id)
			if err != nil || lesson == nil {
				http.NotFound(w, r)
				return
			}
			root, err := storageRoot()
			if err != nil {
				http.Error(w, "storage_root não configurado", http.StatusInternalServerError)
				return
			}
			videoPath := filepath.Join(root, filepath.FromSlash(lesson.VideoPath))
			http.ServeFile(w, r, videoPath)
		})
	}
}
```

- [ ] **Step 4: Rodar os testes e confirmar que passam**

Run: `go test ./services/... -v`
Expected: PASS em todos.

- [ ] **Step 5: Ligar o middleware em `main.go`**

Reescrever `main.go` por completo:

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

	storageRoot := func() (string, error) {
		cfg, err := config.Load()
		if err != nil {
			return "", err
		}
		return cfg.StorageRoot, nil
	}

	startJobWorker(conn, storageRoot)

	app := application.New(application.Options{
		Name:        "Assistente de Idiomas",
		Description: "Arquivo e análise de aulas de inglês do Cambly",
		Services: []application.Service{
			application.NewService(services.NewSetupService()),
			application.NewService(services.NewImportService(conn)),
			application.NewService(services.NewLibraryService(conn)),
		},
		Assets: application.AssetOptions{
			Handler:    application.AssetFileServerFS(assets),
			Middleware: services.VideoAssetMiddleware(conn, storageRoot),
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
// goroutine. storageRoot é resolvido a cada job, não uma vez só aqui — o
// wizard de primeira execução ainda não rodou neste ponto do startup, então
// resolvê-lo antecipadamente falharia sempre na primeira sessão do app (ver
// docs/superpowers/specs/2026-07-22-historia-4-pipeline-jobs-design.md).
// Só o cache de áudio (que não depende do wizard) é resolvido aqui; se isso
// falhar, é um problema de disco/permissão e o worker não inicia.
func startJobWorker(conn *sql.DB, storageRoot jobs.StorageRootResolver) {
	audioCacheDir, err := config.AudioCacheDir()
	if err != nil {
		log.Printf("worker de jobs não iniciado: %v", err)
		return
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

- [ ] **Step 6: Confirmar que o binário compila**

Run: `go build ./internal/... ./services/... .`
Expected: sem erro (o pacote `build/ios` já falha antes desta mudança por motivo não relacionado — não usar `go build ./...` pra este check).

Run: `go vet ./...`
Expected: sem erro.

- [ ] **Step 7: Commit**

```bash
git add services/video_asset.go services/video_asset_test.go main.go
git commit -m "feat: serve video local ao webview via asset handler (risco 1)"
```

---

### Task 8: Frontend — filtro/status/duração na Biblioteca + navegação pro Detalhe

**Files:**
- Modify: `frontend/src/lib/screens/Library.svelte`
- Create: `frontend/src/lib/screens/LessonDetail.svelte`
- Modify: `frontend/src/App.svelte`

**Interfaces:**
- Consumes (bindings geradas na Task 6): `LibraryService.ListLessons(filter: LessonFilter)`, `LibraryService.ListTutors()`, `LibraryService.RetryLesson(lessonID)`, `LibraryService.GetLesson(id)`, tipos `Lesson`/`LessonFilter` de `../../bindings/assistente-idiomas/services/models`. Endpoint de vídeo da Task 7: `GET /media/lesson/{id}`.
- Produces: `Library.svelte` ganha prop `onOpenLesson: (lessonId: number) => void`; `LessonDetail.svelte` (novo) recebe `{ lessonId: number; onBack: () => void }`.

Este projeto não tem testes automatizados de componente Svelte (nenhum existe hoje) — a verificação desta task é `npm run check` (type-check) mais a verificação visual manual já pendente das histórias anteriores.

- [ ] **Step 1: Reescrever `frontend/src/lib/screens/Library.svelte`**

```svelte
<script lang="ts">
  import { onMount } from "svelte";
  import { colors, fonts } from "../theme";
  import * as ImportService from "../../../bindings/assistente-idiomas/services/importservice";
  import * as LibraryService from "../../../bindings/assistente-idiomas/services/libraryservice";
  import type { PendingImport, Lesson, LessonFilter } from "../../../bindings/assistente-idiomas/services/models";
  import ImportConfirmModal from "../ImportConfirmModal.svelte";

  let { onOpenLesson }: { onOpenLesson: (lessonId: number) => void } = $props();

  let pending: PendingImport[] = $state([]);
  let lessons: Lesson[] = $state([]);
  let tutors: string[] = $state([]);
  let loading: boolean = $state(true);
  let syncing: boolean = $state(false);
  let syncMessage: string = $state("");
  let error: string = $state("");
  let reviewing: PendingImport | null = $state(null);
  let retryingId: number | null = $state(null);

  let filterTutor: string = $state("");
  let filterDateFrom: string = $state("");
  let filterDateTo: string = $state("");

  const STATUS_LABEL: Record<string, string> = {
    pronta: "pronta",
    processando: "processando…",
    erro: "erro",
  };

  async function loadPending() {
    pending = (await ImportService.ListPendingImports()) ?? [];
  }

  async function loadLessons() {
    const filter: LessonFilter = { tutor: filterTutor, dateFrom: filterDateFrom, dateTo: filterDateTo };
    lessons = (await LibraryService.ListLessons(filter)) ?? [];
  }

  async function loadTutors() {
    tutors = (await LibraryService.ListTutors()) ?? [];
  }

  // lessonDate é gravado como "AAAA-MM-DD" ou "AAAA-MM-DDTHH:MM" (formato de
  // <input type="datetime-local">); aqui só reformata pra exibição em pt-BR
  // sem depender de fuso horário (não é um timestamp com "Z", é hora local).
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

  async function loadAll() {
    try {
      await Promise.all([loadPending(), loadLessons(), loadTutors()]);
    } catch (e) {
      error = String(e);
    } finally {
      loading = false;
    }
  }

  async function applyFilter() {
    error = "";
    try {
      await loadLessons();
    } catch (e) {
      error = String(e);
    }
  }

  async function syncFolder() {
    error = "";
    syncMessage = "";
    syncing = true;
    try {
      const summary = await ImportService.ScanFolder();
      syncMessage = `${summary.new} novas, ${summary.updated} atualizadas, ${summary.errors} erros`;
      await loadPending();
    } catch (e) {
      error = String(e);
    } finally {
      syncing = false;
    }
  }

  function closeReview() {
    reviewing = null;
  }

  async function onConfirmed() {
    reviewing = null;
    try {
      await Promise.all([loadPending(), loadLessons(), loadTutors()]);
    } catch (e) {
      error = String(e);
    }
  }

  async function retry(lessonId: number) {
    error = "";
    retryingId = lessonId;
    try {
      await LibraryService.RetryLesson(lessonId);
      await loadLessons();
    } catch (e) {
      error = String(e);
    } finally {
      retryingId = null;
    }
  }

  function openLesson(lesson: Lesson) {
    if (lesson.status === "pronta") {
      onOpenLesson(lesson.id);
    }
  }

  onMount(loadAll);
</script>

<div class="screen" style="font-family: {fonts.body}; color: {colors.text};">
  <div class="header-row">
    <h1 style="font-family: {fonts.display};">Biblioteca</h1>
    <button onclick={syncFolder} disabled={syncing}>
      {syncing ? "Sincronizando…" : "Sincronizar pasta"}
    </button>
  </div>

  {#if syncMessage}
    <p class="hint" style="color: {colors.mut};">{syncMessage}</p>
  {/if}
  {#if error}
    <p class="error" style="color: {colors.red};">{error}</p>
  {/if}

  {#if loading}
    <p style="color: {colors.mut};">Carregando…</p>
  {:else}
    {#if pending.length > 0}
      <section class="pending" style="background: {colors.surface}; border: 1px solid {colors.line};">
        <h2 style="font-family: {fonts.display};">{pending.length} aulas aguardando revisão</h2>
        <ul>
          {#each pending as item (item.id)}
            <li>
              <span class="path" style="font-family: {fonts.mono}; color: {colors.mut};">{item.path}</span>
              <button onclick={() => (reviewing = item)}>Revisar</button>
            </li>
          {/each}
        </ul>
      </section>
    {/if}

    <section class="filters" style="background: {colors.surface}; border: 1px solid {colors.line};">
      <label>
        Tutor
        <select bind:value={filterTutor} onchange={applyFilter}>
          <option value="">Todos</option>
          {#each tutors as tutor (tutor)}
            <option value={tutor}>{tutor}</option>
          {/each}
        </select>
      </label>
      <label>
        De
        <input type="date" bind:value={filterDateFrom} onchange={applyFilter} />
      </label>
      <label>
        Até
        <input type="date" bind:value={filterDateTo} onchange={applyFilter} />
      </label>
    </section>

    {#if lessons.length > 0}
      <section class="lessons" style="background: {colors.surface}; border: 1px solid {colors.line};">
        <h2 style="font-family: {fonts.display};">{lessons.length} aulas</h2>
        <ul>
          {#each lessons as lesson (lesson.id)}
            <li>
              <button
                class="lesson-main"
                onclick={() => openLesson(lesson)}
                style="cursor: {lesson.status === 'pronta' ? 'pointer' : 'default'}; opacity: {lesson.status === 'pronta' ? 1 : 0.7};"
              >
                <span class="date" style="color: {colors.text};">{formatLessonDateTime(lesson.lessonDate)}</span>
                <span class="tutor" style="color: {colors.mut};"
                  >{lesson.tutor}{formatDuration(lesson.durationSeconds)
                    ? ` · ${formatDuration(lesson.durationSeconds)}`
                    : ""}</span
                >
              </button>
              {#if lesson.status === "erro"}
                <div class="status-block">
                  <span class="badge" style="color: {colors.red}; background: rgba(224,108,108,.1);">erro</span>
                  <span class="error-message" style="color: {colors.mut};">{lesson.errorMessage}</span>
                  <button onclick={() => retry(lesson.id)} disabled={retryingId === lesson.id}>
                    {retryingId === lesson.id ? "Reprocessando…" : "Reprocessar"}
                  </button>
                </div>
              {:else}
                <span
                  class="badge"
                  style="color: {lesson.status === 'pronta'
                    ? colors.green
                    : colors.blue}; background: {lesson.status === 'pronta'
                    ? 'rgba(111,191,142,.1)'
                    : 'rgba(110,168,254,.1)'};"
                >
                  {STATUS_LABEL[lesson.status] ?? lesson.status}
                </span>
              {/if}
            </li>
          {/each}
        </ul>
      </section>
    {/if}

    {#if pending.length === 0 && lessons.length === 0}
      <p style="color: {colors.mut};">Nenhuma aula importada ainda.</p>
    {/if}
  {/if}
</div>

{#if reviewing}
  <ImportConfirmModal pending={reviewing} {onConfirmed} onClose={closeReview} />
{/if}

<style>
  .screen {
    padding: 2rem;
    max-width: 64rem;
    margin: 0 auto;
    width: 100%;
  }
  .header-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 1rem;
  }
  h1 {
    font-size: 1.4rem;
    margin: 0;
  }
  button {
    padding: 0.5rem 1rem;
    border-radius: 0.5rem;
    cursor: pointer;
  }
  .hint {
    font-size: 0.85rem;
    margin: 0 0 1rem;
  }
  .error {
    font-size: 0.85rem;
    margin: 0 0 1rem;
  }
  .pending,
  .lessons,
  .filters {
    border-radius: 0.75rem;
    padding: 1rem 1.25rem;
    margin-bottom: 1rem;
  }
  .filters {
    display: flex;
    gap: 1.5rem;
    flex-wrap: wrap;
  }
  .filters label {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
    font-size: 0.8rem;
  }
  .pending h2,
  .lessons h2 {
    font-size: 1rem;
    margin: 0 0 0.75rem;
  }
  .pending ul,
  .lessons ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }
  .pending li {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
  }
  .lessons li {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
  }
  .lesson-main {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 0.25rem;
    background: none;
    border: none;
    padding: 0;
    text-align: left;
    flex: 1;
    min-width: 0;
  }
  .lessons .date {
    font-size: 0.85rem;
  }
  .lessons .tutor {
    font-size: 0.8rem;
  }
  .path {
    font-size: 0.8rem;
    word-break: break-all;
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
</style>
```

- [ ] **Step 2: Criar `frontend/src/lib/screens/LessonDetail.svelte`**

```svelte
<script lang="ts">
  import { onMount } from "svelte";
  import { colors, fonts } from "../theme";
  import * as LibraryService from "../../../bindings/assistente-idiomas/services/libraryservice";
  import type { Lesson } from "../../../bindings/assistente-idiomas/services/models";

  let { lessonId, onBack }: { lessonId: number; onBack: () => void } = $props();

  let lesson: Lesson | null = $state(null);
  let loading: boolean = $state(true);
  let error: string = $state("");

  function formatLessonDateTime(value: string): string {
    const [datePart, timePart] = value.split("T");
    const [year, month, day] = datePart.split("-");
    const formattedDate = `${day}/${month}/${year}`;
    return timePart ? `${formattedDate} ${timePart}` : formattedDate;
  }

  onMount(async () => {
    try {
      lesson = await LibraryService.GetLesson(lessonId);
    } catch (e) {
      error = String(e);
    } finally {
      loading = false;
    }
  });
</script>

<div class="screen" style="font-family: {fonts.body}; color: {colors.text};">
  <button class="back" onclick={onBack} style="color: {colors.mut};">← Biblioteca</button>

  {#if loading}
    <p style="color: {colors.mut};">Carregando…</p>
  {:else if error}
    <p class="error" style="color: {colors.red};">{error}</p>
  {:else if lesson}
    <h1 style="font-family: {fonts.display};">{formatLessonDateTime(lesson.lessonDate)}</h1>
    <p class="tutor" style="color: {colors.mut};">{lesson.tutor}</p>
    <!-- eslint-disable-next-line svelte/no-navigation-without-resolve -->
    <video controls src={`/media/lesson/${lesson.id}`}>
      <track kind="captions" />
    </video>
  {/if}
</div>

<style>
  .screen {
    padding: 2rem;
    max-width: 64rem;
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
  h1 {
    font-size: 1.4rem;
    margin: 0 0 0.25rem;
  }
  .tutor {
    font-size: 0.9rem;
    margin: 0 0 1.5rem;
  }
  video {
    width: 100%;
    border-radius: 0.75rem;
    background: black;
  }
  .error {
    font-size: 0.85rem;
  }
</style>
```

- [ ] **Step 3: Ligar a rota do Detalhe em `frontend/src/App.svelte`**

Reescrever o arquivo completo:

```svelte
<script lang="ts">
  import { onMount } from "svelte";
  import Sidebar from "./lib/Sidebar.svelte";
  import Header from "./lib/Header.svelte";
  import Library from "./lib/screens/Library.svelte";
  import LessonDetail from "./lib/screens/LessonDetail.svelte";
  import Progress from "./lib/screens/Progress.svelte";
  import Queue from "./lib/screens/Queue.svelte";
  import SetupWizard from "./lib/SetupWizard.svelte";
  import { colors, fonts } from "./lib/theme";
  import * as SetupService from "../bindings/assistente-idiomas/services/setupservice";

  type NavScreen = "library" | "progress" | "queue";
  type Route =
    | { screen: "library" }
    | { screen: "lesson-detail"; lessonId: number }
    | { screen: "progress" }
    | { screen: "queue" };

  let route: Route = $state({ screen: "library" });
  let checkingFirstRun = $state(true);
  let firstRun = $state(false);

  function navigate(screen: NavScreen) {
    route = { screen };
  }

  function openLesson(lessonId: number) {
    route = { screen: "lesson-detail", lessonId };
  }

  onMount(async () => {
    try {
      firstRun = await SetupService.IsFirstRun();
    } finally {
      checkingFirstRun = false;
    }
  });
</script>

{#if checkingFirstRun}
  <div class="shell" style="background: {colors.bg};"></div>
{:else if firstRun}
  <SetupWizard onComplete={() => (firstRun = false)} />
{:else}
  <div class="shell" style="background: {colors.bg}; font-family: {fonts.body};">
    <Sidebar active={route.screen === "lesson-detail" ? "library" : route.screen} onNavigate={navigate} />
    <main class="main">
      <Header />
      <div class="content">
        {#if route.screen === "library"}
          <Library onOpenLesson={openLesson} />
        {:else if route.screen === "lesson-detail"}
          <LessonDetail lessonId={route.lessonId} onBack={() => navigate("library")} />
        {:else if route.screen === "progress"}
          <Progress />
        {:else}
          <Queue />
        {/if}
      </div>
    </main>
  </div>
{/if}

<style>
  .shell {
    display: flex;
    width: 100%;
    height: 100vh;
  }
  .main {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .content {
    flex: 1;
    overflow-y: auto;
  }
</style>
```

- [ ] **Step 4: Type-check do frontend**

Run: `cd frontend && npm run check`
Expected: `0 ERRORS`. Se aparecer erro de tipo, o mais provável é o shape gerado na Task 6 ter ficado diferente do documentado em **Interfaces** — conferir `frontend/bindings/assistente-idiomas/services/models.ts` e `libraryservice.ts` contra o código acima e ajustar nomes/tipos, não a lógica.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/lib/screens/Library.svelte frontend/src/lib/screens/LessonDetail.svelte frontend/src/App.svelte
git commit -m "feat: filtro/status/duracao na Biblioteca e navegacao para o Detalhe"
```

---

### Task 9: Verificação final e registro de progresso

**Files:**
- Modify: `docs/fase-1-mvp.md`

- [ ] **Step 1: Suite Go completa**

Run: `go vet ./...`
Expected: sem saída (limpo).

Run: `go test ./...`
Expected: `ok` em todos os pacotes com testes (`internal/analysis`, `internal/config`, `internal/db`, `internal/importer`, `internal/jobs`, `internal/media`, `internal/stt`, `services`); `[no test files]` nos demais.

- [ ] **Step 2: Frontend**

Run: `cd frontend && npm run check`
Expected: `0 ERRORS`.

Run: `cd frontend && npm run build:dev`
Expected: build de desenvolvimento conclui sem erro (confirma que o Svelte/TS compila de ponta a ponta, incluindo os bindings regenerados).

- [ ] **Step 3: Build do binário**

Run: `go build ./internal/... ./services/... .`
Expected: sem erro. (Não usar `go build ./...`: `build/ios` já falha antes desta história por não ser um alvo de build válido nesta plataforma — pré-existente, fora de escopo.)

- [ ] **Step 4: Atualizar `docs/fase-1-mvp.md`**

Marcar os critérios de aceite da História 5 (seção `## História 5 — Biblioteca real`) como concluídos, preservando a nota de verificação visual pendente (mesmo padrão das Histórias 1, 3 e 4):

```markdown
### Critérios de aceite
- [x] Lista real do banco: data, tutor, duração, status (processando/pronta/erro), no layout do protótipo.
- [x] Filtro simples por tutor e período (busca full-text fica para fase futura).
- [x] Aula `pronta` abre o Detalhe (stub mínimo: data/tutor/vídeo, sem transcrição — a sincronização é da História 6); `processando` mostra estado; `erro` mostra mensagem e ação de reprocessar (recriar job).
```

Adicionar uma linha à tabela `## Registro de progresso` (ao final do arquivo):

```markdown
| 22/07/2026 | História 5 implementada: status da Biblioteca derivado dos jobs (extract_audio/transcribe) a cada leitura — nunca uma coluna gravada à parte; duração calculada via ffprobe em melhor esforço na confirmação da importação (nunca bloqueia a confirmação); filtro por tutor (dropdown)/período; botão "Reprocessar" reseta os jobs em erro da aula (inclusive o transcribe bloqueado por dependência) sem acordar o worker explicitamente (poll de fallback); Detalhe mínimo (data/tutor/vídeo) resolve o risco técnico 1 do projeto — endpoint `GET /media/lesson/{id}` via `http.ServeFile` da stdlib, que já trata range requests, plugado como `application.Middleware` do Wails v3 | Risco 1 resolvido de verdade (não um placeholder): confirmado lendo o código-fonte do Wails v3 que o webview sempre fala com o servidor Go, tanto em produção (assets embutidos) quanto em `wails3 dev` (proxy pro Vite) — o middleware intercepta antes de qualquer um dos dois; a História 6 reaproveita o mesmo endpoint, só adicionando a transcrição sincronizada; bindings do frontend precisam da flag `-i` além de `-ts` (`wails3 generate bindings -ts -i ./...`) pra gerar interfaces em vez de classes — sem isso o gerador produz `.js` com classes, formato que o frontend deste projeto não usa; fluxo completo (Biblioteca → Detalhe → vídeo tocando) ainda não verificado visualmente numa janela real (sem display neste ambiente de build), mesmo padrão das histórias anteriores |
```

- [ ] **Step 5: Commit**

```bash
git add docs/fase-1-mvp.md
git commit -m "docs: marca Historia 5 concluida e registra progresso"
```

---

## Nota final pro executor

Depois da Task 9, rodar o app de verdade (`wails3 dev` numa máquina com display) pra confirmar visualmente: Biblioteca mostrando status/duração/filtro, botão "Reprocessar" numa aula com erro proposital (ex.: apagar `ffmpeg` do PATH antes de confirmar uma importação), e o Detalhe reproduzindo o vídeo com seek funcionando (arrastar a barra de progresso) — isso fecha a verificação visual pendente desde a História 1, específica desta fatia.
