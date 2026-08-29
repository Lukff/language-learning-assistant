# Phase 0 — Fourth (and last) STT provider (ElevenLabs Scribe) — Design

> Fourth slice of Story 1 (`docs/phase-0-validation.md`), behind the same `stt.Provider` interface
> already validated in the Gladia slice
> (`docs/superpowers/specs/2026-07-18-pipeline-media-stt-gladia-design.md`), the AssemblyAI slice
> (`docs/superpowers/specs/2026-07-18-stt-assemblyai-multi-provider-design.md`), and the Deepgram
> slice (`docs/superpowers/specs/2026-07-19-stt-deepgram-design.md`). Closes out the 4 candidates
> listed in `docs/phase-0-validation.md` — from here on, Story 1 is ready to turn into Story 2's
> comparison.

## Goal

Implement `stt.Provider` for ElevenLabs Scribe (synchronous HTTP call, mapping to the common
domain), and register it in `cmd/spike` as a fourth provider selectable via `-providers`,
alongside Gladia, AssemblyAI, and Deepgram.

## Out of scope for this slice

- Comparison between providers and the STT decision (Story 2) — this slice only produces the
  client and the raw/readable outputs.
- Any flag beyond `-providers` (already exists) — no parallelism, no sophisticated retry.
- Segmenting utterances by silence gap within the same speaker (see mapping section) — an
  explicit user decision not to do this heuristic in this slice.
- Advanced API features unrelated to the project's requirements: `entity_detection`/
  `entity_redaction` (PII), `use_speaker_library`, `keyterms`, `webhook`, multichannel
  (`use_multi_channel`), `additional_formats`. None of these relate to diarization/timestamps/
  code-switching — YAGNI.

## ElevenLabs Scribe API contract (reference: `elevenlabs.io/docs`, checked on 2026-07-19)

Central architectural difference from the other 3: the response **does not come grouped into
utterances**. The API returns a flat `words[]` array, where each entry has a `type`
(`word` | `spacing` | `audio_event`) and, with diarization on, a `speaker_id` — grouping into
speech turns is left to the client (mapping described below). In the other 3 providers, the
service itself already delivers ready-made `utterances[]`.

| Aspect | Gladia / AssemblyAI / Deepgram | ElevenLabs Scribe |
|---|---|---|
| Call pattern | see previous specs | a single synchronous call (same as Deepgram) |
| Endpoint | — | `POST /v1/speech-to-text` |
| Auth header | `x-gladia-key` / `Authorization` / `Authorization: Token` | `xi-api-key` |
| Audio body | multipart (Gladia) / raw binary (AssemblyAI, Deepgram) | multipart (`file`) |
| Grouping by speaker | native (`utterances[]`) | **not native** — flat `words[]` array with a `speaker_id` per entry |
| Timestamps | float64, seconds | float64, seconds |
| Speaker | integer/string, depending on provider | string (`"speaker_0"`, `"speaker_1"`, ...) |
| Multilingual/code-switching model | `solaria-1` / `universal-3-5-pro` / `nova-3` + `language=multi` | `scribe_v2` — natively multilingual (90+ languages); the API doesn't document an explicit "multi/code-switching mode" parameter like Deepgram, so no `language_code` is sent (letting automatic detection cover the language switch mid-speech) |

Request:
```
POST https://api.elevenlabs.io/v1/speech-to-text
Content-Type: multipart/form-data
Header: xi-api-key: <key>

Form fields:
  file                    = WAV bytes
  model_id                = "scribe_v2"
  diarize                 = "true"
  num_speakers            = "2"     (Cambly is always 1:1 student-tutor)
  timestamps_granularity  = "word"  (already the default, but made explicit for clarity —
                                      same convention used for the other providers)
```

Response (200, a single channel — we don't use `use_multi_channel`):
```json
{
  "language_code": "en",
  "language_probability": 0.97,
  "text": "...",
  "words": [
    {"text": "Hi,", "type": "word", "start": 0.42, "end": 0.65, "speaker_id": "speaker_0"},
    {"text": " ", "type": "spacing", "start": 0.65, "end": 0.7, "speaker_id": "speaker_0"}
  ],
  "audio_duration_secs": 7.2,
  "transcription_id": "..."
}
```
No status field: as with Deepgram, the synchronous response only exists once the transcription
has already finished successfully — an error arrives as a non-2xx HTTP status, handled by `do()`
before parsing.

## Components

### `internal/stt/elevenlabs_mapping.go`

A pure function, same pattern as the three previous providers: `mapElevenLabsResponse(raw []byte) (*Result, error)`.

Central difference: it needs to **group** the flat `words[]` array into `[]Utterance` (the other
three only convert an `utterances[]` the API already delivers ready-made). Algorithm (decision
recorded in conversation with the user — see "Alternatives considered"):

- Walks `words[]` in order; opens a new `Utterance` whenever `speaker_id` changes relative to the
  previous entry.
- `Utterance.Speaker` = the entry's `speaker_id` (already comes in `"speaker_N"` format, no need
  for `fmt.Sprintf` like the other three).
- `Utterance.Text` = raw concatenation of `text` from **all** entries in the group, in order
  (`word` + `spacing` + `audio_event`) — preserves the exact spacing delivered by the API and
  keeps non-verbal event markers (e.g. `"(laughter)"`) as reading context for the raw transcript
  (user decision: include, don't filter).
- `Utterance.Words` = only entries with `type == "word"` become `stt.Word` (same criterion as
  the other three: the domain's clickable word list is only actual speech, not pauses/events).
- `Utterance.Start`/`End` = `start` of the group's first entry / `end` of its last.
- Timestamps in float64 seconds, same as Gladia/Deepgram — reuses `secondsToDuration` already
  defined in `internal/stt/gladia_mapping.go` (same `stt` package).
- Mapping errors here are only malformed JSON (there's no status field to validate).

### `internal/stt/elevenlabs.go`

A single HTTP function (via its own `do()`, the same 2xx-status helper as the other three), with
no separate upload and poll — but with a multipart body (unlike Deepgram, which sends the raw
WAV):

1. Opens the audio file, builds the multipart with the `file` field and the config fields listed
   above.
2. `POST`s with the multipart body, headers `xi-api-key: <key>` and
   `Content-Type: <multipart boundary>`.
3. Preserves `RawResponse` even on a mapping failure, same pattern as the other three.

`http.Client` timeout: 10 minutes, for consistency with the other three (even without needing the
short per-poll-call timeout that Gladia/AssemblyAI have — there's no polling here).

Key read from `ELEVENLABS_API_KEY` (environment variable, already documented in `CLAUDE.md`),
fails fast if empty.

### `cmd/spike/main.go`

`providerFactories` only gains one entry:

```go
"elevenlabs": func() (stt.Provider, error) {
    return stt.NewElevenLabsProvider(os.Getenv("ELEVENLABS_API_KEY"))
},
```

No other change to the file: selection via `-providers`, per-provider output
(`local/output/aula-01/elevenlabs/{raw.json,transcript.txt}`), a per-provider error doesn't abort
the run — all of this already exists and works without changes.

### `.env.example`

Add `ELEVENLABS_API_KEY=` (same convention used for the other three).

## Data flow

```
lesson.mp4 --ffmpeg--> audio.wav
                        |
       +----------------+----------------+----------------+
       v                v                v                v
GladiaProvider   AssemblyAIProvider  DeepgramProvider  ElevenLabsProvider
 .Transcribe        .Transcribe         .Transcribe        .Transcribe
       |                |                    |                  |
       v                v                    v                  v
 .../gladia/      .../assemblyai/       .../deepgram/     .../elevenlabs/
  raw.json,         raw.json,             raw.json,          raw.json,
  transcript.txt    transcript.txt        transcript.txt     transcript.txt
```

## Error handling

Same table as the previous slices (missing `ffmpeg`, empty key, non-2xx HTTP status with body,
JSON that fails to parse — raw preserved). As with Deepgram, there's no polling nor "error status
inside a status response body" — a transcription error only ever shows up as a non-2xx HTTP
status on the synchronous call itself, already covered by the generic `do()`.

## Tests

- `internal/stt/elevenlabs_mapping_test.go`: TDD (test before code).
  - `TestMapElevenLabsResponse`: synthetic fixture `testdata/elevenlabs_response.json` (the same
    invented conversation as the other three fixtures — "Hi, how was your week?" / "...saudade..."
    — for side-by-side reading), testing: 2 utterances correctly grouped by `speaker_id`, each
    one's `Speaker`/`Text`/`Start`, `Words` count and content (`Words[8] == "saudade"`, the same
    index used in the other three fixtures), and preservation of `RawResponse`.
  - `TestMapElevenLabsResponse_InvalidJSON`: malformed JSON returns an error.
  - A dedicated test (inline JSON, not the fixture) covering the `audio_event`/`spacing`
    decision: confirms these entries appear concatenated in `Utterance.Text` but **do not** end
    up in `Utterance.Words`.
- `internal/stt/elevenlabs.go`: no unit tests, same justification as the other three (real
  network call, verified manually via CLI).
- `cmd/spike/main.go`: still no tests (disposable); the change here is adding one entry to the
  `providerFactories` map, manual verification via
  `go.exe run ./cmd/spike -providers=elevenlabs`.

## Alternatives considered (utterance grouping)

- **Group only by `speaker_id` change (chosen):** a direct translation of the raw data into the
  domain, with no new heuristic. A speaker talking for a long uninterrupted stretch becomes a
  single long `Utterance` — this is only a display detail in `transcript.txt`, it doesn't affect
  per-word timestamps (used in the future click-to-seek) nor Story 2's comparison.
- **Group by `speaker_id` + silence gap (rejected):** would come closer to the segmentation
  behavior the other three already do on their own servers, but requires inventing and
  calibrating a pause threshold — exactly the kind of complexity the spike's scope rule says to
  actively refuse (`CLAUDE.md`: "no elaborate flags... refuse over-engineering").
- **A single Utterance per file, ignoring `speaker_id` (discarded):** breaks the core
  student×tutor diarization requirement.

## Privacy

Same rules already in effect (`local/` outside git, synthetic fixture in `testdata/`,
`ELEVENLABS_API_KEY` only via environment variable / gitignored `.env` — add it to
`.env.example` in this slice).
</content>
