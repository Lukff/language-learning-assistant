# Pipeline media + stt (Gladia) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extract audio from a Cambly `.mp4` recording and transcribe it via Gladia (diarization + word timestamps + multilingual/code-switching config), saving both the raw provider JSON and a human-readable transcript — validating the `stt.Provider` interface and `media` package that the other 3 STT candidates will reuse later.

**Architecture:** Two pure Go packages (`internal/media`, `internal/stt`) with no UI framework dependency, orchestrated by a disposable `cmd/spike/main.go` CLI with hardcoded paths. `internal/stt` exposes a provider-agnostic interface; Gladia's JSON-to-domain mapping is isolated in its own testable function, kept separate from the live HTTP calls (which are only exercised manually, per spike scope rules).

**Tech Stack:** Go stdlib only (`os/exec`, `net/http`, `encoding/json`, `log/slog`, `mime/multipart`). No third-party dependencies.

## Global Constraints

- No elaborate CLI flags, no parallelism, no sophisticated retry — hardcoded paths and `go run` are acceptable in `main.go` (spike scope rule, `CLAUDE.md`).
- `internal/media` and `internal/stt` are production-quality and permanent; `cmd/spike/main.go` is disposable.
- API keys read only from environment variables (`GLADIA_API_KEY`), never hardcoded or committed.
- Raw provider JSON response is always saved to disk per lesson/provider.
- Every external service call uses diarization + word-level timestamps + the provider's multilingual/code-switching configuration, documented in code with the reason.
- Real recordings, real transcripts, and real provider JSON never enter the repo — only `local/` (gitignored). `testdata/` fixtures are synthetic/anonymized only.
- `go vet ./...` must pass before considering any task done.
- Commit messages: one line, semantic format (`type: description`) — but **do not run `git commit`** during this implementation; the user commits manually. Each task ends with `git add` (staging only).

---

### Task 1: Bootstrap Go module, `.gitignore`, and `internal/media`

**Files:**
- Create: `go.mod`
- Create: `.gitignore`
- Create: `internal/media/media.go`
- Test: `internal/media/media_test.go`

**Interfaces:**
- Produces: `media.ExtractAudio(ctx context.Context, videoPath, outputPath string) error`

- [ ] **Step 1: Initialize the Go module**

Run: `go.exe mod init assistente-idiomas`
Expected: `go: creating new go.mod: module assistente-idiomas`

- [ ] **Step 2: Create `.gitignore`**

```gitignore
/local/
*.mp4
*.wav
*.mov
```

- [ ] **Step 3: Write the failing test**

```go
// internal/media/media_test.go
package media

import (
	"context"
	"testing"
)

func TestExtractAudio_FfmpegNotInPath(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	err := ExtractAudio(context.Background(), "input.mp4", "output.wav")
	if err == nil {
		t.Fatal("expected error when ffmpeg is not in PATH, got nil")
	}
}
```

- [ ] **Step 4: Run test to verify it fails**

Run: `go.exe test ./internal/media/... -v`
Expected: FAIL — build error, `undefined: ExtractAudio` (package `media` has no such function yet)

- [ ] **Step 5: Write the implementation**

```go
// internal/media/media.go
package media

import (
	"context"
	"fmt"
	"os/exec"
)

// ExtractAudio extracts the audio track from videoPath via ffmpeg, writing
// a mono 16kHz WAV to outputPath — a format universally accepted by the
// candidate STT APIs, avoiding codec ambiguity.
func ExtractAudio(ctx context.Context, videoPath, outputPath string) error {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return fmt.Errorf("media: ffmpeg não encontrado no PATH: %w", err)
	}

	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-i", videoPath,
		"-vn",
		"-ac", "1",
		"-ar", "16000",
		"-f", "wav",
		outputPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("media: ffmpeg falhou: %w\n%s", err, output)
	}
	return nil
}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go.exe test ./internal/media/... -v`
Expected: `--- PASS: TestExtractAudio_FfmpegNotInPath` and `ok  	assistente-idiomas/internal/media`

- [ ] **Step 7: Run `go vet`**

Run: `go.exe vet ./...`
Expected: no output (clean)

- [ ] **Step 8: Stage changes (do not commit)**

```bash
git add go.mod .gitignore internal/media/media.go internal/media/media_test.go
```

---

### Task 2: `internal/stt` core types + Gladia response mapping

**Files:**
- Create: `internal/stt/stt.go`
- Create: `internal/stt/gladia_mapping.go`
- Create: `testdata/gladia_response.json`
- Test: `internal/stt/gladia_mapping_test.go`

**Interfaces:**
- Produces: `stt.Provider` interface (`Name() string`, `Transcribe(ctx context.Context, audioPath string) (*Result, error)`)
- Produces: `stt.Result{RawResponse []byte, Utterances []Utterance}`
- Produces: `stt.Utterance{Speaker string, Text string, Start, End time.Duration, Words []Word}`
- Produces: `stt.Word{Text string, Start, End time.Duration}`
- Produces (unexported, used by Task 3): `mapGladiaResponse(raw []byte) (*Result, error)`

- [ ] **Step 1: Write the synthetic fixture**

```json
// testdata/gladia_response.json
{
  "id": "11111111-1111-1111-1111-111111111111",
  "status": "done",
  "result": {
    "transcription": {
      "languages": ["en", "pt"],
      "utterances": [
        {
          "start": 0.42,
          "end": 1.7,
          "text": "Hi, how was your week?",
          "speaker": 0,
          "confidence": 0.95,
          "channel": 0,
          "language": "en",
          "words": [
            {"word": "Hi,", "start": 0.42, "end": 0.65, "confidence": 0.97},
            {"word": "how", "start": 0.7, "end": 0.85, "confidence": 0.96},
            {"word": "was", "start": 0.9, "end": 1.05, "confidence": 0.95},
            {"word": "your", "start": 1.1, "end": 1.3, "confidence": 0.94},
            {"word": "week?", "start": 1.35, "end": 1.7, "confidence": 0.93}
          ]
        },
        {
          "start": 3.5,
          "end": 7.2,
          "text": "It was good, I felt a lot of saudade for my hometown though.",
          "speaker": 1,
          "confidence": 0.9,
          "channel": 0,
          "language": "en",
          "words": [
            {"word": "It", "start": 3.5, "end": 3.6, "confidence": 0.95},
            {"word": "was", "start": 3.65, "end": 3.8, "confidence": 0.95},
            {"word": "good,", "start": 3.85, "end": 4.1, "confidence": 0.94},
            {"word": "I", "start": 4.15, "end": 4.2, "confidence": 0.96},
            {"word": "felt", "start": 4.25, "end": 4.45, "confidence": 0.93},
            {"word": "a", "start": 4.5, "end": 4.55, "confidence": 0.9},
            {"word": "lot", "start": 4.6, "end": 4.75, "confidence": 0.92},
            {"word": "of", "start": 4.8, "end": 4.9, "confidence": 0.91},
            {"word": "saudade", "start": 4.95, "end": 5.4, "confidence": 0.75},
            {"word": "for", "start": 5.45, "end": 5.6, "confidence": 0.9},
            {"word": "my", "start": 5.65, "end": 5.8, "confidence": 0.92},
            {"word": "hometown", "start": 5.85, "end": 6.3, "confidence": 0.89},
            {"word": "though.", "start": 6.35, "end": 6.7, "confidence": 0.88}
          ]
        }
      ]
    }
  }
}
```

This is invented conversation content (no real lesson, no real names), in the shape of Gladia's documented `v2/pre-recorded` result schema (`status`, `result.transcription.utterances[].{start,end,text,speaker,words[]}`, `words[].{word,start,end}`). The Portuguese word "saudade" inside an English utterance exercises the code-switching case the mapping must preserve verbatim.

- [ ] **Step 2: Write the failing tests**

```go
// internal/stt/gladia_mapping_test.go
package stt

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMapGladiaResponse(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "gladia_response.json"))
	if err != nil {
		t.Fatalf("error reading fixture: %v", err)
	}

	result, err := mapGladiaResponse(raw)
	if err != nil {
		t.Fatalf("mapGladiaResponse returned error: %v", err)
	}

	if len(result.Utterances) != 2 {
		t.Fatalf("expected 2 utterances, got %d", len(result.Utterances))
	}

	first := result.Utterances[0]
	if first.Speaker != "speaker_0" {
		t.Errorf("Speaker = %q, expected %q", first.Speaker, "speaker_0")
	}
	if first.Text != "Hi, how was your week?" {
		t.Errorf("Text = %q, unexpected", first.Text)
	}
	if first.Start != 420*time.Millisecond {
		t.Errorf("Start = %v, expected %v", first.Start, 420*time.Millisecond)
	}
	if len(first.Words) != 5 {
		t.Fatalf("expected 5 words, got %d", len(first.Words))
	}
	if first.Words[0].Text != "Hi," {
		t.Errorf("Words[0].Text = %q, unexpected", first.Words[0].Text)
	}

	second := result.Utterances[1]
	if second.Speaker != "speaker_1" {
		t.Errorf("Speaker = %q, expected %q", second.Speaker, "speaker_1")
	}
	if len(second.Words) != 13 {
		t.Fatalf("expected 13 words, got %d", len(second.Words))
	}
	if second.Words[8].Text != "saudade" {
		t.Errorf("Words[8].Text = %q, expected %q", second.Words[8].Text, "saudade")
	}

	if string(result.RawResponse) != string(raw) {
		t.Error("RawResponse should preserve the raw JSON exactly as received")
	}
}

func TestMapGladiaResponse_InvalidJSON(t *testing.T) {
	_, err := mapGladiaResponse([]byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestMapGladiaResponse_UnexpectedStatus(t *testing.T) {
	_, err := mapGladiaResponse([]byte(`{"status":"processing","result":{}}`))
	if err == nil {
		t.Fatal("expected error for status other than done, got nil")
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go.exe test ./internal/stt/... -v`
Expected: FAIL — build error, `undefined: mapGladiaResponse` (nothing in package `stt` yet)

- [ ] **Step 4: Write the core types**

```go
// internal/stt/stt.go
package stt

import (
	"context"
	"time"
)

// Provider is the single interface implemented by each candidate STT
// service (Gladia, AssemblyAI, Deepgram, ElevenLabs Scribe).
type Provider interface {
	Name() string
	Transcribe(ctx context.Context, audioPath string) (*Result, error)
}

// Result carries both the provider's raw JSON (to save to disk without
// loss) and the transcription already mapped to the common domain.
type Result struct {
	RawResponse []byte
	Utterances  []Utterance
}

// Utterance is a speech segment attributed to a speaker. Speaker is the
// provider's raw label (e.g. "speaker_0") — mapping it to
// student/tutor is a manual step in Story 2, outside this slice.
type Utterance struct {
	Speaker    string
	Text       string
	Start, End time.Duration
	Words      []Word
}

type Word struct {
	Text       string
	Start, End time.Duration
}
```

- [ ] **Step 5: Write the mapping implementation**

```go
// internal/stt/gladia_mapping.go
package stt

import (
	"encoding/json"
	"fmt"
	"math"
	"time"
)

type gladiaResponse struct {
	Status string `json:"status"`
	Result struct {
		Transcription struct {
			Utterances []gladiaUtterance `json:"utterances"`
		} `json:"transcription"`
	} `json:"result"`
}

type gladiaUtterance struct {
	Start   float64      `json:"start"`
	End     float64      `json:"end"`
	Text    string       `json:"text"`
	Speaker int          `json:"speaker"`
	Words   []gladiaWord `json:"words"`
}

type gladiaWord struct {
	Word  string  `json:"word"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

// mapGladiaResponse converts the raw JSON from Gladia's
// GET /v2/pre-recorded/{id} endpoint to the common stt.Result domain.
// Kept separate from the HTTP calls (gladia.go) so it is testable with a
// fixture, without needing the network.
func mapGladiaResponse(raw []byte) (*Result, error) {
	var parsed gladiaResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("json inválido: %w", err)
	}
	if parsed.Status != "done" {
		return nil, fmt.Errorf("status inesperado: %q", parsed.Status)
	}

	utterances := make([]Utterance, 0, len(parsed.Result.Transcription.Utterances))
	for _, u := range parsed.Result.Transcription.Utterances {
		words := make([]Word, 0, len(u.Words))
		for _, w := range u.Words {
			words = append(words, Word{
				Text:  w.Word,
				Start: secondsToDuration(w.Start),
				End:   secondsToDuration(w.End),
			})
		}
		utterances = append(utterances, Utterance{
			Speaker: fmt.Sprintf("speaker_%d", u.Speaker),
			Text:    u.Text,
			Start:   secondsToDuration(u.Start),
			End:     secondsToDuration(u.End),
			Words:   words,
		})
	}

	return &Result{
		RawResponse: raw,
		Utterances:  utterances,
	}, nil
}

// secondsToDuration rounds to the nearest millisecond, avoiding
// floating-point artifacts from the float64 coming from the JSON
// (millisecond precision is more than enough given the ~1s tolerance
// required by Story 2).
func secondsToDuration(s float64) time.Duration {
	return time.Duration(math.Round(s*1000)) * time.Millisecond
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go.exe test ./internal/stt/... -v`
Expected: `--- PASS: TestMapGladiaResponse`, `--- PASS: TestMapGladiaResponse_InvalidJSON`, `--- PASS: TestMapGladiaResponse_UnexpectedStatus`, `ok  	assistente-idiomas/internal/stt`

- [ ] **Step 7: Run `go vet`**

Run: `go.exe vet ./...`
Expected: no output (clean)

- [ ] **Step 8: Stage changes (do not commit)**

```bash
git add testdata/gladia_response.json internal/stt/stt.go internal/stt/gladia_mapping.go internal/stt/gladia_mapping_test.go
```

---

### Task 3: `internal/stt/gladia.go` — Gladia HTTP client (`Provider` implementation)

**Files:**
- Create: `internal/stt/gladia.go`

**Interfaces:**
- Consumes: `mapGladiaResponse(raw []byte) (*Result, error)` from Task 2 (`internal/stt/gladia_mapping.go`)
- Produces: `stt.NewGladiaProvider(apiKey string) (*GladiaProvider, error)`
- Produces: `(*GladiaProvider).Name() string` and `(*GladiaProvider).Transcribe(ctx context.Context, audioPath string) (*Result, error)`, satisfying `stt.Provider`

> Per the design spec, live HTTP calls (upload, job creation, polling) are exercised manually via the CLI against a real lesson file, not unit-tested — there is no network mock at this stage. This task's steps are implementation + compile/vet verification; the real end-to-end check happens in Task 4.

- [ ] **Step 1: Write the implementation**

```go
// internal/stt/gladia.go
package stt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const gladiaBaseURL = "https://api.gladia.io"

// GladiaProvider implements stt.Provider using the Gladia API.
//
// Model used: "solaria-1" — it is the only Gladia model with documented
// support for code-switching/multilingual configuration (100+ languages);
// "solaria-3" requires a single language in language_config.languages, which
// is incompatible with the requirement for PT/ES mixed into the student's English.
type GladiaProvider struct {
	apiKey string
	client *http.Client
}

func NewGladiaProvider(apiKey string) (*GladiaProvider, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("stt: GLADIA_API_KEY vazia")
	}
	return &GladiaProvider{apiKey: apiKey, client: &http.Client{}}, nil
}

func (p *GladiaProvider) Name() string { return "gladia" }

func (p *GladiaProvider) Transcribe(ctx context.Context, audioPath string) (*Result, error) {
	audioURL, err := p.upload(ctx, audioPath)
	if err != nil {
		return nil, fmt.Errorf("stt: upload gladia: %w", err)
	}

	jobID, err := p.createJob(ctx, audioURL)
	if err != nil {
		return nil, fmt.Errorf("stt: criar job gladia: %w", err)
	}

	raw, err := p.poll(ctx, jobID)
	if err != nil {
		return nil, fmt.Errorf("stt: aguardar job gladia: %w", err)
	}

	result, err := mapGladiaResponse(raw)
	if err != nil {
		return nil, fmt.Errorf("stt: parsear resposta gladia: %w", err)
	}
	return result, nil
}

func (p *GladiaProvider) upload(ctx context.Context, audioPath string) (string, error) {
	file, err := os.Open(audioPath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("audio", filepath.Base(audioPath))
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, file); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, gladiaBaseURL+"/v2/upload", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("x-gladia-key", p.apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	respBody, err := p.do(req)
	if err != nil {
		return "", err
	}

	var uploadResp struct {
		AudioURL string `json:"audio_url"`
	}
	if err := json.Unmarshal(respBody, &uploadResp); err != nil {
		return "", err
	}
	return uploadResp.AudioURL, nil
}

func (p *GladiaProvider) createJob(ctx context.Context, audioURL string) (string, error) {
	payload, err := json.Marshal(map[string]any{
		"audio_url":   audioURL,
		"model":       "solaria-1",
		"diarization": true,
		"language_config": map[string]any{
			"languages": []string{"en", "pt", "es"},
		},
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, gladiaBaseURL+"/v2/pre-recorded", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("x-gladia-key", p.apiKey)
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

func (p *GladiaProvider) poll(ctx context.Context, jobID string) ([]byte, error) {
	url := fmt.Sprintf("%s/v2/pre-recorded/%s", gladiaBaseURL, jobID)
	deadline := time.Now().Add(10 * time.Minute)

	for {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timeout de 10 minutos aguardando job %s", jobID)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("x-gladia-key", p.apiKey)

		respBody, err := p.do(req)
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
		case "done":
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

// do executes the request and returns the response body, with an error if the
// status is not 2xx (message includes status and body, for debugging).
func (p *GladiaProvider) do(req *http.Request) ([]byte, error) {
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
Expected: no output (both clean — `GladiaProvider` satisfies `stt.Provider` at compile time via its method set)

- [ ] **Step 3: Stage changes (do not commit)**

```bash
git add internal/stt/gladia.go
```

---

### Task 4: `cmd/spike/main.go` — orchestration CLI + manual end-to-end run

**Files:**
- Create: `cmd/spike/main.go`

**Interfaces:**
- Consumes: `media.ExtractAudio` (Task 1), `stt.NewGladiaProvider`, `stt.Result`, `stt.Utterance` (Tasks 2–3)

- [ ] **Step 1: Write the implementation**

```go
// cmd/spike/main.go
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"assistente-idiomas/internal/media"
	"assistente-idiomas/internal/stt"
)

func main() {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	const (
		videoPath = "local/input/aula-01.mp4"
		audioPath = "local/output/aula-01/audio.wav"
		outDir    = "local/output/aula-01"
	)

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		logger.Error("create output directory", "error", err)
		os.Exit(1)
	}

	logger.Info("extracting audio", "video", videoPath)
	start := time.Now()
	if err := media.ExtractAudio(ctx, videoPath, audioPath); err != nil {
		logger.Error("audio extraction failed", "error", err)
		os.Exit(1)
	}
	logger.Info("audio extracted", "duration", time.Since(start))

	provider, err := stt.NewGladiaProvider(os.Getenv("GLADIA_API_KEY"))
	if err != nil {
		logger.Error("create gladia provider", "error", err)
		os.Exit(1)
	}

	logger.Info("transcribing via gladia", "audio", audioPath)
	start = time.Now()
	result, err := provider.Transcribe(ctx, audioPath)
	if err != nil {
		logger.Error("gladia transcription failed", "error", err)
		os.Exit(1)
	}
	logger.Info("transcription completed", "duration", time.Since(start), "utterances", len(result.Utterances))

	rawPath := filepath.Join(outDir, "gladia.json")
	if err := os.WriteFile(rawPath, result.RawResponse, 0o644); err != nil {
		logger.Error("save raw JSON", "error", err)
		os.Exit(1)
	}
	logger.Info("raw JSON saved", "path", rawPath)

	txtPath := filepath.Join(outDir, "gladia.txt")
	if err := writeReadableTranscript(txtPath, result); err != nil {
		logger.Error("save readable transcript", "error", err)
		os.Exit(1)
	}
	logger.Info("readable transcript saved", "path", txtPath)
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

- [ ] **Step 3: Prepare local input**

Create `local/input/` and place a real sample `.mp4` lesson there as `local/input/aula-01.mp4` (already have one locally per earlier conversation). This directory is gitignored.

- [ ] **Step 4: Set the API key and run end-to-end**

Run (adjust for your shell):
```bash
export GLADIA_API_KEY="<your key>"
go.exe run ./cmd/spike
```
Expected: log lines for each stage (`extracting audio`, `audio extracted`, `transcribing via gladia`, `transcription completed`, `raw JSON saved`, `readable transcript saved`), exit code 0, and two new files: `local/output/aula-01/gladia.json` (raw) and `local/output/aula-01/gladia.txt` (readable, one line per utterance with speaker + timestamps).

- [ ] **Step 5: Manually inspect the output**

Open `local/output/aula-01/gladia.txt` and confirm: speaker turns look plausible, timestamps are monotonic, and any PT/ES code-switching in the lesson shows up as text rather than being dropped or garbled. Note anything odd — feeds directly into Story 2's comparison criteria.

- [ ] **Step 6: Stage changes (do not commit)**

```bash
git add cmd/spike/main.go
```

---

## After this plan

- Next slices: repeat Tasks 2–3 (mapping + client) for AssemblyAI, Deepgram, and ElevenLabs Scribe behind the same `stt.Provider`.
- Run the 3–5 sample lessons (the rest of Story 1).
- Side-by-side comparison and STT decision (Story 2), including turning the Gladia fixture validated here into the winner's definitive `testdata/`.
