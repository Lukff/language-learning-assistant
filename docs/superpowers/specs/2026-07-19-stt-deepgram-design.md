# Phase 0 — Third STT provider (Deepgram) — Design

> Third slice of Story 1 (`docs/fase-0-validacao.md`), behind the same `stt.Provider` interface
> already validated in the Gladia slice
> (`docs/superpowers/specs/2026-07-18-pipeline-media-stt-gladia-design.md`) and the AssemblyAI
> slice (`docs/superpowers/specs/2026-07-18-stt-assemblyai-multi-provider-design.md`). Decision
> recorded in conversation with the user: Deepgram is the third candidate — and the pair that
> closes the STT decision in `docs/decisoes-tecnologia.md` ("Preferred candidates: Deepgram or
> AssemblyAI. Decision pending.").

## Objective

Implement `stt.Provider` for Deepgram (synchronous HTTP call, mapping to the common domain), and
register it in `cmd/spike` as a third provider selectable via `-providers`, alongside Gladia and
AssemblyAI.

## Out of scope for this slice

- ElevenLabs Scribe (left for a later slice, if still needed after the Deepgram × AssemblyAI
  decision).
- Comparison between providers and the STT decision (Story 2) — this slice only produces the
  client and the raw/readable outputs; the comparison itself uses `docs/notas-stt.md` as already
  being done.
- Any flag besides `-providers` (already exists) — no parallelism, no sophisticated retry.
- Async mode via Deepgram callback — an explicit decision by the user to go synchronous (see next
  section).

## Deepgram API contract (reference: `developers.deepgram.com`, verified on 07/19/2026)

Key architectural difference from Gladia and AssemblyAI: Deepgram **does not use the
upload → create job → poll** pattern. The batch API (`POST /v1/listen`) is synchronous — a single
call with the audio in the body blocks until the complete transcription comes back in the
response. Deepgram also offers an optional async mode via HTTP callback (returns `request_id` and
does a POST to a callback URL when finished), but that was dropped for this slice: it would
require exposing a local HTTP endpoint (ngrok or a temporary server) for a local spike CLI —
unnecessary complexity compared to the synchronous mode.

| Aspect | Gladia / AssemblyAI | Deepgram |
|---|---|---|
| Call pattern | upload → create job → poll (3 calls) | a single synchronous call |
| Endpoint | `/v2/upload` + `/v2/pre-recorded` (or `/transcript`) + poll | `POST /v1/listen` |
| Auth header | `x-gladia-key` / `Authorization` (raw key) | `Authorization: Token <key>` |
| Audio body | multipart (Gladia) or raw binary (AssemblyAI) | raw binary (`Content-Type: audio/wav`) |
| Transcription options | JSON body | query string on the URL itself |
| Timestamps | float64, seconds | float64, seconds (same as Gladia) |
| Speaker | integer (Gladia) / string (AssemblyAI) | integer (`0`, `1`, ...) |
| Multilingual/code-switching model | `solaria-1` (Gladia) / `universal-3-5-pro` (AssemblyAI) | `model=nova-3` + `language=multi` — supports native code-switching in EN/ES/FR/DE/HI/RU/PT/JA/IT/NL (covers the project's case) |

Call query string:
```
POST https://api.deepgram.com/v1/listen
    ?model=nova-3
    &language=multi
    &diarize_model=latest
    &punctuate=true
    &utterances=true
```
(`diarize_model=latest` is the currently recommended form — it replaces the `diarize=true`
parameter, deprecated but still functional; `diarize_model` alone already enables diarization,
without needing `diarize=true` alongside it.)

Body: raw WAV bytes, `Content-Type: audio/wav`. No multipart, no prior upload step — the file goes
straight into the body of this same request.

Response (200, with `utterances=true`):
```json
{
  "results": {
    "utterances": [
      {
        "start": 0.42,
        "end": 1.7,
        "confidence": 0.95,
        "speaker": 0,
        "transcript": "...",
        "words": [
          {"word": "...", "start": 0.42, "end": 0.65, "confidence": 0.97, "speaker": 0, "punctuated_word": "..."}
        ]
      }
    ]
  }
}
```
`results.utterances[]` already comes grouped by speaker — the same level of convenience that
Gladia and AssemblyAI natively offer, without needing to manually group words from the flat
`results.channels[].alternatives[].words[]` array.

## Components

### `internal/stt/deepgram_mapping.go`

Pure function, same pattern as the two previous providers: `mapDeepgramResponse(raw []byte) (*Result, error)`.

- Timestamps in float64 seconds, same as Gladia — reuses `secondsToDuration` already defined in
  `internal/stt/gladia_mapping.go` (same `stt` package), instead of duplicating the function.
- Deepgram's `Speaker` is an integer (`0`, `1`, ...); mapped to `"speaker_0"`, `"speaker_1"`
  (`fmt.Sprintf("speaker_%d", n)`), the same label pattern already used for Gladia — keeps the
  three outputs visually comparable in Story 2.
- There is no `status` field in the Deepgram payload (the synchronous response only exists once
  transcription has already finished successfully — an error comes back as a non-2xx HTTP status,
  handled by `do()`, not as a status field in the body). The only mapping error here is malformed
  JSON or `results.utterances` missing/empty in an unexpected way.

### `internal/stt/deepgram.go`

A single HTTP function (called `transcribe`, via the generic `do()` — the same helper that checks
for a 2xx status), with no upload and no poll:

1. Builds the URL with the query params above.
2. `POST` with the WAV bytes in the body, headers `Authorization: Token <key>` and
   `Content-Type: audio/wav`.
3. Preserves `RawResponse` even on a mapping failure, same pattern as the other two providers.

`http.Client` timeout: kept at 10 minutes, for consistency with Gladia/AssemblyAI, even though it
doesn't need the short per-poll-call timeout that the other two have (there's no poll here — it's
a single call, so only the client's generous timeout applies).

Key read from `DEEPGRAM_API_KEY` (environment variable), fails fast if empty.

### `cmd/spike/main.go`

`providerFactories` has already been generic since the AssemblyAI slice — this slice only adds
one entry:

```go
"deepgram": func() (stt.Provider, error) {
    return stt.NewDeepgramProvider(os.Getenv("DEEPGRAM_API_KEY"))
},
```

No other changes to the file: selection via `-providers`, per-provider output
(`local/output/aula-01/deepgram/{raw.json,transcript.txt}`), a per-provider error doesn't abort
the run — all of this already exists and works without modification.

## Data flow

```
aula.mp4 --ffmpeg--> audio.wav
                        |
       +----------------+----------------+
       v                v                v
GladiaProvider   AssemblyAIProvider   DeepgramProvider
 .Transcribe        .Transcribe         .Transcribe
       |                |                    |
       v                v                    v
 .../gladia/      .../assemblyai/       .../deepgram/
  raw.json,         raw.json,             raw.json,
  transcript.txt    transcript.txt        transcript.txt
```

## Error handling

Same table as the previous slices (`ffmpeg` missing, empty key, non-2xx HTTP status with body,
JSON that fails to parse — raw preserved), with one simplification: since there's no poll, there
is no "polling timeout" or "error status in the status response body" for Deepgram — a
transcription error shows up only as a non-2xx HTTP status on the synchronous call itself, already
covered by the generic `do()`.

## Tests

- `internal/stt/deepgram_mapping_test.go`: same pattern as the two previous providers — synthetic
  fixture `testdata/deepgram_response.json` (same made-up conversation as the other fixtures, for
  side-by-side reading, including a PT/EN code-switching case), testing mapping of
  utterances/words, timestamp conversion (float seconds → `time.Duration`), speaker label
  formatting (integer → `"speaker_N"`), preservation of `RawResponse`, and the error case (invalid
  JSON).
- `internal/stt/deepgram.go`: no unit tests, same justification as the other two (real network
  call, verified manually via CLI).
- `cmd/spike/main.go`: still no tests (disposable); the change here is adding an entry to the
  `providerFactories` map, verified manually via `go.exe run ./cmd/spike -providers=deepgram`.

## Privacy

Same rules already in effect (`local/` outside git, synthetic fixture in `testdata/`,
`DEEPGRAM_API_KEY` only as an environment variable / `.env` gitignored — add it to `.env.example`
in this slice).
