# Phase 0 — Story 3: LLM Analysis v1 — Design

> Covers the complete architecture of the `internal/analysis` package and the
> `prompts/analyze-v1.md` prompt (`docs/fase-0-validacao.md`, Story 3), including the contract for
> all 6 LLM candidates considered. Implementation, however, proceeds in **slices prioritized by
> cost** (see section below) — this document does not imply implementing all 6 at once.
>
> Unlike Story 2 (STT), this LLM exploration remains **non-binding**: Story 3's criterion in
> `fase-0-validacao.md` already marks the LLM comparison as optional ("no obligation to close the
> decision in this phase"). `decisoes-tecnologia.md` stays "open" on LLM Analysis until the user
> decides to close it, even after this exploration.

## Objective

Implement `analysis.Provider` (a single interface, multiple implementations) that takes a lesson's
diarized transcript and returns student corrections, new vocabulary, and tutor expressions, as
structured JSON — and run that analysis, on lesson 01, against LLM candidates starting with the
cheapest, stopping as soon as the quality is convincing.

## Slice strategy (cost-based prioritization)

Direct/official price per million tokens, researched in July/2026:

| Provider | Input (US$/1M) | Output (US$/1M) |
|---|---|---|
| DeepSeek V4 | ~0.14 (Flash) – ~0.44 (Pro) | ~0.28 (Flash) – ~0.87 (Pro) |
| Qwen 3.7 Max (Alibaba) | ~1.25 (promotional) | ~3.75 |
| GLM 5.2 (Zhipu/Z.ai) | ~1.40 | ~4.40 |
| Anthropic, OpenAI, Gemini | typically above the three above at flagship tiers | — |

Implementation and testing order:

1. **Slice 1 — DeepSeek.** Implement the client, run it on lesson 01, note quality in
   `docs/notas-analise-llm.md`.
2. If the quality isn't convincing → **Slice 2 — Qwen**. If it is, stop here — the following
   slices (including Anthropic/OpenAI/Gemini) are not implemented in this round.
3. If it still isn't convincing → **Slice 3 — GLM**.
4. Only if none of the 3 convince, slices with Anthropic, OpenAI, and Gemini follow — order and
   necessity to be decided at the time, outside this document's planning scope.

Each slice is planned and executed in isolation (same pattern used for STT's 4 slices), not all
at once.

## `internal/analysis` package architecture

```go
package analysis

type Provider interface {
    Name() string
    Analyze(ctx context.Context, transcript string) (*Result, error)
}

type Result struct {
    RawResponse      []byte
    Corrections      []Correction
    Vocabulary       []VocabularyItem
    TutorExpressions []Expression
}

type Correction struct {
    Original    string
    Correction  string
    Explanation string // PT-BR
}

type VocabularyItem struct {
    Term        string
    Translation string
}

type Expression struct {
    Text string
    Note string // PT-BR, usage context
}
```

**Client split (Approach B — approved in discussion, rejecting a fully independent client per
provider):**

- `openai_compatible.go`: a generic `openAICompatibleProvider{name, baseURL, apiKey, model
  string}` struct, satisfying `Provider`. Used by 4 constructors — `NewOpenAIProvider`,
  `NewDeepSeekProvider`, `NewGLMProvider`, `NewQwenProvider` — each only fixing a different
  `baseURL`/`model`, since all 4 expose (per each one's documentation) a `/chat/completions`
  endpoint compatible with the OpenAI format.
- `anthropic.go`: its own client (Messages API — a request/response format different from the
  OpenAI standard).
- `gemini.go`: its own client (Gemini's request/response format).
- `parsing.go`: `parseAnalysisResponse(raw []byte) (*Result, error)`, shared by all 6 — the output
  format is defined by us in the prompt, not by the provider, so there's no provider-specific
  mapping like in `internal/stt`.

**Accepted risk:** if any of the 4 "OpenAI-compatible" ones (especially GLM or Qwen) turns out to
be incompatible in practice (a different response field, JSON mode with a different parameter
name), it leaves `openai_compatible.go` and becomes its own isolated client — a decision to
confirm during each slice's implementation, without contaminating the others.

## Prompt (`prompts/analyze-v1.md`)

Asks for **only** this JSON, with no text outside the object:

```json
{
  "corrections": [{"original": "...", "correction": "...", "explanation": "..."}],
  "vocabulary": [{"term": "...", "translation": "..."}],
  "tutor_expressions": [{"text": "...", "note": "..."}]
}
```

Handles code-switching explicitly (`CLAUDE.md`): a word/phrase in PT or ES in the student's speech
**doesn't** go into `corrections` — it's a candidate for `vocabulary` (the equivalent English the
student "reached for" in their native language instead), never treated as an English mistake.

### Prefill (markdown-wrapping mitigation)

Where the provider supports "assistant prefill" (the conversation's last message already as
`role: "assistant"` with partial content, forcing the model to continue from there), the prefill
sent is `"```json\n"` — the block's opening. The model, already "inside" the block, only needs to
complete the JSON and close it with ` ``` `, which is its natural behavior anyway (a deliberate
choice: leverage that behavior instead of trying to suppress it).

- **Anthropic:** confirmed support — the Messages API accepts the last message as `assistant` with
  partial content, and the response returns only the continuation.
- **DeepSeek:** documented a beta "Chat Prefix Completion" feature — to confirm whether it persists
  in V4.
- **GLM, Qwen:** to confirm during each slice's implementation whether they accept the same
  mechanism (common in vLLM-style OpenAI-compatible backends, but not guaranteed).
- **OpenAI, Gemini:** no prefill support in the hosted API — the defense is just each one's native
  JSON mode (`response_format: json_object` / `responseMimeType: application/json`), which already
  returns pure JSON.

The provider returns only the **continuation** — it doesn't repeat the prefill. Since the prefill
already forces the model to start right at the JSON content (right after `` ```json\n ``), the
continuation itself is already near-pure JSON: there's no need to reconstruct anything with the
prefix. `parseAnalysisResponse` receives that continuation directly and only needs to strip a
possible trailing ` ``` ` closing (and whitespace) before `json.Unmarshal` — a harmless no-op for
OpenAI/Gemini, which already return pure JSON via native JSON mode and don't have that closing
fence.

Confirmed in DeepSeek's documentation (`api-docs.deepseek.com/guides/chat_prefix_completion`): the
prefill feature requires `base_url = "https://api.deepseek.com/beta"`, the last message with
`role: "assistant"` and `"prefix": true`, and accepts an optional `stop` (e.g. `["```"]`) — used
exactly for this case (preventing the model from continuing with an explanation after closing the
block). Setting that `stop` is the first line of defense; the strip in `parseAnalysisResponse` is
the second, for when `stop` isn't supported or doesn't catch the closing fence.

## Data flow

**Input: reuse the STT result without re-transcribing.** Today `cmd/spike` only saves `raw.json`
(the provider's raw JSON) and `transcript.txt` (readable text) per STT provider. Neither is
reconstructible into `[]stt.Utterance` without calling the provider-specific mapping function,
which is deliberately unexported. To allow running the analysis across several slices/candidates
without re-paying/re-running STT every time, the STT step in `cmd/spike` now also saves
`utterances.json` — a direct `encoding/json.Marshal` of `result.Utterances`
(`[]stt.Utterance`, already with exported fields; `time.Duration` serializes as an integer in
nanoseconds and deserializes back without loss). The analysis reads that file directly:

```
local/output/aula-01/elevenlabs/{raw.json, transcript.txt, utterances.json}
                                          |
                                          v
                              analysis.SpeakerExamples(utterances, 3)
                                          |
                              interactive confirmation via stdin
                             (is speaker_0 the student or the tutor?)
                                          |
                                          v
                        analysis.FormatTranscript(utterances, speakerRoles)
                                          |
                       +------------------+------------------+
                       v                  v                  v
                DeepSeekProvider    QwenProvider  ...   (current slice)
                   .Analyze           .Analyze
                       |                  |
                       v                  v
        local/output/aula-01/analysis/deepseek/{raw.json, result.txt}
```

**Interactive speaker → role confirmation:**

```go
// SpeakerExamples returns up to n example utterances per speaker label, for
// a human to confirm who is the student and who is the tutor before
// building the speakerRoles used by FormatTranscript.
func SpeakerExamples(utterances []stt.Utterance, n int) map[string][]string
```

`cmd/spike` prints each speaker's example utterances and asks via stdin which one is the
student/tutor (a Cambly lesson is always 1:1 — confirming one of the two implies the other).
No flag to skip this confirmation: it's quick and avoids silently mismapping, which would
contaminate the whole analysis.

```go
// FormatTranscript converts the diarized utterances into text readable by
// the prompt, labeling each utterance as "Aluno" or "Tutor" via
// speakerRoles. Errors if some Speaker isn't mapped.
func FormatTranscript(utterances []stt.Utterance, speakerRoles map[string]string) (string, error)
```

## `cmd/spike` integration

Same pattern as STT: a `providerFactories` map (now for `analysis.Provider`), an
`-analysis-providers` flag (names: `deepseek`, `qwen`, `glm`, `anthropic`, `openai`, `gemini`),
per-provider failure isolation. Each slice only adds the corresponding entry to the map — no
structural change to the file per new slice (same behavior observed across STT's 4 slices).

## Cost and quality notes

New doc `docs/notas-analise-llm.md`, mirroring `docs/notas-stt.md` (same privacy header:
paraphrased notes, no transcribing literal snippets or identifiable data). Criteria per
provider/lesson:

- **Overall quality**
- **Corrections:** are they real? Does the model invent a mistake that isn't there (false
  positive)?
- **Vocabulary:** useful? Does it correctly treat PT/ES code-switching as vocabulary, not as a
  mistake?
- **Tutor expressions:** reusable?
- **Real cost:** checked manually on each provider's billing dashboard (same convention as STT —
  not worth programmatically extracting `usage.tokens`; each API has a different `usage` format).
- **Other observations**

## Error handling

- Parse failure (even after stripping the trailing code fence): returns `&Result{RawResponse:
  raw}, err` — the call already cost money, so `RawResponse` stays available for the caller to
  save to disk even with an error (same pattern as `ElevenLabsProvider`).
- HTTP error (non-2xx status): handled generically per provider, with no useful `RawResponse` to
  save.
- No retry (the spike's rule).
- `SpeakerExamples`/`FormatTranscript`: an explicit error if a `Speaker` isn't in `speakerRoles` —
  a visible failure, not a silent guess.

## Tests

- `parsing_test.go`: fixtures covering valid JSON as-is, JSON with a leftover trailing code fence
  closing, and invalid JSON.
- `FormatTranscript`/`SpeakerExamples`: tests with synthetic utterances (same style as the STT
  fixtures — an invented conversation, no real data).
- The 6 HTTP clients (`openai_compatible.go`, `anthropic.go`, `gemini.go`): no unit test, same
  justification as STT — a real network call, verified manually via the CLI on each slice.
- `cmd/spike/main.go`: no tests (disposable).

## New credentials

Environment variables (read-only, never hardcoded, same convention as `CLAUDE.md`):
`DEEPSEEK_API_KEY`, `QWEN_API_KEY`, `GLM_API_KEY`, `GEMINI_API_KEY` (Anthropic and OpenAI already
planned for). Each slice adds only that slice's candidate variable to `.env.example` — not all at
once.

## Privacy

Same rules already in effect: `local/` kept out of git, `docs/notas-analise-llm.md` with
paraphrased notes (no literal snippets of real speech, no tutor names), API keys only in
environment variables / gitignored `.env`.

## Alternatives considered

- **A single aggregator (e.g. OpenRouter) for all 6 candidates (rejected):** would simplify
  credentials/billing, but adds a middleman (extra cost/latency) and doesn't faithfully reflect
  each official API's real cost and behavior — the user chose direct access to each provider.
- **A fully independent client per provider, 6 files (Approach A, rejected):** same pattern as
  `internal/stt`, but would create real duplication across the 4 OpenAI-compatible candidates
  (OpenAI, DeepSeek, GLM, Qwen) — not premature abstraction, since the 4 concrete cases already
  exist now.
- **Prefill with a plain `"{"` (rejected in favor of `"```json\n"`):** the version with the code
  fence opening is more predictable — it leverages the model's natural behavior of closing the
  block (something it would try to do anyway) instead of fighting that habit.
- **Programmatic extraction of tokens/cost from each response (rejected):** the `usage` format
  varies too much across 6 APIs to justify the complexity in a spike; kept the same manual process
  (billing dashboard) already used for STT.
