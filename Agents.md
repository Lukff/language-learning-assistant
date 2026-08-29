# Agents.md

## The project

Language-learning assistant: a **personal** desktop app (1 user, 1 dev, no server of its
own) that archives recordings of English lessons from Cambly and generates diarized transcripts
(student × tutor), LLM-based analyses (corrections, vocabulary, tutor expressions), and a progress
view. UI and analyses in PT-BR; lessons are in English with occasional code-switching (PT/ES).

**Sources of truth** (read before deciding anything):
- `docs/decisoes-tecnologia.md` — current technology choices. Do not silently contradict; if a choice needs to change, propose updating the document.
- `docs/fase-1-mvp.md` — stories and tracking for the current phase.
- `docs/fase-0-validacao.md` — completed phase (API validation); kept as history.

## Current phase: Phase 1 (MVP — import → transcribe → watch)

Wails v3 app (alpha, **version pinned** in `go.mod`; upgrading from alpha is a deliberate task,
never in the middle of a feature) + Svelte 5, with a SQLite database. Scope, stories, and
milestones in `docs/fase-1-mvp.md` — follow it, including the **out-of-scope** list (in-UI
analysis, tags, progress, sync, FTS are left for later phases) and the **technical risks to
tackle first** (video with range requests in the asset handler; drag-and-drop in v3; keyring on
Linux).

Phase rules:
- Phase 0 packages (`media`, `stt`, `analysis`) are reused as-is — no copying, no rewriting.
- The `cmd/spike` CLI and the STT providers discarded in the comparison (Gladia, AssemblyAI, Deepgram) have been removed: they served their purpose validating Phase 0 and have no remaining caller in the app. `internal/stt` keeps only ElevenLabs.
- App STT: ElevenLabs Scribe (`scribe_v2`, diarization, multilingual detection) — configuration recorded in `decisoes-tecnologia.md`.
- Resilience principle: a transcription/analysis failure never blocks watching the video.

## Architecture — thin-layer principle

All the core lives in pure Go packages, **with no Wails imports** — Wails only comes in as the
shell (window, bindings, events). This is non-negotiable: it keeps stepping back a framework
version cheap.

```
main.go             # Wails v3 app entry point (shell)
frontend/           # Svelte 5 (runes) — UI ported from the React prototype
internal/media/     # audio extraction (ffmpeg via os/exec)
internal/stt/       # Provider interface + ElevenLabs implementation (the only active one)
internal/analysis/  # LLM-based analysis (actually used in Phase 2)
internal/db/        # SQLite (modernc.org/sqlite, WAL) + goose migrations (embed.FS)
internal/jobs/      # table-backed queue + single worker (states, retry, idempotency)
internal/config/    # machine-local config (paths, keyring)
prompts/            # versioned prompts (analyze-v1.md, ...)
docs/               # decisoes-tecnologia.md, fase-1-mvp.md, fase-0-validacao.md
testdata/           # synthetic/anonymized fixtures
```

Future phases (do not implement now, but do not block on it in the schema/design): in-UI
analysis and active versioned prompts; tags + FTS5; pull-work-push sync between machines with a
snapshot via `VACUUM INTO` (never copy the database file with open connections) and a sha256
manifest.

## Stack and conventions

- Recent Go; prefer **stdlib**: `net/http` for APIs, `os/exec` for ffmpeg, `log/slog` for logging, `encoding/json`.
- External dependencies only with justification (approved ones are in `decisoes-tecnologia.md`).
- **Credentials:** via `zalando/go-keyring` (native OS storage) — never in plain text, never in the synced folder. Nothing hardcoded, nothing committed.
- **Portable SQL** in the repository layer: nothing driver-specific (switching modernc ↔ mattn should be just the import + `sql.Open`).
- **Database never inside the synced folder**; video paths in the database are always **relative** to the storage root — never absolute or machine-specific.
- **Frontend: Svelte 5 with runes, always.** Never use legacy Svelte 3/4 syntax (stores with `$:`, `export let`, etc.) — use `$state`, `$derived`, `$effect`, `$props`. If in doubt between the old and new pattern, stop and ask.
- Code and identifiers in English; documentation in English; user-facing error messages and analysis text in PT-BR.
- STT always with diarization + per-word timestamps + the provider's multilingual/code-switching configuration (document in the code the configuration used and why).
- In the analysis prompt: a PT/ES word in the student's speech is a resort to their native language (a vocabulary candidate), **not** an English mistake.

## Commands

```bash
wails3 dev                # app in dev mode (frontend hot reload)
wails3 build              # build the app
go test ./...             # tests (fixtures in testdata/)
go vet ./...              # before committing
```

## Commits

Commit messages must be a **single line**, in semantic format (`type: description`) — e.g.:
`feat: add audio extraction via ffmpeg`, `fix: correct Gladia timestamp parsing`,
`docs: record STT decision`. Usual types: `feat`, `fix`, `docs`, `refactor`, `test`, `chore`.

## Privacy

The repository is **public** (or may become so at any time — treat it as public from the
start). Recordings and transcripts are personal data belonging to the dev and to third parties
(tutors):

- Videos, audio, and **any JSON/transcript of a real lesson** never enter the repo. Keep working directories in `.gitignore` (e.g., `local/`, `*.mp4`, `*.wav`, raw provider outputs).
- Fixtures in `testdata/` must be **synthetic or anonymized**: invented conversations in the same response format as each provider, no real names, no excerpts from real lessons.
- Never mention tutor names, account IDs, or billing data in code, comments, commits, or documentation.
- API keys only in environment variables; verify that no committed `.env` example contains real values.
