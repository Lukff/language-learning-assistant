# Story 2 — Local Database and Machine Configuration: Design

> Spec for the second implementation slice of Phase 1 (`docs/phase-1-mvp.md`, Story 2).
> Goal: the app creates/opens its SQLite database, knows where the storage root (the user's synced
> folder) lives, and securely stores the ElevenLabs credential — all configured through a
> first-run wizard. No lesson import yet, no job queue actually running yet (those come in
> Stories 3 and 4); the `jobs`/`prompts` schema is born here because it's cheap and avoids a
> painful migration later, but it isn't used in this story.

## Context

Story 1 delivered the Wails v3 + Svelte 5 skeleton (sidebar, empty header, 3 placeholder screens),
with no business logic at all. This story introduces the project's first two pure Go packages
(`internal/db`, `internal/config`), following the thin-layer principle (`Agents.md`): neither of
them imports Wails. The bridge to the UI is a thin binding in the shell (`main.go` or its own
`app.go`) that calls these packages and uses Wails's `runtime.OpenDirectoryDialog` for the native
file picker.

Scope decisions settled during brainstorming:
- Config format: **JSON** (stdlib `encoding/json`, no new dependency).
- `lessons` is born already with a video hash column (dedupe), even though it's only used in
  Story 3 — avoids an extra migration later.
- Keyring failure (risk 3, Secret Service unavailable on Linux): **a clear error, with no
  fallback** to an environment variable or plaintext.
- The ElevenLabs API key is captured in the **same first-run wizard**, not on a separate Settings
  screen (that only arrives in Phase 5).
- The first-run wizard is a real UI (native dialog + validation + visual confirmation), not a
  placeholder — that's what the acceptance criterion calls for.

## Architecture and packages

```
internal/db/
  db.go              # Open(path) *sql.DB — PRAGMA journal_mode=WAL, runs goose migrations
  migrations/
    0001_initial.sql # lessons, transcripts, jobs, prompts
internal/config/
  paths.go           # AppDataDir() — resolves the app's data directory
  config.go          # AppConfig struct, Load/Save for config.json
  credentials.go     # SaveSTTAPIKey/GetSTTAPIKey over a secretStore interface (go-keyring)
```

Neither package imports `github.com/wailsapp/wails/v3` or anything under `internal/media`,
`internal/stt`, `internal/analysis` — they are an isolated foundation, used by the business
packages (jobs, import) in upcoming stories.

In the shell (Wails), a thin service exposed to the frontend (e.g., `SetupService` in `app.go`)
exposes the methods the UI needs: `IsFirstRun`, `ChooseStorageFolder` (opens
`runtime.OpenDirectoryDialog` + validates writability), `CompleteSetup(storageRoot, apiKey string)
error` (writes `config.json` and the credential, in that order — if the credential fails,
`config.json` isn't written, so the app keeps detecting "first run" and the user tries again).

## Paths and config

- **App data dir:** `os.UserConfigDir()/assistente-idiomas/` — on Linux `~/.config/...`, on
  Windows `%AppData%/...`. A single directory for `config.json` and `db/app.db`, avoiding the
  complexity of resolving separate XDG conventions for data vs. config (overkill for a personal
  app used on 2 machines). This also satisfies `CLAUDE.md`'s rule that the database must never
  live inside the synced folder — this directory never is.
- **`config.json`:**
  ```json
  { "storage_root": "/absolute/path/chosen/by/the/user" }
  ```
  An absolute path because it's machine-specific (unlike the video paths *inside* the database,
  which are relative to `storage_root` — a `CLAUDE.md` rule). Structured as a simple struct, open
  to future fields without a migration (it's loose JSON, not a database schema).
- **First-run detection:** `config.json` doesn't exist in `AppDataDir()`. There's no persisted
  intermediate state — if the user closes the app in the middle of the wizard, reopening it simply
  restarts the wizard from scratch.

## Schema v1 (`internal/db/migrations/0001_initial.sql`)

```sql
-- +goose Up
CREATE TABLE lessons (
    id INTEGER PRIMARY KEY,
    lesson_date TEXT NOT NULL,       -- ISO date (YYYY-MM-DD)
    tutor TEXT NOT NULL,
    video_path TEXT NOT NULL,        -- relative to storage_root
    video_hash TEXT,                 -- sha256; filled in Story 3 (import dedupe)
    duration_seconds INTEGER,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE transcripts (
    id INTEGER PRIMARY KEY,
    lesson_id INTEGER NOT NULL REFERENCES lessons(id),
    raw_json_path TEXT NOT NULL,     -- raw JSON from the provider, saved alongside the lesson in storage_root
    utterances TEXT NOT NULL,        -- JSON: diarized utterances + per-word timestamps
    created_at TEXT NOT NULL
);

CREATE TABLE prompts (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    version INTEGER NOT NULL,
    content TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE jobs (
    id INTEGER PRIMARY KEY,
    lesson_id INTEGER NOT NULL REFERENCES lessons(id),
    kind TEXT NOT NULL,              -- extract_audio | transcribe | analyze
    status TEXT NOT NULL DEFAULT 'pending', -- pending | running | done | error
    attempts INTEGER NOT NULL DEFAULT 0,
    last_error TEXT,
    payload TEXT,                    -- JSON
    prompt_id INTEGER REFERENCES prompts(id),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

-- +goose Down
DROP TABLE jobs;
DROP TABLE prompts;
DROP TABLE transcripts;
DROP TABLE lessons;
```

`utterances` stays as loose JSON right in the `transcripts` row (not normalized into a words
table) — normalizing to enable FTS5 is a future-phase migration; it's not worth the complexity
now. This migration's SQL is portable across drivers (`modernc.org/sqlite` or
`mattn/go-sqlite3`), per `CLAUDE.md`'s portable-SQL discipline.

`internal/db.Open(path string) (*sql.DB, error)`: opens the connection, runs `PRAGMA
journal_mode=WAL;`, applies pending migrations via `goose` with the migrations' `embed.FS`.
Creates the database file's parent directory if it doesn't exist.

## Keyring

`internal/config/credentials.go` defines:

```go
type secretStore interface {
    Set(service, user, secret string) error
    Get(service, user string) (string, error)
}
```

implemented by a thin wrapper over `github.com/zalando/go-keyring` (which already satisfies this
shape). `SaveSTTAPIKey` / `GetSTTAPIKey` use `service = "assistente-idiomas"`, `user =
"elevenlabs"`. The interface exists only to allow a fake in unit tests, without depending on the
Secret Service running in the environment where `go test` runs.

Real failure (Secret Service unavailable on Linux — risk 3 of the plan): the call returns an
error, which bubbles up to the wizard UI as a clear message ("Não foi possível acessar o
gerenciador de credenciais do sistema. Verifique se o gnome-keyring/kwallet está rodando e tente
novamente."). No fallback to an environment variable or plaintext — a conscious decision, unlike
`cmd/spike`, which accepts an env var because it's a dev tool, not the final app.

## First-run flow (UI)

A 2-step wizard in Svelte (new component, e.g. `frontend/src/lib/SetupWizard.svelte`), rendered in
place of the normal shell (sidebar + screens) when `IsFirstRun()` returns `true`:

1. **Choose folder:** a button triggers `ChooseStorageFolder()` (native dialog via
   `runtime.OpenDirectoryDialog`). The binding validates writability by trying to create and
   remove a temporary file in the chosen folder; on failure, it returns an error explaining why
   (folder doesn't exist / no write permission). Success shows the chosen path as visual
   confirmation and enables step 2.
2. **ElevenLabs API key:** a password-type text field + a save button, which calls
   `CompleteSetup(storageRoot, apiKey)`. This method, in order: writes the credential via keyring
   → if OK, writes `config.json` with `storage_root`. If writing the credential fails, the error
   bubbles up to the UI (message from the previous paragraph) and `config.json` isn't touched —
   the app keeps detecting first-run and the user can try again without losing the already-chosen
   path (kept in the Svelte component's local state, not persisted).

After `CompleteSetup` succeeds, the app reloads to the normal shell (sidebar +
Library/Progress/Queue from Story 1).

## Data flow

```
App starts → binding calls IsFirstRun()
  false → opens the database (internal/db.Open at the AppDataDir path), proceeds to the normal shell
  true  → renders SetupWizard
            → ChooseStorageFolder() (native dialog + write validation)
            → CompleteSetup(storageRoot, apiKey)
                → keyring.Set(...)
                → config.Save({storage_root: ...})
              success → opens the database, normal shell
              error   → message in the UI, wizard continues
```

## Error handling

- Chosen folder doesn't exist or lacks write permission: error explained in step 1, the user picks
  another folder.
- Keyring unavailable: error explained in step 2 (see the Keyring section above), with no loss of
  the already-validated path.
- Failure to open/migrate the database (corrupted file, AppDataDir permission): a fatal error with
  a clear message in the window — there's no way for the app to function without a database, so
  blocking here is acceptable (unlike the transcription/analysis resilience principle, which is
  about *network/API* failure, not local infrastructure).

## Tests

- `internal/db`: migrations apply cleanly to a database in `t.TempDir()`; a round-trip test
  (insert/select) on each of the 4 tables; a test that reopening an already-migrated database
  doesn't fail (idempotence of `goose up`).
- `internal/config`: a `config.json` round-trip in a temporary directory (via
  `t.Setenv("XDG_CONFIG_HOME", ...)` on Linux, or injecting the base dir into the paths function);
  `SaveSTTAPIKey`/`GetSTTAPIKey` tested with a fake `secretStore` (without touching the OS's real
  keyring). Testing the real keyring is left for manual verification on Windows and Linux (risk 3,
  outside `go test`).
- Manual verification: run `wails3 dev` through the full wizard (choosing a real folder, typing an
  ElevenLabs API key) on at least one machine; visually confirm that reopening the app skips the
  wizard.

## Out of scope for this story

Lesson import (drag-and-drop, video copy, effective hash-based dedupe), a job queue actually
running (worker, states, retry), a complete Settings screen (provider selection), any real data in
`lessons`/`transcripts`/`jobs`/`prompts` beyond the empty schema. All of that is Story 3 and 4
onward (`docs/phase-1-mvp.md`).

## Acceptance criteria (from `docs/phase-1-mvp.md`, Story 2)

- [ ] SQLite (`modernc.org/sqlite`, WAL) created in the OS's data directory — outside the synced
      folder.
- [ ] Embedded `goose` migrations (`embed.FS`); schema v1: `lessons`, `transcripts`, `jobs`,
      `prompts` (the latter two already in the format defined in `technology-decisions.md`, even
      without full use in this phase).
- [ ] Local config (storage root path) in the OS's configuration directory; first run asks for the
      folder with visual validation.
- [ ] STT provider credentials written/read via `go-keyring`; never in plaintext. Tested on
      Windows and Linux (risk 3).
