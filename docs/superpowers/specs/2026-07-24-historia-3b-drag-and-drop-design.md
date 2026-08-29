# Story 3b — Manually importing a lesson (drag-and-drop): design

> Covers the full Story 3b (`docs/fase-1-mvp.md`) and resolves the project's **technical risk 2**
> ("File drag-and-drop in Wails v3: confirm the native DnD API of the pinned version — an
> unstable alpha area"). Reuses the Story 3 confirmation flow
> (`ImportConfirmModal.svelte`, `ImportService.ConfirmImport`, `pending_imports`) unchanged — see
> `docs/superpowers/specs/2026-07-22-historia-3-importar-aula-design.md`.

## Context and motivation

The folder scan (Story 3) covers the case "the storage root already has loose lessons in it".
What's missing is the case "a new lesson just arrived (a Cambly download) and the user wants to
register it without waiting for the next scan" — dragging the file into the app.

## Technical risk 2: resolved

Wails v3 `v3.0.0-alpha2.117` (the pinned version) has native support for file drag-and-drop,
implemented on all three platforms (`webview_window_darwin.go`, `webview_window_linux.go`,
`webview_window_windows.go`) — **it is not the HTML5 File API**, it's an OS-level drop delivered
to the window:

- `application.WebviewWindowOptions{EnableFileDrop: true}` enables the feature on the window.
- `win.OnWindowEvent(events.Common.WindowFilesDropped, func(e *application.WindowEvent) {...})`
  receives `e.Context().DroppedFiles() []string` — absolute paths on disk, ready for
  `os.Open`/`os.Stat`, with no payload size limit (unlike reading bytes via `<input
  type="file">` in a webview).
- The drop is only recognized if the cursor releases over an HTML element marked with the
  `data-file-drop-target` attribute — the runtime injected by Wails already handles the visual
  feedback (adding the `file-drop-target-active` class to that element during drag hover, with
  no code of our own).

The file-dialog fallback (deemed acceptable in the risk document) **is not needed** — the native
API works on all three platforms in the pinned version.

## Scope decisions

- **Opening the modal:** dropping the video opens `ImportConfirmModal` immediately (it doesn't
  just sit in the pending list waiting for a click on "Review").
- **Drop area:** the whole Library screen (`Library.svelte`), not a dedicated zone nor the app's
  entire window.
- **Multiple files:** all are processed; each becomes a pending candidate, and the confirmation
  modal opens one at a time, in sequence.
- **Copy destination folder:** straight into the storage root, no subfolder — the same principle
  as Story 3 of not imposing a directory structure.
- **Source already inside the storage root:** doesn't copy; registers it in place (the same
  treatment the scan already gives an existing file).
- **Name collision at the destination:** an automatic suffix (`name-2.mp4`, `name-3.mp4`, ...),
  without asking the user anything — the same pattern already used for the post-confirmation
  rename
  (`docs/superpowers/specs/2026-07-23-historia-3-renomeacao-padronizada-design.md`).
- **Unrecognized extension or file already imported (same hash):** rejected with a clear error
  message, without copying or hashing needlessly.

## Architecture

```
main.go                    # EnableFileDrop: true; a thin handler forwards DroppedFiles() to the service
services/import.go         # ImportService.DropImport(paths []string) []DropResult — new method
internal/importer/
  importer.go              # dedupe/hash reused (LessonByHash, PendingExists)
  copy.go                  # new: CopyIntoStorageRoot — copy + collision resolution, no DnD/Wails
frontend/src/lib/
  screens/Library.svelte   # data-file-drop-target at the root; subscribes to "import:dropped"; a
                            # PendingImport queue to open the modal one at a time
  ImportConfirmModal.svelte # unchanged — already accepts any PendingImport
```

`internal/importer/copy.go` doesn't know about Wails or the database — it only does file I/O
(copy + collision suffix), the same thin-layer principle as the rest of `internal/`.
`ImportService` (in `services/`, which already knows about `storage_root`, the database, and
Wails events) orchestrates: decides whether to copy or not, calls the hash, inserts into
`pending_imports`, emits the event.

### `main.go`

```go
win := app.Window.NewWithOptions(application.WebviewWindowOptions{
    // ... existing options ...
    EnableFileDrop: true,
})
win.OnWindowEvent(events.Common.WindowFilesDropped, func(e *application.WindowEvent) {
    results := importService.DropImport(e.Context().DroppedFiles())
    // DropImport already emits an event per successful candidate; results (per-file errors)
    // has no synchronous consumer here — see "Errors" below.
})
```

### `ImportService.DropImport`

```go
// DropResult is the outcome of processing a single path received from the drop —
// exposed to the frontend via an event to show a per-file error without blocking
// the others.
type DropResult struct {
    Path  string `json:"path"`  // original dropped path (absolute, only to identify it in the message)
    Error string `json:"error"` // empty if it succeeded
}

func (s *ImportService) DropImport(paths []string) []DropResult
```

For each `path` in `paths`, in the order received:

1. **Extension:** if not `.mp4` (same list as `internal/importer.videoExtensions`) → `DropResult`
   with error "tipo de arquivo não suportado (só .mp4)", move on to the next.
2. **Hash** (`sha256`, the same helper `importer.Scan` uses — extracted so it can be reused
   without duplicating code).
3. **Dedup:**
   - a `lesson` with that hash already exists → error "esta aula já foi importada".
   - a `pending_imports` row with that hash already exists → error "esta aula já está aguardando
     revisão".
4. **Placement:**
   - if `path` resolves (via `filepath.Abs` + prefix comparison, accounting for symlinks via
     `filepath.EvalSymlinks` on both sides) to inside `storage_root` → uses the path relative to
     `storage_root` directly, without copying.
   - otherwise → `importer.CopyIntoStorageRoot(path, storageRoot)`: copies the content
     (`io.Copy` into a temp file at the destination + atomic rename, doesn't write directly to
     the final name — this avoids a candidate ending up pointing at a partially-copied file if
     the copy fails midway), resolving name collisions with a `-2`, `-3`, ... suffix before the
     extension; returns the resulting relative path. A copy failure (disk full, permission) →
     `DropResult` with the error, **does not** insert into `pending_imports` (unlike the
     best-effort post-confirmation rename — here the copy is the only way the file exists in a
     trackable form, so a failure has to be visible).
5. **`suggestedDate`** via the same `suggestDate(filepath.Base(path), mtime)` already used by the
   scan.
6. **Inserts into `pending_imports`** (same table, same shape the scanner's `InsertPending`
   writes). If the insert fails after a successful copy (a database error, rare), the copied
   file is left orphaned in `storage_root` with no record — it is not rolled back (the copy is
   already best-effort with respect to rollback). It's not a permanent state: the next "Sync
   folder" (scan, Story 3) picks it up as a new candidate, since it's inside the storage root
   with no known hash. `DropResult` reports the insert's error normally.
7. **Emits a Wails event** `import:dropped` with the newly-created
   `PendingImport{id, path, suggestedDate}` (the same shape `ListPendingImports` already exposes
   — the frontend doesn't need a new type).

### Errors: how they reach the frontend

`DropImport` runs inside `OnWindowEvent`'s synchronous handler, with no direct call from the
frontend (the drop is initiated by the OS, not a click) — there's no promise on the frontend
waiting for the return value. That's why per-file errors also go out via an event, not through
the `DropResult` return value, which is used only internally/in tests: `DropImport` emits an
additional `import:drop-error` event per `DropResult` with `Error != ""`, with `{path, error}`.
`Library.svelte` subscribes to both events.

## Frontend: `Library.svelte`

- The component root gains `data-file-drop-target`; local CSS for the `.file-drop-target-active`
  state uses the already-imported theme colors (`colors.blue`/dashed border, consistent with the
  rest of the UI — not the Wails example's green marker).
- `onMount` (alongside the existing `loadAll`) subscribes to:
  - `Events.On("import:dropped", (pending) => { pendingQueue.push(pending); maybeOpenNext(); loadPending(); })`
  - `Events.On("import:drop-error", ({path, error}) => { dropErrors = [...dropErrors, \`${path}: ${error}\`]; })`
- A local queue (`let pendingQueue: PendingImport[] = $state([])`) drains one item at a time into
  the same `reviewing` state the Library already uses to open `ImportConfirmModal` — if
  `reviewing` is already occupied (the user is reviewing another candidate), the new item waits
  in the queue; on close/confirm, `maybeOpenNext()` picks up the next one. This covers "multiple
  files: process all, open confirmation one at a time" without duplicating the modal-display
  logic.
- Drop errors (`dropErrors`) appear in the same warning style already used by
  `error`/`syncMessage`, listed (there can be more than one problem file in the same drop).

## Out of scope for this slice

- A dedicated drop zone with visual instructions ("drop here") — the whole screen already
  reacts.
- Working outside the Library screen (Queue, Progress, Detail) — an explicit decision, not the
  whole window.
- Extensions beyond `.mp4` — same limitation as Story 3, no change here.
- Cancelling a copy in progress — lesson files are minutes long, not hours; there's no progress
  bar or cancellation in this slice.

## Tests

- `internal/importer/copy_test.go`: copy to an empty destination; name collision (`-2`,
  `-3` suffix); nonexistent source (clear error); a copy interrupted midway (simulated) doesn't
  leave a partial file under the final name (only the temp file remains, or nothing).
- `services/import_test.go` (`DropImport`):
  - non-`.mp4` extension → `DropResult` with an error, nothing in the database, nothing on disk.
  - hash already exists as a `lesson` → "already imported" error, no copy.
  - hash already exists as `pending_imports` → "already awaiting review" error, no copy.
  - path already inside `storage_root` → no copy (same content, same `os.SameFile`), only writes
    `pending_imports` with the correct relative path.
  - path outside `storage_root` → copied, `pending_imports` points to the new relative path,
    the original file remains intact at the source.
  - two paths in the same `DropImport` call with equal base names but different hashes → the
    second is written with a `-2` suffix.
  - simulated copy failure (e.g. a destination with no write permission) → `DropResult` with an
    error, nothing in `pending_imports`.

## Acceptance criteria (Story 3b, `docs/fase-1-mvp.md`)

- [ ] Native drag-and-drop (Wails v3, `EnableFileDrop` + `WindowFilesDropped`) on the Library
      screen opens `ImportConfirmModal` (date pre-filled from the file's name/mtime, tutor as
      free text) — one modal per dropped file, in sequence if more than one is dropped together.
- [ ] The video is copied to the storage root (no subfolder, collision suffix if needed) when
      it isn't already there; if it's already inside the storage root, it's registered in place
      without copying. The database stores only the relative path.
- [ ] Registration in `pending_imports` reuses the existing hash-based confirmation/dedup
      (Story 3) — confirming writes `lessons` + `extract_audio`/`transcribe` jobs as
      `pending`, with no change to the existing `ConfirmImport`.
- [ ] An unrecognized extension or an already-imported file (same hash, whether as a `lesson` or
      as `pending_imports`) is rejected with a clear error message, without copying or
      duplicating.
