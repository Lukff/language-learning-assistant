# Language Learning Assistant

A **personal** desktop app (one user, one developer, no server of its own) that archives recordings of
English lessons from Cambly and turns them into something you can study from: a diarized transcript
(student × tutor) synchronized with the video, plus on-demand LLM analysis (corrections, topics).

Lessons are in English with occasional code-switching (PT/ES). The UI and analyses are in English.

## Features

- **Import** lesson videos by scanning your storage folder or by drag-and-drop. Files are identified by
  SHA-256 (never by folder convention), deduplicated, and renamed in place to
  `YYYY-MM-DD_HHHMM_tutor-slug.mp4`.
- **Background pipeline**: a single worker extracts audio (ffmpeg) and transcribes it with
  ElevenLabs Scribe (`scribe_v2`, diarization, word-level timestamps, multilingual detection).
  Jobs are stored in SQLite with retry/backoff, idempotency, and recovery of jobs interrupted by a crash.
- **Library** with status (processing / ready / error), and filters by teacher, period, and topic.
- **Lesson detail**: video with a synchronized transcript. Click a line to seek, the current line follows
  playback, and you can mark which speaker is you.
- **Queue** screen with live progress, readable errors, and reprocess.
- **LLM analysis (on demand, DeepSeek `deepseek-v4-flash`)**:
  - *Student corrections*, shown inline in the transcript.
  - *Lesson topics*, editable chips that are also usable as a Library filter.
- **Teachers and topics** management (rename, delete), and lesson editing (date, time, teacher).
- **Settings**: repoint the storage folder (hash-based reconciliation, no file moves) and re-register API credentials.

> **Resilience principle:** a transcription or analysis failure never blocks watching the video.

## Tech stack

| Area | Choice |
|------|--------|
| Shell | [Wails v3](https://v3alpha.wails.io/) (alpha, version pinned in `go.mod`) |
| Frontend | Svelte 5 (runes only) + TypeScript + Vite, managed with pnpm |
| Core | Pure Go packages under `internal/`, with no Wails imports (thin-layer principle) |
| Database | SQLite via `modernc.org/sqlite` (pure Go, WAL) + `pressly/goose` embedded migrations |
| STT | ElevenLabs Scribe (`scribe_v2`) |
| LLM | DeepSeek via an OpenAI-compatible API |
| Audio | ffmpeg / ffprobe via `os/exec` |
| Secrets | OS keyring via `zalando/go-keyring` |

Rationale for each choice: [`docs/technology-decisions.md`](docs/technology-decisions.md).

## Prerequisites

- **Go** 1.25.7 or newer
- **Node.js** and **pnpm** 9 (the frontend is built with pnpm)
- **[Task](https://taskfile.dev)** (the Wails v3 build system)
- **Wails v3 CLI**, at the pinned version. It is not a `go.mod` dependency, so install it manually:
  ```bash
  go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-alpha2.117
  ```
- **ffmpeg** and **ffprobe** on your `PATH`
- API keys:
  - [ElevenLabs](https://elevenlabs.io/) (required, for transcription)
  - [DeepSeek](https://platform.deepseek.com/) (optional, only for corrections and topics analysis)
- **Linux only**: cgo, GTK4 and WebKitGTK 6 development headers (`libgtk-4-dev`, `libwebkitgtk-6.0-dev`),
  and a Secret Service provider (gnome-keyring or KWallet) for the credential store.
- **Windows only**: WebView2 runtime (bundled with Windows 11). Local builds may trigger Defender
  false positives on Go binaries, so consider an exclusion for the app folder.

## Running the app

```bash
# 1. Install frontend dependencies (this also sets git hooksPath to .githooks)
cd frontend && pnpm install && cd ..

# 2. Run in development mode (hot reload)
wails3 dev            # or: task dev

# 3. Or build a production binary (output goes to bin/)
wails3 build          # or: task build
```

If you change exported Go service methods, regenerate the frontend bindings (the `-i` flag matters, since
the frontend expects interfaces rather than classes):

```bash
wails3 generate bindings -ts -i ./...
```

### First run

1. A setup wizard asks for the **storage folder**, the directory that holds (or will hold) your lesson videos.
2. It also asks for your **ElevenLabs API key**, stored in the OS keyring.
3. The folder is scanned automatically. Review each candidate video, confirming date, time, and teacher, and
   its extraction and transcription jobs are queued.
4. To use analysis, add a DeepSeek key in **Settings** (gear icon in the header).

To add more lessons later, drop a video onto the Library screen or use **Sync folder**.

### Where data lives

| What | Where |
|------|-------|
| Config and SQLite database | `%AppData%\assistente-idiomas\` on Windows, `~/.config/assistente-idiomas/` on Linux (`config.json`, `db/app.db`, audio cache) |
| Lesson videos | Your chosen storage folder. The database only stores paths **relative** to it. |
| API keys | OS keyring, service name `assistente-idiomas` |

The database is deliberately kept outside the storage folder, so the folder can be synced (e.g. Google Drive)
without risking database corruption.

## Development

```bash
go test ./...      # unit tests (fixtures in testdata/ are synthetic)
go vet ./...
cd frontend && pnpm run check   # svelte-check
```

`frontend/bindings/` is gitignored and generated by `wails3 dev` / `wails3 build`. On a fresh clone,
`pnpm run check` and `pnpm run build` fail with "Cannot find module '.../bindings/...'" until you generate
them once with `wails3 generate bindings -ts -i ./...` (run from the repo root).

`main.go` embeds `frontend/dist`, so `go vet ./...` and `go test ./...` fail with "pattern all:frontend/dist:
no matching files found" until the frontend has been built once (`wails3 build` does this).

Tests that touch the app config must call `configtest.IsolateConfigDir(t)` (`internal/config/configtest`)
instead of setting `XDG_CONFIG_HOME` directly: that variable is ignored on Windows, so the tests would
otherwise read and overwrite your real `%APPDATA%\assistente-idiomas`. A few permission-based tests are
skipped on Windows, where `os.Chmod` doesn't make directories read-only. Windows App Control (Smart App
Control) may block freshly compiled test executables; pointing `GOTMPDIR` at a dedicated folder helps
in some setups. On Linux, anything that imports Wails needs `CGO_ENABLED=1` and the GTK4/WebKitGTK headers listed above.

Commit messages are a **single line** in semantic format (`feat: ...`, `fix: ...`, `docs: ...`). A
`commit-msg` hook in `.githooks/` rejects `Co-Authored-By` trailers.

### Project layout

```
main.go              Wails v3 entry point (window, services, events)
services/            Wails-bound services (import, library, queue, settings, analysis, teachers, topics, video server)
frontend/            Svelte 5 UI
internal/media/      audio extraction and duration (ffmpeg/ffprobe)
internal/stt/        STT provider interface + ElevenLabs implementation
internal/analysis/   LLM analysis tasks and parsing
internal/db/         SQLite access + goose migrations
internal/jobs/       table-backed queue + single worker
internal/importer/   folder scan, hashing, standardized naming, copy
internal/config/     paths, config.json, keyring credentials
prompts/             versioned analysis prompts (embedded)
docs/                decisions, phase plans, specs, notes
testdata/            synthetic fixtures
```

## Project status

| Phase | Scope | Status |
|-------|-------|--------|
| 0 | API validation (STT and LLM providers) | Done |
| 1 | MVP: import, transcribe, watch (Stories 1-9) | **Done** (in daily use) |
| 2 | LLM analysis in the UI | **In progress** (iterative, per task) |
| 3 | Automatic tags, full-text search (FTS5), Progress screen | Planned |
| 4 | Multi-machine sync (pull-work-push via `VACUUM INTO`) and backups | Planned |
| 5 | Full Settings (provider selection, cost estimation, deeper analysis) | Planned |

**Phase 2 details.** Rather than building every analysis at once, each task goes through its own cycle:
on demand, then UI, then observe on real lessons, then decide keep/refine/discard.

- Implemented: **student corrections** and **lesson topics**, plus topic filtering in the Library.
- Paused by decision (20/08/2026): vocabulary, tutor expressions, tutor taught terms, tutor feedback, and
  tutor corrections. Their prompts and code exist but are not exposed in the UI. Current work is refining
  the two implemented tasks.
- Recent: full switch of UI and prompts from PT-BR to English (29/08/2026), and video is served from a
  loopback HTTP server to fix playback on Linux/WebKitGTK (28/08/2026).

**Known limitations**
- The Progress screen is not built yet (placeholder file kept, removed from navigation).
- Analysis runs only on demand; there is no background analysis job yet.
- Only `.mp4` files are recognized on import.
- Wails v3 is still alpha, so upgrading is a deliberate task, never mid-feature.
- The STT and LLM provider choices were validated on a single sample lesson each.

## Privacy

Recordings and transcripts are personal data (yours and your tutors'). Videos, audio, and any real
transcript or provider output stay out of the repo (see `.gitignore`), fixtures are synthetic, and API keys
live only in the OS keyring, never in plain text or the synced folder.

## Documentation

- [`docs/technology-decisions.md`](docs/technology-decisions.md): current technology choices
- [`docs/phase-1-mvp.md`](docs/phase-1-mvp.md): Phase 1 stories and progress log
- [`docs/phase-2-llm-analysis.md`](docs/phase-2-llm-analysis.md): Phase 2 plan and progress log
- [`docs/phase-0-validation.md`](docs/phase-0-validation.md): API validation history
- [`docs/llm-analysis-notes.md`](docs/llm-analysis-notes.md), [`docs/stt-notes.md`](docs/stt-notes.md), [`docs/backlog.md`](docs/backlog.md)
