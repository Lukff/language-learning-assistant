# Quality notes — LLM-based analysis

> A record of observations by provider/lesson, following the criteria from Story 3 in
> `docs/fase-0-validacao.md`. Notes are **paraphrased** — no literal transcription of speech
> passages, tutor names, or any data identifying a specific lesson/person. Feeds the
> decision recorded in `docs/decisoes-tecnologia.md` ("LLM-based analysis" section).

## DeepSeek (`deepseek-v4-flash`)

### Lesson 01

- **Overall quality:** very good — the dev's assessment: "more than enough for this application,
  worked very well." Convincing already on the first run.
- **Corrections:** no correction flagged in this lesson (the `corrections` list came back empty). Not yet
  evaluated whether this reflects a real absence of errors in the student's speech or a tendency of
  the model to be conservative — that only becomes clear when comparing more lessons.
- **Vocabulary:** 20 items extracted, covering both thematic vocabulary from the conversation (e.g.,
  everyday terms, idiomatic expressions) and more technical vocabulary that came up in the chat (e.g.,
  linguistics terms). No item clearly invalid or out of context was observed.
- **Tutor expressions:** 9 expressions extracted, each with a context note in PT-BR. The dev's read:
  context notes were clear and useful, the kind that helps reuse the expression later.
- **Format/parsing:** the response came back as pure JSON (via prefill + `stop` in DeepSeek's Chat
  Prefix Completion), parsed without error on the first try — 1/1 successful run.
- **Real cost:** 8,575 prompt tokens + 725 completion tokens = 9,300 total tokens → ~US$0.0014
  (less than a tenth of a US cent), at `deepseek-v4-flash` tier pricing.
- **Other observations:** the API rejects `response_format: json_object` combined with prefill
  (a 400 error on a first attempt, before adjusting the client) — fixed in the code
  (`internal/analysis/openai_compatible.go`), with no impact on the observed response quality.

## DeepSeek (`deepseek-v4-pro`) — one-off test, not the decision

Run once on lesson 01 (the same transcript as the Flash test), for comparison — this model isn't
part of the default provider (`NewDeepSeekProvider` remains fixed on `deepseek-v4-flash`); the
model id was changed manually and reverted right after.

- **Corrections:** 6 flagged (against 0 from Flash on the same lesson) — covering fragmented-sentence
  fluency and grammar errors (adverb order, verb tense, incorrect noun). One of the 6, however, is a
  false positive from the prompt itself: the item appears in the `corrections` list but the
  `explanation` explicitly says there was no error there — a sign of an inaccuracy to refine in the
  prompt, not just a matter of the chosen model.
- **Vocabulary:** 19 items, similar quality to Flash.
- **Tutor expressions:** 9, similar quality to Flash.
- **Real cost:** 8,575 prompt tokens + 1,594 completion tokens = 10,169 total → ~US$0.0051 (~3.6x
  the cost of Flash on the same lesson).
- **Dev's assessment:** the analysis was somewhat imprecise in places — more a matter of
  refining the prompt than the model itself. Not convincing enough to justify the higher cost right
  now, but kept **as a backup** for a future more in-depth analysis option.

## Phase 2 Story 2 — Pilot: Student corrections (`analyze-corrections-v1`)

Manual verification of the full flow (credential → speaker selection → "Analyze corrections" →
inline correction in the Detail view → "Reprocess corrections" → discard on speaker switch), running
`wails3 dev` on a real lesson.

- **Lessons observed:** 1.
- **Flow/UI:** worked as designed at every step of the verification script (a hint before
  selecting the speaker, the analysis button, "Analyzing…"/"Reprocess corrections" states,
  confirmation on reprocessing, discard with a warning when switching speakers).
- **Correction quality:** good enough for this stage (pilot), but with a clear inconsistency
  in the prompt: the model flags as an error word repetitions that are part of the student's
  thinking-out-loud process (hesitation, natural self-correction in speech) — it's not useful to
  mark that as an English correction in this feature. It's a prompt fix (`prompts/analyze-corrections-v1.md`),
  not a model issue.
- **"Not located" correction fallback:** it occurred — some corrections came back in the model's
  response but didn't match the actual utterance text (text/index matching didn't find the
  passage). Not yet quantified lesson by lesson; the UI already handles this case without breaking the screen.
- **Decision:** **keep the task as-is** — the infrastructure (credential, on-demand trigger,
  idempotent persistence, inline display, error resilience) is validated and correct. The
  prompt refinement (ignoring repetition/hesitation as not-an-error, reducing the
  "not located" fallback) is recorded as future work, expected at this pilot stage — it does not
  block closing Story 2.

## Untested candidates (on hold)

Qwen, GLM, Anthropic (Claude), and OpenAI (GPT) haven't been run — DeepSeek's quality already
convinced on the first lesson, so the optional comparison planned in Story 3 was consciously
skipped for now. See `docs/decisoes-tecnologia.md` for the full rationale.

**Future exploration recorded (not a decision or current priority):** evaluate even simpler/cheaper
"flash" models than the ones already in use, for complementary analysis tasks (not the
main analysis — e.g., auxiliary classifications, light summarization).
