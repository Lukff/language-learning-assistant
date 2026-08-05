# Fase 2, História 2 — Piloto: Correções do aluno — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Disparar `analyze_corrections` sob demanda numa aula pronta e mostrar as correções do aluno inline na transcrição do Detalhe — primeira tarefa de análise da Fase 2 a rodar em uso real, servindo de piloto pra decidir se ela (e a abordagem) valem a pena manter.

**Architecture:** Camada fina de sempre: `internal/analysis` (framework de tarefas, já existe da História 1) e `internal/config` (credencial) não sabem nada de Wails. `services/analysis.go` é a casca nova que orquestra (busca transcrição + papel do falante → `analysis.FormatTranscript` → `analysis.NewCorrectionsTask().Execute` → persiste em `analysis_results` → `analysis.MatchCorrections` casa cada correção com o texto real da fala) e converte pra DTOs locais ao pacote `services` (mesmo padrão já usado por `Transcript`/`Utterance` em `services/library.go` — `internal/` nunca vaza direto pro binding do frontend). `LessonDetail.svelte` chama a análise sob demanda (botão) e renderiza os pedaços já separados pelo backend, sem lógica de casamento de string no frontend. A escolha de quem é o aluno sai do painel de transcrição e passa a viver dentro de `EditLessonModal`, porque mudar esse mapeamento invalida qualquer análise já feita (`services.LibraryService.SetStudentSpeaker` descarta `analysis_results` da aula quando o falante muda).

**Tech Stack:** Go 1.25.7, `modernc.org/sqlite` v1.54.0, `github.com/zalando/go-keyring` v0.2.8, Wails v3 alpha2.117 (bindings Go↔TS geradas por `wails3 generate bindings -ts -i ./...`), Svelte 5 (runes).

## Global Constraints

- Camada fina: `internal/` nunca importa Wails; `services/` é a única camada que conhece Wails e expõe DTOs locais (nunca tipos de `internal/` direto) pro frontend — mesmo padrão de `services.Transcript`/`services.Utterance` em `services/library.go`.
- Credenciais nunca em texto plano — sempre via `go-keyring` (`internal/config`).
- SQL portável na camada de repositório (`internal/db`) — nada específico de driver.
- Svelte 5 com runes (`$state`, `$derived`, `$props`, `$effect`) — nunca sintaxe legada (`export let`, `$:`).
- Falha de análise (rede/API, parsing, escrita do raw em disco) nunca impede assistir ao vídeo nem quebra a transcrição já carregada.
- `analysis_results` é `UNIQUE(lesson_id, task)` — sobrescrever é sempre um `UPSERT` (`UpsertAnalysisResult`), nunca um `INSERT` cru.
- Bindings do frontend exigem as flags `-ts -i` (`wails3 generate bindings -ts -i ./...`) — sem `-i` o gerador produz classes `.js`, não as interfaces TS que o frontend consome. `frontend/bindings/` é gitignored — regenerar localmente antes de qualquer tarefa de frontend que dependa de tipos/métodos novos.
- Mensagens de commit: uma linha só, formato `tipo: descrição` (`feat`, `fix`, `docs`, `refactor`, `test`, `chore`).

---

### Task 1: `internal/config` — credencial do provedor de análise (DeepSeek)

**Contexto:** Mesmo padrão de `SaveSTTAPIKey`/`GetSTTAPIKey` (`internal/config/credentials.go`), um novo par de funções pra credencial da DeepSeek, usando um `keyringUser` diferente no mesmo `keyringService`.

**Files:**
- Modify: `internal/config/credentials.go`
- Test: `internal/config/credentials_test.go`

**Interfaces:**
- Produces: `config.SaveAnalysisAPIKey(apiKey string) error`, `config.GetAnalysisAPIKey() (string, error)` — usados pela Task 6 (`SettingsService`) e pela Task 9 (`main.go`, provider factory do `AnalysisService`).

- [ ] **Step 1: Escrever o teste falho (round-trip + independência da credencial STT)**

Adicionar em `internal/config/credentials_test.go`:

```go
func TestSaveThenGetAnalysisAPIKey_RoundTrips(t *testing.T) {
	keyring.MockInit()

	if err := SaveAnalysisAPIKey("sk-deepseek-test"); err != nil {
		t.Fatalf("SaveAnalysisAPIKey() erro inesperado: %v", err)
	}

	got, err := GetAnalysisAPIKey()
	if err != nil {
		t.Fatalf("GetAnalysisAPIKey() erro inesperado: %v", err)
	}
	if got != "sk-deepseek-test" {
		t.Errorf("GetAnalysisAPIKey() = %q, esperado \"sk-deepseek-test\"", got)
	}
}

func TestSaveAnalysisAPIKey_KeyringUnavailablePropagatesError(t *testing.T) {
	sentinel := errors.New("secret service indisponível")
	keyring.MockInitWithError(sentinel)
	t.Cleanup(keyring.MockInit)

	err := SaveAnalysisAPIKey("sk-deepseek-test")
	if !errors.Is(err, sentinel) {
		t.Errorf("SaveAnalysisAPIKey() erro = %v, esperado envolver %v", err, sentinel)
	}
}

func TestSaveSTTAndAnalysisAPIKeys_AreIndependent(t *testing.T) {
	keyring.MockInit()

	if err := SaveSTTAPIKey("sk-stt"); err != nil {
		t.Fatalf("SaveSTTAPIKey() erro inesperado: %v", err)
	}
	if err := SaveAnalysisAPIKey("sk-analysis"); err != nil {
		t.Fatalf("SaveAnalysisAPIKey() erro inesperado: %v", err)
	}

	stt, err := GetSTTAPIKey()
	if err != nil {
		t.Fatalf("GetSTTAPIKey() erro inesperado: %v", err)
	}
	if stt != "sk-stt" {
		t.Errorf("GetSTTAPIKey() = %q, esperado \"sk-stt\" (não deve ser sobrescrita pela credencial de análise)", stt)
	}
}
```

- [ ] **Step 2: Rodar e confirmar que falha**

Run: `go test ./internal/config/... -run 'TestSaveThenGetAnalysisAPIKey|TestSaveAnalysisAPIKey|TestSaveSTTAndAnalysisAPIKeys' -v`
Expected: FAIL com `undefined: SaveAnalysisAPIKey`.

- [ ] **Step 3: Implementar `SaveAnalysisAPIKey`/`GetAnalysisAPIKey`**

Em `internal/config/credentials.go`, ajustar o bloco de constantes e adicionar as duas funções (mesmo formato de `SaveSTTAPIKey`/`GetSTTAPIKey`):

```go
const (
	keyringService        = "assistente-idiomas"
	keyringUserElevenLabs = "elevenlabs"
	keyringUserDeepSeek   = "deepseek"
)
```

```go
// SaveAnalysisAPIKey grava a API key da DeepSeek no gerenciador de
// credenciais nativo do SO, via go-keyring. Nunca em texto plano.
func SaveAnalysisAPIKey(apiKey string) error {
	if err := keyring.Set(keyringService, keyringUserDeepSeek, apiKey); err != nil {
		return fmt.Errorf("gravar credencial no gerenciador do sistema: %w", err)
	}
	return nil
}

// GetAnalysisAPIKey lê a API key da DeepSeek previamente salva via
// SaveAnalysisAPIKey.
func GetAnalysisAPIKey() (string, error) {
	apiKey, err := keyring.Get(keyringService, keyringUserDeepSeek)
	if err != nil {
		return "", fmt.Errorf("ler credencial do gerenciador do sistema: %w", err)
	}
	return apiKey, nil
}
```

- [ ] **Step 4: Rodar de novo e confirmar que passa**

Run: `go test ./internal/config/... -v`
Expected: PASS em todos os testes do pacote.

- [ ] **Step 5: `go vet` e commit**

Run: `go vet ./...`
Expected: sem saída.

```bash
git add internal/config/credentials.go internal/config/credentials_test.go
git commit -m "feat: credencial da DeepSeek via go-keyring"
```

---

### Task 2: `internal/analysis` — `Provider` ganha `Model()`

**Contexto:** `analysis_results.model` (schema já existe, História 1) precisa do identificador exato do modelo usado (ex.: `deepseek-v4-flash`), não só do nome do provedor (`Name()` devolve `"deepseek"`). `openAICompatibleProvider` já guarda esse valor no campo privado `model` — falta expô-lo.

**Files:**
- Modify: `internal/analysis/analysis.go` (interface `Provider`)
- Modify: `internal/analysis/openai_compatible.go` (implementação)
- Modify: `internal/analysis/task_test.go` (`fakeProvider` precisa continuar satisfazendo `Provider`)
- Test: `internal/analysis/openai_compatible_test.go`

**Interfaces:**
- Produces: `Provider.Model() string` — usado pela Task 7 (`services/analysis.go`) pra preencher `analysis_results.model`.

- [ ] **Step 1: Escrever o teste falho**

Adicionar em `internal/analysis/openai_compatible_test.go`:

```go
func TestOpenAICompatibleProvider_Model_ReturnsConfiguredModel(t *testing.T) {
	p, err := newOpenAICompatibleProvider("fake", "http://example.invalid", "key", "fake-model", false)
	if err != nil {
		t.Fatalf("newOpenAICompatibleProvider erro: %v", err)
	}
	if got := p.Model(); got != "fake-model" {
		t.Errorf("Model() = %q, esperado %q", got, "fake-model")
	}
}
```

- [ ] **Step 2: Rodar e confirmar que falha**

Run: `go test ./internal/analysis/... -run TestOpenAICompatibleProvider_Model -v`
Expected: FAIL com `p.Model undefined`.

- [ ] **Step 3: Adicionar `Model()` na interface e na implementação**

Em `internal/analysis/analysis.go`:

```go
type Provider interface {
	Name() string
	Model() string
	Complete(ctx context.Context, systemPrompt, transcript string) (json.RawMessage, error)
}
```

Em `internal/analysis/openai_compatible.go`, logo após `func (p *openAICompatibleProvider) Name() string { return p.name }`:

```go
func (p *openAICompatibleProvider) Model() string { return p.model }
```

- [ ] **Step 4: Atualizar `fakeProvider` em `task_test.go`**

Em `internal/analysis/task_test.go`, logo após `func (f fakeProvider) Name() string { return "fake" }`:

```go
func (f fakeProvider) Model() string { return "fake-model" }
```

- [ ] **Step 5: Rodar todos os testes do pacote e confirmar que passam**

Run: `go test ./internal/analysis/... -v`
Expected: PASS em todos.

- [ ] **Step 6: `go vet` e commit**

Run: `go vet ./...`
Expected: sem saída.

```bash
git add internal/analysis/analysis.go internal/analysis/openai_compatible.go internal/analysis/task_test.go internal/analysis/openai_compatible_test.go
git commit -m "feat: Provider de análise expõe o modelo exato usado"
```

---

### Task 3: `internal/analysis` — exporta `NewCorrectionsTask` e `ParseCorrectionsResult`

**Contexto:** `services/analysis.go` (Task 7) precisa disparar a tarefa de corrections e reconstituir `[]Correction` a partir do `result_json` já persistido, sem duplicar o schema — hoje `newCorrectionsTask` é privada ao pacote e não existe nenhuma função pra decodificar de volta.

**Files:**
- Modify: `internal/analysis/tasks_corrections.go`
- Modify: `internal/analysis/tasks.go` (chamador de `newCorrectionsTask`)
- Modify: `internal/analysis/tasks_corrections_test.go`

**Interfaces:**
- Produces: `analysis.NewCorrectionsTask() TaskDef`, `analysis.ParseCorrectionsResult(resultJSON json.RawMessage) ([]Correction, error)` — usados pela Task 7.

- [ ] **Step 1: Escrever os testes falhos**

Em `internal/analysis/tasks_corrections_test.go`, trocar a chamada de `newCorrectionsTask()` por `NewCorrectionsTask()` em `TestNewCorrectionsTask_HasNameAndPrompt` e adicionar:

```go
func TestParseCorrectionsResult_Valid(t *testing.T) {
	resultJSON := json.RawMessage(`[{"utterance_index":0,"original":"I go","correction":"I went","explanation":"passado"}]`)
	got, err := ParseCorrectionsResult(resultJSON)
	if err != nil {
		t.Fatalf("ParseCorrectionsResult erro inesperado: %v", err)
	}
	if len(got) != 1 || got[0].Original != "I go" || got[0].CorrectionTx != "I went" {
		t.Errorf("got = %+v, inesperado", got)
	}
}

func TestParseCorrectionsResult_InvalidJSON(t *testing.T) {
	if _, err := ParseCorrectionsResult(json.RawMessage("not json")); err == nil {
		t.Fatal("esperava erro para JSON inválido, obteve nil")
	}
}
```

- [ ] **Step 2: Rodar e confirmar que falha**

Run: `go test ./internal/analysis/... -run 'TestNewCorrectionsTask|TestParseCorrectionsResult' -v`
Expected: FAIL com `undefined: NewCorrectionsTask` (e `ParseCorrectionsResult`).

- [ ] **Step 3: Renomear a função e adicionar `ParseCorrectionsResult`**

Em `internal/analysis/tasks_corrections.go`, adicionar `"fmt"` aos imports e trocar:

```go
func newCorrectionsTask() TaskDef {
	return task[[]Correction]{name: "analyze_corrections", version: 1, prompt: mustLoadPrompt("analyze-corrections-v1.md"), parse: parseCorrections}
}
```

por:

```go
func NewCorrectionsTask() TaskDef {
	return task[[]Correction]{name: "analyze_corrections", version: 1, prompt: mustLoadPrompt("analyze-corrections-v1.md"), parse: parseCorrections}
}

// ParseCorrectionsResult decodifica um result_json já persistido (gravado
// por Execute a partir desta mesma tarefa — um array JSON de Correction,
// sem envelope) de volta em []Correction. Reaproveitado por quem precisa
// reconstituir o resultado salvo sem chamar o provedor de novo
// (services.AnalysisService).
func ParseCorrectionsResult(resultJSON json.RawMessage) ([]Correction, error) {
	var out []Correction
	if err := json.Unmarshal(resultJSON, &out); err != nil {
		return nil, fmt.Errorf("analysis: desserializar resultado de analyze_corrections: %w", err)
	}
	return out, nil
}
```

Em `internal/analysis/tasks.go`, trocar `newCorrectionsTask()` por `NewCorrectionsTask()` na lista `Tasks`.

- [ ] **Step 4: Rodar todos os testes do pacote e confirmar que passam**

Run: `go test ./internal/analysis/... -v`
Expected: PASS em todos.

- [ ] **Step 5: `go vet` e commit**

Run: `go vet ./...`
Expected: sem saída.

```bash
git add internal/analysis/tasks_corrections.go internal/analysis/tasks.go internal/analysis/tasks_corrections_test.go
git commit -m "refactor: exporta NewCorrectionsTask e ParseCorrectionsResult"
```

---

### Task 4: `internal/analysis` — `CorrectionDisplay` e `MatchCorrections`

**Contexto:** O campo `original` de cada `Correction` é um trecho da fala do aluno (não a fala inteira) copiado pelo modelo — pode não bater 100% com o texto real da utterance (maiúscula, pontuação, espaço). `MatchCorrections` localiza esse trecho no texto real e devolve os pedaços já separados (antes/errado/depois), pra o frontend só concatenar — decisão de rodar isso no backend (testável) em vez do frontend (sem test runner), registrada na spec desta história.

**Files:**
- Create: `internal/analysis/corrections_display.go`
- Test: `internal/analysis/corrections_display_test.go`

**Interfaces:**
- Consumes: `Correction` (já existe, `tasks_corrections.go`), `stt.Utterance` (já existe, `internal/stt`).
- Produces: `analysis.CorrectionDisplay{UtteranceIndex int, Original, Before, Wrong, After, Correction, Explanation string}`, `analysis.MatchCorrections(utterances []stt.Utterance, corrections []Correction) []CorrectionDisplay` — usado pela Task 7 (`services/analysis.go`).

- [ ] **Step 1: Escrever os testes falhos**

Criar `internal/analysis/corrections_display_test.go`:

```go
// internal/analysis/corrections_display_test.go
package analysis

import (
	"testing"

	"assistente-idiomas/internal/stt"
)

func TestMatchCorrections_SplitsTextAroundTheWrongSpan(t *testing.T) {
	tests := []struct {
		name       string
		text       string
		original   string
		wantBefore string
		wantWrong  string
		wantAfter  string
	}{
		{
			name:       "exact match",
			text:       "I go to school yesterday",
			original:   "I go",
			wantBefore: "",
			wantWrong:  "I go",
			wantAfter:  " to school yesterday",
		},
		{
			name:       "case difference",
			text:       "I GO to school yesterday",
			original:   "i go",
			wantBefore: "",
			wantWrong:  "I GO",
			wantAfter:  " to school yesterday",
		},
		{
			name:       "whitespace difference",
			text:       "I  go to school yesterday",
			original:   "I go",
			wantBefore: "",
			wantWrong:  "I  go",
			wantAfter:  " to school yesterday",
		},
		{
			name:       "not found",
			text:       "I go to school yesterday",
			original:   "she goes",
			wantBefore: "",
			wantWrong:  "",
			wantAfter:  "",
		},
		{
			name:       "first occurrence used when original repeats",
			text:       "go go go",
			original:   "go",
			wantBefore: "",
			wantWrong:  "go",
			wantAfter:  " go go",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			utterances := []stt.Utterance{{Speaker: "speaker_0", Text: tt.text}}
			corrections := []Correction{{UtteranceIdx: 0, Original: tt.original, CorrectionTx: "fix", Explanation: "why"}}
			got := MatchCorrections(utterances, corrections)
			if len(got) != 1 {
				t.Fatalf("len(got) = %d, esperado 1", len(got))
			}
			if got[0].Before != tt.wantBefore || got[0].Wrong != tt.wantWrong || got[0].After != tt.wantAfter {
				t.Errorf("got[0] = %+v, esperado Before=%q Wrong=%q After=%q", got[0], tt.wantBefore, tt.wantWrong, tt.wantAfter)
			}
		})
	}
}

func TestMatchCorrections_OriginalAlwaysPopulatedEvenWithoutMatch(t *testing.T) {
	utterances := []stt.Utterance{{Speaker: "speaker_0", Text: "I go to school yesterday"}}
	corrections := []Correction{{UtteranceIdx: 0, Original: "she goes", CorrectionTx: "fix", Explanation: "why"}}
	got := MatchCorrections(utterances, corrections)
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, esperado 1", len(got))
	}
	if got[0].Original != "she goes" {
		t.Errorf("Original = %q, esperado preservado mesmo sem match", got[0].Original)
	}
	if got[0].Wrong != "" {
		t.Errorf("Wrong = %q, esperado vazio (sem match)", got[0].Wrong)
	}
}

func TestMatchCorrections_MultipleCorrectionsAcrossUtterances(t *testing.T) {
	utterances := []stt.Utterance{
		{Speaker: "speaker_0", Text: "I go yesterday"},
		{Speaker: "speaker_1", Text: "OK"},
		{Speaker: "speaker_0", Text: "She go too"},
	}
	corrections := []Correction{
		{UtteranceIdx: 0, Original: "I go", CorrectionTx: "I went", Explanation: "a"},
		{UtteranceIdx: 2, Original: "She go", CorrectionTx: "She goes", Explanation: "b"},
	}
	got := MatchCorrections(utterances, corrections)
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, esperado 2", len(got))
	}
	if got[0].UtteranceIndex != 0 || got[0].Wrong != "I go" {
		t.Errorf("got[0] = %+v, inesperado", got[0])
	}
	if got[1].UtteranceIndex != 2 || got[1].Wrong != "She go" {
		t.Errorf("got[1] = %+v, inesperado", got[1])
	}
}
```

- [ ] **Step 2: Rodar e confirmar que falha**

Run: `go test ./internal/analysis/... -run TestMatchCorrections -v`
Expected: FAIL com `undefined: MatchCorrections`.

- [ ] **Step 3: Implementar `corrections_display.go`**

```go
// internal/analysis/corrections_display.go
package analysis

import (
	"strings"
	"unicode"

	"assistente-idiomas/internal/stt"
)

// CorrectionDisplay é uma Correction já pronta para o frontend renderizar:
// o trecho errado (Wrong) já separado do resto da fala (Before/After) via
// matching normalizado contra o texto real da utterance correspondente.
// Original é sempre preenchido com o trecho cru devolvido pelo modelo,
// mesmo quando Wrong == "" (não localizado) — é o que o chamador usa pra
// montar a nota avulsa de fallback, já que Before/Wrong/After ficam vazios
// nesse caso.
type CorrectionDisplay struct {
	UtteranceIndex int
	Original       string
	Before         string
	Wrong          string
	After          string
	Correction     string
	Explanation    string
}

// MatchCorrections localiza, para cada Correction, o trecho Original dentro
// do texto real da utterance correspondente (utterances[c.UtteranceIdx]),
// normalizando (case-insensitive, espaços consecutivos colapsados em um só)
// quando o match exato falha. Usa a primeira ocorrência quando Original
// aparece mais de uma vez na fala. Quando não encontra (nem normalizado),
// Before/Wrong/After ficam vazios — o chamador mostra a correção como nota
// avulsa nesse caso, nunca a descarta. utterances e corrections já vieram
// com utterance_index validado (filterAnchored, História 1); um índice fora
// do range aqui seria bug de chamador, não um caminho a tratar
// graciosamente de novo.
func MatchCorrections(utterances []stt.Utterance, corrections []Correction) []CorrectionDisplay {
	out := make([]CorrectionDisplay, 0, len(corrections))
	for _, c := range corrections {
		text := utterances[c.UtteranceIdx].Text
		before, wrong, after := splitByOriginal(text, c.Original)
		out = append(out, CorrectionDisplay{
			UtteranceIndex: c.UtteranceIdx,
			Original:       c.Original,
			Before:         before,
			Wrong:          wrong,
			After:          after,
			Correction:     c.CorrectionTx,
			Explanation:    c.Explanation,
		})
	}
	return out
}

// splitByOriginal localiza original dentro de text e devolve os três
// pedaços (antes, o próprio trecho como aparece em text, depois). Tenta
// primeiro um match exato (mais rápido, preserva índices originais sem
// mapeamento); se falhar, normaliza (minúsculas + espaços consecutivos
// colapsados) e mapeia o índice encontrado de volta pro texto original via
// normalizeWithOffsets. Sem nenhum match, devolve três strings vazias — o
// chamador interpreta wrong == "" como "não localizado".
func splitByOriginal(text, original string) (before, wrong, after string) {
	if original == "" {
		return "", "", ""
	}
	if idx := strings.Index(text, original); idx != -1 {
		return text[:idx], text[idx : idx+len(original)], text[idx+len(original):]
	}

	normText, offsets := normalizeWithOffsets(text)
	normOriginal, _ := normalizeWithOffsets(original)
	if normOriginal == "" {
		return "", "", ""
	}
	idx := strings.Index(normText, normOriginal)
	if idx == -1 {
		return "", "", ""
	}
	startRune := len([]rune(normText[:idx]))
	endRune := startRune + len([]rune(normOriginal))
	start := offsets[startRune]
	end := offsets[endRune]
	return text[:start], text[start:end], text[end:]
}

// normalizeWithOffsets minusculiza text e colapsa cada run de espaços em
// branco consecutivos num único espaço, devolvendo junto um slice offsets
// onde offsets[i] é o índice em bytes, no text original, do rune
// normalizado de posição i — offsets[len(runes normalizados)] == len(text),
// pra permitir localizar o fim de um match que termina no fim da string.
func normalizeWithOffsets(text string) (string, []int) {
	runes := []rune(text)
	byteOffsets := make([]int, len(runes)+1)
	pos := 0
	for i, r := range runes {
		byteOffsets[i] = pos
		pos += len(string(r))
	}
	byteOffsets[len(runes)] = pos

	var b strings.Builder
	offsets := make([]int, 0, len(runes)+1)
	lastWasSpace := false
	for i, r := range runes {
		if unicode.IsSpace(r) {
			if lastWasSpace {
				continue
			}
			lastWasSpace = true
			b.WriteRune(' ')
			offsets = append(offsets, byteOffsets[i])
			continue
		}
		lastWasSpace = false
		b.WriteRune(unicode.ToLower(r))
		offsets = append(offsets, byteOffsets[i])
	}
	offsets = append(offsets, byteOffsets[len(runes)])
	return b.String(), offsets
}
```

- [ ] **Step 4: Rodar todos os testes do pacote e confirmar que passam**

Run: `go test ./internal/analysis/... -v`
Expected: PASS em todos.

- [ ] **Step 5: `go vet` e commit**

Run: `go vet ./...`
Expected: sem saída.

```bash
git add internal/analysis/corrections_display.go internal/analysis/corrections_display_test.go
git commit -m "feat: casa correções do aluno com o texto real da fala"
```

---

### Task 5: `internal/db` — `DeleteAnalysisResultsForLesson`

**Contexto:** Quando o mapeamento aluno/tutor de uma aula muda (Task 8), qualquer análise já feita fica apontando pro papel errado — precisa ser descartada. `analysis_results` é genérica por `(lesson_id, task)`, então apagar por `lesson_id` já cobre qualquer tarefa futura sem precisar de mudança de schema.

**Files:**
- Modify: `internal/db/analysis_results.go`
- Test: `internal/db/analysis_results_test.go`

**Interfaces:**
- Produces: `db.DeleteAnalysisResultsForLesson(conn *sql.DB, lessonID int64) error` — usado pela Task 8 (`services/library.go`).

- [ ] **Step 1: Escrever o teste falho**

Adicionar em `internal/db/analysis_results_test.go`:

```go
func TestDeleteAnalysisResultsForLesson_DeletesAllTasksForLessonOnly(t *testing.T) {
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
		t.Fatalf("UpsertAnalysisResult() lessonA erro inesperado: %v", err)
	}
	if err := UpsertAnalysisResult(conn, lessonA, "analyze_vocabulary", promptID, "deepseek", "[]", "a2.json"); err != nil {
		t.Fatalf("UpsertAnalysisResult() lessonA (segunda task) erro inesperado: %v", err)
	}
	if err := UpsertAnalysisResult(conn, lessonB, "analyze_corrections", promptID, "deepseek", "[]", "b.json"); err != nil {
		t.Fatalf("UpsertAnalysisResult() lessonB erro inesperado: %v", err)
	}

	if err := DeleteAnalysisResultsForLesson(conn, lessonA); err != nil {
		t.Fatalf("DeleteAnalysisResultsForLesson() erro inesperado: %v", err)
	}

	var countA int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM analysis_results WHERE lesson_id = ?`, lessonA).Scan(&countA); err != nil {
		t.Fatalf("contar analysis_results de lessonA falhou: %v", err)
	}
	if countA != 0 {
		t.Errorf("countA = %d, esperado 0 (todas as tasks apagadas)", countA)
	}

	var countB int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM analysis_results WHERE lesson_id = ?`, lessonB).Scan(&countB); err != nil {
		t.Fatalf("contar analysis_results de lessonB falhou: %v", err)
	}
	if countB != 1 {
		t.Errorf("countB = %d, esperado 1 (não deve ser afetada)", countB)
	}
}
```

- [ ] **Step 2: Rodar e confirmar que falha**

Run: `go test ./internal/db/... -run TestDeleteAnalysisResultsForLesson -v`
Expected: FAIL com `undefined: DeleteAnalysisResultsForLesson`.

- [ ] **Step 3: Implementar**

Em `internal/db/analysis_results.go`:

```go
// DeleteAnalysisResultsForLesson apaga toda análise já feita pra lessonID
// (todas as tasks) — chamado quando o mapeamento aluno/tutor muda (ver
// services.LibraryService.SetStudentSpeaker), já que qualquer análise
// ancorada em utterance_index passa a apontar pro papel errado assim que os
// rótulos Aluno/Tutor trocam de falante.
func DeleteAnalysisResultsForLesson(conn *sql.DB, lessonID int64) error {
	if _, err := conn.Exec(`DELETE FROM analysis_results WHERE lesson_id = ?`, lessonID); err != nil {
		return fmt.Errorf("apagar analysis_results da lesson %d: %w", lessonID, err)
	}
	return nil
}
```

- [ ] **Step 4: Rodar de novo e confirmar que passa**

Run: `go test ./internal/db/... -v`
Expected: PASS em todos.

- [ ] **Step 5: `go vet` e commit**

Run: `go vet ./...`
Expected: sem saída.

```bash
git add internal/db/analysis_results.go internal/db/analysis_results_test.go
git commit -m "feat: DeleteAnalysisResultsForLesson descarta análises ao trocar falante"
```

---

### Task 6: `services/settings.go` — expõe a credencial de análise

**Contexto:** Mesmo padrão de `HasSTTCredential`/`SaveSTTAPIKey` já em `SettingsService` — dois métodos novos.

**Files:**
- Modify: `services/settings.go`
- Test: `services/settings_test.go`

**Interfaces:**
- Consumes: `config.SaveAnalysisAPIKey`, `config.GetAnalysisAPIKey` (Task 1).
- Produces: `(*SettingsService) HasAnalysisCredential() (bool, error)`, `(*SettingsService) SaveAnalysisAPIKey(apiKey string) error` — usados pela Task 10 (`Settings.svelte`).

- [ ] **Step 1: Escrever os testes falhos**

Adicionar em `services/settings_test.go`, logo após os testes de `HasSTTCredential`:

```go
func TestSettingsService_HasAnalysisCredential_FalseWhenNotConfigured(t *testing.T) {
	keyring.MockInit()

	svc := NewSettingsService(nil, configStorageRoot)
	has, err := svc.HasAnalysisCredential()
	if err != nil {
		t.Fatalf("HasAnalysisCredential() erro inesperado: %v", err)
	}
	if has {
		t.Error("HasAnalysisCredential() = true, esperado false (nenhuma credencial gravada ainda)")
	}
}

func TestSettingsService_HasAnalysisCredential_TrueAfterSave(t *testing.T) {
	keyring.MockInit()

	svc := NewSettingsService(nil, configStorageRoot)
	if err := svc.SaveAnalysisAPIKey("sk-deepseek-test"); err != nil {
		t.Fatalf("SaveAnalysisAPIKey() erro inesperado: %v", err)
	}

	has, err := svc.HasAnalysisCredential()
	if err != nil {
		t.Fatalf("HasAnalysisCredential() erro inesperado: %v", err)
	}
	if !has {
		t.Error("HasAnalysisCredential() = false, esperado true após SaveAnalysisAPIKey")
	}
}

func TestSettingsService_HasAnalysisCredential_KeyringUnavailablePropagatesError(t *testing.T) {
	sentinel := errors.New("secret service indisponível")
	keyring.MockInitWithError(sentinel)
	t.Cleanup(keyring.MockInit)

	svc := NewSettingsService(nil, configStorageRoot)
	_, err := svc.HasAnalysisCredential()
	if !errors.Is(err, sentinel) {
		t.Errorf("HasAnalysisCredential() erro = %v, esperado envolver %v", err, sentinel)
	}
	if !strings.Contains(err.Error(), "gnome-keyring") {
		t.Errorf("erro não menciona gnome-keyring/kwallet: %v", err)
	}
}
```

(`strings` já precisa estar importado em `services/settings_test.go` — se ainda não estiver, adicionar `"strings"` aos imports.)

- [ ] **Step 2: Rodar e confirmar que falha**

Run: `go test ./services/... -run TestSettingsService_HasAnalysisCredential -v`
Expected: FAIL com `svc.HasAnalysisCredential undefined`.

- [ ] **Step 3: Implementar os dois métodos**

Em `services/settings.go`, logo após `SaveSTTAPIKey`:

```go
// HasAnalysisCredential indica se há uma credencial do provedor de análise
// (DeepSeek) gravada no keyring, sem revelar o valor. Mesmo comportamento
// de HasSTTCredential: false (sem erro) se não configurada ainda; erro só
// em falha real de acesso ao keyring.
func (s *SettingsService) HasAnalysisCredential() (bool, error) {
	_, err := config.GetAnalysisAPIKey()
	if err == nil {
		return true, nil
	}
	if errors.Is(err, keyring.ErrNotFound) {
		return false, nil
	}
	return false, fmt.Errorf("não foi possível acessar o gerenciador de credenciais do sistema (verifique se o gnome-keyring/kwallet está rodando): %w", err)
}

// SaveAnalysisAPIKey grava/sobrescreve a credencial do provedor de análise.
func (s *SettingsService) SaveAnalysisAPIKey(apiKey string) error {
	return config.SaveAnalysisAPIKey(apiKey)
}
```

- [ ] **Step 4: Rodar de novo e confirmar que passa**

Run: `go test ./services/... -run TestSettingsService -v`
Expected: PASS em todos, incluindo os já existentes de STT.

- [ ] **Step 5: `go vet` e commit**

Run: `go vet ./...`
Expected: sem saída.

```bash
git add services/settings.go services/settings_test.go
git commit -m "feat: SettingsService expõe credencial do provedor de análise"
```

---

### Task 7: `services/analysis.go` (novo) — `AnalysisService`

**Contexto:** O serviço novo desta história. Três métodos explícitos, sem flag booleana escondida: `GetCorrections` (leitura, nunca chama a API), `AnalyzeCorrections` (idempotente — se já existe resultado, devolve sem rechamar a API) e `ReprocessCorrections` (sempre chama a API e sobrescreve). Os três devolvem `CorrectionsResult{Analyzed bool, Items []CorrectionDisplay}` — `Analyzed` distingue "a tarefa nunca rodou" de "rodou e não achou nenhuma correção" (uma aula em que o aluno não errou nada), o que `len(Items) == 0` sozinho não conseguiria distinguir.

**Files:**
- Create: `services/analysis.go`
- Test: `services/analysis_test.go`

**Interfaces:**
- Consumes: `analysis.Provider` (Task 2, com `Model()`), `analysis.NewCorrectionsTask()`/`ParseCorrectionsResult`/`MatchCorrections` (Tasks 3-4), `analysis.FormatTranscript` (já existe), `db.FindLessonByID`, `db.FindTranscriptByLessonID`, `db.UpsertPrompt`, `db.UpsertAnalysisResult`, `db.FindAnalysisResult` (já existem).
- Produces: `services.CorrectionDisplay{UtteranceIndex int, Original, Before, Wrong, After, Correction, Explanation string}`, `services.CorrectionsResult{Analyzed bool, Items []CorrectionDisplay}`, `NewAnalysisService(conn *sql.DB, storageRoot func() (string, error), providerFactory func() (analysis.Provider, error)) *AnalysisService`, `(*AnalysisService) GetCorrections(lessonID int64) (CorrectionsResult, error)`, `(*AnalysisService) AnalyzeCorrections(lessonID int64) (CorrectionsResult, error)`, `(*AnalysisService) ReprocessCorrections(lessonID int64) (CorrectionsResult, error)` — usados pela Task 9 (`main.go`) e Task 11 (`LessonDetail.svelte`).

- [ ] **Step 1: Escrever o teste falho de `GetCorrections` (nunca analisada)**

Criar `services/analysis_test.go`:

```go
// services/analysis_test.go
package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"assistente-idiomas/internal/analysis"
	"assistente-idiomas/internal/db"
)

type fakeAnalysisProvider struct {
	model string
	raw   json.RawMessage
	err   error
	calls int
}

func (p *fakeAnalysisProvider) Name() string  { return "fake" }
func (p *fakeAnalysisProvider) Model() string { return p.model }
func (p *fakeAnalysisProvider) Complete(ctx context.Context, systemPrompt, transcript string) (json.RawMessage, error) {
	p.calls++
	return p.raw, p.err
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func TestAnalysisService_GetCorrections_NotAnalyzedYet(t *testing.T) {
	conn := openTestDB(t)
	lessonID := mustInsertLesson(t, conn, "2026-08-01", "Sarah M.", "aula.mp4")

	svc := NewAnalysisService(conn, testStorageRoot(t), nil)
	got, err := svc.GetCorrections(lessonID)
	if err != nil {
		t.Fatalf("GetCorrections() erro inesperado: %v", err)
	}
	if got.Analyzed {
		t.Error("GetCorrections().Analyzed = true, esperado false (tarefa ainda não rodou)")
	}
	if len(got.Items) != 0 {
		t.Errorf("GetCorrections().Items = %+v, esperado vazio", got.Items)
	}
}
```

- [ ] **Step 2: Rodar e confirmar que falha**

Run: `go test ./services/... -run TestAnalysisService_GetCorrections_NotAnalyzedYet -v`
Expected: FAIL com `undefined: NewAnalysisService`.

- [ ] **Step 3: Criar `services/analysis.go` com a estrutura base, DTOs e `GetCorrections`**

```go
// services/analysis.go
package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"

	"assistente-idiomas/internal/analysis"
	"assistente-idiomas/internal/db"
	"assistente-idiomas/internal/stt"
)

// AnalysisService dispara tarefas de análise sob demanda e expõe o
// resultado já persistido (Fase 2, História 2 — Piloto: Correções do
// aluno). providerFactory resolve a credencial do provedor de análise a
// cada chamada, não uma vez só na construção do serviço — mesmo motivo do
// sttFactory em internal/jobs/worker.go: a credencial pode ser gravada pela
// tela de Configurações depois que o app já iniciou.
type AnalysisService struct {
	conn            *sql.DB
	storageRoot     func() (string, error)
	providerFactory func() (analysis.Provider, error)
}

func NewAnalysisService(conn *sql.DB, storageRoot func() (string, error), providerFactory func() (analysis.Provider, error)) *AnalysisService {
	return &AnalysisService{conn: conn, storageRoot: storageRoot, providerFactory: providerFactory}
}

const correctionsTaskName = "analyze_corrections"

// CorrectionDisplay é uma correção do aluno já pronta pro frontend
// renderizar — DTO local ao pacote services (nunca analysis.CorrectionDisplay
// direto: mesmo princípio de services.Transcript/services.Utterance em
// library.go, internal/ não vaza pro binding do Wails).
type CorrectionDisplay struct {
	UtteranceIndex int    `json:"utteranceIndex"`
	Original       string `json:"original"`
	Before         string `json:"before"`
	Wrong          string `json:"wrong"`
	After          string `json:"after"`
	Correction     string `json:"correction"`
	Explanation    string `json:"explanation"`
}

// CorrectionsResult é o resultado de analyze_corrections exposto ao
// frontend. Analyzed distingue "a tarefa nunca rodou pra essa aula" (false,
// Items vazio) de "rodou e não achou nenhuma correção" (true, Items vazio)
// — o botão do Detalhe usa esse campo pra decidir entre "Analisar
// correções" e "Reprocessar correções", não o tamanho de Items.
type CorrectionsResult struct {
	Analyzed bool                 `json:"analyzed"`
	Items    []CorrectionDisplay `json:"items"`
}

func toCorrectionDisplays(items []analysis.CorrectionDisplay) []CorrectionDisplay {
	out := make([]CorrectionDisplay, 0, len(items))
	for _, it := range items {
		out = append(out, CorrectionDisplay{
			UtteranceIndex: it.UtteranceIndex,
			Original:       it.Original,
			Before:         it.Before,
			Wrong:          it.Wrong,
			After:          it.After,
			Correction:     it.Correction,
			Explanation:    it.Explanation,
		})
	}
	return out
}

// GetCorrections devolve o resultado já salvo de analyze_corrections pra
// lessonID, sem chamar a API. Analyzed == false se a tarefa nunca rodou.
func (s *AnalysisService) GetCorrections(lessonID int64) (CorrectionsResult, error) {
	result, err := db.FindAnalysisResult(s.conn, lessonID, correctionsTaskName)
	if err != nil {
		return CorrectionsResult{}, fmt.Errorf("buscar análise de correções da lesson %d: %w", lessonID, err)
	}
	if result == nil {
		return CorrectionsResult{}, nil
	}
	return s.buildResult(lessonID, result.ResultJSON)
}

// buildResult busca a transcrição da lesson e monta o CorrectionsResult a
// partir de resultJSON já persistido.
func (s *AnalysisService) buildResult(lessonID int64, resultJSON string) (CorrectionsResult, error) {
	transcript, err := db.FindTranscriptByLessonID(s.conn, lessonID)
	if err != nil {
		return CorrectionsResult{}, fmt.Errorf("buscar transcrição da lesson %d: %w", lessonID, err)
	}
	if transcript == nil {
		return CorrectionsResult{}, fmt.Errorf("aula %d não tem mais transcrição", lessonID)
	}
	return s.buildResultFromTranscript(transcript.Utterances, resultJSON)
}

func (s *AnalysisService) buildResultFromTranscript(utterances []stt.Utterance, resultJSON string) (CorrectionsResult, error) {
	corrections, err := analysis.ParseCorrectionsResult(json.RawMessage(resultJSON))
	if err != nil {
		return CorrectionsResult{}, fmt.Errorf("desserializar correções: %w", err)
	}
	return CorrectionsResult{Analyzed: true, Items: toCorrectionDisplays(analysis.MatchCorrections(utterances, corrections))}, nil
}
```

- [ ] **Step 4: Rodar de novo e confirmar que passa**

Run: `go test ./services/... -run TestAnalysisService_GetCorrections_NotAnalyzedYet -v`
Expected: PASS.

- [ ] **Step 5: Escrever os testes falhos de `AnalyzeCorrections`/`ReprocessCorrections`**

Adicionar em `services/analysis_test.go`:

```go
func insertTranscriptFixture(t *testing.T, conn *sql.DB, lessonID int64, rawPath string, utterances []map[string]any) {
	t.Helper()
	b, err := json.Marshal(utterances)
	if err != nil {
		t.Fatalf("marshal de utterances de fixture falhou: %v", err)
	}
	if err := db.InsertTranscript(conn, lessonID, rawPath, string(b)); err != nil {
		t.Fatalf("InsertTranscript() de fixture falhou: %v", err)
	}
}

func TestAnalysisService_AnalyzeCorrections_PersistsAndReturnsCorrections(t *testing.T) {
	conn := openTestDB(t)
	lessonID := mustInsertLesson(t, conn, "2026-08-01", "Sarah M.", "aulas/2026/aula.mp4")
	if err := db.SetStudentSpeaker(conn, lessonID, "speaker_0"); err != nil {
		t.Fatalf("SetStudentSpeaker() de fixture falhou: %v", err)
	}
	insertTranscriptFixture(t, conn, lessonID, "aulas/2026/aula.transcript.json", []map[string]any{
		{"Speaker": "speaker_0", "Text": "I go to school yesterday", "Start": 0, "End": 2000000000},
		{"Speaker": "speaker_1", "Text": "OK, tell me more", "Start": 2000000000, "End": 4000000000},
	})

	fake := &fakeAnalysisProvider{
		model: "deepseek-v4-flash",
		raw:   json.RawMessage(`[{"utterance_index":0,"original":"I go to school yesterday","correction":"I went to school yesterday","explanation":"Passado simples: went, não go."}]`),
	}
	storageRoot := t.TempDir()
	svc := NewAnalysisService(conn, func() (string, error) { return storageRoot, nil }, func() (analysis.Provider, error) { return fake, nil })

	got, err := svc.AnalyzeCorrections(lessonID)
	if err != nil {
		t.Fatalf("AnalyzeCorrections() erro inesperado: %v", err)
	}
	if !got.Analyzed || len(got.Items) != 1 {
		t.Fatalf("AnalyzeCorrections() = %+v, esperado Analyzed=true e 1 item", got)
	}
	if got.Items[0].UtteranceIndex != 0 || got.Items[0].Wrong != "I go" || got.Items[0].Correction != "I went to school yesterday" {
		t.Errorf("Items[0] = %+v, campos inesperados", got.Items[0])
	}
	if fake.calls != 1 {
		t.Errorf("provider chamado %d vezes, esperado 1", fake.calls)
	}

	persisted, err := db.FindAnalysisResult(conn, lessonID, correctionsTaskName)
	if err != nil {
		t.Fatalf("FindAnalysisResult() erro inesperado: %v", err)
	}
	if persisted == nil {
		t.Fatal("FindAnalysisResult() = nil, esperado persistido após AnalyzeCorrections")
	}
	if persisted.Model != "deepseek-v4-flash" {
		t.Errorf("persisted.Model = %q, esperado %q", persisted.Model, "deepseek-v4-flash")
	}

	rawAbsPath := filepath.Join(storageRoot, "aulas", "2026", "aula.analysis.analyze_corrections.json")
	if _, err := os.Stat(rawAbsPath); err != nil {
		t.Errorf("raw response não foi gravado em %s: %v", rawAbsPath, err)
	}
}

func TestAnalysisService_AnalyzeCorrections_IsIdempotent(t *testing.T) {
	conn := openTestDB(t)
	lessonID := mustInsertLesson(t, conn, "2026-08-01", "Sarah M.", "aula.mp4")
	if err := db.SetStudentSpeaker(conn, lessonID, "speaker_0"); err != nil {
		t.Fatalf("SetStudentSpeaker() de fixture falhou: %v", err)
	}
	insertTranscriptFixture(t, conn, lessonID, "aula.transcript.json", []map[string]any{
		{"Speaker": "speaker_0", "Text": "I go yesterday", "Start": 0, "End": 1000000000},
	})

	fake := &fakeAnalysisProvider{
		model: "deepseek-v4-flash",
		raw:   json.RawMessage(`[{"utterance_index":0,"original":"I go","correction":"I went","explanation":"a"}]`),
	}
	svc := NewAnalysisService(conn, testStorageRoot(t), func() (analysis.Provider, error) { return fake, nil })

	if _, err := svc.AnalyzeCorrections(lessonID); err != nil {
		t.Fatalf("primeira AnalyzeCorrections() erro inesperado: %v", err)
	}
	got, err := svc.AnalyzeCorrections(lessonID)
	if err != nil {
		t.Fatalf("segunda AnalyzeCorrections() erro inesperado: %v", err)
	}
	if fake.calls != 1 {
		t.Errorf("provider chamado %d vezes, esperado 1 (segunda chamada deve ser idempotente)", fake.calls)
	}
	if !got.Analyzed || len(got.Items) != 1 {
		t.Errorf("segunda AnalyzeCorrections() = %+v, esperado o mesmo resultado persistido", got)
	}
}

func TestAnalysisService_ReprocessCorrections_AlwaysCallsProviderAndOverwrites(t *testing.T) {
	conn := openTestDB(t)
	lessonID := mustInsertLesson(t, conn, "2026-08-01", "Sarah M.", "aula.mp4")
	if err := db.SetStudentSpeaker(conn, lessonID, "speaker_0"); err != nil {
		t.Fatalf("SetStudentSpeaker() de fixture falhou: %v", err)
	}
	insertTranscriptFixture(t, conn, lessonID, "aula.transcript.json", []map[string]any{
		{"Speaker": "speaker_0", "Text": "I go yesterday", "Start": 0, "End": 1000000000},
	})

	fake := &fakeAnalysisProvider{
		model: "deepseek-v4-flash",
		raw:   json.RawMessage(`[{"utterance_index":0,"original":"I go","correction":"I went","explanation":"a"}]`),
	}
	svc := NewAnalysisService(conn, testStorageRoot(t), func() (analysis.Provider, error) { return fake, nil })

	if _, err := svc.AnalyzeCorrections(lessonID); err != nil {
		t.Fatalf("AnalyzeCorrections() erro inesperado: %v", err)
	}

	fake.raw = json.RawMessage(`[{"utterance_index":0,"original":"I go","correction":"I did go","explanation":"b"}]`)
	got, err := svc.ReprocessCorrections(lessonID)
	if err != nil {
		t.Fatalf("ReprocessCorrections() erro inesperado: %v", err)
	}
	if fake.calls != 2 {
		t.Errorf("provider chamado %d vezes, esperado 2 (Reprocess sempre chama)", fake.calls)
	}
	if len(got.Items) != 1 || got.Items[0].Correction != "I did go" {
		t.Errorf("ReprocessCorrections() = %+v, esperado o resultado sobrescrito", got)
	}
}

func TestAnalysisService_AnalyzeCorrections_RequiresStudentSpeakerChosen(t *testing.T) {
	conn := openTestDB(t)
	lessonID := mustInsertLesson(t, conn, "2026-08-01", "Sarah M.", "aula.mp4")
	insertTranscriptFixture(t, conn, lessonID, "aula.transcript.json", []map[string]any{
		{"Speaker": "speaker_0", "Text": "Hello", "Start": 0, "End": 1000000000},
	})

	svc := NewAnalysisService(conn, testStorageRoot(t), func() (analysis.Provider, error) {
		t.Fatal("providerFactory não deveria ser chamado sem student_speaker_label definido")
		return nil, nil
	})

	if _, err := svc.AnalyzeCorrections(lessonID); err == nil {
		t.Fatal("AnalyzeCorrections() esperava erro sem student_speaker_label definido, veio nil")
	}
}

func TestAnalysisService_AnalyzeCorrections_ProviderErrorDoesNotPersist(t *testing.T) {
	conn := openTestDB(t)
	lessonID := mustInsertLesson(t, conn, "2026-08-01", "Sarah M.", "aula.mp4")
	if err := db.SetStudentSpeaker(conn, lessonID, "speaker_0"); err != nil {
		t.Fatalf("SetStudentSpeaker() de fixture falhou: %v", err)
	}
	insertTranscriptFixture(t, conn, lessonID, "aula.transcript.json", []map[string]any{
		{"Speaker": "speaker_0", "Text": "Hello", "Start": 0, "End": 1000000000},
	})

	fake := &fakeAnalysisProvider{err: fmt.Errorf("erro de rede simulado")}
	svc := NewAnalysisService(conn, testStorageRoot(t), func() (analysis.Provider, error) { return fake, nil })

	if _, err := svc.AnalyzeCorrections(lessonID); err == nil {
		t.Fatal("AnalyzeCorrections() esperava erro do provider, veio nil")
	}

	persisted, err := db.FindAnalysisResult(conn, lessonID, correctionsTaskName)
	if err != nil {
		t.Fatalf("FindAnalysisResult() erro inesperado: %v", err)
	}
	if persisted != nil {
		t.Errorf("FindAnalysisResult() = %+v, esperado nil (falha do provider não deve persistir)", persisted)
	}
}
```

- [ ] **Step 6: Rodar e confirmar que falham só por `AnalyzeCorrections`/`ReprocessCorrections` não existirem**

Run: `go test ./services/... -run 'TestAnalysisService_AnalyzeCorrections|TestAnalysisService_ReprocessCorrections' -v`
Expected: FAIL com `svc.AnalyzeCorrections undefined`.

- [ ] **Step 7: Implementar `AnalyzeCorrections`, `ReprocessCorrections` e `runCorrections`**

Adicionar em `services/analysis.go`:

```go
// AnalyzeCorrections roda analyze_corrections se ainda não houver resultado
// salvo pra essa lesson; se já houver, devolve o existente sem chamar a API
// de novo (idempotente).
func (s *AnalysisService) AnalyzeCorrections(lessonID int64) (CorrectionsResult, error) {
	return s.runCorrections(lessonID, false)
}

// ReprocessCorrections roda analyze_corrections e sobrescreve o resultado
// existente, mesmo que já haja um — ação explícita, nunca automática.
func (s *AnalysisService) ReprocessCorrections(lessonID int64) (CorrectionsResult, error) {
	return s.runCorrections(lessonID, true)
}

func (s *AnalysisService) runCorrections(lessonID int64, overwrite bool) (CorrectionsResult, error) {
	lesson, err := db.FindLessonByID(s.conn, lessonID)
	if err != nil {
		return CorrectionsResult{}, fmt.Errorf("buscar lesson %d: %w", lessonID, err)
	}
	if lesson == nil {
		return CorrectionsResult{}, fmt.Errorf("aula %d não encontrada", lessonID)
	}
	if lesson.StudentSpeakerLabel == nil {
		return CorrectionsResult{}, fmt.Errorf("escolha quem é você na aula antes de analisar correções")
	}

	transcript, err := db.FindTranscriptByLessonID(s.conn, lessonID)
	if err != nil {
		return CorrectionsResult{}, fmt.Errorf("buscar transcrição da lesson %d: %w", lessonID, err)
	}
	if transcript == nil {
		return CorrectionsResult{}, fmt.Errorf("aula %d ainda não tem transcrição", lessonID)
	}

	if !overwrite {
		existing, err := db.FindAnalysisResult(s.conn, lessonID, correctionsTaskName)
		if err != nil {
			return CorrectionsResult{}, fmt.Errorf("buscar análise de correções da lesson %d: %w", lessonID, err)
		}
		if existing != nil {
			return s.buildResultFromTranscript(transcript.Utterances, existing.ResultJSON)
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
		return CorrectionsResult{}, fmt.Errorf("formatar transcrição da lesson %d: %w", lessonID, err)
	}

	provider, err := s.providerFactory()
	if err != nil {
		return CorrectionsResult{}, fmt.Errorf("obter provedor de análise: %w", err)
	}

	task := analysis.NewCorrectionsTask()
	resultJSON, raw, err := task.Execute(context.Background(), provider, formatted, len(transcript.Utterances))
	if raw != nil {
		if writeErr := s.writeRawResponse(lesson.VideoPath, task.Name(), raw); writeErr != nil {
			slog.Warn("analysis: falha ao gravar resposta bruta em disco", "lesson_id", lessonID, "task", task.Name(), "erro", writeErr)
		}
	}
	if err != nil {
		return CorrectionsResult{}, fmt.Errorf("analisar correções da lesson %d: %w", lessonID, err)
	}

	promptID, err := db.UpsertPrompt(s.conn, task.Name(), task.Version(), task.Prompt())
	if err != nil {
		return CorrectionsResult{}, fmt.Errorf("registrar prompt %s: %w", task.Name(), err)
	}

	rawRelPath := analysisRawRelPath(lesson.VideoPath, task.Name())
	if err := db.UpsertAnalysisResult(s.conn, lessonID, task.Name(), promptID, provider.Model(), string(resultJSON), rawRelPath); err != nil {
		return CorrectionsResult{}, fmt.Errorf("gravar resultado da análise: %w", err)
	}

	return s.buildResultFromTranscript(transcript.Utterances, string(resultJSON))
}

// analysisRawRelPath calcula o path (relativo à storage_root, sempre com
// "/") da resposta bruta do provedor pra uma tarefa: mesmo diretório do
// vídeo, nome "<basename-sem-extensão>.analysis.<task>.json" — mesmo
// esquema de rawJSONRelPath (internal/jobs, transcrição), com o segmento
// ".analysis." extra pra não colidir com o arquivo de transcrição
// (<basename>.transcript.json) nem entre tarefas de análise diferentes.
func analysisRawRelPath(videoRelPath, task string) string {
	dir := path.Dir(videoRelPath)
	base := strings.TrimSuffix(path.Base(videoRelPath), path.Ext(videoRelPath))
	return path.Join(dir, base+".analysis."+task+".json")
}

func (s *AnalysisService) writeRawResponse(videoRelPath, task string, raw json.RawMessage) error {
	root, err := s.storageRoot()
	if err != nil {
		return err
	}
	relPath := analysisRawRelPath(videoRelPath, task)
	absPath := filepath.Join(root, filepath.FromSlash(relPath))
	return os.WriteFile(absPath, raw, 0o644)
}
```

- [ ] **Step 8: Rodar todos os testes do pacote e confirmar que passam**

Run: `go test ./services/... -v`
Expected: PASS em todos, incluindo os novos e os já existentes.

- [ ] **Step 9: `go build`, `go vet` de todo o módulo e commit**

Run: `go build ./... && go vet ./...`
Expected: sem erros, sem saída do vet.

```bash
git add services/analysis.go services/analysis_test.go
git commit -m "feat: AnalysisService dispara e reprocessa correções do aluno sob demanda"
```

---

### Task 8: `services/library.go` — `SetStudentSpeaker` descarta análises ao trocar falante

**Contexto:** Trocar quem é o aluno depois de já ter análise salva invalida essa análise (o mapeamento Aluno/Tutor enviado ao modelo já não bate mais com o toggle atual). Definir o label pela primeira vez, ou repetir o mesmo label já atual, não descarta nada.

**Files:**
- Modify: `services/library.go`
- Test: `services/library_test.go`

**Interfaces:**
- Consumes: `db.FindLessonByID`, `db.DeleteAnalysisResultsForLesson` (Task 5), `db.SetStudentSpeaker` (já existe).

- [ ] **Step 1: Escrever os testes falhos**

Adicionar em `services/library_test.go`:

```go
func TestLibraryService_SetStudentSpeaker_DeletesAnalysisResultsOnChange(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLesson(t, conn, "2026-07-20", "Sarah M.", "aula.mp4")
	if err := db.SetStudentSpeaker(conn, lessonID, "speaker_0"); err != nil {
		t.Fatalf("db.SetStudentSpeaker() de fixture falhou: %v", err)
	}
	promptID, err := db.UpsertPrompt(conn, "analyze_corrections", 1, "conteúdo")
	if err != nil {
		t.Fatalf("UpsertPrompt() falhou: %v", err)
	}
	if err := db.UpsertAnalysisResult(conn, lessonID, "analyze_corrections", promptID, "deepseek", "[]", "aula.analysis.json"); err != nil {
		t.Fatalf("UpsertAnalysisResult() falhou: %v", err)
	}

	svc := NewLibraryService(conn, testStorageRoot(t))
	if err := svc.SetStudentSpeaker(lessonID, "speaker_1"); err != nil {
		t.Fatalf("SetStudentSpeaker() erro inesperado: %v", err)
	}

	result, err := db.FindAnalysisResult(conn, lessonID, "analyze_corrections")
	if err != nil {
		t.Fatalf("FindAnalysisResult() erro inesperado: %v", err)
	}
	if result != nil {
		t.Errorf("FindAnalysisResult() = %+v, esperado nil (análise descartada pela troca de falante)", result)
	}
}

func TestLibraryService_SetStudentSpeaker_FirstChoiceKeepsAnalysisResults(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLesson(t, conn, "2026-07-20", "Sarah M.", "aula.mp4")
	promptID, err := db.UpsertPrompt(conn, "analyze_corrections", 1, "conteúdo")
	if err != nil {
		t.Fatalf("UpsertPrompt() falhou: %v", err)
	}
	if err := db.UpsertAnalysisResult(conn, lessonID, "analyze_corrections", promptID, "deepseek", "[]", "aula.analysis.json"); err != nil {
		t.Fatalf("UpsertAnalysisResult() falhou: %v", err)
	}

	svc := NewLibraryService(conn, testStorageRoot(t))
	if err := svc.SetStudentSpeaker(lessonID, "speaker_0"); err != nil {
		t.Fatalf("SetStudentSpeaker() erro inesperado: %v", err)
	}

	result, err := db.FindAnalysisResult(conn, lessonID, "analyze_corrections")
	if err != nil {
		t.Fatalf("FindAnalysisResult() erro inesperado: %v", err)
	}
	if result == nil {
		t.Error("FindAnalysisResult() = nil, esperado preservado (primeira escolha de falante não descarta nada)")
	}
}

func TestLibraryService_SetStudentSpeaker_SameLabelKeepsAnalysisResults(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLesson(t, conn, "2026-07-20", "Sarah M.", "aula.mp4")
	if err := db.SetStudentSpeaker(conn, lessonID, "speaker_0"); err != nil {
		t.Fatalf("db.SetStudentSpeaker() de fixture falhou: %v", err)
	}
	promptID, err := db.UpsertPrompt(conn, "analyze_corrections", 1, "conteúdo")
	if err != nil {
		t.Fatalf("UpsertPrompt() falhou: %v", err)
	}
	if err := db.UpsertAnalysisResult(conn, lessonID, "analyze_corrections", promptID, "deepseek", "[]", "aula.analysis.json"); err != nil {
		t.Fatalf("UpsertAnalysisResult() falhou: %v", err)
	}

	svc := NewLibraryService(conn, testStorageRoot(t))
	if err := svc.SetStudentSpeaker(lessonID, "speaker_0"); err != nil {
		t.Fatalf("SetStudentSpeaker() erro inesperado: %v", err)
	}

	result, err := db.FindAnalysisResult(conn, lessonID, "analyze_corrections")
	if err != nil {
		t.Fatalf("FindAnalysisResult() erro inesperado: %v", err)
	}
	if result == nil {
		t.Error("FindAnalysisResult() = nil, esperado preservado (mesmo label não descarta nada)")
	}
}
```

- [ ] **Step 2: Rodar e confirmar que falha**

Run: `go test ./services/... -run TestLibraryService_SetStudentSpeaker -v`
Expected: FAIL — `TestLibraryService_SetStudentSpeaker_DeletesAnalysisResultsOnChange` não passa (o resultado ainda existe, já que `SetStudentSpeaker` ainda não descarta nada).

- [ ] **Step 3: Implementar**

Em `services/library.go`, trocar:

```go
func (s *LibraryService) SetStudentSpeaker(lessonID int64, speakerLabel string) error {
	return db.SetStudentSpeaker(s.conn, lessonID, speakerLabel)
}
```

por:

```go
// SetStudentSpeaker grava qual speaker bruto (ex.: "speaker_0") é o aluno
// nesta lesson — escolha que vive dentro do EditLessonModal (Fase 2,
// História 2; antes um toggle solto no Detalhe, Fase 1/História 6). Trocar
// um label já definido por um diferente apaga qualquer análise já feita da
// lesson: qualquer resultado ancorado em utterance_index passa a apontar
// pro papel errado assim que os rótulos Aluno/Tutor mudam de falante —
// trocar de novo exige reprocessar. Definir o label pela primeira vez
// (StudentSpeakerLabel ainda nil) ou repetir o label já atual não descarta
// nada.
func (s *LibraryService) SetStudentSpeaker(lessonID int64, speakerLabel string) error {
	lesson, err := db.FindLessonByID(s.conn, lessonID)
	if err != nil {
		return fmt.Errorf("buscar lesson %d: %w", lessonID, err)
	}
	if lesson == nil {
		return fmt.Errorf("aula %d não encontrada", lessonID)
	}
	if lesson.StudentSpeakerLabel != nil && *lesson.StudentSpeakerLabel != speakerLabel {
		if err := db.DeleteAnalysisResultsForLesson(s.conn, lessonID); err != nil {
			return fmt.Errorf("descartar análises antigas da lesson %d: %w", lessonID, err)
		}
	}
	return db.SetStudentSpeaker(s.conn, lessonID, speakerLabel)
}
```

- [ ] **Step 4: Rodar todos os testes do pacote e confirmar que passam**

Run: `go test ./services/... -v`
Expected: PASS em todos, incluindo os já existentes (`TestLibraryService_...` de Fase 1 não devem quebrar).

- [ ] **Step 5: `go vet` e commit**

Run: `go vet ./...`
Expected: sem saída.

```bash
git add services/library.go services/library_test.go
git commit -m "feat: SetStudentSpeaker descarta análises ao trocar quem é o aluno"
```

---

### Task 9: Registrar `AnalysisService` em `main.go` e gerar bindings

**Files:**
- Modify: `main.go`
- Regenerate: `frontend/bindings/` (via `wails3 generate bindings -ts -i ./...`, não versionado)

**Interfaces:**
- Consumes: `services.NewAnalysisService` (Task 7), `config.GetAnalysisAPIKey` (Task 1), `analysis.NewDeepSeekProvider` (já existe).
- Produces: bindings TS em `frontend/bindings/assistente-idiomas/services/analysisservice/` e o tipo `CorrectionsResult`/`CorrectionDisplay` em `frontend/bindings/assistente-idiomas/services/models.ts` — consumidos pelas Tasks 10-11.

- [ ] **Step 1: Adicionar o provider factory e registrar o serviço**

Em `main.go`, dentro de `func main()`, logo após a definição de `storageRoot` e antes de `startJobWorker(conn, storageRoot)`:

```go
	analysisProviderFactory := func() (analysis.Provider, error) {
		apiKey, err := config.GetAnalysisAPIKey()
		if err != nil {
			return nil, err
		}
		return analysis.NewDeepSeekProvider(apiKey)
	}
```

E no slice `Services`, logo após `application.NewService(services.NewSettingsService(conn, storageRoot))`:

```go
			application.NewService(services.NewAnalysisService(conn, storageRoot, analysisProviderFactory)),
```

- [ ] **Step 2: Build e vet do módulo inteiro**

Run: `go build ./... && go vet ./...`
Expected: sem erros.

- [ ] **Step 3: Gerar bindings**

Run (na raiz do projeto): `wails3 generate bindings -ts -i ./...`
Expected: sem erro; `frontend/bindings/assistente-idiomas/services/analysisservice/index.ts` (ou `analysisservice.ts`, conforme o padrão dos demais serviços) criado com `GetCorrections`/`AnalyzeCorrections`/`ReprocessCorrections`, e `CorrectionsResult`/`CorrectionDisplay` adicionados a `frontend/bindings/assistente-idiomas/services/models.ts`.

- [ ] **Step 4: Commit**

```bash
git add main.go
git commit -m "feat: registra AnalysisService no app Wails"
```

(`frontend/bindings/` é gitignored — nada a adicionar dessa regeneração.)

---

### Task 10: Frontend — credencial de análise em `Settings.svelte`

**Contexto:** Mesma seção-cartão da credencial STT, duplicada pro provedor de análise, usando os métodos da Task 6.

**Files:**
- Modify: `frontend/src/lib/screens/Settings.svelte`

**Interfaces:**
- Consumes: `SettingsService.HasAnalysisCredential()`, `SettingsService.SaveAnalysisAPIKey(apiKey: string)` (bindings gerados na Task 9, mesmo módulo `settingsservice` já importado).

- [ ] **Step 1: Adicionar estado e funções de carregamento/salvamento**

Em `frontend/src/lib/screens/Settings.svelte`, no bloco `<script>`, logo após as variáveis de estado da credencial STT (`hasCredential`, `credentialError`, `apiKeyInput`, `savingCredential`, `saveCredentialError`, `saveCredentialSuccess`):

```ts
  let hasAnalysisCredential: boolean = $state(false);
  let analysisCredentialError: string = $state("");
  let analysisApiKeyInput: string = $state("");
  let savingAnalysisCredential: boolean = $state(false);
  let saveAnalysisCredentialError: string = $state("");
  let saveAnalysisCredentialSuccess: boolean = $state(false);
```

Logo após `loadCredentialStatus`:

```ts
  async function loadAnalysisCredentialStatus() {
    try {
      hasAnalysisCredential = await SettingsService.HasAnalysisCredential();
      analysisCredentialError = "";
    } catch (e) {
      analysisCredentialError = String(e);
    }
  }
```

Logo após `saveCredential`:

```ts
  async function saveAnalysisCredential() {
    saveAnalysisCredentialError = "";
    saveAnalysisCredentialSuccess = false;
    savingAnalysisCredential = true;
    try {
      await SettingsService.SaveAnalysisAPIKey(analysisApiKeyInput);
      analysisApiKeyInput = "";
      saveAnalysisCredentialSuccess = true;
      await loadAnalysisCredentialStatus();
    } catch (e) {
      saveAnalysisCredentialError = String(e);
    } finally {
      savingAnalysisCredential = false;
    }
  }
```

- [ ] **Step 2: Incluir no carregamento inicial**

Em `onMount`, adicionar `loadAnalysisCredentialStatus()` ao `Promise.all` existente:

```ts
  onMount(async () => {
    try {
      await Promise.all([loadStorageRoot(), loadCredentialStatus(), loadAnalysisCredentialStatus(), loadTeachers()]);
    } catch (e) {
      loadError = String(e);
    } finally {
      loading = false;
    }
  });
```

- [ ] **Step 3: Adicionar a seção no template**

No template, logo após a `<section class="card">` de "Credencial do provedor de transcrição" e antes da de "Professores":

```svelte
    <section class="card" style="background: {colors.surface}; border: 1px solid {colors.line};">
      <h2 style="font-family: {fonts.display};">Credencial do provedor de análise</h2>
      {#if analysisCredentialError}
        <p class="error" style="color: {colors.red};">{analysisCredentialError}</p>
      {:else}
        <p class="status" style="color: {hasAnalysisCredential ? colors.green : colors.mut};">
          {hasAnalysisCredential ? "Credencial configurada" : "Nenhuma credencial configurada"}
        </p>
      {/if}
      <div class="credential-form">
        <input
          type="password"
          bind:value={analysisApiKeyInput}
          placeholder="Nova API key da DeepSeek"
          autocomplete="off"
          style="border: 1px solid {colors.line}; background: transparent; color: {colors.text};"
        />
        <button onclick={saveAnalysisCredential} disabled={savingAnalysisCredential || !analysisApiKeyInput}>
          {savingAnalysisCredential ? "Salvando…" : "Salvar"}
        </button>
      </div>
      {#if saveAnalysisCredentialSuccess}
        <p class="hint" style="color: {colors.green};">Credencial salva.</p>
      {/if}
      {#if saveAnalysisCredentialError}
        <p class="error" style="color: {colors.red};">{saveAnalysisCredentialError}</p>
      {/if}
    </section>
```

- [ ] **Step 4: Type-check e build do frontend**

Run: `cd frontend && npm run check && npm run build`
Expected: `check` (svelte-check) sem erros de tipo; `build` concluído.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/lib/screens/Settings.svelte
git commit -m "feat: adiciona credencial do provedor de análise em Configurações"
```

---

### Task 11: Frontend — falante no `EditLessonModal` + botão de análise e correções inline em `LessonDetail`

**Contexto:** `EditLessonModal` e `LessonDetail` mudam juntos nesta task porque são mutuamente dependentes e não compilam isolados: os props novos do modal (`speakerOptions`/`currentStudentSpeaker`/`hasAnalysisResults`) só existem de um lado se o outro lado já os estiver passando — dividir em duas tasks deixaria uma etapa intermediária com o frontend quebrado, o que viola "cada task termina num estado testável". A escolha de falante sai do painel de transcrição do Detalhe e passa a viver dentro do modal: com exatamente 2 falantes e uma escolha já feita, um único botão "Inverter falantes"; em qualquer outro caso (primeira escolha, ou 3+ falantes — diarização ruidosa), botões individuais "Speaker X é você". Trocar um falante já escolhido, quando já existe análise feita, pede confirmação antes de aplicar. `LessonDetail` ganha o botão de análise (rótulo e comportamento decididos por `corrections.analyzed`) e a renderização inline: `Before` + `Wrong` riscado + `Correction` em âmbar (com `title` = `Explanation`) + `After`; sem match (`Wrong === ""`), nota de fallback com `Original`.

**Files:**
- Modify: `frontend/src/lib/EditLessonModal.svelte`
- Modify: `frontend/src/lib/screens/LessonDetail.svelte`

**Interfaces:**
- Consumes: `LibraryService.SetStudentSpeaker(lessonID: number, speakerLabel: string)` (já existe, assinatura inalterada), `AnalysisService.GetCorrections(lessonId: number)`, `AnalysisService.AnalyzeCorrections(lessonId: number)`, `AnalysisService.ReprocessCorrections(lessonId: number)` (bindings da Task 9), tipo `CorrectionsResult` de `bindings/assistente-idiomas/services/models`.
- Produces: props novos do `EditLessonModal` — `speakerOptions: string[]`, `currentStudentSpeaker: string | null`, `hasAnalysisResults: boolean`.

#### Parte A — `EditLessonModal.svelte`

- [ ] **Step 1: Adicionar os novos props**

Em `frontend/src/lib/EditLessonModal.svelte`, trocar a desestruturação de `$props()`:

```ts
  let {
    lessonId,
    initialLessonDate,
    initialTeacherName,
    speakerOptions,
    currentStudentSpeaker,
    hasAnalysisResults,
    onSaved,
    onClose,
  }: {
    lessonId: number;
    initialLessonDate: string;
    initialTeacherName: string;
    speakerOptions: string[];
    currentStudentSpeaker: string | null;
    hasAnalysisResults: boolean;
    onSaved: () => void;
    onClose: () => void;
  } = $props();
```

- [ ] **Step 2: Estado e funções de escolha de falante**

Logo após `let saving: boolean = $state(false);`:

```ts
  let studentSpeaker: string | null = $state(untrack(() => currentStudentSpeaker));
  let speakerError: string = $state("");
  let savingSpeaker: boolean = $state(false);

  function speakerLabel(speaker: string): string {
    const idx = speakerOptions.indexOf(speaker);
    return `Speaker ${String.fromCharCode(65 + (idx < 0 ? 0 : idx))}`;
  }

  async function chooseSpeaker(newSpeaker: string) {
    if (newSpeaker === studentSpeaker) return;
    if (currentStudentSpeaker !== null && hasAnalysisResults) {
      const confirmed = confirm(
        "Trocar quem é você descarta as análises já feitas dessa aula — você vai precisar reprocessar. Continuar?",
      );
      if (!confirmed) return;
    }
    speakerError = "";
    savingSpeaker = true;
    try {
      await LibraryService.SetStudentSpeaker(lessonId, newSpeaker);
      studentSpeaker = newSpeaker;
      onSaved();
    } catch (e) {
      speakerError = String(e);
    } finally {
      savingSpeaker = false;
    }
  }
```

- [ ] **Step 3: Adicionar a seção no template**

No template, logo após o bloco de `TeacherCombobox` (`<label for="edit-tutor">Tutor</label> <TeacherCombobox ... />`) e antes do `{#if error}`:

```svelte
    {#if speakerOptions.length > 0}
      <span class="speaker-section-title">Quem é você</span>
      {#if speakerOptions.length === 2 && studentSpeaker !== null}
        <button
          type="button"
          class="secondary"
          onclick={() => chooseSpeaker(speakerOptions.find((s) => s !== studentSpeaker) ?? speakerOptions[0])}
          disabled={savingSpeaker}
        >
          {savingSpeaker ? "Salvando…" : "Inverter falantes"}
        </button>
      {:else}
        <div class="speaker-buttons">
          {#each speakerOptions as speaker (speaker)}
            <button
              type="button"
              class="secondary"
              class:active={studentSpeaker === speaker}
              onclick={() => chooseSpeaker(speaker)}
              disabled={savingSpeaker}
            >
              {speakerLabel(speaker)} é você
            </button>
          {/each}
        </div>
      {/if}
      {#if studentSpeaker}
        <p class="hint">Atualmente: {speakerLabel(studentSpeaker)}</p>
      {/if}
      {#if speakerError}
        <p class="error" style="color: {colors.red};">{speakerError}</p>
      {/if}
    {/if}
```

- [ ] **Step 4: Estilos novos**

No bloco `<style>`, logo após o seletor `label`:

```css
  .speaker-section-title {
    display: block;
    font-size: 0.8rem;
    margin-bottom: 0.5rem;
  }
  .speaker-buttons {
    display: flex;
    gap: 0.5rem;
    flex-wrap: wrap;
    margin-bottom: 0.75rem;
  }
  .speaker-buttons button.active {
    font-weight: 600;
  }
  .hint {
    font-size: 0.75rem;
    margin: 0 0 0.75rem;
  }
```

#### Parte B — `LessonDetail.svelte`

- [ ] **Step 5: Importar o serviço e o tipo**

No topo do `<script>`, junto aos imports existentes:

```ts
  import * as AnalysisService from "../../../bindings/assistente-idiomas/services/analysisservice";
```

No import de tipos já existente, adicionar `CorrectionsResult`:

```ts
  import type { Lesson, Transcript, CorrectionsResult } from "../../../bindings/assistente-idiomas/services/models";
```

- [ ] **Step 6: Estado de correções**

Logo após `let retrying: boolean = $state(false);`:

```ts
  let corrections: CorrectionsResult | null = $state(null);
  let analyzingCorrections: boolean = $state(false);
  let correctionsError: string = $state("");
```

- [ ] **Step 7: Funções de busca/disparo**

Logo após `fetchTranscriptIfReady`:

```ts
  async function fetchCorrectionsIfReady() {
    if (!lesson || lesson.status !== "pronta" || !lesson.studentSpeakerLabel) {
      corrections = null;
      return;
    }
    try {
      corrections = await AnalysisService.GetCorrections(lessonId);
    } catch {
      corrections = null;
    }
  }

  async function analyzeCorrections() {
    correctionsError = "";
    analyzingCorrections = true;
    try {
      corrections = await AnalysisService.AnalyzeCorrections(lessonId);
    } catch (e) {
      correctionsError = String(e);
    } finally {
      analyzingCorrections = false;
    }
  }

  async function reprocessCorrections() {
    const confirmed = confirm("Isso sobrescreve a análise atual e gera uma nova chamada à API. Continuar?");
    if (!confirmed) return;
    correctionsError = "";
    analyzingCorrections = true;
    try {
      corrections = await AnalysisService.ReprocessCorrections(lessonId);
    } catch (e) {
      correctionsError = String(e);
    } finally {
      analyzingCorrections = false;
    }
  }
```

- [ ] **Step 8: Buscar correções no mount e depois de editar a aula**

Em `onMount`, logo após `await fetchTranscriptIfReady();`:

```ts
      await fetchCorrectionsIfReady();
```

Em `onLessonSaved`, logo após `lesson = await LibraryService.GetLesson(lessonId);`:

```ts
      await fetchCorrectionsIfReady();
```

(o `try/catch` de `onLessonSaved` já existe e cobre isso — `fetchCorrectionsIfReady` também absorve seu próprio erro internamente, mas deixa o `lesson` já atualizado disparar a rebusca mesmo assim.)

- [ ] **Step 9: Remover o toggle solto e passar os novos props pro `EditLessonModal`**

Remover a função `chooseStudentSpeaker` e o bloco `<div class="speaker-toggle">...</div>` do template (o painel de transcrição, dentro do `{:else}` que mostra a transcrição carregada).

Trocar a instanciação do modal:

```svelte
{#if editing && lesson}
  <EditLessonModal
    lessonId={lesson.id}
    initialLessonDate={lesson.lessonDate}
    initialTeacherName={lesson.tutor}
    speakerOptions={speakerOrder}
    currentStudentSpeaker={lesson.studentSpeakerLabel}
    hasAnalysisResults={corrections?.analyzed ?? false}
    onSaved={onLessonSaved}
    onClose={() => (editing = false)}
  />
{/if}
```

- [ ] **Step 10: Botão de análise, no lugar onde estava o toggle**

No template, no mesmo lugar onde vivia `.speaker-toggle` (painel de transcrição, antes de `.transcript`):

```svelte
          {#if !lesson.studentSpeakerLabel}
            <p class="hint" style="color: {colors.mut};">
              Escolha quem é você em "Editar" para habilitar a análise de correções.
            </p>
          {:else}
            <div class="corrections-actions">
              {#if corrections?.analyzed}
                <button onclick={reprocessCorrections} disabled={analyzingCorrections}>
                  {analyzingCorrections ? "Reprocessando…" : "Reprocessar correções"}
                </button>
              {:else}
                <button onclick={analyzeCorrections} disabled={analyzingCorrections}>
                  {analyzingCorrections ? "Analisando…" : "Analisar correções"}
                </button>
              {/if}
              {#if correctionsError}
                <p class="error" style="color: {colors.red};">{correctionsError}</p>
              {/if}
            </div>
          {/if}
```

- [ ] **Step 11: Renderizar o texto corrigido inline**

Substituir o `<p class="text" style="color: {colors.text};">{utterance.text}</p>` dentro do `{#each transcript.utterances as utterance, i (i)}` (logo depois do `{@const role = roleFor(utterance.speaker)}` já existente) por:

```svelte
              {@const correctionForRow = role === "aluno" ? corrections?.items.find((c) => c.utteranceIndex === i) : undefined}
              {#if correctionForRow && correctionForRow.wrong}
                <p class="text" style="color: {colors.text};">{correctionForRow.before}<span
                    class="corrected-original"
                    style="color: {colors.mut};">{correctionForRow.wrong}</span
                  > <span class="corrected-fix" style="color: {colors.amber};" title={correctionForRow.explanation}
                    >{correctionForRow.correction}</span
                  >{correctionForRow.after}</p>
              {:else if correctionForRow}
                <p class="text" style="color: {colors.text};">{utterance.text}</p>
                <p class="correction-fallback" style="color: {colors.mut};">
                  ⚠ correção não localizada: "{correctionForRow.original}" → "{correctionForRow.correction}" — {correctionForRow.explanation}
                </p>
              {:else}
                <p class="text" style="color: {colors.text};">{utterance.text}</p>
              {/if}
```

- [ ] **Step 12: Estilos novos**

No bloco `<style>`, logo após `.text`:

```css
  .corrections-actions {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    flex-wrap: wrap;
    margin-bottom: 0.5rem;
  }
  .corrections-actions button {
    font-size: 0.8rem;
    padding: 0.4rem 0.8rem;
    border-radius: 0.5rem;
    cursor: pointer;
  }
  .corrected-original {
    text-decoration: line-through;
  }
  .corrected-fix {
    font-weight: 600;
  }
  .correction-fallback {
    font-size: 0.75rem;
    font-style: italic;
    margin: 0 0.75rem 0.25rem;
  }
```

- [ ] **Step 13: Type-check e build do frontend**

Run: `cd frontend && npm run check && npm run build`
Expected: sem erros de tipo, build concluído — os dois arquivos (`EditLessonModal.svelte` e `LessonDetail.svelte`) só fecham juntos, já que os props novos do modal só existem preenchidos do lado de quem o instancia.

- [ ] **Step 14: Commit**

```bash
git add frontend/src/lib/EditLessonModal.svelte frontend/src/lib/screens/LessonDetail.svelte
git commit -m "feat: escolha de falante no EditLessonModal e correções inline no Detalhe"
```

---

### Task 12: Remover `cmd/validate-analysis` e verificar a suíte completa

**Contexto:** A validação em bloco via CLI foi substituída pela validação visual desta própria história (decisão já registrada em `docs/fase-2-analise-llm.md` e na spec de replanejamento). `cmd/validate-analysis` cumpriu seu papel de design/experimentação e sai do repositório sem ter rodado — mesmo destino do extinto `cmd/spike` da Fase 0.

**Files:**
- Delete: `cmd/validate-analysis/` (diretório inteiro)

- [ ] **Step 1: Remover o CLI temporário**

```bash
git rm -r cmd/validate-analysis
```

- [ ] **Step 2: Build, vet e suíte completa do módulo Go**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: build limpo, vet sem saída, todos os testes passando (a remoção do CLI não afeta nenhum pacote testado — `cmd/validate-analysis` não tem testes próprios nem é importado por ninguém).

- [ ] **Step 3: Build do frontend de novo, garantindo que nada quebrou**

Run: `cd frontend && npm run build`
Expected: sem erros.

- [ ] **Step 4: Commit**

```bash
git commit -m "chore: remove cmd/validate-analysis (substituído pela validação visual da História 2)"
```

---

### Task 13: Verificação manual numa aula real e fechamento da história

**Contexto:** Critério de aceite explícito da História 2 — depois de observar o resultado em aulas reais, registrar a decisão (manter/refinar/descartar) em `docs/notas-analise-llm.md`. É o que fecha a história, não uma contagem fixa de aulas.

**Files:**
- Modify: `docs/notas-analise-llm.md`
- Modify: `docs/fase-2-analise-llm.md`

- [ ] **Step 1: Verificação manual (executar por conta própria — não delegar a um subagent)**

1. Ter ao menos uma aula com status "pronta" no banco local.
2. Configurar a credencial da DeepSeek em Configurações.
3. Rodar `wails3 dev` (ou `wails3 build` + abrir o binário), abrir essa aula no Detalhe.
4. Sem falante escolhido ainda: confirmar que aparece a dica "Escolha quem é você em 'Editar'…" no lugar do botão de análise, e que o toggle antigo não existe mais solto no painel.
5. Abrir "Editar", escolher o falante (botões individuais na primeira escolha) e salvar — confirmar que o modal fecha e a dica vira o botão "Analisar correções".
6. Clicar em "Analisar correções": observar "Analisando…", depois "Reprocessar correções"; falas do aluno com correção mostram o trecho riscado (cinza) seguido da correção em destaque (âmbar), com a explicação aparecendo no hover.
7. Clicar em "Reprocessar correções": confirmar que aparece o diálogo de confirmação antes de disparar de novo.
8. Abrir "Editar" de novo e trocar o falante (agora com 2 speakers, deve aparecer "Inverter falantes"): confirmar que aparece o aviso de descarte, e que depois de confirmar a análise sumiu (botão volta a "Analisar correções").
9. Revisar a qualidade das correções encontradas (comparando com a leitura da própria transcrição), incluindo qualquer nota de fallback (correção não localizada no texto).

- [ ] **Step 2: Registrar os achados em `docs/notas-analise-llm.md`**

Adicionar uma seção nova ao arquivo (mesmo formato das seções existentes de Fase 0), cobrindo: qualidade observada (correções fazem sentido? algum falso positivo/negativo? quantas caíram no fallback de "não localizada"?), quantas aulas foram observadas, e a **decisão explícita** que fecha a História 2 — manter como está, refinar o prompt (`prompts/analyze-corrections-v1.md`, bump de versão), ou descartar a tarefa.

- [ ] **Step 3: Marcar a História 2 como concluída em `docs/fase-2-analise-llm.md`**

Marcar os critérios de aceite da seção "História 2 — Piloto: Correções do aluno" como `[x]`, e adicionar uma linha ao "Registro de progresso" (data de hoje) resumindo o que foi implementado e a decisão registrada no Step 2.

- [ ] **Step 4: Commit final da história**

```bash
git add docs/notas-analise-llm.md docs/fase-2-analise-llm.md
git commit -m "docs: registra validação e fecha a Fase 2, História 2 (Piloto: Correções do aluno)"
```
