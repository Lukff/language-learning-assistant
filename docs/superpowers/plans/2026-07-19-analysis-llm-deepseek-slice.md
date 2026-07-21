# Fase 0 — História 3, Fatia 1: Análise LLM via DeepSeek Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the `internal/analysis` package (domain types, JSON-schema-only prompt,
shared response parsing, and one HTTP client) for DeepSeek — the cheapest of the 6 LLM candidates
being explored for the lesson analysis feature — and wire it into `cmd/spike` as the first
runnable analysis provider, reusing the already-saved ElevenLabs transcript instead of
re-transcribing.

**Architecture:** `internal/analysis/analysis.go` (the `Provider` interface + domain types) +
`internal/analysis/parsing.go` (shared, provider-agnostic JSON parsing — the schema is ours, not
the provider's) + `internal/analysis/transcript.go` (formats `stt.Utterance` into prompt text,
plus a helper for interactive speaker-role confirmation) + `internal/analysis/openai_compatible.go`
(one generic HTTP client for any `/chat/completions`-style API, today only wired to DeepSeek) +
`prompts/analyze-v1.md` (the versioned system prompt) + `cmd/spike` (disposable CLI wiring: a new
`-analysis-providers` flag, a new `cmd/spike/analysis.go` orchestration file, and a small addition
to the existing STT flow so it persists `utterances.json`).

**Tech Stack:** Go stdlib only (`net/http`, `encoding/json`, `bufio`, `context`). No third-party
dependencies.

## Global Constraints

- `internal/analysis` stays production-quality and permanent; `cmd/spike/main.go` and the new
  `cmd/spike/analysis.go` stay disposable.
- `DEEPSEEK_API_KEY` read only from environment (or `.env` via the existing `loadDotEnv`), never
  hardcoded/committed.
- DeepSeek auth header is `Authorization: Bearer <chave>` (OpenAI-compatible format).
- DeepSeek's Chat Prefix Completion (the mechanism behind the `` ```json `` prefill) requires
  `base_url = "https://api.deepseek.com/beta"` and the last message in the request to have
  `role: "assistant"`, `content: "```json\n"`, and `"prefix": true`. Model used:
  `deepseek-v4-flash` (cheapest tier: ~$0.14/M input, ~$0.28/M output — confirmed at
  `api-docs.deepseek.com/quick_start/pricing/`, julho/2026).
- Also send `response_format: {"type": "json_object"}` (DeepSeek's native JSON mode — belt and
  suspenders with the prefill) and `stop: ["```"]` (stops generation right after the model closes
  the fence, so there's usually nothing to strip).
- The model's response is the **continuation only** — it does not repeat the `` ```json\n ``
  prefix. `parseAnalysisResponse` receives that continuation directly (no reconstruction with the
  prefix) and only strips a trailing `` ``` `` (and whitespace) if present.
- Raw provider JSON (the full chat-completion envelope) is always preserved on disk, even when
  parsing fails — same pattern as `internal/stt`.
- No elaborate flags, no parallelism, no sophisticated retry.
- Commit messages: one line, semantic format (`tipo: descrição`) — do not run `git commit`
  automatically; stage only (`git add`), per this project's established workflow preference.
- This plan implements **only the DeepSeek slice**. `analysisProviderFactories` in
  `cmd/spike/analysis.go` gets exactly one entry (`"deepseek"`); Qwen/GLM/Anthropic/OpenAI/Gemini
  are out of scope here — they get their own future plans if DeepSeek's quality doesn't convince
  (see `docs/superpowers/specs/2026-07-19-analysis-llm-v1-design.md`, "Estratégia de fatias").

---

### Task 1: `internal/analysis` domain types and shared JSON parsing (fixture-tested)

**Files:**
- Create: `internal/analysis/analysis.go`
- Create: `internal/analysis/parsing.go`
- Test: `internal/analysis/parsing_test.go`

**Interfaces:**
- Produces (used by Task 4): `Provider` interface, `Result`, `Correction`, `VocabularyItem`,
  `Expression` structs (all in `analysis.go`); `parseAnalysisResponse(raw []byte) (*Result, error)`
  (unexported, in `parsing.go`)

- [ ] **Step 1: Write `internal/analysis/analysis.go`**

```go
// internal/analysis/analysis.go
package analysis

import "context"

// Provider é a interface única implementada por cada serviço de análise LLM
// candidato (DeepSeek, e em fatias futuras: Anthropic, OpenAI, Gemini, GLM,
// Qwen — ver docs/superpowers/specs/2026-07-19-analysis-llm-v1-design.md).
type Provider interface {
	Name() string
	Analyze(ctx context.Context, transcript string) (*Result, error)
}

// Result carrega tanto o JSON bruto retornado pelo provedor (envelope HTTP
// completo, pra salvar em disco sem perda) quanto a análise já mapeada para
// o domínio comum.
type Result struct {
	RawResponse      []byte
	Corrections      []Correction
	Vocabulary       []VocabularyItem
	TutorExpressions []Expression
}

// Correction é uma correção de uma fala do Aluno.
type Correction struct {
	Original    string
	Correction  string
	Explanation string // PT-BR
}

// VocabularyItem é uma palavra ou expressão nova pro Aluno aprender —
// inclui palavras em PT/ES usadas como recurso ao idioma nativo, nunca
// tratadas como erro de inglês.
type VocabularyItem struct {
	Term        string
	Translation string
}

// Expression é uma expressão do Tutor que vale a pena o Aluno reutilizar.
type Expression struct {
	Text string
	Note string // PT-BR, contexto de uso
}
```

- [ ] **Step 2: Write the failing tests for `parseAnalysisResponse`**

```go
// internal/analysis/parsing_test.go
package analysis

import "testing"

func TestParseAnalysisResponse(t *testing.T) {
	raw := []byte(`{
		"corrections": [{"original": "I go yesterday", "correction": "I went yesterday", "explanation": "Passado simples irregular."}],
		"vocabulary": [{"term": "homesick", "translation": "com saudade de casa"}],
		"tutor_expressions": [{"text": "let's circle back to that", "note": "retomar um assunto depois"}]
	}`)

	result, err := parseAnalysisResponse(raw)
	if err != nil {
		t.Fatalf("parseAnalysisResponse retornou erro: %v", err)
	}

	if len(result.Corrections) != 1 || result.Corrections[0].Original != "I go yesterday" {
		t.Errorf("Corrections = %+v, inesperado", result.Corrections)
	}
	if result.Corrections[0].Correction != "I went yesterday" || result.Corrections[0].Explanation != "Passado simples irregular." {
		t.Errorf("Corrections[0] = %+v, inesperado", result.Corrections[0])
	}
	if len(result.Vocabulary) != 1 || result.Vocabulary[0].Term != "homesick" || result.Vocabulary[0].Translation != "com saudade de casa" {
		t.Errorf("Vocabulary = %+v, inesperado", result.Vocabulary)
	}
	if len(result.TutorExpressions) != 1 || result.TutorExpressions[0].Text != "let's circle back to that" {
		t.Errorf("TutorExpressions = %+v, inesperado", result.TutorExpressions)
	}
}

func TestParseAnalysisResponse_EmptyLists(t *testing.T) {
	raw := []byte(`{"corrections": [], "vocabulary": [], "tutor_expressions": []}`)

	result, err := parseAnalysisResponse(raw)
	if err != nil {
		t.Fatalf("parseAnalysisResponse retornou erro: %v", err)
	}
	if len(result.Corrections) != 0 || len(result.Vocabulary) != 0 || len(result.TutorExpressions) != 0 {
		t.Errorf("esperava listas vazias, obteve %+v", result)
	}
}

func TestParseAnalysisResponse_StripsTrailingCodeFence(t *testing.T) {
	raw := []byte("{\"corrections\": [], \"vocabulary\": [], \"tutor_expressions\": []}\n```")

	result, err := parseAnalysisResponse(raw)
	if err != nil {
		t.Fatalf("parseAnalysisResponse retornou erro: %v", err)
	}
	if len(result.Corrections) != 0 || len(result.Vocabulary) != 0 || len(result.TutorExpressions) != 0 {
		t.Errorf("esperava listas vazias após remover o fence, obteve %+v", result)
	}
}

func TestParseAnalysisResponse_InvalidJSON(t *testing.T) {
	_, err := parseAnalysisResponse([]byte("not json"))
	if err == nil {
		t.Fatal("esperava erro para JSON inválido, obteve nil")
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go.exe test ./internal/analysis/... -v`
Expected: FAIL — build error, `undefined: parseAnalysisResponse`

- [ ] **Step 4: Write `internal/analysis/parsing.go`**

```go
// internal/analysis/parsing.go
package analysis

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// parseAnalysisResponse converte o texto de conteúdo devolvido pelo LLM (já
// sem o envelope HTTP do provedor) para o domínio comum Result. O schema é
// definido por nós em prompts/analyze-v1.md, não pelo provedor — por isso
// esta função é compartilhada por todos os clients, ao contrário do
// mapeamento por provedor do internal/stt.
//
// Quando o provedor suporta prefill (ver openai_compatible.go), o modelo já
// começa a resposta direto no conteúdo do JSON — a única sujeira possível é
// um fechamento de code fence sobrando no final, removido abaixo. Não há
// abertura de fence a remover, e provedores sem prefill (modo JSON nativo)
// já devolvem JSON puro, então o strip é um no-op inofensivo para eles.
func parseAnalysisResponse(raw []byte) (*Result, error) {
	trimmed := stripTrailingCodeFence(raw)

	var parsed analysisJSON
	if err := json.Unmarshal(trimmed, &parsed); err != nil {
		return nil, fmt.Errorf("analysis: json inválido: %w", err)
	}

	return &Result{
		Corrections:      parsed.corrections(),
		Vocabulary:       parsed.vocabulary(),
		TutorExpressions: parsed.tutorExpressions(),
	}, nil
}

func stripTrailingCodeFence(raw []byte) []byte {
	trimmed := bytes.TrimSpace(raw)
	trimmed = bytes.TrimSuffix(trimmed, []byte("```"))
	return bytes.TrimSpace(trimmed)
}

type analysisJSON struct {
	Corrections []struct {
		Original    string `json:"original"`
		Correction  string `json:"correction"`
		Explanation string `json:"explanation"`
	} `json:"corrections"`
	Vocabulary []struct {
		Term        string `json:"term"`
		Translation string `json:"translation"`
	} `json:"vocabulary"`
	TutorExpressions []struct {
		Text string `json:"text"`
		Note string `json:"note"`
	} `json:"tutor_expressions"`
}

func (a analysisJSON) corrections() []Correction {
	out := make([]Correction, 0, len(a.Corrections))
	for _, c := range a.Corrections {
		out = append(out, Correction{Original: c.Original, Correction: c.Correction, Explanation: c.Explanation})
	}
	return out
}

func (a analysisJSON) vocabulary() []VocabularyItem {
	out := make([]VocabularyItem, 0, len(a.Vocabulary))
	for _, v := range a.Vocabulary {
		out = append(out, VocabularyItem{Term: v.Term, Translation: v.Translation})
	}
	return out
}

func (a analysisJSON) tutorExpressions() []Expression {
	out := make([]Expression, 0, len(a.TutorExpressions))
	for _, e := range a.TutorExpressions {
		out = append(out, Expression{Text: e.Text, Note: e.Note})
	}
	return out
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go.exe test ./internal/analysis/... -v`
Expected: all 4 tests pass —
`ok  	assistente-idiomas/internal/analysis`

- [ ] **Step 6: Run `go vet`**

Run: `go.exe vet ./...`
Expected: no output (clean)

- [ ] **Step 7: Stage changes (do not commit)**

```bash
git add internal/analysis/analysis.go internal/analysis/parsing.go internal/analysis/parsing_test.go
```

---

### Task 2: Transcript formatting and speaker-example helper (fixture-tested)

**Files:**
- Create: `internal/analysis/transcript.go`
- Test: `internal/analysis/transcript_test.go`

**Interfaces:**
- Consumes: `stt.Utterance` (`internal/stt/stt.go`, unchanged — fields `Speaker`, `Text`, `Start`,
  `End`, `Words`, all exported)
- Produces (used by Task 5): `FormatTranscript(utterances []stt.Utterance, speakerRoles
  map[string]string) (string, error)`, `SpeakerExamples(utterances []stt.Utterance, n int)
  map[string][]string`

- [ ] **Step 1: Write the failing tests**

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

	want := "Tutor: Hi, how was your week?\n" +
		"Aluno: It was good, I felt a lot of saudade for my hometown though.\n" +
		"Tutor: That's understandable.\n"
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

- [ ] **Step 2: Run tests to verify they fail**

Run: `go.exe test ./internal/analysis/... -v -run "TestFormatTranscript|TestSpeakerExamples"`
Expected: FAIL — build error, `undefined: FormatTranscript`

- [ ] **Step 3: Write `internal/analysis/transcript.go`**

```go
// internal/analysis/transcript.go
package analysis

import (
	"fmt"
	"strings"

	"assistente-idiomas/internal/stt"
)

// FormatTranscript converte as utterances diarizadas num texto legível pro
// prompt, rotulando cada fala como "Aluno" ou "Tutor" conforme
// speakerRoles (valores aceitos: "aluno" ou "tutor"). Erro se algum
// Speaker não estiver mapeado ou tiver um papel diferente desses dois —
// falha explícita, sem chute silencioso que contaminaria toda a análise.
func FormatTranscript(utterances []stt.Utterance, speakerRoles map[string]string) (string, error) {
	var b strings.Builder
	for _, u := range utterances {
		role, ok := speakerRoles[u.Speaker]
		if !ok {
			return "", fmt.Errorf("analysis: locutor %q sem papel mapeado em speakerRoles", u.Speaker)
		}

		var label string
		switch role {
		case "aluno":
			label = "Aluno"
		case "tutor":
			label = "Tutor"
		default:
			return "", fmt.Errorf("analysis: papel %q inválido para locutor %q (esperado \"aluno\" ou \"tutor\")", role, u.Speaker)
		}

		fmt.Fprintf(&b, "%s: %s\n", label, u.Text)
	}
	return b.String(), nil
}

// SpeakerExamples retorna até n falas de exemplo por rótulo de speaker, na
// ordem em que aparecem em utterances — insumo pra um humano confirmar quem
// é aluno e quem é tutor antes de montar o speakerRoles usado por
// FormatTranscript.
func SpeakerExamples(utterances []stt.Utterance, n int) map[string][]string {
	examples := make(map[string][]string)
	for _, u := range utterances {
		if len(examples[u.Speaker]) >= n {
			continue
		}
		examples[u.Speaker] = append(examples[u.Speaker], u.Text)
	}
	return examples
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go.exe test ./internal/analysis/... -v`
Expected: all tests pass, including the 4 from Task 1 and 5 from this task —
`ok  	assistente-idiomas/internal/analysis`

- [ ] **Step 5: Run `go vet`**

Run: `go.exe vet ./...`
Expected: no output (clean)

- [ ] **Step 6: Stage changes (do not commit)**

```bash
git add internal/analysis/transcript.go internal/analysis/transcript_test.go
```

---

### Task 3: The versioned analysis prompt

**Files:**
- Create: `prompts/analyze-v1.md`

**Interfaces:**
- Produces: a text file read at runtime by `cmd/spike` (Task 5) and passed as the `systemPrompt`
  argument to `analysis.NewDeepSeekProvider`

- [ ] **Step 1: Write the prompt**

```markdown
# Prompt de análise de aula — v1

Você é um assistente que analisa a transcrição diarizada de uma aula particular de inglês entre
um Aluno e um Tutor (plataforma Cambly). A aula é majoritariamente em inglês, com eventual troca
para português ou espanhol (code-switching) por parte do Aluno.

Sua tarefa é produzir **apenas um objeto JSON**, sem nenhum texto antes ou depois, seguindo
exatamente este formato:

```json
{
  "corrections": [
    {"original": "...", "correction": "...", "explanation": "..."}
  ],
  "vocabulary": [
    {"term": "...", "translation": "..."}
  ],
  "tutor_expressions": [
    {"text": "...", "note": "..."}
  ]
}
```

## Regras

1. **corrections**: liste erros de inglês nas falas do **Aluno**. Cada item tem a fala original
   (`original`), a correção (`correction`) e uma explicação curta em português (`explanation`).
   Não invente correções para frases já corretas.
2. **vocabulary**: liste palavras ou expressões novas que valem a pena o Aluno aprender, com
   tradução para português (`translation`). **Importante:** se o Aluno usar uma palavra ou frase
   em português ou espanhol no meio da fala em inglês, isso **não é um erro de inglês** — é um
   recurso ao idioma nativo, e a palavra/expressão em inglês que faltou ao Aluno é candidata a
   `vocabulary`, nunca a `corrections`.
3. **tutor_expressions**: liste expressões que o **Tutor** usou e que seriam úteis para o Aluno
   reutilizar no futuro, com uma nota curta em português (`note`) sobre o contexto de uso.
4. Se não houver itens para alguma categoria, devolva uma lista vazia (`[]`) para ela — nunca
   omita a chave.
5. A resposta deve ser **apenas o objeto JSON** acima: sem markdown, sem comentários, sem texto
   explicativo fora do JSON.

A transcrição da aula será enviada na mensagem seguinte, com cada fala rotulada "Aluno:" ou
"Tutor:".
```

- [ ] **Step 2: Confirm the file is non-empty and readable**

Run: `wc -l prompts/analyze-v1.md`
Expected: a positive line count (no output error)

- [ ] **Step 3: Stage changes (do not commit)**

```bash
git add prompts/analyze-v1.md
```

---

### Task 4: `internal/analysis/openai_compatible.go` — DeepSeek client (`Provider` implementation)

**Files:**
- Create: `internal/analysis/openai_compatible.go`

**Interfaces:**
- Consumes: `parseAnalysisResponse(raw []byte) (*Result, error)` (Task 1); `Provider`, `Result`
  (Task 1)
- Produces: `analysis.NewDeepSeekProvider(apiKey, systemPrompt string) (Provider, error)`,
  satisfying `analysis.Provider`

> No unit tests for this task, same justification as every `internal/stt` client: a live HTTP call
> is only exercised manually via the CLI, not unit-tested. Verification here is build + vet only.

- [ ] **Step 1: Write the implementation**

```go
// internal/analysis/openai_compatible.go
package analysis

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	systemPrompt    string
	supportsPrefill bool
	client          *http.Client
}

func newOpenAICompatibleProvider(name, baseURL, apiKey, model, systemPrompt string, supportsPrefill bool) (*openAICompatibleProvider, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("analysis: chave de API vazia para %s", name)
	}
	if systemPrompt == "" {
		return nil, fmt.Errorf("analysis: prompt de sistema vazio para %s", name)
	}
	return &openAICompatibleProvider{
		name:            name,
		baseURL:         baseURL,
		apiKey:          apiKey,
		model:           model,
		systemPrompt:    systemPrompt,
		supportsPrefill: supportsPrefill,
		client:          &http.Client{Timeout: 5 * time.Minute},
	}, nil
}

// NewDeepSeekProvider cria um Provider pra API do DeepSeek, modelo
// deepseek-v4-flash (tier mais barato — ver "Estratégia de fatias" no design
// doc). Usa o base URL beta, exigido pelo recurso de "Chat Prefix
// Completion" que sustenta o prefill de ```json.
func NewDeepSeekProvider(apiKey, systemPrompt string) (Provider, error) {
	return newOpenAICompatibleProvider("deepseek", "https://api.deepseek.com/beta", apiKey, "deepseek-v4-flash", systemPrompt, true)
}

func (p *openAICompatibleProvider) Name() string { return p.name }

func (p *openAICompatibleProvider) Analyze(ctx context.Context, transcript string) (*Result, error) {
	req, err := p.buildRequest(ctx, transcript)
	if err != nil {
		return nil, fmt.Errorf("analysis: montar requisição %s: %w", p.name, err)
	}

	raw, err := p.do(req)
	if err != nil {
		return nil, fmt.Errorf("analysis: chamar %s: %w", p.name, err)
	}

	var envelope openAICompatibleEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		// Preserva o envelope bruto mesmo em falha de parse: a chamada já
		// custou dinheiro, então o chamador deve conseguir salvar
		// result.RawResponse em disco mesmo com err != nil.
		return &Result{RawResponse: raw}, fmt.Errorf("analysis: parsear envelope %s: %w", p.name, err)
	}
	if len(envelope.Choices) == 0 {
		return &Result{RawResponse: raw}, fmt.Errorf("analysis: %s não retornou choices", p.name)
	}

	result, err := parseAnalysisResponse([]byte(envelope.Choices[0].Message.Content))
	if err != nil {
		return &Result{RawResponse: raw}, fmt.Errorf("analysis: parsear conteúdo %s: %w", p.name, err)
	}
	result.RawResponse = raw
	return result, nil
}

func (p *openAICompatibleProvider) buildRequest(ctx context.Context, transcript string) (*http.Request, error) {
	messages := []chatMessage{
		{Role: "system", Content: p.systemPrompt},
		{Role: "user", Content: transcript},
	}

	reqBody := chatCompletionRequest{
		Model:          p.model,
		Messages:       messages,
		ResponseFormat: &responseFormat{Type: "json_object"},
	}

	if p.supportsPrefill {
		reqBody.Messages = append(reqBody.Messages, chatMessage{
			Role:    "assistant",
			Content: "```json\n",
			Prefix:  true,
		})
		reqBody.Stop = []string{"```"}
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
}
```

- [ ] **Step 2: Verify it compiles and vets clean**

Run: `go.exe build ./... && go.exe vet ./...`
Expected: no output (both clean — `*openAICompatibleProvider` satisfies `analysis.Provider` at
compile time via its method set)

- [ ] **Step 3: Stage changes (do not commit)**

```bash
git add internal/analysis/openai_compatible.go
```

---

### Task 5: Wire the DeepSeek analysis flow into `cmd/spike`

**Files:**
- Create: `cmd/spike/analysis.go`
- Modify: `cmd/spike/main.go` (add `-analysis-providers` flag, branch in `main()`, persist
  `utterances.json` at the end of `runProvider`)
- Modify: `.env.example`

**Interfaces:**
- Consumes: `analysis.NewDeepSeekProvider` (Task 4), `analysis.FormatTranscript`,
  `analysis.SpeakerExamples` (Task 2), `stt.Utterance` (`internal/stt/stt.go`, unchanged)

- [ ] **Step 1: Add `utterances.json` persistence to the existing STT flow**

In `cmd/spike/main.go`, add `"encoding/json"` to the import block:

```go
import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"assistente-idiomas/internal/media"
	"assistente-idiomas/internal/stt"
)
```

Then, in `runProvider`, right after the existing `saveRawResponse` call succeeds (the block that
logs `"JSON bruto salvo"`), add a call to persist the utterances too — insert this right before
the `txtPath := ...` line:

```go
	if err := saveUtterances(providerOutDir, result); err != nil {
		logger.Error("salvar utterances", "provedor", provider.Name(), "erro", err)
		return false
	}
```

And add the new helper function near `saveRawResponse`:

```go
// saveUtterances grava result.Utterances como JSON em outDir/utterances.json
// — permite que a análise LLM (História 3) reaproveite a transcrição
// diarizada sem re-rodar o STT nem depender das funções de mapeamento
// não-exportadas de cada provedor de STT.
func saveUtterances(outDir string, result *stt.Result) error {
	data, err := json.Marshal(result.Utterances)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, "utterances.json"), data, 0o644)
}
```

- [ ] **Step 2: Write `cmd/spike/analysis.go`**

```go
// cmd/spike/analysis.go
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"assistente-idiomas/internal/analysis"
	"assistente-idiomas/internal/stt"
)

var analysisProviderFactories = map[string]func(apiKey, systemPrompt string) (analysis.Provider, error){
	"deepseek": func(apiKey, systemPrompt string) (analysis.Provider, error) {
		return analysis.NewDeepSeekProvider(apiKey, systemPrompt)
	},
}

var analysisProviderEnvVars = map[string]string{
	"deepseek": "DEEPSEEK_API_KEY",
}

// runAnalysis roda a análise LLM v1 (História 3) sobre a transcrição já
// salva pelo provedor de STT vencedor (ElevenLabs — ver
// docs/decisoes-tecnologia.md). Não re-executa STT: lê o utterances.json já
// salvo por runProvider.
func runAnalysis(ctx context.Context, logger *slog.Logger, names []string) bool {
	const (
		utterancesPath = "local/output/aula-01/elevenlabs/utterances.json"
		promptPath     = "prompts/analyze-v1.md"
		outDir         = "local/output/aula-01/analysis"
	)

	systemPrompt, err := os.ReadFile(promptPath)
	if err != nil {
		logger.Error("ler prompt de análise", "path", promptPath, "erro", err)
		return false
	}

	utterances, err := loadUtterances(utterancesPath)
	if err != nil {
		logger.Error("ler utterances", "path", utterancesPath, "erro", err)
		return false
	}

	speakerRoles, err := confirmSpeakerRoles(utterances)
	if err != nil {
		logger.Error("confirmar papéis dos locutores", "erro", err)
		return false
	}

	transcript, err := analysis.FormatTranscript(utterances, speakerRoles)
	if err != nil {
		logger.Error("formatar transcrição", "erro", err)
		return false
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		logger.Error("criar diretório de saída da análise", "erro", err)
		return false
	}

	hadFailure := false
	for _, name := range names {
		if !runAnalysisProvider(ctx, logger, name, string(systemPrompt), transcript, outDir) {
			hadFailure = true
		}
	}
	return !hadFailure
}

func loadUtterances(path string) ([]stt.Utterance, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var utterances []stt.Utterance
	if err := json.Unmarshal(data, &utterances); err != nil {
		return nil, err
	}
	return utterances, nil
}

// confirmSpeakerRoles mostra até 3 falas de exemplo por locutor e pergunta
// ao usuário, via stdin, qual dos dois é o Aluno — o outro vira Tutor (aula
// do Cambly é sempre 1:1). Falha explícita se a transcrição não tiver
// exatamente 2 locutores, ou se a resposta não for "aluno"/"tutor".
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

func runAnalysisProvider(ctx context.Context, logger *slog.Logger, name, systemPrompt, transcript, outDir string) bool {
	factory, ok := analysisProviderFactories[name]
	if !ok {
		logger.Error("provedor de análise desconhecido", "nome", name)
		return false
	}
	envVar := analysisProviderEnvVars[name]
	provider, err := factory(os.Getenv(envVar), systemPrompt)
	if err != nil {
		logger.Error("criar provider de análise", "provedor", name, "erro", err)
		return false
	}

	providerOutDir := filepath.Join(outDir, provider.Name())
	if err := os.MkdirAll(providerOutDir, 0o755); err != nil {
		logger.Error("criar diretório de saída da análise", "provedor", name, "erro", err)
		return false
	}

	logger.Info("analisando", "provedor", provider.Name())
	result, err := provider.Analyze(ctx, transcript)
	if err != nil {
		logger.Error("análise falhou", "provedor", provider.Name(), "erro", err)
		if result != nil && len(result.RawResponse) > 0 {
			if saveErr := os.WriteFile(filepath.Join(providerOutDir, "raw.json"), result.RawResponse, 0o644); saveErr != nil {
				logger.Error("salvar JSON bruto após falha", "provedor", provider.Name(), "erro", saveErr)
			} else {
				logger.Info("JSON bruto salvo apesar da falha de análise", "provedor", provider.Name())
			}
		}
		return false
	}

	if err := os.WriteFile(filepath.Join(providerOutDir, "raw.json"), result.RawResponse, 0o644); err != nil {
		logger.Error("salvar JSON bruto", "provedor", provider.Name(), "erro", err)
		return false
	}

	if err := writeReadableAnalysis(filepath.Join(providerOutDir, "result.txt"), result); err != nil {
		logger.Error("salvar análise legível", "provedor", provider.Name(), "erro", err)
		return false
	}

	logger.Info("análise concluída", "provedor", provider.Name(),
		"correções", len(result.Corrections), "vocabulário", len(result.Vocabulary), "expressões", len(result.TutorExpressions))
	return true
}

func writeReadableAnalysis(path string, result *analysis.Result) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	fmt.Fprintln(file, "== Correções ==")
	for _, c := range result.Corrections {
		fmt.Fprintf(file, "- %q -> %q (%s)\n", c.Original, c.Correction, c.Explanation)
	}

	fmt.Fprintln(file, "\n== Vocabulário ==")
	for _, v := range result.Vocabulary {
		fmt.Fprintf(file, "- %s: %s\n", v.Term, v.Translation)
	}

	fmt.Fprintln(file, "\n== Expressões do tutor ==")
	for _, e := range result.TutorExpressions {
		fmt.Fprintf(file, "- %s (%s)\n", e.Text, e.Note)
	}

	return nil
}
```

- [ ] **Step 3: Add the `-analysis-providers` flag and branch in `main()`**

In `cmd/spike/main.go`, replace the flag parsing and validation block:

```go
	providersFlag := flag.String("providers", "", "lista separada por vírgula dos provedores a rodar (ex.: gladia,assemblyai)")
	flag.Parse()

	if *providersFlag == "" {
		logger.Error("flag -providers é obrigatória", "exemplo", "-providers=gladia,assemblyai")
		os.Exit(1)
	}
	names := strings.Split(*providersFlag, ",")
	for _, name := range names {
		if _, ok := providerFactories[name]; !ok {
			logger.Error("provedor desconhecido", "nome", name)
			os.Exit(1)
		}
	}
```

with:

```go
	providersFlag := flag.String("providers", "", "lista separada por vírgula dos provedores de STT a rodar (ex.: gladia,assemblyai)")
	analysisProvidersFlag := flag.String("analysis-providers", "", "lista separada por vírgula dos provedores de análise LLM a rodar (ex.: deepseek)")
	flag.Parse()

	if *providersFlag == "" && *analysisProvidersFlag == "" {
		logger.Error("informe -providers (STT) ou -analysis-providers (análise LLM)")
		os.Exit(1)
	}
	if *providersFlag != "" && *analysisProvidersFlag != "" {
		logger.Error("rode -providers e -analysis-providers em invocações separadas")
		os.Exit(1)
	}

	if *analysisProvidersFlag != "" {
		names := strings.Split(*analysisProvidersFlag, ",")
		for _, name := range names {
			if _, ok := analysisProviderFactories[name]; !ok {
				logger.Error("provedor de análise desconhecido", "nome", name)
				os.Exit(1)
			}
		}
		if !runAnalysis(ctx, logger, names) {
			os.Exit(1)
		}
		return
	}

	names := strings.Split(*providersFlag, ",")
	for _, name := range names {
		if _, ok := providerFactories[name]; !ok {
			logger.Error("provedor desconhecido", "nome", name)
			os.Exit(1)
		}
	}
```

No other change to `main()` — the rest of the STT flow (audio extraction, `runProvider` loop)
stays exactly as-is below this block.

- [ ] **Step 4: Add `DEEPSEEK_API_KEY` to `.env.example`**

Current `.env.example`:

```
# Copie este arquivo para .env e preencha com sua chave real.
# O .env nunca deve ser commitado (já está no .gitignore).
GLADIA_API_KEY=
ASSEMBLYAI_API_KEY=
DEEPGRAM_API_KEY=
ELEVENLABS_API_KEY=
```

Change it to:

```
# Copie este arquivo para .env e preencha com sua chave real.
# O .env nunca deve ser commitado (já está no .gitignore).
GLADIA_API_KEY=
ASSEMBLYAI_API_KEY=
DEEPGRAM_API_KEY=
ELEVENLABS_API_KEY=
DEEPSEEK_API_KEY=
```

- [ ] **Step 5: Verify it compiles and vets clean**

Run: `go.exe build ./... && go.exe vet ./...`
Expected: no output (both clean)

- [ ] **Step 6: Verify the unknown-provider validation still rejects bad names (safe — no
  network, no ffmpeg, no API cost)**

Run: `go.exe run ./cmd/spike -analysis-providers=deepseek,nonexistent`
Expected: logs an error about the unknown analysis provider `nonexistent` and exits with a
non-zero status, before any file read or network call. (This also confirms `"deepseek"` alone is
now recognized as a valid name, since the loop reaches `nonexistent` instead of failing on
`deepseek`.)

- [ ] **Step 7: Verify the mutually-exclusive-flags validation**

Run: `go.exe run ./cmd/spike -providers=elevenlabs -analysis-providers=deepseek`
Expected: logs the error about running the two flags in separate invocations, exits non-zero,
before any ffmpeg/network call.

- [ ] **Step 8: Stage changes (do not commit)**

```bash
git add cmd/spike/main.go cmd/spike/analysis.go .env.example
```

- [ ] **Step 9: Note for the human — real end-to-end run is manual**

Running the full DeepSeek slice against the real aula 01 requires, in order: (1)
`go.exe run ./cmd/spike -providers=elevenlabs` (if `local/output/aula-01/elevenlabs/utterances.json`
doesn't already exist from an earlier run — real ffmpeg, real network, real billed usage), then
(2) `go.exe run ./cmd/spike -analysis-providers=deepseek` (real `DEEPSEEK_API_KEY`, interactive
stdin prompt to confirm which speaker is the Aluno, real network, real billed usage). This is
intentionally NOT part of this task's automated steps — happens later, directly with the human.
Afterwards, fill in `docs/notas-analise-llm.md` (new file, to be created by hand at that point,
mirroring `docs/notas-stt.md`'s structure) with the DeepSeek quality/cost notes from aula 01, and
decide whether to stop here or move to the Qwen slice per the spec's "Estratégia de fatias".

---

## Após este plano

Se a qualidade do DeepSeek convencer, a exploração de LLM para (`fase-0-validacao.md`, História 3)
já tem uma resposta suficiente por ora — sem necessidade de implementar Qwen/GLM/Anthropic/
OpenAI/Gemini nesta rodada. Se não convencer, a próxima fatia (Qwen) segue o mesmo padrão deste
plano: um novo construtor em `openai_compatible.go` (ou um cliente próprio, se a API não for
compatível na prática) + uma entrada em `analysisProviderFactories` — sem mudança estrutural no
resto do pacote.
