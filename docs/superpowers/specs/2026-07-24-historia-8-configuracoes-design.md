# Story 8 — Basic Settings (path + credential): design

> Covers the full Story 8 (`docs/fase-1-mvp.md`). Deliberately minimal scope: only what covers the
> known gap logged in Story 2's progress notes (keyring credential lost/cleared with no recovery
> UI) and the need to repoint the storage root when the user moves the folder on their own (new
> disk, reorganization). STT/LLM provider selection, cost estimation, and the rest of the full
> Settings screen remain out of scope — Phase 5.

## Context and motivation

Two known gaps from previous stories:

1. **Credential:** if the ElevenLabs credential is lost/cleared from the keyring after the
   first-run wizard (Story 2), the app doesn't detect this on its own and there was no UI to
   re-register it — only deleting `config.json` and redoing the whole wizard.
2. **Storage folder:** `config.json` stores an absolute, machine-specific path (`storage_root`).
   If the user moves this folder manually (outside the app), there's currently no way to repoint
   it without hand-editing the JSON.

This story introduces a Settings screen that resolves both, reusing as much as possible of what
already exists (the wizard's folder dialog, Story 3's hash-based scan, `config.SaveSTTAPIKey`) —
no new business logic beyond the "missing video" check.

## Scope decisions

- **Changing the folder doesn't move or copy files.** The user has already moved the folder
  manually outside the app; the app just records the new `storage_root` and reconciles whatever
  it can.
- **Changing the folder is never blocked.** Even if videos aren't found in the new folder, the
  change is accepted — missing ones are just flagged, never preventing the user from moving
  forward.
- **Hash-based reconciliation reuses Story 3's scan** (`importer.Scan`): a file found at a
  different path (same hash) automatically has its `video_path` updated — this covers the case of
  videos renamed during the folder move. Accepted and intended side effect: new videos found in
  the new folder (with no matching lesson) also become pending candidates, the same behavior as
  "Sync folder".
- **"Missing video" is always recalculated, never persisted.** Same pattern as the
  processing/ready/error status (`internal/db/lesson_status.go`, `deriveStatus`) — derived on
  every read, never a separately stored column. Self-healing: if the file reappears at the
  expected path (for example, another "Sync folder" run resolved it by hash, or the user put the
  file back manually), the warning disappears on its own the next time the Library loads.
- **The credential is never displayed** — only a boolean status ("configured" / "not configured");
  the (re-)registration field is always available, never pre-filled with anything.
- **Settings is its own screen** (a route, not a modal), accessed via a gear icon in the `Header`
  — it doesn't appear in the Sidebar's navigation list (it isn't used day-to-day like
  Library/Queue).
- **The post-folder-change scan summary appears as static text on the Settings screen itself**
  (the same `new/updated/skipped/errors` format the Library already uses for "Sync folder") — no
  toast, no new Wails event (it's a single screen, no need to notify other tabs).

## Architecture

```
services/
  settings.go            # NEW: SettingsService
  storage_folder.go       # NEW: shared folder-picker + write-validation helper
  setup.go                 # ChooseStorageFolder now delegates to the shared helper
  library.go               # ListLessons/GetLesson gain VideoMissing (existence check)
  import.go                # dbRepo (unexported) is reused by SettingsService, same package
main.go                    # registers SettingsService; also passes storageRoot to LibraryService
frontend/src/lib/
  Header.svelte            # gear icon
  screens/Settings.svelte  # NEW
  screens/Library.svelte   # "missing video" badge per lesson
App.svelte                 # new { screen: "settings" } route
```

No new package under `internal/` — `internal/importer.Scan` and `internal/config` already cover
everything the business logic needs; `services/` just orchestrates.

### `services/storage_folder.go` (new, extracted from `setup.go`)

```go
// chooseStorageFolder opens the native folder-picker dialog and validates
// that it's writable. Returns an empty path (no error) if the user cancels.
// Shared by SetupService (wizard) and SettingsService (folder change).
func chooseStorageFolder(title string) (string, error)

func isDirWritable(dir string) error // moved from setup.go, no behavior change
```

`SetupService.ChooseStorageFolder` now calls `chooseStorageFolder("Escolha a pasta onde as
aulas ficarão guardadas")`; `SettingsService.ChooseStorageFolder` calls the same function with a
slightly different title ("Escolha a nova pasta — os arquivos já devem estar lá dentro").

### `services/settings.go` (new)

```go
// SettingsService covers the Settings screen (Story 8): viewing/changing the
// storage root and (re-)registering the STT provider credential.
type SettingsService struct {
    conn        *sql.DB
    storageRoot func() (string, error)
}

func NewSettingsService(conn *sql.DB, storageRoot func() (string, error)) *SettingsService

// GetStorageRoot returns the currently configured storage_root.
func (s *SettingsService) GetStorageRoot() (string, error)

// ChooseStorageFolder opens the native dialog (same write validation as the
// wizard) and returns the chosen path, without saving anything yet.
func (s *SettingsService) ChooseStorageFolder() (string, error)

// ChangeStorageFolder saves newRoot to config.json (always, even if the
// scan that follows runs into problems) and runs the same hash-based
// reconciliation as Story 3. Never blocks the change. ScanSummary is the
// same type already defined in import.go (services.ScanSummary) — same
// package, reused without duplicating the summary format.
func (s *SettingsService) ChangeStorageFolder(newRoot string) (ScanSummary, error)

// HasSTTCredential reports whether a credential is stored in the keyring,
// without revealing its value. false (no error) if simply not configured
// yet (keyring.ErrNotFound); error only on an actual keyring access
// failure.
func (s *SettingsService) HasSTTCredential() (bool, error)

// SaveSTTAPIKey saves/overwrites the credential — delegates straight to
// config.SaveSTTAPIKey.
func (s *SettingsService) SaveSTTAPIKey(apiKey string) error
```

`ChangeStorageFolder`:

```go
func (s *SettingsService) ChangeStorageFolder(newRoot string) (ScanSummary, error) {
    if newRoot == "" {
        return ScanSummary{}, fmt.Errorf("pasta de armazenamento não pode ser vazia")
    }
    if err := isDirWritable(newRoot); err != nil {
        return ScanSummary{}, err
    }
    if err := config.Save(&config.AppConfig{StorageRoot: newRoot}); err != nil {
        return ScanSummary{}, fmt.Errorf("gravar configuração: %w", err)
    }
    sum, err := importer.Scan(newRoot, &dbRepo{conn: s.conn})
    if err != nil {
        // storage_root has already been changed at this point — a
        // deliberate decision (see "Error handling"): reconciliation is
        // best-effort, and the folder change itself shouldn't be rolled
        // back because of a scan failure.
        return ScanSummary{}, err
    }
    return ScanSummary{New: sum.New, Updated: sum.Updated, Skipped: sum.Skipped, Errors: sum.Errors}, nil
}
```

`HasSTTCredential`:

```go
func (s *SettingsService) HasSTTCredential() (bool, error) {
    _, err := config.GetSTTAPIKey()
    if err == nil {
        return true, nil
    }
    if errors.Is(err, keyring.ErrNotFound) {
        return false, nil
    }
    return false, fmt.Errorf("não foi possível acessar o gerenciador de credenciais do sistema (verifique se o gnome-keyring/kwallet está rodando): %w", err)
}
```

`config.GetSTTAPIKey` already wraps the `keyring` error with `fmt.Errorf("...: %w", err)` — since
it already uses `%w`, `errors.Is(err, keyring.ErrNotFound)` keeps working through the wrap.

### `services/library.go` (extended)

```go
type LibraryService struct {
    conn        *sql.DB
    storageRoot func() (string, error) // NEW — same resolver the worker/middleware already use
}

func NewLibraryService(conn *sql.DB, storageRoot func() (string, error)) *LibraryService

type Lesson struct {
    // ... existing fields ...
    VideoMissing bool `json:"videoMissing"` // NEW
}
```

`ListLessons`/`GetLesson` now call an internal helper:

```go
// videoMissing reports whether a lesson's video file cannot be found in
// the current storage_root. Any os.Stat error (not just "doesn't exist")
// is treated as missing — resilience: this should never break the
// Library, and it's not worth distinguishing "missing" from "no
// permission" in this slice.
func (s *LibraryService) videoMissing(videoPath string) bool {
    root, err := s.storageRoot()
    if err != nil {
        return true
    }
    _, err = os.Stat(filepath.Join(root, filepath.FromSlash(videoPath)))
    return err != nil
}
```

### `main.go`

```go
importService := services.NewImportService(conn)
libraryService := services.NewLibraryService(conn, storageRoot) // storageRoot already exists (closure over config.Load)

Services: []application.Service{
    application.NewService(services.NewSetupService()),
    application.NewService(importService),
    application.NewService(libraryService),
    application.NewService(services.NewQueueService(conn)),
    application.NewService(services.NewSettingsService(conn, storageRoot)), // NEW
},
```

## Frontend

### `Header.svelte`

A gear icon (⚙) aligned to the right of the existing header; `onclick` calls an `onOpenSettings:
() => void` prop (the same callback pattern `Sidebar` already uses with `onNavigate`).

### `App.svelte`

```ts
type Route =
  | { screen: "library" }
  | { screen: "lesson-detail"; lessonId: number }
  | { screen: "progress" }
  | { screen: "queue" }
  | { screen: "settings" }; // NEW
```

`<Header onOpenSettings={() => (route = { screen: "settings" })} />`; `Sidebar` still receives
only `route.screen`, mapped to `"library"` when the route is `"lesson-detail"` or `"settings"`
(the same treatment already applied to `"lesson-detail"`), since neither is a nav item.

### `Settings.svelte` (new)

Two sections, in the same visual style as the other screens (`theme.ts`, cards with
`colors.surface2`):

1. **Storage**
   - Text showing the current `storage_root` (`SettingsService.GetStorageRoot`, loaded on
     `onMount`).
   - "Change folder" button → `ChooseStorageFolder()` → if the path isn't empty, calls
     `ChangeStorageFolder(path)`; the button shows a loading state while the promise is pending
     (the scan can take a few seconds, the same order of magnitude documented in Story 3:
     ~1-3s per video).
   - The result (success or error) is shown as static text below the button: success shows
     `N new, M updated, S skipped, E errors`; an error shows the message.

2. **STT provider credential**
   - Status loaded on `onMount` via `HasSTTCredential()`: "Credential configured" or "No
     credential configured" (or an error message if the call fails).
   - A `<input type="password">` field plus a "Save" button, always visible and empty (never
     pre-filled); on a successful save, updates the status to "Credential configured" and clears
     the field; an error shows inline, the same pattern as step 2 of `SetupWizard`.

### `Library.svelte` / `LessonDetail.svelte`

A lesson with `videoMissing: true` gets a short badge ("video not found in the current folder"),
visually in the same family as the "error" warning (alert color), but as an indicator independent
of the pipeline `status` — a `ready` lesson (transcription ok) can have `videoMissing: true` at
the same time, and the badge shows up in both cases without interfering with the existing
`status` text.

## Data flow

**Changing the folder:**

```
Settings.svelte: click "Change folder"
  → SettingsService.ChooseStorageFolder() (native dialog + write validation)
  → user confirms (path not empty)
  → SettingsService.ChangeStorageFolder(newFolder)
        → isDirWritable(newFolder)
        → config.Save({storage_root: newFolder})        // always saved, even if the scan below fails
        → importer.Scan(newFolder, &dbRepo{conn})
              → known hash, new path    → db.UpdateLessonPath (renamed/moved)
              → unknown hash            → becomes pending_import (same flow as always)
              → stat matches (unchanged) → skipped
        ← ScanSummary { new, updated, skipped, errors }
  ← static text on screen with the summary
```

**Credential:**

```
Settings.svelte: onMount → HasSTTCredential() → shows status
Settings.svelte: click "Save" → SaveSTTAPIKey(apiKey) → config.SaveSTTAPIKey (keyring)
```

**Missing video (recalculated on every Library load, independent of Settings):**

```
Library.svelte: onMount/refresh → LibraryService.ListLessons(filter)
  → db.ListLessonsWithStatus(...)                        // unchanged
  → for each lesson: os.Stat(storageRoot + "/" + video_path)
        exists     → VideoMissing = false
        not found  → VideoMissing = true
  ← []Lesson (with VideoMissing)
```

No new Wails event — everything is a direct request/response, the same pattern as the other
services; there aren't multiple screens/tabs open at once that would need to be notified.

## Error handling

- **Dialog cancelled** (`ChooseStorageFolder` returns empty): `Settings.svelte` doesn't call
  `ChangeStorageFolder`, no error.
- **Chosen folder without write permission**: `isDirWritable` rejects it before saving
  `storage_root` — "folder without write permission".
- **Error writing `config.json`**: `ChangeStorageFolder` returns an error, the old `storage_root`
  keeps being used (the `config.Save` that failed never replaces the file).
- **Error during the scan** (`importer.Scan` returns `err != nil`, e.g. `root` becomes
  inaccessible mid-scan): at this point `storage_root` **has already been changed** — a
  deliberate decision, since the folder itself was already validated as writable beforehand; the
  reconciliation is best-effort and a failure in it shouldn't prevent the user from having
  already pointed to the right folder. The error shows up in the screen's static text.
- **Per-file errors during the scan** (unreadable file, permissions): don't abort — this is
  already `importer.Scan`'s behavior (counted in `Errors`, continues with the rest); they show up
  in the summary.
- **Failure saving the credential to the keyring** (Secret Service unavailable on Linux, risk 3):
  same message already used in the wizard (`services/setup.go`).
- **Failure reading the credential status** (`HasSTTCredential`): distinguishes "not configured"
  (`keyring.ErrNotFound`, not an error) from an actual access failure (error message, same as the
  wizard).
- **`os.Stat` on the video fails for a reason other than "doesn't exist"** (e.g. permission):
  also treated as `VideoMissing = true` — resilience; not worth distinguishing the reason in this
  slice.
- **Resilience principle upheld:** nothing in this story prevents watching a lesson whose video
  is actually present; `VideoMissing` is purely informational, it never blocks the Detail view.

## Out of scope for this story

- Automatically moving or copying files when changing folders.
- STT/LLM provider selection, cost estimation, or any other configuration field besides
  `storage_root` and the credential (Phase 5, `docs/fase-1-mvp.md`).
- Validating `raw_json_path` (raw transcripts) against the new folder — only `video_path` is
  checked; losing the raw JSON doesn't prevent watching the lesson or affect the transcription
  already persisted in `transcripts.utterances`.
- Editing `storage_root` by typing the path manually (only via the native dialog, same pattern as
  the wizard) — avoids hand-typed invalid paths.
- Cancelling a scan in progress.

## Tests

**`services/settings_test.go`** (new, same pattern as the rest of `services/`):
- `ChangeStorageFolder`: database in `t.TempDir()` + two temp folders simulating an
  already-moved source/destination; a lesson with an existing hash appearing at a different path
  in the new folder gets its `video_path` updated; `config.Load()` reflects the new folder after
  the call.
- `ChangeStorageFolder` with a folder without write permission: returns an error, the old
  `storage_root` is preserved (config isn't touched).
- `HasSTTCredential`: fake `secretStore` (same pattern as `credentials_test.go`) covering the
  three cases — configured, not configured (`ErrNotFound`), real access error.
- `SaveSTTAPIKey`: a thin test confirming it delegates to `config.SaveSTTAPIKey`.

**`services/library_test.go`** (extended):
- `ListLessons`/`GetLesson` with a test `storageRoot` (`t.TempDir()`): a lesson whose
  `video_path` exists on disk (an empty file is enough for `os.Stat`) → `VideoMissing: false`; a
  lesson whose file doesn't exist → `VideoMissing: true`.

**`internal/importer`**: no new tests — `Scan` is already covered by Story 3's suite and its
behavior doesn't change, it's just called from one more place (`SettingsService`, in addition to
`ImportService.ScanFolder`).

**Manual verification** (same pattern logged in previous stories, pending a real window on
Windows/Linux): open Settings via the Header icon; actually change the storage folder by moving a
video under a different name and confirm the Library reflects the new `video_path`; delete a video
from disk and confirm the Library shows "missing video" without breaking the screen; re-register
the credential and confirm the status changes to "configured".

## Acceptance criteria (from `docs/fase-1-mvp.md`, Story 8)

- [ ] Settings screen reachable via a Header icon, shows the configured storage root and allows
      changing it (native dialog + write validation), without moving files — the user has already
      moved them manually. The change is never blocked by videos that aren't found.
- [ ] Changing the folder runs the same hash-based reconciliation as Story 3: videos with a
      different name in the new folder automatically have their `video_path` updated; videos
      whose hash isn't found in the new folder are flagged as "missing video" in the Library
      (recalculated on every load, never a persisted column) until the file shows up again at the
      expected path.
- [ ] A field to (re-)register the STT provider credential (ElevenLabs) via `go-keyring`, reusing
      `config.SaveSTTAPIKey`; the screen shows whether a credential is already configured (without
      revealing its value) — covers the known gap from Story 2.
- [ ] No provider selection, cost estimation, or any other configuration option — that's Phase 5.
