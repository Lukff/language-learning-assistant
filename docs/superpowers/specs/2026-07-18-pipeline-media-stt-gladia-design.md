# Phase 0 — media + stt pipeline (Gladia) — Design

> Initial slice of Story 1 (`docs/fase-0-validacao.md`). Scope deliberately narrowed to
> **one** STT provider (Gladia) to validate the interface and the end-to-end pipeline before
> adding the other 3 candidates (AssemblyAI, Deepgram, ElevenLabs Scribe) in later slices.
> Decision recorded in conversation with the user: incremental approach, Gladia first.

## Goal

Extract the audio from a Cambly `.mp4` recording and transcribe it via Gladia, with diarization
and word-level timestamps, producing:
- the provider's raw JSON response, saved to disk;
- a plain-text readable version (speaker + timestamp + speech).

This validates the `stt` interface (designed for the 4 future implementations) and the `media`
package, both final per `CLAUDE.md`.

## Out of scope for this slice

- AssemblyAI, Deepgram, ElevenLabs Scribe (interface already prepared for them, implementation comes later).
- Automatic mapping of `speaker_N` → student/tutor (manual step in Story 2).
- Provider comparison (Story 2) and LLM analysis (Story 3).
- CLI flags, parallelism, sophisticated retry — forbidden by the spike's scope rule.

## Architecture

```
cmd/spike/main.go       # orchestrates: extract audio → transcribe → save outputs. Hardcoded paths.
internal/media/         # audio extraction via ffmpeg
internal/stt/           # Provider interface + Gladia implementation
testdata/               # synthetic Gladia response fixture (anonymized)
local/                  # real outputs (audio, raw JSON, txt) — outside git
```

## Components

### `internal/media`

```go
package media

func ExtractAudio(ctx context.Context, videoPath, outputPath string) error
```

- Implementation via `os/exec`, calling:
  `ffmpeg -i <videoPath> -vn -ac 1 -ar 16000 -f wav <outputPath>`
  (mono, 16kHz, WAV — a format universally accepted by STT APIs, avoids codec ambiguity).
- Checks `exec.LookPath("ffmpeg")` before running; if missing, returns a clear error
  (`ffmpeg não encontrado no PATH`) without attempting to execute.
- If `ffmpeg` returns a non-zero exit code, the error includes the process's stderr for diagnosis.

### `internal/stt`

Single interface, used by all present and future implementations:

```go
package stt

type Provider interface {
    Name() string
    Transcribe(ctx context.Context, audioPath string) (*Result, error)
}

type Result struct {
    RawResponse []byte      // raw JSON exactly as received, to save without loss
    Utterances  []Utterance
}

type Utterance struct {
    Speaker    string        // provider's raw label (e.g. "speaker_0")
    Text       string
    Start, End time.Duration
    Words      []Word
}

type Word struct {
    Text       string
    Start, End time.Duration
}
```

Design decisions:
- `Speaker` remains the provider's raw string — there is no student/tutor normalization at this
  stage (each provider labels differently; comparison and mapping happen in Story 2).
- `RawResponse` travels along with `Result` so that `cmd/spike` can write the raw data without the
  implementation needing to know about file I/O — keeps `internal/stt` free of disk side effects.
- `Word.Start/End` are always present (Gladia returns word-level timestamps by default); future
  providers that don't guarantee this must document the limitation in their own implementation file.

### `internal/stt/gladia.go`

Asynchronous flow per the Gladia API (`docs.gladia.io`):

1. `POST https://api.gladia.io/v2/upload` (multipart, the WAV file) → response returns `audio_url`.
2. `POST https://api.gladia.io/v2/pre-recorded` with body:
   ```json
   {
     "audio_url": "<received in step 1>",
     "model": "solaria-1",
     "diarization": true,
     "language_config": { "languages": ["en", "pt", "es"] }
   }
   ```
   Reason for `model: "solaria-1"`: it's the only Gladia model with documented support for
   code-switching/multilingual input (100+ languages); `solaria-3` requires a single language in
   `language_config.languages`, incompatible with the requirement of PT/ES mixed into English.
   Response returns the job's `id`.
3. Poll `GET https://api.gladia.io/v2/pre-recorded/{id}` every 5s (fixed sleep, no backoff)
   until `status == "done"`, with a 10-minute total timeout (clear error if exceeded — a ~30min
   lesson shouldn't take that long, this is a safety net against a stuck job).
4. Maps the response JSON (Gladia's utterances/words field) to `stt.Result`.

Authentication: `x-gladia-key: <GLADIA_API_KEY>` header, read from an environment variable. If the
variable is empty, fails immediately with a clear message, without attempting the HTTP call.

HTTP client: stdlib `net/http`, no third-party lib.

### `cmd/spike/main.go`

Disposable, hardcoded paths (no flags), per the spike rule:

```go
videoPath := "local/input/aula-01.mp4"
audioPath := "local/output/aula-01/audio.wav"
outDir    := "local/output/aula-01/"
```

Steps:
1. `media.ExtractAudio` — generates the WAV.
2. Instantiates `stt.NewGladiaProvider(os.Getenv("GLADIA_API_KEY"))` and calls `Transcribe`.
3. Writes `outDir/gladia.json` with `Result.RawResponse` (raw, indented or not — as received).
4. Generates and writes `outDir/gladia.txt`, one line per utterance:
   `[00:12.3 - 00:18.9] speaker_0: utterance text...`
5. Logs each step via `log/slog` (stdout), including timing of each external call.

Errors at any step halt the program with a `log.Fatal`-equivalent (non-zero exit code, clear
message) — no retry, per the spike's scope rule.

## Data flow

```
lesson.mp4 --ffmpeg--> audio.wav --upload+POST+poll--> Gladia JSON --map--> stt.Result
                                                          |                    |
                                                          v                    v
                                                    local/.../gladia.json  local/.../gladia.txt
```

## Error handling

| Situation | Behavior |
|---|---|
| `ffmpeg` missing from PATH | Clear error before attempting to run, no generic stack trace |
| `ffmpeg` fails (corrupted file, etc.) | Error includes the process's stderr |
| `GLADIA_API_KEY` empty | Clear error before any HTTP call |
| Upload or POST return status != 2xx | Error includes status code and response body (for debugging) |
| Polling exceeds 10 minutes | Explicit timeout error |
| Response JSON doesn't parse as expected | Clear error; `RawResponse` must already have been captured before parsing, so the data isn't lost even if mapping fails |

## Tests

- `internal/media`: test that `ExtractAudio` returns a clear error when `ffmpeg` is not on the
  PATH (via a manipulated `PATH` in the test). Real extraction is left for manual verification via
  the CLI (depends on ffmpeg + a real file).
- `internal/stt`: test of the JSON→`stt.Result` mapping function using a synthetic fixture at
  `testdata/gladia_response.json` — an invented conversation, with no real names or excerpts from
  real lessons, in Gladia's documented response format (diarization + words). The real HTTP call
  (upload, POST, polling) is exercised manually via `go run ./cmd/spike` against the local sample
  lesson — there is no network mock at this stage, per the spike's scope rule.
- `go vet ./...` before any commit (already required by `CLAUDE.md`).

## Privacy

- The entire `local/` directory goes into `.gitignore` (extracted audio, real raw JSON, real
  readable lesson txt) — a `.gitignore` doesn't yet exist in the repo, it will be created as part
  of the implementation.
- `testdata/gladia_response.json` is a synthetic fixture, never a real JSON saved from `local/`.
- `GLADIA_API_KEY` only in an environment variable, never committed.

## Next slices (not implemented now)

- Repeat the same `stt.Provider` interface for AssemblyAI, Deepgram, and ElevenLabs Scribe.
- Run the 3-5 sample lessons (Story 1 complete).
- Side-by-side comparison and STT decision (Story 2).
