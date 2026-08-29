# Quality notes — STT comparison

> A record of observations by provider/lesson, following the comparison criteria defined in
> `docs/fase-0-validacao.md` (Story 2). Notes are **paraphrased** — no literal transcription of
> speech passages, tutor names, or any data identifying a specific lesson/person.
> Feeds Story 2's final comparison table.

## Gladia

### Lesson 01

- **Overall quality:** good.
- **Diarization:** confuses speakers in overlapping-speech segments (when one speaker
  starts talking before the other finishes their sentence).
- **PT/ES code-switching:** detection works well overall, but the transcription loses track at some
  specific moments of the language switch.
- **Word-level timestamps:** good.
- **Punctuation/formatting:** punctuation doesn't always match the speech; some pauses are
  recorded as punctuation in a way that doesn't correspond to what was actually said.
- **Real cost:** free tier of 10h/month of transcription.
- **Other observations:** some transition words between sentences were lost (outside the
  overlapping-speech segments), along with quite a few "filler words" (hesitation fillers)
  in general.

## AssemblyAI

### Lesson 01

- **Overall quality:**
- **Diarization:** separates speakers very well, including in overlapping-speech segments.
- **PT/ES code-switching:** manages the language switch at some points, but gets fairly confused
  when isolated words are used (outside a full sentence in the other language).
- **Word-level timestamps:** accurate.
- **Punctuation/formatting:** sentence punctuation is quite accurate.
- **Real cost:** the official price is US$0.21/h for the model + US$0.02/h for diarization
  (billed separately, but both based on actual audio duration) — for a 29:16min video, the
  observed real cost was US$0.1025 (model) + US$0.0098 (diarization) = US$0.11225 total. There's
  also the option to include previously known terms to improve detection, at an additional
  cost of US$0.05/h. There's a one-time (not a recurring monthly) US$50 in free credits.
- **Other observations:**

## Deepgram

### Lesson 01

- **Overall quality:** medium/poor.
- **Diarization:** the provider's main problem — got the speaker identification wrong for
  most of the transcript, and also mixed up speech from different speakers.
- **PT/ES code-switching:** the provider's strongest point — good detection of words spoken in
  a language other than English.
- **Word-level timestamps:**
- **Punctuation/formatting:** "ok", but doesn't capture different intonations well, such as
  interrogative sentences.
- **Real cost:**
- **Other observations:**

## ElevenLabs Scribe

### Lesson 01

- **Overall quality:** very good.
- **Diarization:** speaker separation is nearly perfect.
- **PT/ES code-switching:** sometimes confuses Portuguese and Spanish when switching languages
  during the conversation.
- **Word-level timestamps:**
- **Punctuation/formatting:** at some points doesn't capture the intonation of questions well
  (a minor issue).
- **Real cost:** cost in credits, not directly in US$/hour — transcribing this video consumed
  1.95k credits, equivalent to US$0.195. There are 10k free credits per month.
- **Other observations:** captures many extra speech details, like pauses and laughter. Overall,
  the issues found were minor compared to the quality of the transcription delivered.
