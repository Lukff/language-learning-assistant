# STT AssemblyAI + Multi-Provider CLI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement `stt.Provider` for AssemblyAI (upload, submit, poll, mapping to the shared domain), and rework `cmd/spike` so a single invocation can run any chosen subset of the already-implemented providers (today: Gladia, AssemblyAI) against the same lesson, without ever overwriting another provider's previous output.

**Architecture:** `internal/stt/assemblyai_mapping.go` (pure JSON→domain mapping, fixture-tested) + `internal/stt/assemblyai.go` (live HTTP client: upload → submit → poll), mirroring the existing Gladia implementation and both satisfying the same `stt.Provider` interface. `cmd/spike/main.go` gains a required `-providers` flag and a per-provider output subdirectory so multiple providers coexist cleanly.

**Tech Stack:** Go stdlib only (`net/http`, `encoding/json`, `flag`, `log/slog`). No third-party dependencies.

## Global Constraints

- `internal/stt` stays production-quality and permanent; `cmd/spike/main.go` stays disposable.
- `ASSEMBLYAI_API_KEY` read only from environment (or `.env` via the existing `loadDotEnv`), never hardcoded/committed.
- AssemblyAI auth header is `Authorization: <chave>` — raw key, no `Bearer` prefix.
- AssemblyAI upload is `POST /v2/upload` with a raw binary body (`application/octet-stream`), NOT multipart (this differs from Gladia's multipart upload).
- Model: `speech_models: ["universal-3-pro"]`, with `speaker_labels: true` and `language_detection: true` in the submit request — `universal-3-pro` is the AssemblyAI model with documented native code-switching support across EN/PT/ES (among others), matching the project's PT/ES-in-English requirement.
- AssemblyAI status values: `"completed"` (not `"done"` like Gladia) and `"error"`; timestamps are integer **milliseconds** (not float seconds like Gladia) — no rounding needed, direct `time.Duration(ms) * time.Millisecond` conversion.
- Raw provider JSON is always preserved on disk, even when parsing/mapping fails (same pattern already in `internal/stt/gladia.go`'s `Transcribe`).
- HTTP client timeout pattern: a generous client-level timeout (10 minutes, covers large file uploads) PLUS a short per-call context timeout (30s) specifically on the poll GET request — copy this exact pattern from `internal/stt/gladia.go`, which was hardened this way after a real timeout bug during manual testing. Do not reintroduce a single blanket short timeout that would break large uploads.
- `-providers` flag on `cmd/spike` is required (no silent default) — omitting it, or naming an unknown provider, is a clear error before any network call or audio extraction.
- Output layout: `local/output/aula-01/<provider.Name()>/raw.json` and `.../transcript.txt` — one subdirectory per provider, so running one provider never touches another's files.
- A single provider's failure during a multi-provider run is logged and does NOT abort the run — the CLI proceeds to the next provider, and only exits with a non-zero status at the end if at least one provider failed.
- No elaborate flags beyond `-providers`, no parallelism, no sophisticated retry.
- Commit messages: one line, semantic format (`tipo: descrição`) — do not run `git commit` automatically; stage only (`git add`), per this project's established workflow preference.

---

### Task 1: `internal/stt` AssemblyAI response mapping (fixture-tested)

**Files:**
- Create: `internal/stt/assemblyai_mapping.go`
- Create: `testdata/assemblyai_response.json`
- Test: `internal/stt/assemblyai_mapping_test.go`

**Interfaces:**
- Consumes: `stt.Result`, `stt.Utterance`, `stt.Word` (already defined in `internal/stt/stt.go`, unchanged by this task)
- Produces (unexported, used by Task 2): `mapAssemblyAIResponse(raw []byte) (*Result, error)`

- [ ] **Step 1: Write the synthetic fixture**

```json
// testdata/assemblyai_response.json
{
  "id": "22222222-2222-2222-2222-222222222222",
  "status": "completed",
  "utterances": [
    {
      "speaker": "A",
      "text": "Hi, how was your week?",
      "start": 420,
      "end": 1700,
      "confidence": 0.95,
      "words": [
        {"text": "Hi,", "start": 420, "end": 650, "confidence": 0.97, "speaker": "A"},
        {"text": "how", "start": 700, "end": 850, "confidence": 0.96, "speaker": "A"},
        {"text": "was", "start": 900, "end": 1050, "confidence": 0.95, "speaker": "A"},
        {"text": "your", "start": 1100, "end": 1300, "confidence": 0.94, "speaker": "A"},
        {"text": "week?", "start": 1350, "end": 1700, "confidence": 0.93, "speaker": "A"}
      ]
    },
    {
      "speaker": "B",
      "text": "It was good, I felt a lot of saudade for my hometown though.",
      "start": 3500,
      "end": 7200,
      "confidence": 0.9,
      "words": [
        {"text": "It", "start": 3500, "end": 3600, "confidence": 0.95, "speaker": "B"},
        {"text": "was", "start": 3650, "end": 3800, "confidence": 0.95, "speaker": "B"},
        {"text": "good,", "start": 3850, "end": 4100, "confidence": 0.94, "speaker": "B"},
        {"text": "I", "start": 4150, "end": 4200, "confidence": 0.96, "speaker": "B"},
        {"text": "felt", "start": 4250, "end": 4450, "confidence": 0.93, "speaker": "B"},
        {"text": "a", "start": 4500, "end": 4550, "confidence": 0.9, "speaker": "B"},
        {"text": "lot", "start": 4600, "end": 4750, "confidence": 0.92, "speaker": "B"},
        {"text": "of", "start": 4800, "end": 4900, "confidence": 0.91, "speaker": "B"},
        {"text": "saudade", "start": 4950, "end": 5400, "confidence": 0.75, "speaker": "B"},
        {"text": "for", "start": 5450, "end": 5600, "confidence": 0.9, "speaker": "B"},
        {"text": "my", "start": 5650, "end": 5800, "confidence": 0.92, "speaker": "B"},
        {"text": "hometown", "start": 5850, "end": 6300, "confidence": 0.89, "speaker": "B"},
        {"text": "though.", "start": 6350, "end": 6700, "confidence": 0.88, "speaker": "B"}
      ]
    }
  ]
}
```

This is invented conversation content (no real lesson, no real names) in the shape of AssemblyAI's documented `GET /v2/transcript/{id}` response (`status`, `utterances[].{speaker,text,start,end,words[]}`, `words[].{text,start,end}`, timestamps in integer milliseconds). Same invented dialogue as the Gladia fixture (`testdata/gladia_response.json`) — including the PT/EN code-switching word "saudade" — for easy side-by-side reading later, but with two-letter speaker labels (`"A"`/`"B"`) and millisecond timestamps matching AssemblyAI's actual format.

- [ ] **Step 2: Write the failing tests**

```go
// internal/stt/assemblyai_mapping_test.go
package stt

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMapAssemblyAIResponse(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "assemblyai_response.json"))
	if err != nil {
		t.Fatalf("erro lendo fixture: %v", err)
	}

	result, err := mapAssemblyAIResponse(raw)
	if err != nil {
		t.Fatalf("mapAssemblyAIResponse retornou erro: %v", err)
	}

	if len(result.Utterances) != 2 {
		t.Fatalf("esperava 2 utterances, obteve %d", len(result.Utterances))
	}

	first := result.Utterances[0]
	if first.Speaker != "speaker_A" {
		t.Errorf("Speaker = %q, esperava %q", first.Speaker, "speaker_A")
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
	if second.Speaker != "speaker_B" {
		t.Errorf("Speaker = %q, esperava %q", second.Speaker, "speaker_B")
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

func TestMapAssemblyAIResponse_InvalidJSON(t *testing.T) {
	_, err := mapAssemblyAIResponse([]byte("not json"))
	if err == nil {
		t.Fatal("esperava erro para JSON inválido, obteve nil")
	}
}

func TestMapAssemblyAIResponse_UnexpectedStatus(t *testing.T) {
	_, err := mapAssemblyAIResponse([]byte(`{"status":"processing","utterances":[]}`))
	if err == nil {
		t.Fatal("esperava erro para status diferente de completed, obteve nil")
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go.exe test ./internal/stt/... -v -run TestMapAssemblyAIResponse`
Expected: FAIL — build error, `undefined: mapAssemblyAIResponse`

- [ ] **Step 4: Write the mapping implementation**

```go
// internal/stt/assemblyai_mapping.go
package stt

import (
	"encoding/json"
	"fmt"
	"time"
)

type assemblyAIResponse struct {
	Status     string                `json:"status"`
	Utterances []assemblyAIUtterance `json:"utterances"`
}

type assemblyAIUtterance struct {
	Speaker string           `json:"speaker"`
	Text    string           `json:"text"`
	Start   int64            `json:"start"`
	End     int64            `json:"end"`
	Words   []assemblyAIWord `json:"words"`
}

type assemblyAIWord struct {
	Text  string `json:"text"`
	Start int64  `json:"start"`
	End   int64  `json:"end"`
}

// mapAssemblyAIResponse converte o JSON bruto do endpoint
// GET /v2/transcript/{id} do AssemblyAI para o domínio comum stt.Result.
// Mantida separada das chamadas HTTP (assemblyai.go) para ser testável com
// fixture, sem precisar de rede.
func mapAssemblyAIResponse(raw []byte) (*Result, error) {
	var parsed assemblyAIResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("json inválido: %w", err)
	}
	if parsed.Status != "completed" {
		return nil, fmt.Errorf("status inesperado: %q", parsed.Status)
	}

	utterances := make([]Utterance, 0, len(parsed.Utterances))
	for _, u := range parsed.Utterances {
		words := make([]Word, 0, len(u.Words))
		for _, w := range u.Words {
			words = append(words, Word{
				Text:  w.Text,
				Start: millisToDuration(w.Start),
				End:   millisToDuration(w.End),
			})
		}
		utterances = append(utterances, Utterance{
			Speaker: fmt.Sprintf("speaker_%s", u.Speaker),
			Text:    u.Text,
			Start:   millisToDuration(u.Start),
			End:     millisToDuration(u.End),
			Words:   words,
		})
	}

	return &Result{
		RawResponse: raw,
		Utterances:  utterances,
	}, nil
}

// millisToDuration converte milissegundos inteiros (formato do AssemblyAI)
// para time.Duration. Sem arredondamento necessário — ao contrário da
// Gladia (float64 segundos), o AssemblyAI já entrega inteiros.
func millisToDuration(ms int64) time.Duration {
	return time.Duration(ms) * time.Millisecond
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go.exe test ./internal/stt/... -v`
Expected: all tests pass, including `TestMapAssemblyAIResponse`, `TestMapAssemblyAIResponse_InvalidJSON`, `TestMapAssemblyAIResponse_UnexpectedStatus`, and the pre-existing Gladia mapping tests (unaffected). `ok  	assistente-idiomas/internal/stt`

- [ ] **Step 6: Run `go vet`**

Run: `go.exe vet ./...`
Expected: no output (clean)

- [ ] **Step 7: Stage changes (do not commit)**

```bash
git add testdata/assemblyai_response.json internal/stt/assemblyai_mapping.go internal/stt/assemblyai_mapping_test.go
```

---

### Task 2: `internal/stt/assemblyai.go` — AssemblyAI HTTP client (`Provider` implementation)

**Files:**
- Create: `internal/stt/assemblyai.go`

**Interfaces:**
- Consumes: `mapAssemblyAIResponse(raw []byte) (*Result, error)` from Task 1 (`internal/stt/assemblyai_mapping.go`)
- Produces: `stt.NewAssemblyAIProvider(apiKey string) (*AssemblyAIProvider, error)`
- Produces: `(*AssemblyAIProvider).Name() string` and `(*AssemblyAIProvider).Transcribe(ctx context.Context, audioPath string) (*Result, error)`, satisfying `stt.Provider`

> No unit tests for this task, same justification as `internal/stt/gladia.go`: live HTTP calls (upload, job creation, polling) are only exercised manually via the CLI, not unit-tested — there is no network mock at this stage. Verification here is build + vet only.

- [ ] **Step 1: Write the implementation**

```go
// internal/stt/assemblyai.go
package stt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const assemblyAIBaseURL = "https://api.assemblyai.com"

// AssemblyAIProvider implementa stt.Provider usando a API do AssemblyAI.
//
// Modelo usado: "universal-3-pro" — suporta code-switching nativo em
// EN/PT/ES/FR/DE/IT, cobrindo exatamente o caso do projeto (aluno fala
// inglês com trechos em português/espanhol).
type AssemblyAIProvider struct {
	apiKey string
	client *http.Client
}

func NewAssemblyAIProvider(apiKey string) (*AssemblyAIProvider, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("stt: ASSEMBLYAI_API_KEY vazia")
	}
	// Timeout generoso: o upload envia o WAV inteiro da aula (dezenas de MB),
	// cuja duração real depende da banda de upload do usuário, não só do
	// processamento do servidor. O poll (chamadas pequenas e repetidas) usa
	// seu próprio timeout curto por chamada — ver poll().
	return &AssemblyAIProvider{apiKey: apiKey, client: &http.Client{Timeout: 10 * time.Minute}}, nil
}

func (p *AssemblyAIProvider) Name() string { return "assemblyai" }

func (p *AssemblyAIProvider) Transcribe(ctx context.Context, audioPath string) (*Result, error) {
	audioURL, err := p.upload(ctx, audioPath)
	if err != nil {
		return nil, fmt.Errorf("stt: upload assemblyai: %w", err)
	}

	jobID, err := p.createJob(ctx, audioURL)
	if err != nil {
		return nil, fmt.Errorf("stt: criar job assemblyai: %w", err)
	}

	raw, err := p.poll(ctx, jobID)
	if err != nil {
		return nil, fmt.Errorf("stt: aguardar job assemblyai: %w", err)
	}

	result, err := mapAssemblyAIResponse(raw)
	if err != nil {
		// Preserva o JSON bruto mesmo em falha de parse: a chamada à API já foi
		// feita (custa dinheiro e minutos de transcrição), então o chamador deve
		// conseguir salvar result.RawResponse em disco mesmo com err != nil.
		return &Result{RawResponse: raw}, fmt.Errorf("stt: parsear resposta assemblyai: %w", err)
	}
	return result, nil
}

func (p *AssemblyAIProvider) upload(ctx context.Context, audioPath string) (string, error) {
	data, err := os.ReadFile(audioPath)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, assemblyAIBaseURL+"/v2/upload", bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", p.apiKey)
	req.Header.Set("Content-Type", "application/octet-stream")

	respBody, err := p.do(req)
	if err != nil {
		return "", err
	}

	var uploadResp struct {
		UploadURL string `json:"upload_url"`
	}
	if err := json.Unmarshal(respBody, &uploadResp); err != nil {
		return "", err
	}
	return uploadResp.UploadURL, nil
}

func (p *AssemblyAIProvider) createJob(ctx context.Context, audioURL string) (string, error) {
	payload, err := json.Marshal(map[string]any{
		"audio_url":          audioURL,
		"speech_models":      []string{"universal-3-pro"},
		"speaker_labels":     true,
		"language_detection": true,
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, assemblyAIBaseURL+"/v2/transcript", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", p.apiKey)
	req.Header.Set("Content-Type", "application/json")

	respBody, err := p.do(req)
	if err != nil {
		return "", err
	}

	var jobResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(respBody, &jobResp); err != nil {
		return "", err
	}
	return jobResp.ID, nil
}

func (p *AssemblyAIProvider) poll(ctx context.Context, jobID string) ([]byte, error) {
	url := fmt.Sprintf("%s/v2/transcript/%s", assemblyAIBaseURL, jobID)
	deadline := time.Now().Add(10 * time.Minute)

	for {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timeout de 10 minutos aguardando job %s", jobID)
		}

		// Timeout curto por chamada (bem menor que o orçamento de 10 minutos e
		// menor que o timeout generoso do cliente para o upload), para que uma
		// única requisição de poll travada não impeça o loop de checar o
		// deadline geral na próxima iteração. Mesmo padrão de internal/stt/gladia.go.
		reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
		if err != nil {
			cancel()
			return nil, err
		}
		req.Header.Set("Authorization", p.apiKey)

		respBody, err := p.do(req)
		cancel()
		if err != nil {
			return nil, err
		}

		var statusResp struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal(respBody, &statusResp); err != nil {
			return nil, err
		}

		switch statusResp.Status {
		case "completed":
			return respBody, nil
		case "error":
			return nil, fmt.Errorf("job retornou status error: %s", string(respBody))
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

// do executa a requisição e retorna o corpo da resposta, com erro se o
// status não for 2xx (mensagem inclui status e corpo, para depuração).
func (p *AssemblyAIProvider) do(req *http.Request) ([]byte, error) {
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
Expected: no output (both clean — `AssemblyAIProvider` satisfies `stt.Provider` at compile time via its method set)

- [ ] **Step 3: Stage changes (do not commit)**

```bash
git add internal/stt/assemblyai.go
```

---

### Task 3: `cmd/spike/main.go` — multi-provider selection + per-provider output

**Files:**
- Modify: `cmd/spike/main.go` (full rewrite of the file — replace entire contents with the version below)

**Interfaces:**
- Consumes: `media.ExtractAudio` (unchanged), `stt.NewGladiaProvider` (unchanged, existing), `stt.NewAssemblyAIProvider` (Task 2), `stt.Provider`, `stt.Result`, `stt.Utterance` (unchanged)

- [ ] **Step 1: Replace the file with the full new implementation**

```go
// cmd/spike/main.go
package main

import (
	"context"
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

var providerFactories = map[string]func() (stt.Provider, error){
	"gladia": func() (stt.Provider, error) {
		return stt.NewGladiaProvider(os.Getenv("GLADIA_API_KEY"))
	},
	"assemblyai": func() (stt.Provider, error) {
		return stt.NewAssemblyAIProvider(os.Getenv("ASSEMBLYAI_API_KEY"))
	},
}

func main() {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	if err := loadDotEnv(".env"); err != nil {
		logger.Error("carregar .env", "erro", err)
		os.Exit(1)
	}

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

	const (
		videoPath = "local/input/aula-01.mp4"
		audioPath = "local/output/aula-01/audio.wav"
		outDir    = "local/output/aula-01"
	)

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		logger.Error("criar diretório de saída", "erro", err)
		os.Exit(1)
	}

	logger.Info("extraindo áudio", "video", videoPath)
	start := time.Now()
	if err := media.ExtractAudio(ctx, videoPath, audioPath); err != nil {
		logger.Error("extração de áudio falhou", "erro", err)
		os.Exit(1)
	}
	logger.Info("áudio extraído", "duração", time.Since(start))

	hadFailure := false
	for _, name := range names {
		if !runProvider(ctx, logger, providerFactories[name], name, audioPath, outDir) {
			hadFailure = true
		}
	}

	if hadFailure {
		os.Exit(1)
	}
}

// runProvider roda um único provedor de STT e salva suas saídas. Retorna
// false se o provedor falhou (erro já logado) — o chamador decide se isso
// deve interromper o processo ou apenas seguir para o próximo provedor.
func runProvider(ctx context.Context, logger *slog.Logger, factory func() (stt.Provider, error), name, audioPath, outDir string) bool {
	provider, err := factory()
	if err != nil {
		logger.Error("criar provider", "provedor", name, "erro", err)
		return false
	}

	providerOutDir := filepath.Join(outDir, provider.Name())
	if err := os.MkdirAll(providerOutDir, 0o755); err != nil {
		logger.Error("criar diretório de saída do provedor", "provedor", name, "erro", err)
		return false
	}

	logger.Info("transcrevendo", "provedor", provider.Name(), "audio", audioPath)
	start := time.Now()
	result, err := provider.Transcribe(ctx, audioPath)
	if err != nil {
		logger.Error("transcrição falhou", "provedor", provider.Name(), "erro", err)
		if result != nil && len(result.RawResponse) > 0 {
			if saveErr := saveRawResponse(providerOutDir, result); saveErr != nil {
				logger.Error("salvar JSON bruto após falha", "provedor", provider.Name(), "erro", saveErr)
			} else {
				logger.Info("JSON bruto salvo apesar da falha de transcrição", "provedor", provider.Name(), "path", filepath.Join(providerOutDir, "raw.json"))
			}
		}
		return false
	}
	logger.Info("transcrição concluída", "provedor", provider.Name(), "duração", time.Since(start), "utterances", len(result.Utterances))

	if err := saveRawResponse(providerOutDir, result); err != nil {
		logger.Error("salvar JSON bruto", "provedor", provider.Name(), "erro", err)
		return false
	}
	logger.Info("JSON bruto salvo", "provedor", provider.Name(), "path", filepath.Join(providerOutDir, "raw.json"))

	txtPath := filepath.Join(providerOutDir, "transcript.txt")
	if err := writeReadableTranscript(txtPath, result); err != nil {
		logger.Error("salvar transcrição legível", "provedor", provider.Name(), "erro", err)
		return false
	}
	logger.Info("transcrição legível salva", "provedor", provider.Name(), "path", txtPath)
	return true
}

// loadDotEnv lê pares CHAVE=VALOR de path e os define como variáveis de
// ambiente, sem sobrescrever variáveis já definidas no processo. Arquivo
// ausente não é erro (uso do .env é opcional — export manual continua
// funcionando).
func loadDotEnv(path string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if _, exists := os.LookupEnv(key); !exists {
			os.Setenv(key, value)
		}
	}
	return nil
}

// saveRawResponse grava o JSON bruto do provedor em outDir/raw.json.
func saveRawResponse(outDir string, result *stt.Result) error {
	rawPath := filepath.Join(outDir, "raw.json")
	return os.WriteFile(rawPath, result.RawResponse, 0o644)
}

func writeReadableTranscript(path string, result *stt.Result) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	for _, u := range result.Utterances {
		line := fmt.Sprintf("[%s - %s] %s: %s\n", formatTimestamp(u.Start), formatTimestamp(u.End), u.Speaker, u.Text)
		if _, err := file.WriteString(line); err != nil {
			return err
		}
	}
	return nil
}

func formatTimestamp(d time.Duration) string {
	minutes := int(d.Minutes())
	seconds := d.Seconds() - float64(minutes)*60
	return fmt.Sprintf("%02d:%04.1f", minutes, seconds)
}
```

- [ ] **Step 2: Verify it compiles and vets clean**

Run: `go.exe build ./... && go.exe vet ./...`
Expected: no output (both clean)

- [ ] **Step 3: Verify the required-flag validation (safe — no network, no ffmpeg, no API cost)**

Run: `go.exe run ./cmd/spike`
Expected: logs an error about the missing `-providers` flag and exits with a non-zero status. No `local/output` directory activity, no network calls.

- [ ] **Step 4: Verify the unknown-provider validation (safe — same reason)**

Run: `go.exe run ./cmd/spike -providers=gladia,nonexistent`
Expected: logs an error about the unknown provider `nonexistent` and exits with a non-zero status, before any audio extraction or network call.

- [ ] **Step 5: Stage changes (do not commit)**

```bash
git add cmd/spike/main.go
```

- [ ] **Step 6: Note for the human — real end-to-end run is manual**

Running `go.exe run ./cmd/spike -providers=gladia,assemblyai` against the real sample lesson (real API keys, real network calls, real billed usage on both providers) is intentionally NOT part of this task's automated steps — that happens later, directly with the human. Leave a note in the task report that this is the next manual step once the code is merged.

---

## Após este plano

- Repetir o mesmo padrão (mapping + client) para Deepgram e ElevenLabs Scribe.
- Rodar as 3–5 aulas da amostra com todos os candidatos ainda não eliminados (resto da História 1).
- Comparação lado a lado e decisão de STT (História 2), usando `docs/notas-stt.md` como insumo.
