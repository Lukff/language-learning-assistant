# Story 3 — Standardized filename on confirmation: design

> Additional slice on top of Story 3 (`docs/fase-1-mvp.md`), on the confirmation flow
> (`ImportService.ConfirmImport`) already implemented and described in
> `docs/superpowers/specs/2026-07-22-historia-3-importar-aula-design.md`. Does not reopen what was
> already delivered in that slice (scanning, staging, dedupe) — it only adds a new step after
> confirmation, and a new validation before it.

## Context and motivation

Today, `ConfirmPendingImport` writes `video_path` exactly as the file was found by the scan —
the original name from the Cambly download (or whatever name the file already had). This leaves
the storage root with inconsistent names across files, making it harder to locate a specific
file outside the app (Explorer/Finder, manual backup, etc.).

Decision: after the user fills in the metadata (date, time, tutor) in the confirmation modal, the
video file is renamed *in place* (same folder) to a standardized name derived from that metadata.

**Coupled consequence:** for the name to be reliably derivable, time stops being optional — date,
time, and tutor all become **mandatory** to confirm a candidate. A candidate missing any of these
stays in `pending_imports` (pending), and confirmation is refused with a clear error.

## Filename format

```
YYYY-MM-DD_HHHMM_tutor-slug.ext
```

Example: tutor "Maria José", 2026-07-23 14:30 → `2026-07-23_14H30_maria-jose.mp4`.

- Hour separator: `H` between hour and minute (`14H30`), not `:` (invalid in a filename on
  Windows) nor `-` (ambiguous with the date separator itself).
- Separator between date and time: `_`.
- Extension: preserved from the original file, lowercased.
- **No "no time" fallback**: the validation in `ConfirmImport` (see below) guarantees that
  `lessonDate` always arrives here with both date **and** time. There is no longer a confirmation
  candidate without a time.

### Tutor slug

- Lowercase.
- Accents removed via a manual substitution table (á→a, ã→a, ç→c, etc. — covers PT/ES, the
  code-switching languages of the project); no new dependency (`CLAUDE.md`: external dependency
  only with justification, and this is solvable in stdlib with a small table).
- Any run of characters outside `[a-z0-9]` (spaces, punctuation, etc.) becomes a single `-`; no
  `-` at the ends.
- An empty tutor never reaches here — it's already validated as mandatory in `ConfirmImport`
  (pre-existing validation, kept as-is).

### Name collision

Two lessons confirmed at the same instant (minute) with the same tutor would produce the same
target name (rare, but possible). If the target name already exists in the directory **and it is
not the file being renamed itself**, it tries suffixes `-2`, `-3`, ... before the extension
(`..._maria-jose-2.mp4`) until it finds a free name.

## Mandatory validation in `ConfirmImport`

`services/import.go`'s `ConfirmImport(id, lessonDate, tutor)` already validates `lessonDate != ""`
and `tutor != ""`. It now also validates that `lessonDate` has a time component — it rejects a
plain `"YYYY-MM-DD"` (without `T...`), not just an empty string. Clear error message in PT-BR
(e.g. `"horário da aula é obrigatório"`).

Since `db.ConfirmPendingImport` is only called after this validation passes, a failure here means
the transaction (insert into `lessons` + jobs, delete from `pending_imports`) **never runs** —
the candidate remains intact in `pending_imports`, i.e. it stays pending review. This is already
the function's natural behavior today for the two existing fields; the change is just extending
the "not empty" check to "not empty and has a time", in the same place.

The modal's `datetime-local` input (`ImportConfirmModal.svelte`) is already atomic — there's no
way to submit just the date without a time through that UI — so this backend validation is
mainly defense in depth (other future callers, malformed data) and an explicit error message,
not an observable behavior change in the current UI.

## Architecture

```
internal/importer/
  naming.go        # StandardFilename(lessonDate, tutor, ext string) string — pure, no I/O
  naming_test.go
services/
  import.go         # ConfirmImport: time validation + call to the new best-effort step
  import_test.go
```

`internal/importer.StandardFilename` does no I/O (it doesn't know about directories or disk
collisions) — it only computes the name from the three values. Resolving collisions requires
checking the filesystem, so that lives in `services/import.go`, which is already the layer that
knows about `storage_root`.

### `StandardFilename`

```go
// internal/importer/naming.go
package importer

// StandardFilename derives the standardized filename after confirmation, from
// the date/time and tutor provided in the confirmation modal.
// lessonDate is the raw value from <input type="datetime-local">
// ("YYYY-MM-DDTHH:MM"); ConfirmImport guarantees this format before calling
// this function — there is no fallback here for a date without a time.
func StandardFilename(lessonDate, tutor, ext string) string
```

### Best-effort step in `services/import.go`

Same pattern already established by `setDurationBestEffort` (called right after it, inside
`ConfirmImport`, once `db.ConfirmPendingImport` has succeeded):

1. Load the lesson (`db.FindLessonByID`) — it would already have been loaded by
   `setDurationBestEffort`; can reuse the same read instead of two.
2. Resolve `storage_root` (`config.Load()`, already used in this file).
3. Compute the target name via `importer.StandardFilename(lesson.LessonDate, lesson.Tutor,
   filepath.Ext(lesson.VideoPath))`.
4. Resolve the video's current directory (`filepath.Dir` of the relative path) — the rename is
   always within that same folder, never moves across directories.
5. Resolve collision: if a file already exists at the target path and it isn't the current file,
   try suffixes until a free name is found.
6. If the target name (after resolving collision) already equals the current name, do nothing
   (idempotent — avoids an unnecessary rename if the function runs again over an already
   standardized lesson).
7. `os.Rename(currentAbsolutePath, targetAbsolutePath)`.
8. Re-`os.Stat` on the new path to get the current size/mtime (doesn't assume rename preserves
   mtime on every OS/filesystem).
9. `db.UpdateLessonPath(conn, lessonID, newRelativePath, size, mtime)` — an already-existing
   function (created in the original Story 3 for the "file just moved" case), reused without a
   signature change.

Any error in any of these steps (permission denied, I/O, unresolvable collision after N attempts)
is only logged (`slog.Warn`, with `lesson_id` and the error) — **never** propagated as a
`ConfirmImport` error. The lesson has already been successfully confirmed in the transaction; the
filename is cosmetic, not critical to the app's operation (resilience principle from
`CLAUDE.md`). If the rename fails, the video keeps its original name, fully functional.

### Downstream jobs (`internal/jobs/worker.go`)

No change needed. `runExtractAudio`/`runTranscribe` resolve `lesson.VideoPath` from the database
at run time (they reread the lesson on every job, they don't cache the path) — if the rename has
already run before the worker picks up the jobs (the common case, freshly-created `pending`
jobs), they already use the new name automatically. Even if the worker somehow ran before the
best-effort rename finished (it shouldn't, it's synchronous inside `ConfirmImport`, but
hypothetically), the worst case is the worker reading the old path in a race — harmless, because
the path is read fresh on every job transition, not just once.

## Out of scope for this slice

- Moving the file into a subfolder structure (`aulas/YYYY/YYYY-MM-DD/`) — a directory-structure
  decision that only applies once Story 3b (drag-and-drop) is designed; renaming here is always
  *in place*, same folder.
- Retroactively renaming lessons already confirmed before this change — no real lesson has been
  confirmed in real use yet.
- Changes to Story 3b itself — once it's designed, it reuses
  `internal/importer.StandardFilename` unchanged.

## Tests

- `internal/importer/naming_test.go` (table-driven): tutor with accents (PT/ES), tutor with
  multiple spaces/punctuation, uppercase extension normalized, full-format example
  (`2026-07-23T14:30` + `"Maria José"` + `.mp4` → `2026-07-23_14H30_maria-jose.mp4`).
- `services/import_test.go`:
  - `ConfirmImport` with a date-only `lessonDate` (no time) returns an error; the candidate stays
    in `pending_imports` (doesn't become a lesson, no job created).
  - Successful rename: file on disk (`t.TempDir()`) renamed, `lessons.video_path` updated,
    `file_size`/`file_mtime` consistent with the file at the new path.
  - Collision: create a pre-existing file with the target name before confirming; assert that the
    result uses the `-2` suffix.
  - Simulated rename failure (e.g. the target path is an existing directory, causing an
    `os.Rename` error) doesn't break `ConfirmImport` — the lesson stays confirmed, with the
    original `video_path` intact.

## Acceptance criterion (new, added to Story 3 in `docs/fase-1-mvp.md`)

- [ ] After confirmation (date/time/tutor in the modal), the video file is renamed *in
      place* to `YYYY-MM-DD_HHHMM_tutor-slug.ext`; a rename failure doesn't prevent confirmation
      (best-effort, logged). Time becomes mandatory at confirmation — a candidate missing
      date, time, or tutor stays pending.
