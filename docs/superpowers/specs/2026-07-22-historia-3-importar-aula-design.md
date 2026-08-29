# Story 3 — Import lesson (scanning the existing folder): design

> Spec for the third implementation slice of Phase 1 (`docs/fase-1-mvp.md`, Story 3).
> Goal: the storage folder chosen in the wizard likely **already** has videos from previous
> lessons, loose, with no subfolder convention at all. The app needs to find them, let the
> user review and confirm each one (date + tutor) at their own pace, and register them as real
> `lessons` with the processing jobs pending.
>
> **Out of scope for this slice** (brainstorming decision, 2026-07-22): drag-and-drop of new
> files. The app already covers the most urgent case (old lessons already in the folder); manual
> import via drag-and-drop becomes a separate story later, reusing the same confirmation modal
> this story creates.

## Context

Story 2 delivered the database (`internal/db`), config (`internal/config`), and the first-run
wizard that saves `storage_root`. No real rows exist yet in `lessons`/`jobs`. This story
introduces the project's first pure business package (`internal/importer`) and the first
repository layer over `internal/db` (which until now only had `Open`).

Scope decisions closed during brainstorming:
- **No assumed folder structure.** Identification and dedup are always by **filename +
  SHA-256**, never by path/subfolder convention — the scan is recursive over the whole folder.
- **Extension recognized in this slice: `.mp4`** only (the most common format from
  Cambly/browser). An easy list to extend later (`.mov`, `.mkv`, `.webm`) without changing the
  logic.
- **SHA-256 is not a bottleneck:** measured in the sandbox, ~1-3s per typical lesson video even
  without hardware acceleration (Go's `crypto/sha256` uses SHA-NI on amd64 when available, which
  is even faster). Even so, a **stat-cache** (path+size+mtime) avoids rereading the entire
  content of already-registered files on every repeated sync.
- **Review at the user's own pace**, not a queue of modals blocking the sync: the scan writes
  candidates into a staging table (`pending_imports`); the Library reads that table and shows
  "N lessons awaiting review", and the user confirms them one at a time whenever they want.
- **No "Ignore" button** — an explicit user decision: the folder should only ever contain actual
  lessons, so every video found is a candidate for confirmation, with no "dismissed" state in the
  schema.
- A file that's already a known `lesson` but changed name/folder (same hash, different path) is
  treated as a **path update**, not a new candidate nor a duplicate.

## Architecture and packages

```
internal/importer/
  importer.go        # Scan(root string, repo Repo) (Summary, error) — pure, no Wails
  importer_test.go
internal/db/
  lessons.go          # FindLessonByHash, FindLessonByPath, UpdateLessonPath, InsertLesson
  pending_imports.go  # InsertPendingImport, ListPendingImports, ConfirmPendingImport, ...
  migrations/
    00002_pending_imports.sql
services/
  import.go           # ImportService: Wails shell over internal/importer + internal/db
  import_test.go
frontend/src/lib/
  screens/Library.svelte        # "N lessons awaiting review" section + "Sync folder" button
  ImportConfirmModal.svelte     # confirmation modal (date/tutor) — new, reusable
```

`internal/importer` does not import Wails nor `internal/db` directly — it receives a `Repo`
interface (implemented by `internal/db`'s functions) so it stays testable with an in-memory fake,
with no need for a real SQLite in the scan tests.

```go
// internal/importer/importer.go
type Repo interface {
    FindLessonByPathStat(path string, size int64, mtime time.Time) (found bool, hash string, err error)
    FindLessonByHash(hash string) (lessonID int64, path string, found bool, err error)
    UpdateLessonPath(lessonID int64, path string, size int64, mtime time.Time) error
    FindPendingByHash(hash string) (found bool, err error)
    InsertPending(c Candidate) error
}
```

## Schema (migration `00002_pending_imports.sql`)

```sql
-- +goose Up
ALTER TABLE lessons ADD COLUMN file_size INTEGER;
ALTER TABLE lessons ADD COLUMN file_mtime TEXT;
CREATE UNIQUE INDEX idx_lessons_video_hash ON lessons(video_hash) WHERE video_hash IS NOT NULL;

CREATE TABLE pending_imports (
    id INTEGER PRIMARY KEY,
    path TEXT NOT NULL,
    file_size INTEGER NOT NULL,
    file_mtime TEXT NOT NULL,
    sha256 TEXT NOT NULL UNIQUE,
    suggested_date TEXT,
    created_at TEXT NOT NULL
);

-- +goose Down
DROP TABLE pending_imports;
DROP INDEX idx_lessons_video_hash;
ALTER TABLE lessons DROP COLUMN file_mtime;
ALTER TABLE lessons DROP COLUMN file_size;
```

`file_size`/`file_mtime` on `lessons` exist only for the stat-cache (avoiding a rehash of an
unchanged file); `video_hash` already existed since migration 00001, and gains here the unique
index that turns "duplicate import" into a database guarantee, not just application logic.

## Scan flow (`internal/importer.Scan`)

Walks `storage_root` recursively (`filepath.WalkDir`), filtering by the `.mp4` extension. For
each file:

1. `os.Stat` → do path + size + mtime match an already-registered `lesson`?
   **Yes** → skip, it's already a known lesson and nothing changed (short-circuit, without
   reading the content).
2. Otherwise, compute the SHA-256 of the content.
   - Hash matches an existing `lesson`, different path → **the file just moved/was renamed**:
     `UpdateLessonPath` (updates `video_path`, `file_size`, `file_mtime`), without becoming a
     candidate.
   - Hash is already in `pending_imports` → skip (already in the review queue from a previous
     scan).
   - New hash → `InsertPending`, with `suggested_date` extracted from the filename (common date
     regex, e.g. `2026-07-15` or `15-07-2026`) or, failing that, from the `mtime`.

`Scan` returns a summary (`Summary{New, Updated, Skipped, Errors int}`) — failure to read/hash a
specific file (permission, I/O) does not abort the scan: it's counted in `Errors` and the loop
continues. This is the resilience principle from `CLAUDE.md` applied to the scan: a failure on
one file never prevents finding the others.

## Confirmation (staging → real lesson)

`ConfirmPendingImport(id, lessonDate, tutor string) (lessonID int64, err error)` in
`internal/db/pending_imports.go`, in a single transaction:
1. Reads the `pending_imports` row by `id`.
2. `INSERT INTO lessons (lesson_date, tutor, video_path, video_hash, file_size, file_mtime,
   created_at, updated_at)`.
3. `INSERT INTO jobs (lesson_id, kind, status)` twice: `extract_audio` and `transcribe`, both
   `pending`.
4. `DELETE FROM pending_imports WHERE id = ?`.

If any step fails, the transaction rolls everything back — the candidate remains intact in
`pending_imports`, and the user can try again.

## Wails service (`services/import.go`)

```go
type ImportService struct{ db *sql.DB }

func (s *ImportService) ScanFolder() (ScanSummaryDTO, error)
func (s *ImportService) ListPendingImports() ([]PendingImportDTO, error)
func (s *ImportService) ConfirmImport(id int64, lessonDate string, tutor string) error
```

`ScanFolder` reads `storage_root` from `config.Load()`, calls `internal/importer.Scan`. It is
called:
- Automatically at the end of the first-run wizard (after `CompleteSetup` succeeds), with a
  visual step "Looking for lessons in the folder…" before the wizard closes.
- On demand, via the **"Sync folder"** button in the Library.

## Data flow

```
ScanFolder() → config.Load() (storage_root) → importer.Scan(root, repo)
  → writes/updates pending_imports and lessons (moved paths)
  → returns Summary → UI shows a toast ("N new, M updated, E errors")

Library.svelte (mount / after ScanFolder) → ListPendingImports() → "awaiting review" list
  click on an item → ImportConfirmModal (suggested date pre-filled, free-text tutor)
    confirm → ConfirmImport(id, date, tutor) → transaction (lessons + jobs, deletes pending)
      success → item disappears from the pending list
      error    → message in the modal, candidate stays pending
```

## Error handling

- Error reading/hashing a specific file during the scan: does not abort the rest, goes into the
  summary's `Errors` counter (shown to the user, not silent).
- `config.Load()` failing inside `ScanFolder` (shouldn't happen post first-run, but defensive):
  clear error, scan doesn't run, nothing breaks — the user is already in the normal app.
- `ConfirmImport` failing (violation of the `video_hash` unique index, for example a rare race
  between two syncs): error explained in the modal, candidate remains in `pending_imports`.

## Tests

- `internal/importer`: fixtures in a `t.TempDir()` with synthetic `.mp4` files (arbitrary
  content, doesn't need to be a real video — only the hash matters). Cases: a new file becomes a
  candidate; an already-known hash is skipped; the stat-cache avoids rehashing (fake `Repo`
  counts calls); the same hash at a different path updates instead of duplicating; an error on
  one file doesn't interrupt the rest.
- `internal/db`: round-trip of `pending_imports` (insert/list/delete); `ConfirmPendingImport`
  creates `lessons` + 2 `jobs` in a transaction; the `video_hash` unique index rejects a
  duplicate.
- `services/import_test.go`: error wrapping, following the `setup_test.go` pattern.
- Manual verification: `wails3 dev` with a test folder containing a few loose `.mp4` files (no
  structure) — run the wizard, watch the scan find the files, confirm one, watch it disappear
  from the pending list.

## Out of scope for this story

Drag-and-drop manual import (future story, reuses `ImportConfirmModal.svelte`); an "Ignore"
button for a candidate (decision: the folder should only contain lessons); copying the video
into an `aulas/AAAA/AAAA-MM-DD/` structure (only applies to the drag-and-drop flow, not the
scan); a job queue actually running (worker, Story 4) — the jobs are only created as `pending`,
nobody processes them yet; a real listing on the Library screen beyond the pending section
(Story 5).

## Acceptance criteria (from `docs/fase-1-mvp.md`, Story 3)

- [ ] Recursive scan of the storage folder, without assuming a subfolder structure; identifies
      `.mp4` files and computes SHA-256 for each.
- [ ] Stat-cache (path+size+mtime) avoids recomputing the hash of videos already registered and
      unchanged.
- [ ] Already-registered videos (same hash) are skipped; the same hash at a different path
      updates the lesson's path instead of duplicating.
- [ ] New videos appear as pending review in the Library; confirmation (date/tutor modal) writes
      the `lesson` + `extract_audio`/`transcribe` jobs as `pending`.
- [ ] The scan runs automatically at the end of the first-run wizard and on demand via
      "Sync folder".
- [ ] Duplicate import (same hash) is detected and not duplicated — guaranteed by a unique index
      in the database.
</content>
