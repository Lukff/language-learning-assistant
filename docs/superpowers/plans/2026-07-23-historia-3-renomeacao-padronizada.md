# História 3 — Renomeação padronizada do arquivo ao confirmar: Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Depois que o usuário confirma data/horário/tutor no modal de importação, o arquivo de
vídeo é renomeado *in place* para um nome padronizado (`AAAA-MM-DD_HHHMM_tutor-slug.ext`), e a
confirmação passa a exigir horário (não só data) como obrigatório.

**Architecture:** Uma função pura nova `internal/importer.StandardFilename` computa o nome
padronizado a partir de `lessonDate`/`tutor`/extensão. `services/import.go` ganha (a) uma
validação mais estrita em `ConfirmImport` (horário obrigatório, não só não-vazio) e (b) um passo
best-effort `renameVideoBestEffort`, chamado depois que a lesson já foi confirmada com sucesso,
que resolve colisão de nome no disco, executa `os.Rename` e atualiza `lessons.video_path` via
`db.UpdateLessonPath` (função já existente, reaproveitada). Nenhuma mudança em
`internal/jobs/worker.go` — ele sempre relê `video_path` do banco em tempo de execução.

**Tech Stack:** Go stdlib (`path/filepath`, `os`, `strings`, `regexp`, `fmt`, `time`) — sem
dependência nova.

## Global Constraints

- Nenhuma dependência externa nova (`CLAUDE.md`): acentos removidos via tabela de substituição
  manual em Go puro, não uma lib de transliteração.
- SQL portável na camada de repositório — esta fatia não adiciona SQL novo (reaproveita
  `db.UpdateLessonPath`, já existente).
- Falha no rename nunca pode derrubar `ConfirmImport` nem deixar a lesson em estado inconsistente
  (princípio de resiliência do `CLAUDE.md`) — sempre best-effort, só logado via `slog.Warn`.
- Formato do nome: `AAAA-MM-DD_HHHMM_tutor-slug.ext` (ex.: `2026-07-23_14H30_maria-jose.mp4`) —
  `H` como separador de hora/minuto, `_` entre data e hora e entre hora e slug, sem fallback
  "sem horário" (validação upstream garante horário sempre presente).
- Colisão de nome: sufixo `-2`, `-3`, ... antes da extensão.
- Rename é sempre *in place* (mesma pasta) — nunca move pra estrutura de subpastas.

---

### Task 1: `internal/importer.StandardFilename` (função pura)

**Files:**
- Create: `internal/importer/naming.go`
- Test: `internal/importer/naming_test.go`

**Interfaces:**
- Produces: `func StandardFilename(lessonDate, tutor, ext string) string` — usada pela Task 3
  (`services/import.go`). `lessonDate` é sempre `"AAAA-MM-DDTHH:MM"` (garantido pela validação da
  Task 2 antes de qualquer chamador usar esta função em produção); `ext` é a extensão com ponto
  (ex.: `".mp4"`, como retornado por `filepath.Ext`).

- [ ] **Step 1: Escrever o teste que falha**

Criar `internal/importer/naming_test.go`:

```go
package importer

import "testing"

func TestStandardFilename(t *testing.T) {
	tests := []struct {
		name       string
		lessonDate string
		tutor      string
		ext        string
		want       string
	}{
		{
			name:       "data, hora e tutor simples",
			lessonDate: "2026-07-23T14:30",
			tutor:      "Maria José",
			ext:        ".mp4",
			want:       "2026-07-23_14H30_maria-jose.mp4",
		},
		{
			name:       "tutor com ponto e espaço",
			lessonDate: "2026-07-15T09:05",
			tutor:      "Sarah M.",
			ext:        ".mp4",
			want:       "2026-07-15_09H05_sarah-m.mp4",
		},
		{
			name:       "extensão em maiúsculas normalizada",
			lessonDate: "2026-07-15T09:05",
			tutor:      "Sarah",
			ext:        ".MP4",
			want:       "2026-07-15_09H05_sarah.mp4",
		},
		{
			name:       "tutor com múltiplos espaços e acentos variados",
			lessonDate: "2026-01-05T23:59",
			tutor:      "  João  Ñandú  ",
			ext:        ".mp4",
			want:       "2026-01-05_23H59_joao-nandu.mp4",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StandardFilename(tt.lessonDate, tt.tutor, tt.ext)
			if got != tt.want {
				t.Errorf("StandardFilename(%q, %q, %q) = %q, want %q", tt.lessonDate, tt.tutor, tt.ext, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Rodar o teste e confirmar que falha**

Run: `go test ./internal/importer/... -run TestStandardFilename -v`
Expected: FAIL — `undefined: StandardFilename` (função ainda não existe).

- [ ] **Step 3: Implementar `StandardFilename`**

Criar `internal/importer/naming.go`:

```go
// Package importer — ver importer.go. Este arquivo cobre a padronização do
// nome do arquivo de vídeo pós-confirmação (História 3, critério adicional
// registrado em docs/fase-1-mvp.md e desenhado em
// docs/superpowers/specs/2026-07-23-historia-3-renomeacao-padronizada-design.md).
package importer

import (
	"fmt"
	"regexp"
	"strings"
)

// accentReplacer remove os acentos mais comuns em nomes próprios PT/ES —
// evita depender de uma lib de transliteração só pra isso (CLAUDE.md: sem
// dependência nova sem justificativa). Espera-se que a entrada já esteja em
// minúsculas (strings.ToLower já normaliza a maioria das formas
// maiúsculas/acentuadas correspondentes).
var accentReplacer = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a",
	"é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i",
	"ó", "o", "ò", "o", "ô", "o", "õ", "o", "ö", "o",
	"ú", "u", "ù", "u", "û", "u", "ü", "u",
	"ç", "c", "ñ", "n", "ý", "y",
)

var nonSlugRun = regexp.MustCompile(`[^a-z0-9]+`)

// slugify normaliza um nome de tutor pra uso em nome de arquivo: minúsculas,
// sem acento, qualquer sequência de caracteres fora de [a-z0-9] vira um
// único "-", sem "-" nas pontas.
func slugify(s string) string {
	s = strings.ToLower(s)
	s = accentReplacer.Replace(s)
	s = nonSlugRun.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// StandardFilename deriva o nome de arquivo padronizado pós-confirmação, no
// formato "AAAA-MM-DD_HHHMM_tutor-slug.ext" (ex.: "2026-07-23_14H30_maria-jose.mp4").
// lessonDate é o valor bruto de <input type="datetime-local">
// ("AAAA-MM-DDTHH:MM"); o chamador (services/import.go ConfirmImport) já
// garante esse formato antes de chamar esta função — não há fallback aqui
// para data sem horário. ext inclui o ponto (ex.: ".mp4"), como retornado
// por filepath.Ext, e é normalizada para minúsculas.
func StandardFilename(lessonDate, tutor, ext string) string {
	datePart, timePart, _ := strings.Cut(lessonDate, "T")
	timePart = strings.ReplaceAll(timePart, ":", "H")
	slug := slugify(tutor)
	return fmt.Sprintf("%s_%s_%s%s", datePart, timePart, slug, strings.ToLower(ext))
}
```

- [ ] **Step 4: Rodar o teste e confirmar que passa**

Run: `go test ./internal/importer/... -run TestStandardFilename -v`
Expected: PASS em todos os subtestes.

- [ ] **Step 5: Rodar `go vet` e o pacote inteiro**

Run: `go vet ./internal/importer/... && go test ./internal/importer/... -v`
Expected: `go vet` sem saída; todos os testes do pacote (incluindo os já existentes de `Scan`)
passando.

- [ ] **Step 6: Commit**

```bash
git add internal/importer/naming.go internal/importer/naming_test.go
git commit -m "feat: adiciona StandardFilename para nome padronizado de vídeo"
```

---

### Task 2: Validação de horário obrigatório em `ConfirmImport`

**Files:**
- Modify: `services/import.go:77-90` (`ConfirmImport`)
- Modify: `services/import_test.go` — ajustar `TestImportService_ConfirmImport_SucceedsEvenWhenDurationProbeFails` (usava data sem horário) e adicionar teste novo de rejeição.

**Interfaces:**
- Consumes: nada de tasks anteriores ainda (esta task não usa `StandardFilename`).
- Produces: `ConfirmImport` agora retorna erro (`"horário da aula é obrigatório"`) quando
  `lessonDate` não tem componente de horário. Task 3 depende deste comportamento já estar em
  vigor antes de conectar o rename (senão `renameVideoBestEffort` teria que lidar com o caso
  "sem horário", que o design explicitamente evita).

- [ ] **Step 1: Escrever o teste que falha**

Em `services/import_test.go`, adicionar (após `TestImportService_ConfirmImport_RejectsEmptyTutorOrDate`):

```go
func TestImportService_ConfirmImport_RejectsDateWithoutTime(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	storageRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(storageRoot, "aula.mp4"), []byte("conteudo"), 0o644); err != nil {
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

	if err := svc.ConfirmImport(pending[0].ID, "2026-07-15", "Sarah M."); err == nil {
		t.Error("ConfirmImport() com data sem horário esperava erro, veio nil")
	}

	pending, err = svc.ListPendingImports()
	if err != nil {
		t.Fatalf("ListPendingImports() erro inesperado: %v", err)
	}
	if len(pending) != 1 {
		t.Errorf("candidato deveria continuar pendente após confirmação recusada, ListPendingImports() = %+v", pending)
	}

	var count int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM lessons`).Scan(&count); err != nil {
		t.Fatalf("count de lessons falhou: %v", err)
	}
	if count != 0 {
		t.Errorf("nenhuma lesson deveria ter sido criada, count = %d", count)
	}
}
```

Também ajustar a chamada existente em `TestImportService_ConfirmImport_SucceedsEvenWhenDurationProbeFails`
(`services/import_test.go:150`), que hoje usa `"2026-07-22"` (sem horário) — passa a falhar com a
validação nova se não for ajustada:

```go
	if err := svc.ConfirmImport(pending[0].ID, "2026-07-22", "Sarah M."); err != nil {
```
vira:
```go
	if err := svc.ConfirmImport(pending[0].ID, "2026-07-22T09:00", "Sarah M."); err != nil {
```

- [ ] **Step 2: Rodar os testes e confirmar que falham**

Run: `go test ./services/... -run TestImportService_ConfirmImport -v`
Expected: `TestImportService_ConfirmImport_RejectsDateWithoutTime` FAIL (erro esperado, veio nil —
`ConfirmImport` ainda não valida horário). Os demais testes de `ConfirmImport` continuam
passando (o ajuste do Step 1 já os deixou compatíveis).

- [ ] **Step 3: Implementar a validação**

Em `services/import.go`, modificar `ConfirmImport` (linhas 77-90):

```go
func (s *ImportService) ConfirmImport(id int64, lessonDate string, tutor string) error {
	if lessonDate == "" {
		return fmt.Errorf("data da aula não pode ser vazia")
	}
	if !hasTimeComponent(lessonDate) {
		return fmt.Errorf("horário da aula é obrigatório")
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

// hasTimeComponent indica se lessonDate (formato de <input type="datetime-local">,
// "AAAA-MM-DDTHH:MM") tem um componente de horário não vazio depois do "T".
// ConfirmImport exige isso porque o nome padronizado do arquivo
// (StandardFilename, internal/importer) depende de sempre haver horário —
// ver docs/superpowers/specs/2026-07-23-historia-3-renomeacao-padronizada-design.md.
func hasTimeComponent(lessonDate string) bool {
	_, timePart, found := strings.Cut(lessonDate, "T")
	return found && timePart != ""
}
```

Adicionar `"strings"` ao bloco de imports de `services/import.go` (ainda não importado nesse
arquivo).

- [ ] **Step 4: Rodar os testes e confirmar que passam**

Run: `go test ./services/... -run TestImportService_ConfirmImport -v`
Expected: PASS em todos, incluindo `TestImportService_ConfirmImport_RejectsDateWithoutTime`.

- [ ] **Step 5: Rodar o pacote inteiro e `go vet`**

Run: `go vet ./services/... && go test ./services/... -v`
Expected: sem saída do `go vet`; todos os testes do pacote passando (nenhuma regressão nos
testes de `ScanFolder`/fila/biblioteca que já existiam).

- [ ] **Step 6: Commit**

```bash
git add services/import.go services/import_test.go
git commit -m "feat: exige horário na confirmação da importação"
```

---

### Task 3: Rename best-effort do vídeo ao confirmar

**Files:**
- Modify: `services/import.go` (adicionar `renameVideoBestEffort` e chamá-la em `ConfirmImport`)
- Modify: `services/import_test.go` (novo teste de sucesso)

**Interfaces:**
- Consumes: `importer.StandardFilename(lessonDate, tutor, ext string) string` (Task 1);
  `db.FindLessonByID(conn *sql.DB, id int64) (*db.Lesson, error)` e
  `db.UpdateLessonPath(conn *sql.DB, lessonID int64, path string, size int64, fileMTime string) error`
  (já existentes em `internal/db/lessons.go`); `hasTimeComponent` (Task 2, garante que
  `lesson.LessonDate` sempre tem horário quando este código roda).
- Produces: `func (s *ImportService) renameVideoBestEffort(lessonID int64)` — chamada só
  internamente por `ConfirmImport`, sem retorno (best-effort, erros só logados). A Task 4
  (colisão) e a Task 5 (falha de rename) estendem o comportamento desta mesma função — nenhuma
  mudança de assinatura esperada nelas.

- [ ] **Step 1: Escrever o teste que falha**

Em `services/import_test.go`, adicionar:

```go
func TestImportService_ConfirmImport_RenamesVideoToStandardFilename(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	storageRoot := t.TempDir()
	originalName := "cambly-download-xyz.mp4"
	if err := os.WriteFile(filepath.Join(storageRoot, originalName), []byte("conteudo-de-video"), 0o644); err != nil {
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

	if err := svc.ConfirmImport(pending[0].ID, "2026-07-23T14:30", "Maria José"); err != nil {
		t.Fatalf("ConfirmImport() erro inesperado: %v", err)
	}

	wantPath := "2026-07-23_14H30_maria-jose.mp4"
	lesson, err := db.FindLessonByPath(conn, wantPath)
	if err != nil {
		t.Fatalf("FindLessonByPath() erro inesperado: %v", err)
	}
	if lesson == nil {
		t.Fatalf("lesson não encontrada no path padronizado %q — video_path não foi atualizado", wantPath)
	}

	if _, err := os.Stat(filepath.Join(storageRoot, wantPath)); err != nil {
		t.Errorf("arquivo renomeado não existe no disco em %q: %v", wantPath, err)
	}
	if _, err := os.Stat(filepath.Join(storageRoot, originalName)); !os.IsNotExist(err) {
		t.Errorf("arquivo original %q ainda existe no disco após rename (err=%v)", originalName, err)
	}
}
```

- [ ] **Step 2: Rodar o teste e confirmar que falha**

Run: `go test ./services/... -run TestImportService_ConfirmImport_RenamesVideoToStandardFilename -v`
Expected: FAIL — `lesson não encontrada no path padronizado` (o rename ainda não acontece).

- [ ] **Step 3: Implementar `renameVideoBestEffort` e conectar em `ConfirmImport`**

Em `services/import.go`, adicionar a chamada em `ConfirmImport` (logo após
`s.setDurationBestEffort(lessonID)`):

```go
	s.setDurationBestEffort(lessonID)
	s.renameVideoBestEffort(lessonID)
	return nil
}
```

E adicionar a função nova (após `setDurationBestEffort`):

```go
// renameVideoBestEffort renomeia o vídeo recém-confirmado pro nome
// padronizado (internal/importer.StandardFilename), sempre na mesma pasta
// (nunca move de diretório). É melhor esforço, no mesmo espírito de
// setDurationBestEffort: qualquer falha (permissão, I/O, colisão
// irresolúvel) é só logada — o nome do arquivo é cosmético, nunca crítico
// pro funcionamento do app (princípio de resiliência, CLAUDE.md). Ver
// docs/superpowers/specs/2026-07-23-historia-3-renomeacao-padronizada-design.md.
func (s *ImportService) renameVideoBestEffort(lessonID int64) {
	lesson, err := db.FindLessonByID(s.conn, lessonID)
	if err != nil || lesson == nil {
		return
	}
	cfg, err := config.Load()
	if err != nil {
		return
	}

	relDir := filepath.Dir(filepath.FromSlash(lesson.VideoPath))
	ext := filepath.Ext(lesson.VideoPath)
	targetName := importer.StandardFilename(lesson.LessonDate, lesson.Tutor, ext)

	currentAbsPath := filepath.Join(cfg.StorageRoot, filepath.FromSlash(lesson.VideoPath))
	targetAbsDir := filepath.Join(cfg.StorageRoot, relDir)

	candidate := targetName
	for i := 2; ; i++ {
		candidateAbsPath := filepath.Join(targetAbsDir, candidate)
		if candidateAbsPath == currentAbsPath {
			break // já tem esse nome — nada a fazer
		}
		if _, err := os.Stat(candidateAbsPath); os.IsNotExist(err) {
			break // nome livre
		} else if err != nil {
			slog.Warn("importer: erro ao checar colisão de nome padronizado", "lesson_id", lessonID, "erro", err)
			return
		}
		base := strings.TrimSuffix(targetName, ext)
		candidate = fmt.Sprintf("%s-%d%s", base, i, ext)
	}

	targetAbsPath := filepath.Join(targetAbsDir, candidate)
	if targetAbsPath == currentAbsPath {
		return
	}

	if err := os.Rename(currentAbsPath, targetAbsPath); err != nil {
		slog.Warn("importer: não foi possível renomear o vídeo pro nome padronizado", "lesson_id", lessonID, "erro", err)
		return
	}

	info, err := os.Stat(targetAbsPath)
	if err != nil {
		slog.Warn("importer: não foi possível reler o vídeo após renomear", "lesson_id", lessonID, "erro", err)
		return
	}
	targetRelPath := filepath.ToSlash(filepath.Join(relDir, candidate))
	mtime := info.ModTime().UTC().Format(time.RFC3339)
	if err := db.UpdateLessonPath(s.conn, lessonID, targetRelPath, info.Size(), mtime); err != nil {
		slog.Warn("importer: não foi possível atualizar o path da lesson após renomear", "lesson_id", lessonID, "erro", err)
	}
}
```

Adicionar `"os"`, `"strings"` e `"time"` ao bloco de imports de `services/import.go` (`"strings"`
já foi adicionado na Task 2; `"os"` e `"time"` são novos nesse arquivo — confirmar se já não
estão importados antes de duplicar).

- [ ] **Step 4: Rodar o teste e confirmar que passa**

Run: `go test ./services/... -run TestImportService_ConfirmImport_RenamesVideoToStandardFilename -v`
Expected: PASS.

- [ ] **Step 5: Rodar o pacote inteiro e `go vet`**

Run: `go vet ./services/... && go test ./services/... -v`
Expected: sem saída do `go vet`; todos os testes do pacote passando, incluindo
`TestImportService_ScanFolderThenListThenConfirm` (que agora também aciona o rename, mas não faz
asserção sobre o path final — deve continuar passando sem alteração).

- [ ] **Step 6: Commit**

```bash
git add services/import.go services/import_test.go
git commit -m "feat: renomeia vídeo pro nome padronizado ao confirmar importação"
```

---

### Task 4: Colisão de nome (sufixo `-2`)

**Files:**
- Modify: `services/import_test.go` (novo teste)

**Interfaces:**
- Consumes: `renameVideoBestEffort` (Task 3, sem mudança de assinatura — o loop de colisão já
  implementado na Task 3 é exatamente o que este teste valida).
- Produces: nenhuma interface nova — task de verificação/cobertura sobre comportamento já
  implementado na Task 3.

- [ ] **Step 1: Escrever o teste que falha (se a Task 3 tiver algum bug de colisão)**

Em `services/import_test.go`, adicionar:

```go
func TestImportService_ConfirmImport_ResolvesFilenameCollisionWithSuffix(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	storageRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(storageRoot, "a.mp4"), []byte("conteudo-a"), 0o644); err != nil {
		t.Fatalf("preparar vídeo a.mp4 falhou: %v", err)
	}
	if err := os.WriteFile(filepath.Join(storageRoot, "b.mp4"), []byte("conteudo-b-bem-diferente"), 0o644); err != nil {
		t.Fatalf("preparar vídeo b.mp4 falhou: %v", err)
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
	if err != nil || len(pending) != 2 {
		t.Fatalf("setup: ListPendingImports() = %+v, %v", pending, err)
	}

	var idA, idB int64
	for _, p := range pending {
		switch p.Path {
		case "a.mp4":
			idA = p.ID
		case "b.mp4":
			idB = p.ID
		}
	}
	if idA == 0 || idB == 0 {
		t.Fatalf("não achei os dois candidatos esperados (a.mp4/b.mp4) em %+v", pending)
	}

	// Mesma data/horário/tutor pras duas aulas — mesmo nome-alvo, força colisão.
	if err := svc.ConfirmImport(idA, "2026-07-23T14:30", "Maria José"); err != nil {
		t.Fatalf("ConfirmImport(a) erro inesperado: %v", err)
	}
	if err := svc.ConfirmImport(idB, "2026-07-23T14:30", "Maria José"); err != nil {
		t.Fatalf("ConfirmImport(b) erro inesperado: %v", err)
	}

	if _, err := os.Stat(filepath.Join(storageRoot, "2026-07-23_14H30_maria-jose.mp4")); err != nil {
		t.Errorf("primeira aula deveria ter o nome base, sem sufixo: %v", err)
	}
	if _, err := os.Stat(filepath.Join(storageRoot, "2026-07-23_14H30_maria-jose-2.mp4")); err != nil {
		t.Errorf("segunda aula deveria ter o sufixo -2: %v", err)
	}
}
```

- [ ] **Step 2: Rodar o teste**

Run: `go test ./services/... -run TestImportService_ConfirmImport_ResolvesFilenameCollisionWithSuffix -v`
Expected: PASS (a lógica de colisão já foi implementada na Task 3 — este teste é a cobertura
dela; se falhar, revisar o loop de `renameVideoBestEffort` antes de prosseguir).

- [ ] **Step 3: Rodar o pacote inteiro**

Run: `go test ./services/... -v`
Expected: todos os testes passando, nenhuma regressão.

- [ ] **Step 4: Commit**

```bash
git add services/import_test.go
git commit -m "test: cobre colisão de nome padronizado com sufixo -2"
```

---

### Task 5: Falha no rename não derruba a confirmação

**Files:**
- Modify: `services/import_test.go` (novo teste)

**Interfaces:**
- Consumes: `renameVideoBestEffort` (Task 3) — este teste valida o caminho de erro (best-effort)
  já implementado nela.
- Produces: nenhuma interface nova — cobertura do comportamento de resiliência.

- [ ] **Step 1: Escrever o teste**

Em `services/import_test.go`, adicionar:

```go
func TestImportService_ConfirmImport_SucceedsEvenWhenRenameFails(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	storageRoot := t.TempDir()
	originalName := "aula-original.mp4"
	if err := os.WriteFile(filepath.Join(storageRoot, originalName), []byte("conteudo"), 0o644); err != nil {
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

	// Remove a permissão de escrita da pasta de armazenamento — os.Rename não
	// consegue criar/remover entradas de diretório sem ela, forçando a
	// renomeação a falhar de um jeito real (não simulado), sem depender de
	// rodar como root (que ignoraria a permissão).
	if err := os.Chmod(storageRoot, 0o555); err != nil {
		t.Fatalf("chmod da pasta de armazenamento falhou: %v", err)
	}
	defer os.Chmod(storageRoot, 0o755) // permite o t.TempDir() limpar depois

	if err := svc.ConfirmImport(pending[0].ID, "2026-07-23T14:30", "Maria José"); err != nil {
		t.Fatalf("ConfirmImport() não deveria falhar mesmo com rename impossível: %v", err)
	}

	lesson, err := db.FindLessonByPath(conn, originalName)
	if err != nil {
		t.Fatalf("FindLessonByPath() erro inesperado: %v", err)
	}
	if lesson == nil {
		t.Fatal("lesson deveria ter sido confirmada com o path original, já que o rename falhou")
	}
}
```

- [ ] **Step 2: Rodar o teste**

Run: `go test ./services/... -run TestImportService_ConfirmImport_SucceedsEvenWhenRenameFails -v`
Expected: PASS. Se falhar porque o teste está rodando como root (permissão ignorada), rodar
`whoami`/`id` pra confirmar — o ambiente de desenvolvimento deste projeto não roda os testes como
root (`agent`, uid 1000), então isso não deveria acontecer aqui; se acontecer num ambiente
diferente, é um sinal de que o teste precisa de uma estratégia diferente de forçar falha (fora do
escopo desta fatia resolver isso agora).

- [ ] **Step 3: Rodar a suíte completa do projeto**

Run: `go vet ./... && go test ./... -count=1`
Expected: `go vet` sem saída; todos os pacotes `ok`, nenhuma regressão em nenhum outro pacote
(worker de jobs, biblioteca, fila, etc.).

- [ ] **Step 4: Marcar o critério de aceite em `docs/fase-1-mvp.md`**

Em `docs/fase-1-mvp.md`, na seção da História 3, trocar:

```markdown
- [ ] Depois de confirmado (data/horário/tutor no modal), o arquivo de vídeo é renomeado *in place* para `AAAA-MM-DD_HHHMM_tutor-slug.ext` (ex.: `2026-07-23_14H30_maria-jose.mp4`); falha no rename não impede a confirmação (best-effort, logada). Horário passa a ser obrigatório na confirmação — candidato sem data, horário ou tutor continua pendente. Design em `docs/superpowers/specs/2026-07-23-historia-3-renomeacao-padronizada-design.md`.
```

por:

```markdown
- [x] Depois de confirmado (data/horário/tutor no modal), o arquivo de vídeo é renomeado *in place* para `AAAA-MM-DD_HHHMM_tutor-slug.ext` (ex.: `2026-07-23_14H30_maria-jose.mp4`); falha no rename não impede a confirmação (best-effort, logada). Horário passa a ser obrigatório na confirmação — candidato sem data, horário ou tutor continua pendente. Design em `docs/superpowers/specs/2026-07-23-historia-3-renomeacao-padronizada-design.md`.
```

Também adicionar uma linha na tabela "Registro de progresso" (final do arquivo), seguindo o
padrão das demais linhas (data de hoje, o que foi feito, observações relevantes — ex.: mencionar
que é uma fatia adicional sobre um fluxo já entregue, e a ausência de verificação visual real
segue o mesmo padrão das histórias anteriores se aplicável).

- [ ] **Step 5: Commit**

```bash
git add services/import_test.go docs/fase-1-mvp.md
git commit -m "test: cobre resiliência da confirmação a falha de rename e fecha critério da História 3"
```

---

## Self-Review

**Cobertura do spec:** formato do nome (Task 1), slug de tutor com acentos (Task 1), colisão com
sufixo (Task 4), horário obrigatório / candidato continua pendente (Task 2), rename best-effort
sem quebrar confirmação (Task 3 e 5), nenhuma mudança em `internal/jobs/worker.go` (confirmado
nas notas de arquitetura do spec — não há task pra isso porque não há mudança de código
necessária lá). Todos os itens do "Fora de escopo" do spec (subpastas, retroatividade, História
3b) não geram tasks, como esperado.

**Placeholders:** nenhum "TBD"/"implementar depois" — todo código é completo e executável como
escrito.

**Consistência de tipos:** `StandardFilename(lessonDate, tutor, ext string) string` (Task 1) é
usada em `renameVideoBestEffort` (Task 3) com os mesmos três `string` na mesma ordem;
`hasTimeComponent(lessonDate string) bool` (Task 2) e `renameVideoBestEffort(lessonID int64)`
(Task 3) não têm conflito de nome com nada existente em `services/import.go` (conferido contra o
arquivo atual).
