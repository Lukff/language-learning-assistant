# STT Deepgram Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement `stt.Provider` for Deepgram (single synchronous HTTP call, mapping to the
shared domain) and register it as a third selectable provider in `cmd/spike`.

**Architecture:** `internal/stt/deepgram_mapping.go` (pure JSON→domain mapping, fixture-tested) +
`internal/stt/deepgram.go` (live HTTP client: one synchronous `POST /v1/listen` call — no upload
step, no polling, unlike Gladia/AssemblyAI), both satisfying the same `stt.Provider` interface.
`cmd/spike/main.go`'s existing `providerFactories` map gains one entry; no other change needed
there, since multi-provider selection and per-provider output already exist.

**Tech Stack:** Go stdlib only (`net/http`, `net/url`, `encoding/json`). No third-party
dependencies.

## Global Constraints

- `internal/stt` stays production-quality and permanent; `cmd/spike/main.go` stays disposable.
- `DEEPGRAM_API_KEY` read only from environment (or `.env` via the existing `loadDotEnv`), never
  hardcoded/committed.
- Deepgram auth header is `Authorization: Token <key>`.
- Deepgram's batch endpoint is a **single synchronous call**: `POST https://api.deepgram.com/v1/listen`
  with the raw WAV bytes as the request body (`Content-Type: audio/wav`) — no separate upload
  step, no job ID, no polling. This differs from Gladia and AssemblyAI's upload→job→poll pattern.
- Query string parameters on the request: `model=nova-3`, `language=multi`,
  `diarize_model=latest`, `punctuate=true`, `utterances=true`. `language=multi` is what enables
  Nova-3's native code-switching (documented support for EN/ES/FR/DE/HI/RU/PT/JA/IT/NL), matching
  the project's PT/ES-in-English requirement. `diarize_model=latest` enables diarization by
  itself — do not also set the deprecated `diarize=true`.
- Deepgram's response has **no status field to validate** (unlike Gladia's `"done"` or
  AssemblyAI's `"completed"`) — the synchronous response only exists on success; errors surface as
  a non-2xx HTTP status, already handled generically by the `do()` helper.
- Response shape: utterances live under `results.utterances[]` (not at the root like AssemblyAI,
  not under `result.transcription.utterances` like Gladia). Each utterance has `start`, `end`,
  `speaker` (integer), `transcript`, and `words[]`; each word has `word` (raw), `punctuated_word`
  (with punctuation/capitalization), `start`, `end`.
- Timestamps are float64 seconds (like Gladia, not milliseconds like AssemblyAI) — reuse the
  existing `secondsToDuration(s float64) time.Duration` helper already defined in
  `internal/stt/gladia_mapping.go` (same package `stt`, no import needed). Do not duplicate it.
- Map `Word.Text` from the word's `punctuated_word` field, not the raw `word` field — this keeps
  punctuation in `Word.Text` consistent with how the Gladia and AssemblyAI fixtures already work.
- Speaker is an integer (like Gladia, not a string like AssemblyAI) → format as
  `fmt.Sprintf("speaker_%d", n)`, same pattern as `gladia_mapping.go`.
- Raw provider JSON is always preserved on disk, even when parsing/mapping fails (same pattern
  already in `internal/stt/gladia.go`'s `Transcribe`).
- HTTP client timeout: a single generous timeout (10 minutes) at the `http.Client` level, for
  consistency with Gladia/AssemblyAI — no separate short per-call timeout is needed here since
  there is no polling loop.
- No elaborate flags, no parallelism, no sophisticated retry.
- Commit messages: one line, semantic format (`type: description`) — do not run `git commit`
  automatically; stage only (`git add`), per this project's established workflow preference.

---

### Task 1: `internal/stt` Deepgram response mapping (fixture-tested)

**Files:**
- Create: `internal/stt/deepgram_mapping.go`
- Create: `testdata/deepgram_response.json`
- Test: `internal/stt/deepgram_mapping_test.go`

**Interfaces:**
- Consumes: `stt.Result`, `stt.Utterance`, `stt.Word` (already defined in `internal/stt/stt.go`,
  unchanged by this task); `secondsToDuration(s float64) time.Duration` (already defined in
  `internal/stt/gladia_mapping.go`, same package, unchanged by this task)
- Produces (unexported, used by Task 2): `mapDeepgramResponse(raw []byte) (*Result, error)`

- [ ] **Step 1: Write the synthetic fixture**

```json
// testdata/deepgram_response.json
{
  "results": {
    "utterances": [
      {
        "start": 0.42,
        "end": 1.7,
        "confidence": 0.95,
        "speaker": 0,
        "transcript": "Hi, how was your week?",
        "words": [
          {"word": "hi", "punctuated_word": "Hi,", "start": 0.42, "end": 0.65, "confidence": 0.97, "speaker": 0},
          {"word": "how", "punctuated_word": "how", "start": 0.7, "end": 0.85, "confidence": 0.96, "speaker": 0},
          {"word": "was", "punctuated_word": "was", "start": 0.9, "end": 1.05, "confidence": 0.95, "speaker": 0},
          {"word": "your", "punctuated_word": "your", "start": 1.1, "end": 1.3, "confidence": 0.94, "speaker": 0},
          {"word": "week", "punctuated_word": "week?", "start": 1.35, "end": 1.7, "confidence": 0.93, "speaker": 0}
        ]
      },
      {
        "start": 3.5,
        "end": 7.2,
        "confidence": 0.9,
        "speaker": 1,
        "transcript": "It was good, I felt a lot of saudade for my hometown though.",
        "words": [
          {"word": "it", "punctuated_word": "It", "start": 3.5, "end": 3.6, "confidence": 0.95, "speaker": 1},
          {"word": "was", "punctuated_word": "was", "start": 3.65, "end": 3.8, "confidence": 0.95, "speaker": 1},
          {"word": "good", "punctuated_word": "good,", "start": 3.85, "end": 4.1, "confidence": 0.94, "speaker": 1},
          {"word": "i", "punctuated_word": "I", "start": 4.15, "end": 4.2, "confidence": 0.96, "speaker": 1},
          {"word": "felt", "punctuated_word": "felt", "start": 4.25, "end": 4.45, "confidence": 0.93, "speaker": 1},
          {"word": "a", "punctuated_word": "a", "start": 4.5, "end": 4.55, "confidence": 0.9, "speaker": 1},
          {"word": "lot", "punctuated_word": "lot", "start": 4.6, "end": 4.75, "confidence": 0.92, "speaker": 1},
          {"word": "of", "punctuated_word": "of", "start": 4.8, "end": 4.9, "confidence": 0.91, "speaker": 1},
          {"word": "saudade", "punctuated_word": "saudade", "start": 4.95, "end": 5.4, "confidence": 0.75, "speaker": 1},
          {"word": "for", "punctuated_word": "for", "start": 5.45, "end": 5.6, "confidence": 0.9, "speaker": 1},
          {"word": "my", "punctuated_word": "my", "start": 5.65, "end": 5.8, "confidence": 0.92, "speaker": 1},
          {"word": "hometown", "punctuated_word": "hometown", "start": 5.85, "end": 6.3, "confidence": 0.89, "speaker": 1},
          {"word": "though", "punctuated_word": "though.", "start": 6.35, "end": 6.7, "confidence": 0.88, "speaker": 1}
        ]
      }
    ]
  }
}
```

This is invented conversation content (no real lesson, no real names) in the shape of Deepgram's
documented `POST /v1/listen` response with `diarize_model=latest`, `punctuate=true`, and
`utterances=true` (`results.utterances[]`, `words[].{word,punctuated_word,start,end}`, timestamps
in float seconds). Same invented dialogue as the Gladia and AssemblyAI fixtures — including the
PT/EN code-switching word "saudade" — for easy side-by-side reading, but with integer speaker
labels (`0`/`1`, like Gladia) and both a raw `word` and a `punctuated_word` per word (Deepgram's
actual shape).

- [ ] **Step 2: Write the failing tests**

```go
// internal/stt/deepgram_mapping_test.go
package stt

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMapDeepgramResponse(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "deepgram_response.json"))
	if err != nil {
		t.Fatalf("error reading fixture: %v", err)
	}

	result, err := mapDeepgramResponse(raw)
	if err != nil {
		t.Fatalf("mapDeepgramResponse returned error: %v", err)
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

func TestMapDeepgramResponse_InvalidJSON(t *testing.T) {
	_, err := mapDeepgramResponse([]byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go.exe test ./internal/stt/... -v -run TestMapDeepgramResponse`
Expected: FAIL — build error, `undefined: mapDeepgramResponse`

- [ ] **Step 4: Write the mapping implementation**

```go
// internal/stt/deepgram_mapping.go
package stt

import (
	"encoding/json"
	"fmt"
)

type deepgramResponse struct {
	Results struct {
		Utterances []deepgramUtterance `json:"utterances"`
	} `json:"results"`
}

type deepgramUtterance struct {
	Start      float64        `json:"start"`
	End        float64        `json:"end"`
	Speaker    int            `json:"speaker"`
	Transcript string         `json:"transcript"`
	Words      []deepgramWord `json:"words"`
}

type deepgramWord struct {
	PunctuatedWord string  `json:"punctuated_word"`
	Start          float64 `json:"start"`
	End            float64 `json:"end"`
}

// mapDeepgramResponse converts the raw JSON from Deepgram's synchronous
// POST /v1/listen response into the common stt.Result domain. Kept
// separate from the HTTP call (deepgram.go) so it can be tested with a
// fixture, without needing the network.
//
// Unlike Gladia/AssemblyAI, there's no status field to validate here:
// Deepgram's synchronous response only exists once transcription has
// already finished successfully — a transcription error arrives as a
// non-2xx HTTP status, handled in deepgram.go before this function is
// called.
func mapDeepgramResponse(raw []byte) (*Result, error) {
	var parsed deepgramResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("invalid json: %w", err)
	}

	utterances := make([]Utterance, 0, len(parsed.Results.Utterances))
	for _, u := range parsed.Results.Utterances {
		words := make([]Word, 0, len(u.Words))
		for _, w := range u.Words {
			words = append(words, Word{
				Text:  w.PunctuatedWord,
				Start: secondsToDuration(w.Start),
				End:   secondsToDuration(w.End),
			})
		}
		utterances = append(utterances, Utterance{
			Speaker: fmt.Sprintf("speaker_%d", u.Speaker),
			Text:    u.Transcript,
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
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go.exe test ./internal/stt/... -v`
Expected: all tests pass, including `TestMapDeepgramResponse`, `TestMapDeepgramResponse_InvalidJSON`,
and the pre-existing Gladia and AssemblyAI mapping tests (unaffected).
`ok  	assistente-idiomas/internal/stt`

- [ ] **Step 6: Run `go vet`**

Run: `go.exe vet ./...`
Expected: no output (clean)

- [ ] **Step 7: Stage changes (do not commit)**

```bash
git add testdata/deepgram_response.json internal/stt/deepgram_mapping.go internal/stt/deepgram_mapping_test.go
```

---

### Task 2: `internal/stt/deepgram.go` — Deepgram HTTP client (`Provider` implementation)

**Files:**
- Create: `internal/stt/deepgram.go`

**Interfaces:**
- Consumes: `mapDeepgramResponse(raw []byte) (*Result, error)` from Task 1
  (`internal/stt/deepgram_mapping.go`)
- Produces: `stt.NewDeepgramProvider(apiKey string) (*DeepgramProvider, error)`
- Produces: `(*DeepgramProvider).Name() string` and
  `(*DeepgramProvider).Transcribe(ctx context.Context, audioPath string) (*Result, error)`,
  satisfying `stt.Provider`

> No unit tests for this task, same justification as `internal/stt/gladia.go` and
> `internal/stt/assemblyai.go`: a live HTTP call is only exercised manually via the CLI, not
> unit-tested — there is no network mock at this stage. Verification here is build + vet only.

- [ ] **Step 1: Write the implementation**

```go
// internal/stt/deepgram.go
package stt

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

const deepgramBaseURL = "https://api.deepgram.com/v1/listen"

// DeepgramProvider implements stt.Provider using the Deepgram API.
//
// Model used: "nova-3" with language=multi — supports native
// code-switching between EN/ES/FR/DE/HI/RU/PT/JA/IT/NL, covering exactly
// the project's case (the student speaks English with bits of
// Portuguese/Spanish).
//
// Unlike Gladia and AssemblyAI, Deepgram's batch API is synchronous: a
// single POST call with the audio in the body already returns the
// complete transcription — no prior upload, no job polling.
type DeepgramProvider struct {
	apiKey string
	client *http.Client
}

func NewDeepgramProvider(apiKey string) (*DeepgramProvider, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("stt: empty DEEPGRAM_API_KEY")
	}
	// Generous timeout: covers uploading the entire lesson WAV (tens of
	// MB) plus synchronous server-side processing. Same value used by
	// Gladia/AssemblyAI, for consistency, even though there's no separate
	// polling here with its own short per-call timeout.
	return &DeepgramProvider{apiKey: apiKey, client: &http.Client{Timeout: 10 * time.Minute}}, nil
}

func (p *DeepgramProvider) Name() string { return "deepgram" }

func (p *DeepgramProvider) Transcribe(ctx context.Context, audioPath string) (*Result, error) {
	data, err := os.ReadFile(audioPath)
	if err != nil {
		return nil, fmt.Errorf("stt: read audio for deepgram: %w", err)
	}

	query := url.Values{
		"model":         {"nova-3"},
		"language":      {"multi"},
		"diarize_model": {"latest"},
		"punctuate":     {"true"},
		"utterances":    {"true"},
	}
	reqURL := deepgramBaseURL + "?" + query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("stt: build deepgram request: %w", err)
	}
	req.Header.Set("Authorization", "Token "+p.apiKey)
	req.Header.Set("Content-Type", "audio/wav")

	raw, err := p.do(req)
	if err != nil {
		return nil, fmt.Errorf("stt: transcribe deepgram: %w", err)
	}

	result, err := mapDeepgramResponse(raw)
	if err != nil {
		// Preserve the raw JSON even on a parse failure: the API call has
		// already been made (it costs money), so the caller should still be
		// able to save result.RawResponse to disk even with err != nil.
		return &Result{RawResponse: raw}, fmt.Errorf("stt: parse deepgram response: %w", err)
	}
	return result, nil
}

// do executes the request and returns the response body, with an error if
// the status isn't 2xx (the message includes the status and body, for
// debugging).
func (p *DeepgramProvider) do(req *http.Request) ([]byte, error) {
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
Expected: no output (both clean — `DeepgramProvider` satisfies `stt.Provider` at compile time via
its method set)

- [ ] **Step 3: Stage changes (do not commit)**

```bash
git add internal/stt/deepgram.go
```

---

### Task 3: Register Deepgram in `cmd/spike` and `.env.example`

**Files:**
- Modify: `cmd/spike/main.go` (add one entry to the existing `providerFactories` map)
- Modify: `.env.example` (add `DEEPGRAM_API_KEY=` line)

**Interfaces:**
- Consumes: `stt.NewDeepgramProvider(apiKey string) (*DeepgramProvider, error)` (Task 2)

`cmd/spike/main.go` already has a generic `-providers` flag, per-provider output directories, and
per-provider failure isolation (all built in the AssemblyAI multi-provider slice) — this task only
needs to make Deepgram a recognized name.

- [ ] **Step 1: Add the Deepgram entry to `providerFactories`**

In `cmd/spike/main.go`, the current map is:

```go
var providerFactories = map[string]func() (stt.Provider, error){
	"gladia": func() (stt.Provider, error) {
		return stt.NewGladiaProvider(os.Getenv("GLADIA_API_KEY"))
	},
	"assemblyai": func() (stt.Provider, error) {
		return stt.NewAssemblyAIProvider(os.Getenv("ASSEMBLYAI_API_KEY"))
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
}
```

No other change to this file — flag parsing, validation, output directory logic, and the
per-provider failure loop already handle any name present in this map.

- [ ] **Step 2: Add `DEEPGRAM_API_KEY` to `.env.example`**

Current `.env.example`:

```
# Copy this file to .env and fill in your real key.
# .env must never be committed (already in .gitignore).
GLADIA_API_KEY=
ASSEMBLYAI_API_KEY=
```

Change the last line block to:

```
# Copy this file to .env and fill in your real key.
# .env must never be committed (already in .gitignore).
GLADIA_API_KEY=
ASSEMBLYAI_API_KEY=
DEEPGRAM_API_KEY=
```

- [ ] **Step 3: Verify it compiles and vets clean**

Run: `go.exe build ./... && go.exe vet ./...`
Expected: no output (both clean)

- [ ] **Step 4: Verify the unknown-provider validation still rejects bad names (safe — no network, no ffmpeg, no API cost)**

Run: `go.exe run ./cmd/spike -providers=deepgram,nonexistent`
Expected: logs an error about the unknown provider `nonexistent` and exits with a non-zero status,
before any audio extraction or network call. (This also confirms `"deepgram"` alone is now
recognized as a valid name, since the loop reaches `nonexistent` instead of failing on
`deepgram`.)

- [ ] **Step 5: Stage changes (do not commit)**

```bash
git add cmd/spike/main.go .env.example
```

- [ ] **Step 6: Note for the human — real end-to-end run is manual**

Running `go.exe run ./cmd/spike -providers=deepgram` (or `-providers=gladia,assemblyai,deepgram`)
against the real sample lesson (real API key, real network call, real billed usage) is
intentionally NOT part of this task's automated steps — that happens later, directly with the
human, same as the other two providers.

---

## After this plan

- Side-by-side comparison of Deepgram × AssemblyAI and the STT decision (Story 2), using
  `docs/notas-stt.md` as input — closes the open decision in `decisoes-tecnologia.md`.
- ElevenLabs Scribe stays open: only worth implementing if the Deepgram × AssemblyAI decision
  isn't conclusive enough.
