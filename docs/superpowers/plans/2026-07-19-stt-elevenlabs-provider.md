# STT ElevenLabs Scribe Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement `stt.Provider` for ElevenLabs Scribe (single synchronous multipart HTTP call,
mapping to the shared domain) and register it as a fourth selectable provider in `cmd/spike` —
closing out all 4 STT candidates from `docs/fase-0-validacao.md`.

**Architecture:** `internal/stt/elevenlabs_mapping.go` (pure JSON→domain mapping, fixture-tested) +
`internal/stt/elevenlabs.go` (live HTTP client: one synchronous multipart `POST
/v1/speech-to-text` call — no upload step, no polling, like Deepgram but with a multipart body
since the API requires a file upload), both satisfying the same `stt.Provider` interface.
`cmd/spike/main.go`'s existing `providerFactories` map gains one entry; no other change needed
there.

**Tech Stack:** Go stdlib only (`net/http`, `mime/multipart`, `encoding/json`, `strings`). No
third-party dependencies.

## Global Constraints

- `internal/stt` stays production-quality and permanent; `cmd/spike/main.go` stays disposable.
- `ELEVENLABS_API_KEY` read only from environment (or `.env` via the existing `loadDotEnv`), never
  hardcoded/committed.
- ElevenLabs auth header is `xi-api-key: <chave>` (not `Authorization`).
- ElevenLabs' Speech-to-Text endpoint is a **single synchronous call**:
  `POST https://api.elevenlabs.io/v1/speech-to-text`, body `multipart/form-data` — the audio file
  itself requires a multipart upload (unlike Deepgram, which sends the raw WAV bytes directly as
  the body). No separate upload step, no job ID, no polling.
- Multipart form fields to send: `file` (the WAV bytes), `model_id=scribe_v2` (current documented
  model), `diarize=true`, `num_speakers=2` (Cambly lessons are always 1:1 student+tutor — this hint
  is always correct for this project), `timestamps_granularity=word` (already the API default, but
  set explicitly for clarity, same convention as the other 3 providers' explicit params).
- Do **not** send `language_code`: the API has no documented explicit "multi/code-switching mode"
  like Deepgram's `language=multi` — omitting it lets Scribe's native multilingual detection cover
  PT/ES-in-English code-switching.
- **Central architectural difference from all 3 other providers:** the response is **not**
  pre-grouped into utterances. It returns a flat `words[]` array at the root; each entry has a
  `type` (`"word"` | `"spacing"` | `"audio_event"`), `text`, `start`, `end` (float64 seconds), and
  `speaker_id` (string, already in `"speaker_0"`/`"speaker_1"` form — no `fmt.Sprintf` needed,
  unlike the other 3 providers). Utterances must be grouped client-side: open a new `Utterance`
  every time `speaker_id` changes from the previous entry. Do **not** add silence-gap segmentation
  within the same speaker — that heuristic was explicitly rejected (see the design spec's
  "Alternativas consideradas") as unnecessary complexity for the spike.
- `Utterance.Text` = concatenation of the raw `text` field of **every** entry in the group, in
  order — `word`, `spacing`, and `audio_event` all included. This preserves exact spacing and keeps
  non-speech markers (e.g., `"(laughs)"`) as readable context (explicit product decision — do not
  filter them out).
- `Utterance.Words` = only entries with `type == "word"` become `stt.Word`. `spacing` and
  `audio_event` entries must be excluded from `Words`, even though they contribute to `Text`.
- Timestamps are float64 seconds — reuse the existing `secondsToDuration(s float64) time.Duration`
  helper already defined in `internal/stt/gladia_mapping.go` (same package `stt`, no import
  needed). Do not duplicate it.
- ElevenLabs' response has **no status field to validate** (like Deepgram) — the synchronous
  response only exists on success; errors surface as a non-2xx HTTP status, already handled
  generically by the `do()` helper.
- Raw provider JSON is always preserved on disk, even when parsing/mapping fails (same pattern as
  the other 3 providers).
- HTTP client timeout: a single generous timeout (10 minutes) at the `http.Client` level, for
  consistency with the other 3 providers.
- No elaborate flags, no parallelism, no sophisticated retry.
- Commit messages: one line, semantic format (`tipo: descrição`) — do not run `git commit`
  automatically; stage only (`git add`), per this project's established workflow preference.

---

### Task 1: `internal/stt` ElevenLabs response mapping (fixture-tested)

**Files:**
- Create: `internal/stt/elevenlabs_mapping.go`
- Create: `testdata/elevenlabs_response.json`
- Test: `internal/stt/elevenlabs_mapping_test.go`

**Interfaces:**
- Consumes: `stt.Result`, `stt.Utterance`, `stt.Word` (already defined in `internal/stt/stt.go`,
  unchanged by this task); `secondsToDuration(s float64) time.Duration` (already defined in
  `internal/stt/gladia_mapping.go`, same package, unchanged by this task)
- Produces (unexported, used by Task 2): `mapElevenLabsResponse(raw []byte) (*Result, error)`

- [ ] **Step 1: Write the synthetic fixture**

```json
// testdata/elevenlabs_response.json
{
  "language_code": "en",
  "language_probability": 0.97,
  "text": "Hi, how was your week? It was good, I felt a lot of saudade for my hometown though.",
  "words": [
    {"text": "Hi,", "type": "word", "start": 0.42, "end": 0.65, "speaker_id": "speaker_0"},
    {"text": " ", "type": "spacing", "start": 0.65, "end": 0.7, "speaker_id": "speaker_0"},
    {"text": "how", "type": "word", "start": 0.7, "end": 0.85, "speaker_id": "speaker_0"},
    {"text": " ", "type": "spacing", "start": 0.85, "end": 0.9, "speaker_id": "speaker_0"},
    {"text": "was", "type": "word", "start": 0.9, "end": 1.05, "speaker_id": "speaker_0"},
    {"text": " ", "type": "spacing", "start": 1.05, "end": 1.1, "speaker_id": "speaker_0"},
    {"text": "your", "type": "word", "start": 1.1, "end": 1.3, "speaker_id": "speaker_0"},
    {"text": " ", "type": "spacing", "start": 1.3, "end": 1.35, "speaker_id": "speaker_0"},
    {"text": "week?", "type": "word", "start": 1.35, "end": 1.7, "speaker_id": "speaker_0"},
    {"text": "It", "type": "word", "start": 3.5, "end": 3.6, "speaker_id": "speaker_1"},
    {"text": " ", "type": "spacing", "start": 3.6, "end": 3.65, "speaker_id": "speaker_1"},
    {"text": "was", "type": "word", "start": 3.65, "end": 3.8, "speaker_id": "speaker_1"},
    {"text": " ", "type": "spacing", "start": 3.8, "end": 3.85, "speaker_id": "speaker_1"},
    {"text": "good,", "type": "word", "start": 3.85, "end": 4.1, "speaker_id": "speaker_1"},
    {"text": " ", "type": "spacing", "start": 4.1, "end": 4.15, "speaker_id": "speaker_1"},
    {"text": "I", "type": "word", "start": 4.15, "end": 4.2, "speaker_id": "speaker_1"},
    {"text": " ", "type": "spacing", "start": 4.2, "end": 4.25, "speaker_id": "speaker_1"},
    {"text": "felt", "type": "word", "start": 4.25, "end": 4.45, "speaker_id": "speaker_1"},
    {"text": " ", "type": "spacing", "start": 4.45, "end": 4.5, "speaker_id": "speaker_1"},
    {"text": "a", "type": "word", "start": 4.5, "end": 4.55, "speaker_id": "speaker_1"},
    {"text": " ", "type": "spacing", "start": 4.55, "end": 4.6, "speaker_id": "speaker_1"},
    {"text": "lot", "type": "word", "start": 4.6, "end": 4.75, "speaker_id": "speaker_1"},
    {"text": " ", "type": "spacing", "start": 4.75, "end": 4.8, "speaker_id": "speaker_1"},
    {"text": "of", "type": "word", "start": 4.8, "end": 4.9, "speaker_id": "speaker_1"},
    {"text": " ", "type": "spacing", "start": 4.9, "end": 4.95, "speaker_id": "speaker_1"},
    {"text": "saudade", "type": "word", "start": 4.95, "end": 5.4, "speaker_id": "speaker_1"},
    {"text": " ", "type": "spacing", "start": 5.4, "end": 5.45, "speaker_id": "speaker_1"},
    {"text": "for", "type": "word", "start": 5.45, "end": 5.6, "speaker_id": "speaker_1"},
    {"text": " ", "type": "spacing", "start": 5.6, "end": 5.65, "speaker_id": "speaker_1"},
    {"text": "my", "type": "word", "start": 5.65, "end": 5.8, "speaker_id": "speaker_1"},
    {"text": " ", "type": "spacing", "start": 5.8, "end": 5.85, "speaker_id": "speaker_1"},
    {"text": "hometown", "type": "word", "start": 5.85, "end": 6.3, "speaker_id": "speaker_1"},
    {"text": " ", "type": "spacing", "start": 6.3, "end": 6.35, "speaker_id": "speaker_1"},
    {"text": "though.", "type": "word", "start": 6.35, "end": 6.7, "speaker_id": "speaker_1"}
  ],
  "audio_duration_secs": 7.2,
  "transcription_id": "synthetic-fixture-01"
}
```

This is invented conversation content (no real lesson, no real names) in the shape of
ElevenLabs Scribe's documented `POST /v1/speech-to-text` response with `diarize=true` and
`timestamps_granularity=word`: a flat root-level `words[]` array, each entry carrying `type`
(`word`/`spacing`), `start`/`end` in float seconds, and `speaker_id` already as `"speaker_0"` /
`"speaker_1"`. Same invented dialogue as the Gladia/AssemblyAI/Deepgram fixtures — including the
PT/EN code-switching word "saudade" at the same word index (8) — for easy side-by-side reading,
with `spacing` entries interleaved between every word (ElevenLabs' actual shape) and no gap
between utterances handled the same way the other fixtures do (a plain speaker change at index 9).

- [ ] **Step 2: Write the failing tests**

```go
// internal/stt/elevenlabs_mapping_test.go
package stt

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMapElevenLabsResponse(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "elevenlabs_response.json"))
	if err != nil {
		t.Fatalf("erro lendo fixture: %v", err)
	}

	result, err := mapElevenLabsResponse(raw)
	if err != nil {
		t.Fatalf("mapElevenLabsResponse retornou erro: %v", err)
	}

	if len(result.Utterances) != 2 {
		t.Fatalf("esperava 2 utterances, obteve %d", len(result.Utterances))
	}

	first := result.Utterances[0]
	if first.Speaker != "speaker_0" {
		t.Errorf("Speaker = %q, esperava %q", first.Speaker, "speaker_0")
	}
	if first.Text != "Hi, how was your week?" {
		t.Errorf("Text = %q, inesperado", first.Text)
	}
	if first.Start != 420*time.Millisecond {
		t.Errorf("Start = %v, esperava %v", first.Start, 420*time.Millisecond)
	}
	if len(first.Words) != 5 {
		t.Fatalf("esperava 5 words, obteve %d", len(first.Words))
	}
	if first.Words[0].Text != "Hi," {
		t.Errorf("Words[0].Text = %q, inesperado", first.Words[0].Text)
	}

	second := result.Utterances[1]
	if second.Speaker != "speaker_1" {
		t.Errorf("Speaker = %q, esperava %q", second.Speaker, "speaker_1")
	}
	if len(second.Words) != 13 {
		t.Fatalf("esperava 13 words, obteve %d", len(second.Words))
	}
	if second.Words[8].Text != "saudade" {
		t.Errorf("Words[8].Text = %q, esperava %q", second.Words[8].Text, "saudade")
	}

	if string(result.RawResponse) != string(raw) {
		t.Error("RawResponse deveria preservar o JSON bruto exatamente como recebido")
	}
}

func TestMapElevenLabsResponse_InvalidJSON(t *testing.T) {
	_, err := mapElevenLabsResponse([]byte("not json"))
	if err == nil {
		t.Fatal("esperava erro para JSON inválido, obteve nil")
	}
}

func TestMapElevenLabsResponse_ExcludesNonWordEntriesFromWords(t *testing.T) {
	raw := []byte(`{
		"words": [
			{"text": "Well", "type": "word", "start": 0.0, "end": 0.3, "speaker_id": "speaker_0"},
			{"text": " ", "type": "spacing", "start": 0.3, "end": 0.35, "speaker_id": "speaker_0"},
			{"text": "(laughs)", "type": "audio_event", "start": 0.35, "end": 0.9, "speaker_id": "speaker_0"},
			{"text": " ", "type": "spacing", "start": 0.9, "end": 0.95, "speaker_id": "speaker_0"},
			{"text": "ok", "type": "word", "start": 0.95, "end": 1.1, "speaker_id": "speaker_0"}
		]
	}`)

	result, err := mapElevenLabsResponse(raw)
	if err != nil {
		t.Fatalf("mapElevenLabsResponse retornou erro: %v", err)
	}
	if len(result.Utterances) != 1 {
		t.Fatalf("esperava 1 utterance, obteve %d", len(result.Utterances))
	}

	u := result.Utterances[0]
	if u.Text != "Well (laughs) ok" {
		t.Errorf("Text = %q, esperava %q", u.Text, "Well (laughs) ok")
	}
	if len(u.Words) != 2 {
		t.Fatalf("esperava 2 words (spacing/audio_event excluídos), obteve %d", len(u.Words))
	}
	if u.Words[0].Text != "Well" || u.Words[1].Text != "ok" {
		t.Errorf("Words = %+v, inesperado", u.Words)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go.exe test ./internal/stt/... -v -run TestMapElevenLabsResponse`
Expected: FAIL — build error, `undefined: mapElevenLabsResponse`

- [ ] **Step 4: Write the mapping implementation**

```go
// internal/stt/elevenlabs_mapping.go
package stt

import (
	"encoding/json"
	"fmt"
	"strings"
)

type elevenLabsResponse struct {
	Words []elevenLabsWord `json:"words"`
}

type elevenLabsWord struct {
	Text      string  `json:"text"`
	Type      string  `json:"type"`
	Start     float64 `json:"start"`
	End       float64 `json:"end"`
	SpeakerID string  `json:"speaker_id"`
}

// mapElevenLabsResponse converte o JSON bruto do endpoint POST
// /v1/speech-to-text do ElevenLabs Scribe para o domínio comum stt.Result.
// Mantida separada da chamada HTTP (elevenlabs.go) para ser testável com
// fixture, sem precisar de rede.
//
// Ao contrário da Gladia/AssemblyAI/Deepgram, a API não agrupa a resposta em
// utterances — devolve um array plano de words[], cada uma com type
// (word/spacing/audio_event) e speaker_id. O agrupamento em turnos de fala é
// feito aqui: uma nova Utterance começa sempre que o speaker_id muda.
//
// Também não há campo de status a validar: a resposta síncrona só existe
// quando a transcrição já terminou com sucesso — um erro chega como HTTP
// não-2xx, tratado em elevenlabs.go antes desta função ser chamada.
func mapElevenLabsResponse(raw []byte) (*Result, error) {
	var parsed elevenLabsResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("json inválido: %w", err)
	}

	return &Result{
		RawResponse: raw,
		Utterances:  groupElevenLabsWords(parsed.Words),
	}, nil
}

// groupElevenLabsWords agrupa entradas consecutivas do array plano de words
// pelo mesmo speaker_id em uma Utterance. O texto da utterance concatena o
// texto bruto de toda entrada do grupo (word, spacing e audio_event) na
// ordem original, preservando o espaçamento e mantendo marcadores de eventos
// não-verbais (ex.: "(laughs)") como contexto de leitura — decisão de
// produto, não filtrar. Só entradas type=="word" viram stt.Word: a lista de
// palavras clicáveis do domínio é só fala real, mesmo critério usado por
// Gladia/AssemblyAI/Deepgram. Não há segmentação por pausa de silêncio
// dentro do mesmo locutor — só a troca de speaker_id abre uma nova
// Utterance (decisão registrada na spec: evitar heurística extra no spike).
func groupElevenLabsWords(items []elevenLabsWord) []Utterance {
	utterances := make([]Utterance, 0)
	var current *Utterance
	var text strings.Builder

	flush := func() {
		if current == nil {
			return
		}
		current.Text = text.String()
		utterances = append(utterances, *current)
		text.Reset()
	}

	for _, w := range items {
		if current == nil || w.SpeakerID != current.Speaker {
			flush()
			current = &Utterance{Speaker: w.SpeakerID, Start: secondsToDuration(w.Start)}
		}
		text.WriteString(w.Text)
		current.End = secondsToDuration(w.End)
		if w.Type == "word" {
			current.Words = append(current.Words, Word{
				Text:  w.Text,
				Start: secondsToDuration(w.Start),
				End:   secondsToDuration(w.End),
			})
		}
	}
	flush()

	return utterances
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go.exe test ./internal/stt/... -v`
Expected: all tests pass, including `TestMapElevenLabsResponse`,
`TestMapElevenLabsResponse_InvalidJSON`, `TestMapElevenLabsResponse_ExcludesNonWordEntriesFromWords`,
and the pre-existing Gladia/AssemblyAI/Deepgram mapping tests (unaffected).
`ok  	assistente-idiomas/internal/stt`

- [ ] **Step 6: Run `go vet`**

Run: `go.exe vet ./...`
Expected: no output (clean)

- [ ] **Step 7: Stage changes (do not commit)**

```bash
git add testdata/elevenlabs_response.json internal/stt/elevenlabs_mapping.go internal/stt/elevenlabs_mapping_test.go
```

---

### Task 2: `internal/stt/elevenlabs.go` — ElevenLabs HTTP client (`Provider` implementation)

**Files:**
- Create: `internal/stt/elevenlabs.go`

**Interfaces:**
- Consumes: `mapElevenLabsResponse(raw []byte) (*Result, error)` from Task 1
  (`internal/stt/elevenlabs_mapping.go`)
- Produces: `stt.NewElevenLabsProvider(apiKey string) (*ElevenLabsProvider, error)`
- Produces: `(*ElevenLabsProvider).Name() string` and
  `(*ElevenLabsProvider).Transcribe(ctx context.Context, audioPath string) (*Result, error)`,
  satisfying `stt.Provider`

> No unit tests for this task, same justification as the other 3 providers: a live HTTP call is
> only exercised manually via the CLI, not unit-tested — there is no network mock at this stage.
> Verification here is build + vet only.

- [ ] **Step 1: Write the implementation**

```go
// internal/stt/elevenlabs.go
package stt

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const elevenLabsBaseURL = "https://api.elevenlabs.io/v1/speech-to-text"

// ElevenLabsProvider implementa stt.Provider usando a API do ElevenLabs Scribe.
//
// Modelo usado: "scribe_v2" — modelo atual documentado pela ElevenLabs,
// multilíngue nativo (90+ idiomas). A API não expõe um parâmetro explícito de
// "modo multi/code-switching" como o Deepgram; nenhum language_code é
// enviado, deixando a detecção automática cobrir troca de idioma no meio da
// fala (PT/ES no meio do inglês do aluno).
//
// num_speakers=2 é sempre passado: aulas do Cambly são 1:1 (aluno e tutor),
// então esse hint melhora a diarização sem custo.
//
// Assim como o Deepgram, a API batch do ElevenLabs é síncrona: uma única
// chamada POST (aqui multipart, por exigir upload do arquivo de áudio) já
// retorna a transcrição completa — sem upload prévio nem polling de job.
type ElevenLabsProvider struct {
	apiKey string
	client *http.Client
}

func NewElevenLabsProvider(apiKey string) (*ElevenLabsProvider, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("stt: ELEVENLABS_API_KEY vazia")
	}
	// Timeout generoso: cobre o envio do WAV inteiro da aula (dezenas de MB)
	// mais o processamento síncrono no servidor. Mesmo valor usado pelos
	// outros três provedores, por consistência.
	return &ElevenLabsProvider{apiKey: apiKey, client: &http.Client{Timeout: 10 * time.Minute}}, nil
}

func (p *ElevenLabsProvider) Name() string { return "elevenlabs" }

func (p *ElevenLabsProvider) Transcribe(ctx context.Context, audioPath string) (*Result, error) {
	req, err := p.buildRequest(ctx, audioPath)
	if err != nil {
		return nil, fmt.Errorf("stt: montar requisição elevenlabs: %w", err)
	}

	raw, err := p.do(req)
	if err != nil {
		return nil, fmt.Errorf("stt: transcrever elevenlabs: %w", err)
	}

	result, err := mapElevenLabsResponse(raw)
	if err != nil {
		// Preserva o JSON bruto mesmo em falha de parse: a chamada à API já foi
		// feita (custa dinheiro), então o chamador deve conseguir salvar
		// result.RawResponse em disco mesmo com err != nil.
		return &Result{RawResponse: raw}, fmt.Errorf("stt: parsear resposta elevenlabs: %w", err)
	}
	return result, nil
}

func (p *ElevenLabsProvider) buildRequest(ctx context.Context, audioPath string) (*http.Request, error) {
	file, err := os.Open(audioPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	part, err := writer.CreateFormFile("file", filepath.Base(audioPath))
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(part, file); err != nil {
		return nil, err
	}

	fields := map[string]string{
		"model_id":               "scribe_v2",
		"diarize":                "true",
		"num_speakers":           "2",
		"timestamps_granularity": "word",
	}
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, elevenLabsBaseURL, &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("xi-api-key", p.apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req, nil
}

// do executa a requisição e retorna o corpo da resposta, com erro se o
// status não for 2xx (mensagem inclui status e corpo, para depuração).
func (p *ElevenLabsProvider) do(req *http.Request) ([]byte, error) {
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
```

- [ ] **Step 2: Verify it compiles and vets clean**

Run: `go.exe build ./... && go.exe vet ./...`
Expected: no output (both clean — `ElevenLabsProvider` satisfies `stt.Provider` at compile time via
its method set)

- [ ] **Step 3: Stage changes (do not commit)**

```bash
git add internal/stt/elevenlabs.go
```

---

### Task 3: Register ElevenLabs in `cmd/spike` and `.env.example`

**Files:**
- Modify: `cmd/spike/main.go` (add one entry to the existing `providerFactories` map)
- Modify: `.env.example` (add `ELEVENLABS_API_KEY=` line)

**Interfaces:**
- Consumes: `stt.NewElevenLabsProvider(apiKey string) (*ElevenLabsProvider, error)` (Task 2)

`cmd/spike/main.go` already has a generic `-providers` flag, per-provider output directories, and
per-provider failure isolation — this task only needs to make `elevenlabs` a recognized name.

- [ ] **Step 1: Add the ElevenLabs entry to `providerFactories`**

In `cmd/spike/main.go`, the current map is:

```go
var providerFactories = map[string]func() (stt.Provider, error){
	"gladia": func() (stt.Provider, error) {
		return stt.NewGladiaProvider(os.Getenv("GLADIA_API_KEY"))
	},
	"assemblyai": func() (stt.Provider, error) {
		return stt.NewAssemblyAIProvider(os.Getenv("ASSEMBLYAI_API_KEY"))
	},
	"deepgram": func() (stt.Provider, error) {
		return stt.NewDeepgramProvider(os.Getenv("DEEPGRAM_API_KEY"))
	},
}
```

Change it to:

```go
var providerFactories = map[string]func() (stt.Provider, error){
	"gladia": func() (stt.Provider, error) {
		return stt.NewGladiaProvider(os.Getenv("GLADIA_API_KEY"))
	},
	"assemblyai": func() (stt.Provider, error) {
		return stt.NewAssemblyAIProvider(os.Getenv("ASSEMBLYAI_API_KEY"))
	},
	"deepgram": func() (stt.Provider, error) {
		return stt.NewDeepgramProvider(os.Getenv("DEEPGRAM_API_KEY"))
	},
	"elevenlabs": func() (stt.Provider, error) {
		return stt.NewElevenLabsProvider(os.Getenv("ELEVENLABS_API_KEY"))
	},
}
```

No other change to this file — flag parsing, validation, output directory logic, and the
per-provider failure loop already handle any name present in this map.

- [ ] **Step 2: Add `ELEVENLABS_API_KEY` to `.env.example`**

Current `.env.example`:

```
# Copie este arquivo para .env e preencha com sua chave real.
# O .env nunca deve ser commitado (já está no .gitignore).
GLADIA_API_KEY=
ASSEMBLYAI_API_KEY=
DEEPGRAM_API_KEY=
```

Change it to:

```
# Copie este arquivo para .env e preencha com sua chave real.
# O .env nunca deve ser commitado (já está no .gitignore).
GLADIA_API_KEY=
ASSEMBLYAI_API_KEY=
DEEPGRAM_API_KEY=
ELEVENLABS_API_KEY=
```

- [ ] **Step 3: Verify it compiles and vets clean**

Run: `go.exe build ./... && go.exe vet ./...`
Expected: no output (both clean)

- [ ] **Step 4: Verify the unknown-provider validation still rejects bad names (safe — no network, no ffmpeg, no API cost)**

Run: `go.exe run ./cmd/spike -providers=elevenlabs,nonexistent`
Expected: logs an error about the unknown provider `nonexistent` and exits with a non-zero status,
before any audio extraction or network call. (This also confirms `"elevenlabs"` alone is now
recognized as a valid name, since the loop reaches `nonexistent` instead of failing on
`elevenlabs`.)

- [ ] **Step 5: Stage changes (do not commit)**

```bash
git add cmd/spike/main.go .env.example
```

- [ ] **Step 6: Note for the human — real end-to-end run is manual**

Running `go.exe run ./cmd/spike -providers=elevenlabs` (or
`-providers=gladia,assemblyai,deepgram,elevenlabs`) against the real sample lesson (real API key,
real network call, real billed usage) is intentionally NOT part of this task's automated steps —
that happens later, directly with the human, same as the other 3 providers.

---

## Após este plano

- Todos os 4 candidatos de STT (`docs/fase-0-validacao.md`) têm cliente implementado — próximo
  passo é a comparação lado a lado e a decisão registrada em `docs/decisoes-tecnologia.md`
  (História 2), usando `docs/notas-stt.md` como insumo.
