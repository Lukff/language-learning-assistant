# Phase 0 — API Validation Spike

> Goal: validate, with real lessons and minimal cost, the two biggest uncertainties in the project —
> transcription quality with diarization (including PT/ES code-switching) and the usefulness of LLM-based analysis —
> **before** building any visual application.
>
> Format: Go CLI (`go run`), no Wails. The packages created here (`media`, `stt`, `analysis`)
> are the app's final ones; only the CLI's `main.go` is disposable.
>
> Scope rule: no elaborate flags, no parallelism, no sophisticated retry, hardcoded paths.
> Robustness comes later, with the job queue.

## STT candidates (updated 18/07/2026)

All support batch + diarization + word-level timestamps. At the project's volume (~10 lessons/month),
cost is a technical tie (~US$0.08–0.17/lesson); the differentiators are quality and free tier.

| Provider | Batch price | Per ~30 min lesson | Notes |
|----------|------------|------------------|-------------|
| AssemblyAI (Universal-2) | US$0.0025/min | ~US$0.08 | Mature; generous free tier (US$50) |
| Deepgram (Nova-3) | US$0.0043/min + US$0.0015/min diarization | ~US$0.17 | Mature; US$200 in free credits |
| ElevenLabs Scribe | US$0.004/min (diarization included) | ~US$0.12 | Released in 2026; strong at multilingual/mid-conversation language switching |
| Gladia | Free tier: 10h/month (600 min) | US$0 at current volume | Self-proclaimed leader in code-switching (source: their own blog — verify in testing) |

Discarded: OpenAI gpt-4o-transcribe (25 MB/file limit is friction for 30-min audio;
less established diarization).

## Preparation — sample selection

- [ ] Select 3–5 lessons from the current tutor, varying by:
  - [ ] one with poor audio/connection
  - [ ] one with a lot of speech overlap (interruptions, laughter)
  - [ ] one where the student talked a lot and another where they talked little
  - [ ] at least one with **code-switching** (words/phrases in Portuguese or Spanish mixed into English)
  - [ ] if possible, lessons from different months (mic setup and call quality vary)
- [ ] Note, from memory, 2–3 passages per lesson to serve as an answer key (e.g., "here I said X wrong", "here I said 'saudade' in Portuguese") — becomes an objective reference for the comparison.

**Recorded limitation:** all lessons are from the same tutor. The STT decision is validated for
this tutor; when switching tutors (especially one with a very different accent), run a
sanity-check lesson through the CLI before trusting the result.

---

## Story 1 — Audio extraction and dual transcription

**As** the project developer, **I want** a CLI that extracts audio from a Cambly recording
and transcribes it with **all candidate providers** (Deepgram, AssemblyAI, ElevenLabs Scribe,
Gladia), **so that** I get comparable results across the same real lessons.

### Acceptance criteria
- [x] `media` package: given a Cambly `.mp4`, extracts audio via ffmpeg (`os/exec`) into a format accepted by the two services; fails with a clear message if ffmpeg isn't on the PATH.
- [x] `stt` package: a single interface with one implementation per candidate (Deepgram, AssemblyAI, ElevenLabs Scribe, Gladia), using stdlib `net/http`; API keys read from environment variables (never hardcoded/committed). No candidate was eliminated at integration time — all 4 made it to the Story 2 comparison.
- [x] Each service is called **with diarization and word-level timestamps enabled** and **in the provider's appropriate multilingual/code-switching configuration** (document in the code which configuration was used and why).
- [x] Each service's raw response is saved to disk (JSON) per lesson/provider — input for Story 2 and a free regression test for the future.
- [x] A readable (plain-text) output is generated per lesson/provider: utterances with speaker identified and timestamps.
- [ ] Ran end-to-end on the 3–5 sample lessons, across all non-eliminated candidates — **recorded limitation:** only lesson 01 has been run so far (the same limitation consciously accepted in the STT/LLM decision).

### Dependencies
Sample selected; accounts and API keys created with the candidate providers (take advantage of free tiers/credits — the whole spike can come out free).

---

## Story 2 — Side-by-side comparison and STT decision

**As** the project developer, **I want** to compare the candidates' transcriptions against
criteria defined in advance, **so that** I can close the open decision in `technology-decisions.md`
based on evidence from my own lessons.

> **Decision closed based on lesson 01 alone:** the diarization difference between the candidates was
> already clear enough to close the choice without running the remaining sample lessons — a
> conscious decision not to complete the comparison, recorded as a limitation in `technology-decisions.md`.

### Comparison criteria (defined before looking at the results)
- [x] **Diarization:** are speaker changes correct? (a fatal error for the product — corrections depend on knowing who spoke). Count student/tutor mix-ups per lesson.
- [x] **PT/ES code-switching:** are words/phrases in Portuguese or Spanish transcribed correctly, garbled, or omitted? Does the foreign word break diarization or the surrounding timestamps? Evaluate on the answer-key passages.
- [x] **Word-level timestamps:** accurate enough for click-a-line-to-jump-the-video (tolerance ~1s).
- [x] **Punctuation/formatting:** readable sentences without heavy post-processing.
- [x] **Real cost per ~30-min lesson**, per service (the amount actually charged, not the list price), including whether the provider's free tier covers the project's monthly volume (~300 min/month).

### Acceptance criteria
- [x] Comparison table filled in (one row per criterion × service) with lesson 01 data — see `docs/stt-notes.md` (limitation: doesn't cover the rest of the sample lessons, decision made anyway).
- [x] Decision made and recorded in `technology-decisions.md` (Speech-to-text section moves out of "open": ElevenLabs Scribe chosen, AssemblyAI kept as a documented alternative; multilingual configuration and real costs recorded).
- [x] Test fixtures for the `stt` package derived from the winning service's (ElevenLabs) JSONs, **anonymized** (public repo) — already exist at `testdata/elevenlabs_response.json` / `internal/stt/elevenlabs_mapping_test.go`.

### Dependencies
Story 1 completed.

---

## Story 3 — LLM analysis v1 over the transcript

**As** the project developer, **I want** to run an LLM analysis over the transcripts from the
chosen service, **so that** I can validate that the corrections and vocabulary extracted are useful and that the
structured output is reliable to parse.

### Acceptance criteria
- [x] `analysis` package: receives the diarized transcript and calls the LLM via `net/http`; the prompt requests **JSON only** with: corrections for the student's speech (original + correction + short explanation in PT-BR), new vocabulary with translation, tutor expressions worth reusing.
- [x] The prompt explicitly handles code-switching: a PT/ES word in the student's speech should be recognized as a "recourse to the native language" (a vocabulary candidate to learn), not as an English error.
- [x] The prompt is saved as a versioned file in the repository (`prompts/analyze-v1.md`) — this is where versioned prompt #1 of the future database is born.
- [x] Parsing of the response JSON with error handling — tested with synthetic fixtures (`internal/analysis/parsing_test.go`) and confirmed in one real run (DeepSeek) on lesson 01. The 4/5-runs success rate wasn't measured (only 1 real run done so far).
- [x] Manual evaluation on lesson 01: corrections, vocabulary (including code-switching candidates), and tutor expressions extracted were rated as useful by the dev — see `docs/llm-analysis-notes.md`. Evaluation on the rest of the sample lessons not done.
- [x] Real cost per lesson recorded (`docs/technology-decisions.md`, "LLM-based analysis" section).
- [ ] (Optional, if time allows) Run the same prompt on Anthropic and OpenAI and note impressions — **conscious decision not to do this**: DeepSeek's quality already proved convincing on the first run (see `docs/technology-decisions.md`), so the other candidates remain on hold without needing a comparison at this phase.

### Dependencies
Story 2 completed (uses the winning service).

---

## Phase 0 exit criteria

- [x] STT decision closed and recorded in `technology-decisions.md` (ElevenLabs Scribe chosen; AssemblyAI documented as an alternative).
- [x] Analysis prompt v1 versioned in the repository (`prompts/analyze-v1.md`).
- [x] Real costs per lesson (STT + LLM) recorded in `technology-decisions.md`.
- [ ] `media`, `stt`, and `analysis` packages working end-to-end on at least 3 real lessons — **recorded limitation:** only lesson 01 has been run so far, for both packages.
- [x] Honest written verdict: does the quality validate the product? Does anything change in the MVP design? — yes, it validates: DeepSeek is already "more than enough" for the application in the dev's assessment (see `docs/llm-analysis-notes.md`); nothing changes in the MVP design.

## Progress log

| Date | What was done | Notes |
|------|-----------------|-------------|
| 19/07/2026 | STT decision closed (Story 2): **ElevenLabs Scribe** chosen as the primary provider; **AssemblyAI** kept as a documented alternative for possible provider selection in the final app. | Decision made with evidence from 1 lesson (lesson 01); the full comparison on the remaining sample lessons wasn't done. |
| 19/07/2026 | LLM analysis decision closed (Story 3): **DeepSeek** (`deepseek-v4-flash`) chosen as the primary provider — quality rated by the dev as "more than enough" on lesson 01, negligible cost. Qwen/GLM/Anthropic/OpenAI/Gemini remain on hold. | Decision made with 1 run on 1 lesson; the optional comparison with Anthropic/OpenAI wasn't done (consciously skipped). Future exploration recorded: simpler "flash" models for complementary analysis tasks. |
| 19/07/2026 | One-off test with `deepseek-v4-pro` on lesson 01 (not the default provider): more corrections than Flash (6 vs. 0), but with inaccuracy (a false positive from the prompt itself) and ~3.6x the cost. | Not convincing enough to switch the default; kept as a documented backup for a future more in-depth analysis option, after refining the prompt. See `docs/llm-analysis-notes.md` and `docs/technology-decisions.md`. |
