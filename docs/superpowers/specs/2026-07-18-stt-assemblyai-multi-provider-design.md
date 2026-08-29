# Phase 0 — Second STT provider (AssemblyAI) + multi-provider selection — Design

> Story 1's second slice (`docs/phase-0-validation.md`), behind the same `stt.Provider` interface
> already validated in the Gladia slice (`docs/superpowers/specs/2026-07-18-pipeline-media-stt-gladia-design.md`).
> Decision recorded in conversation with the user: AssemblyAI is the second candidate; `cmd/spike`
> is also reworked in this slice to allow choosing which providers run per execution, without
> losing the outputs from other providers' previous runs.

## Objective

Implement `stt.Provider` for AssemblyAI (upload, submission, polling, mapping to the common
domain), and let `cmd/spike` run any subset of the already-implemented providers (today: Gladia,
AssemblyAI) in a single run, against the same lesson.

## Out of scope for this slice

- Deepgram and ElevenLabs Scribe (left for future slices, behind the same interface).
- Comparison between providers and the STT decision (Story 2).
- Any flag beyond `-providers` — no parallelism, no sophisticated retry.

## AssemblyAI API contract (reference: `assemblyai.com/docs`, checked on 2026-07-18)

Relevant differences from Gladia (already implemented):

| Aspect | Gladia | AssemblyAI |
|---|---|---|
| Auth header | `x-gladia-key` | `Authorization` (raw key, no `Bearer` prefix) |
| Upload | `POST /v2/upload`, multipart | `POST /v2/upload`, raw binary body (`application/octet-stream`) |
| Returned URL field | `audio_url` | `upload_url` |
| Submission endpoint | `POST /v2/pre-recorded` | `POST /v2/transcript` |
| Poll endpoint | `GET /v2/pre-recorded/{id}` | `GET /v2/transcript/{id}` |
| "Completed" status | `"done"` | `"completed"` |
| "Error" status | `"error"` | `"error"` |
| Timestamps | float64, seconds | integer, **milliseconds** |
| Speaker | integer (`0`, `1`, ...) | string (e.g. `"A"`, `"B"`) |
| Multilingual/code-switching model | `solaria-1` | `speech_models: ["universal-3-pro"]` — supports native code-switching in EN/PT/ES/FR/DE/IT (covers exactly the project's case) |

Submission request:
```json
{
  "audio_url": "<upload_url from step 1>",
  "speech_models": ["universal-3-pro"],
  "speaker_labels": true,
  "language_detection": true
}
```

Poll response (`GET /v2/transcript/{id}`, when `status == "completed"`):
```json
{
  "id": "...",
  "status": "completed",
  "utterances": [
    {
      "speaker": "A",
      "text": "...",
      "start": 420,
      "end": 1700,
      "confidence": 0.95,
      "words": [
        {"text": "...", "start": 420, "end": 650, "confidence": 0.97, "speaker": "A"}
      ]
    }
  ]
}
```
Note the flatter structure compared to Gladia's (`utterances` directly at the root, not under
`result.transcription`).

## Components

### `internal/stt/assemblyai_mapping.go`

A pure function, same pattern as Gladia: `mapAssemblyAIResponse(raw []byte) (*Result, error)`.

- Timestamps already come in integer milliseconds — direct conversion
  (`time.Duration(ms) * time.Millisecond`), without the `math.Round` rounding Gladia needed (no
  float64 artifact here).
- AssemblyAI's `Speaker` is already a string (e.g. `"A"`); mapped to `"speaker_A"` (`speaker_`
  prefix + raw value), keeping the same label pattern used for Gladia (`"speaker_0"`,
  `"speaker_1"`) so both providers' outputs stay visually comparable in Story 2.
- Error if `status != "completed"` (same pattern as Gladia, adapted to AssemblyAI's value).

### `internal/stt/assemblyai.go`

Same three-HTTP-call pattern as Gladia, adapted to the contract above:
1. `upload`: `POST /v2/upload`, body = the file's bytes (`application/octet-stream`, no
   multipart), returns `upload_url`.
2. `createJob`: `POST /v2/transcript`, the JSON above, returns `id`.
3. `poll`: `GET /v2/transcript/{id}` every 5s, 10-minute total timeout — same short
   per-call timeout (30s) + generous `http.Client` timeout (10 min, covers large file uploads)
   pattern Gladia uses today, for the same reasons (see
   `internal/stt/gladia.go` — a fix applied after a real test with a ~30min/53MB lesson).
4. Preserves `RawResponse` even on a mapping failure, same pattern as Gladia.

Key read from `ASSEMBLYAI_API_KEY` (environment variable), fails fast if empty.

### `cmd/spike/main.go` (rework)

**Provider selection:** required `-providers` flag (comma-separated list, e.g.
`gladia,assemblyai`). No default — omitting the flag is a clear error, avoiding accidentally
running (and paying for) everything again. An unknown name in the list is also a clear error,
before any network call.

```go
var providerFactories = map[string]func() (stt.Provider, error){
    "gladia":     func() (stt.Provider, error) { return stt.NewGladiaProvider(os.Getenv("GLADIA_API_KEY")) },
    "assemblyai": func() (stt.Provider, error) { return stt.NewAssemblyAIProvider(os.Getenv("ASSEMBLYAI_API_KEY")) },
}
```

**Output per provider:** reorganized into `local/output/aula-01/<provider.Name()>/raw.json` and
`.../transcript.txt` (it used to be a loose `local/output/aula-01/gladia.json`). This guarantees
that running one provider never overwrites another's output — each has its own directory — and
uses `Provider.Name()`, which until now had no consumer at all.

**A per-provider error doesn't abort the whole run:** since the same run can now include several
independent providers, a failure in one provider is logged and the CLI moves on to the next. At
the end, the process exits with a non-zero code if **any** provider failed, but the outputs from
the ones that succeeded are still saved normally. (Within a single provider, audio extraction →
transcription → saving still has no retry, same pattern as before.)

**Audio extraction:** still happens only once, before the provider loop (doesn't depend on which
provider was selected).

Flow pseudocode:
```
extract audio (once)
for each name in -providers:
    provider, err := providerFactories[name]()  // clear error if the name is unknown or the key is empty
    result, err := provider.Transcribe(ctx, audioPath)
    save result.RawResponse to local/output/<lesson>/<name>/raw.json (even if err != nil and RawResponse isn't empty)
    if err == nil: save transcript.txt and move on; otherwise: log the error, mark it as failed, move on to the next name
exit with code 1 if any failure occurred
```

## Data flow

```
aula.mp4 --ffmpeg--> audio.wav
                        |
          +-------------+-------------+
          v                           v
   GladiaProvider.Transcribe   AssemblyAIProvider.Transcribe
          |                           |
          v                           v
local/output/aula-01/gladia/     local/output/aula-01/assemblyai/
  raw.json, transcript.txt         raw.json, transcript.txt
```

## Error handling

Same table as the Gladia slice (missing `ffmpeg`, empty key, non-2xx HTTP status with body,
polling timeout, JSON that fails to parse — raw preserved), applied to AssemblyAI as well. Added
in this slice: an unknown provider name in the `-providers` flag is a clear error before any
network call; one provider's failure doesn't block the others (see the `main.go` section above).

## Tests

- `internal/stt/assemblyai_mapping_test.go`: same pattern as Gladia — a synthetic fixture
  `testdata/assemblyai_response.json` (invented conversation, no real names, including a PT/EN
  code-switching case), testing utterance/word mapping, timestamp conversion
  (milliseconds → `time.Duration`), speaker label formatting (`"A"` → `"speaker_A"`),
  `RawResponse` preservation, and the error cases (invalid JSON, unexpected status).
- `internal/stt/assemblyai.go`: no unit tests, same justification as Gladia (real network calls,
  verified manually via the CLI).
- `cmd/spike/main.go`: still no tests (disposable), manual verification via
  `go run ./cmd/spike -providers=...`.

## Privacy

Same rules already in effect (`local/` kept out of git, synthetic fixtures in `testdata/`,
`ASSEMBLYAI_API_KEY` only in an environment variable / gitignored `.env`).
