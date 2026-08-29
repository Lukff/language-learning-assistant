# Story 8 — Basic Settings (path + credential) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a Settings screen (icon in the Header) where the user views/changes the storage root
(without moving files — hash-based reconciliation reusing Story 3's scan) and (re-)registers the
STT provider credential in the keyring; the Library starts flagging "missing video" per lesson,
recalculated on every load.

**Architecture:** No new package under `internal/`. A new `SettingsService` (in `services/`, same
pattern as `SetupService`) orchestrates `internal/config` (credential + `storage_root`) and reuses
`internal/importer.Scan` (already used by `ImportService.ScanFolder`) to reconcile renamed files
in the new folder. `LibraryService` gains a `storage_root` resolver and starts checking
(`os.Stat`) whether each lesson's video exists on every listing — never persisted, same pattern as
the processing/ready/error status. Frontend: a new `Settings.svelte` screen (its own route,
reached via an icon in the `Header`).

**Tech Stack:** Go (stdlib + `zalando/go-keyring`, `modernc.org/sqlite`), Wails v3
`v3.0.0-alpha2.117`, Svelte 5 (runes), TypeScript.

## Global Constraints

- Svelte 5 with runes always (`$state`/`$derived`/`$effect`/`$props`) — never legacy Svelte 3/4
  syntax.
- Go: prefer stdlib; no new dependency without justification (none is needed for this story).
- Credentials via `go-keyring`, never plain text, never committed.
- Portable SQL in the repository layer — nothing driver-specific.
- Video paths in the database always relative to `storage_root`, never absolute.
- Commits: single line, semantic format (`type: description`).
- `go vet ./...` clean and `go test ./...` passing before every commit that touches Go.
- Wails v3 pinned at `v3.0.0-alpha2.117` (`go.mod`) — no upgrade in this task.
- Frontend bindings generated via `wails3 generate bindings -ts -i ./...` (the `-i` flag is
  mandatory — without it the generator produces classes, not interfaces, a format this frontend
  doesn't use) — never hand-edited, never committed (`frontend/bindings` is in `.gitignore`).
- Code/identifiers in English; UI text and user-facing error messages in PT-BR.
- Resilience principle: nothing in this story may prevent watching a lesson whose video is
  actually present.

---

### Task 1: Extract shared folder-picker helper

**Files:**
- Create: `services/storage_folder.go`
- Create: `services/storage_folder_test.go`
- Modify: `services/setup.go`
- Modify: `services/setup_test.go`

**Interfaces:**
- Produces: `chooseStorageFolder(title string) (string, error)` and `isDirWritable(dir string) error`
  — unexported functions of the `services` package, used by `SetupService` (already existing) and
  by `SettingsService` (Task 2).
- Consumes: nothing new — `github.com/wailsapp/wails/v3/pkg/application` (already a project
  dependency).

This is a mechanical refactor (moving code, no behavior change): `ChooseStorageFolder` from the
first-run wizard and the write-validation logic move from `setup.go` into a shared file, since
Story 8 needs the same dialog+validation on the Settings screen.

- [ ] **Step 1: Create `services/storage_folder.go`**

```go
package services

import (
	"fmt"
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// chooseStorageFolder opens the native folder-choice dialog with the given
// title and validates that it's writable. Returns an empty path (no error)
// if the user cancels the dialog. Shared by SetupService (first-run
// wizard) and SettingsService (folder change, Story 8).
func chooseStorageFolder(title string) (string, error) {
	dir, err := application.Get().Dialog.OpenFile().
		SetTitle(title).
		CanChooseFiles(false).
		CanChooseDirectories(true).
		CanCreateDirectories(true).
		PromptForSingleSelection()
	if err != nil {
		return "", fmt.Errorf("abrir diálogo de pasta: %w", err)
	}
	if dir == "" {
		return "", nil
	}
	if err := isDirWritable(dir); err != nil {
		return "", err
	}
	return dir, nil
}

// isDirWritable confirms that dir accepts writes, by creating and removing
// a temporary file in it.
func isDirWritable(dir string) error {
	f, err := os.CreateTemp(dir, ".assistente-idiomas-write-test-*")
	if err != nil {
		return fmt.Errorf("pasta sem permissão de escrita: %w", err)
	}
	name := f.Name()
	f.Close()
	if err := os.Remove(name); err != nil {
		return fmt.Errorf("não foi possível limpar arquivo de teste na pasta: %w", err)
	}
	return nil
}
```

- [ ] **Step 2: Create `services/storage_folder_test.go`** (tests moved from `setup_test.go`)

```go
package services

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsDirWritable_WritableDirReturnsNil(t *testing.T) {
	if err := isDirWritable(t.TempDir()); err != nil {
		t.Errorf("isDirWritable() erro inesperado num dir gravável: %v", err)
	}
}

func TestIsDirWritable_ReadOnlyDirReturnsError(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("não foi possível preparar dir somente-leitura: %v", err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })

	if err := isDirWritable(dir); err == nil {
		t.Error("isDirWritable() esperava erro num dir somente-leitura, veio nil")
	}
}

func TestIsDirWritable_MissingDirReturnsError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nao-existe")
	if err := isDirWritable(missing); err == nil {
		t.Error("isDirWritable() esperava erro num dir inexistente, veio nil")
	}
}

func TestIsDirWritable_LeavesNoTempFileBehind(t *testing.T) {
	dir := t.TempDir()
	if err := isDirWritable(dir); err != nil {
		t.Fatalf("isDirWritable() erro inesperado: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir() erro inesperado: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("esperava dir vazio após isDirWritable, achou %d entradas", len(entries))
	}
}
```

- [ ] **Step 3: Update `services/setup.go`** — remove `isDirWritable` and the dialog body of
      `ChooseStorageFolder`, delegating to the helper. Final file contents:

```go
// Package services contains the services exposed to the frontend via
// Wails v3 bindings — the shell that connects internal/config and
// internal/db to the UI. Unlike internal/, this package imports Wails on
// purpose.
package services

import (
	"fmt"

	"assistente-idiomas/internal/config"
)

// SetupService covers the first-run wizard: choosing the storage folder
// and registering the ElevenLabs API key.
type SetupService struct{}

func NewSetupService() *SetupService {
	return &SetupService{}
}

// IsFirstRun reports whether the app doesn't have config.json saved yet
// (no complete configuration so far). Any error loading the config
// (missing file, corrupted, no permission) is treated as "not configured
// yet" — the worst case is the user redoing the wizard, not data loss:
// CompleteSetup just overwrites config.json and the credential.
func (s *SetupService) IsFirstRun() bool {
	_, err := config.Load()
	return err != nil
}

// ChooseStorageFolder opens the native folder-choice dialog and validates
// that it's writable. Returns an empty path (no error) if the user cancels
// the dialog.
func (s *SetupService) ChooseStorageFolder() (string, error) {
	return chooseStorageFolder("Escolha a pasta onde as aulas ficarão guardadas")
}

// CompleteSetup saves the ElevenLabs credential (keyring) and, only if
// that succeeds, saves storageRoot in config.json. In that order: if the
// credential fails, config.json isn't touched and the app keeps detecting
// first-run.
func (s *SetupService) CompleteSetup(storageRoot string, apiKey string) error {
	if storageRoot == "" {
		return fmt.Errorf("pasta de armazenamento não pode ser vazia")
	}
	if err := config.SaveSTTAPIKey(apiKey); err != nil {
		return fmt.Errorf("não foi possível acessar o gerenciador de credenciais do sistema (verifique se o gnome-keyring/kwallet está rodando): %w", err)
	}
	if err := config.Save(&config.AppConfig{StorageRoot: storageRoot}); err != nil {
		return fmt.Errorf("gravar configuração: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Update `services/setup_test.go`** — remove the `isDirWritable` tests (moved to
      Step 2), keeping only the `CompleteSetup` ones. Final file contents:

```go
package services

import (
	"errors"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestCompleteSetup_KeyringUnavailableReturnsActionableError(t *testing.T) {
	sentinel := errors.New("secret service indisponível")
	keyring.MockInitWithError(sentinel)
	t.Cleanup(keyring.MockInit)

	svc := NewSetupService()
	err := svc.CompleteSetup("/some/path", "sk-test")
	if err == nil {
		t.Fatal("esperava erro, veio nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("erro não envolve o erro original do keyring: %v", err)
	}
	if !strings.Contains(err.Error(), "gnome-keyring") {
		t.Errorf("erro não menciona gnome-keyring/kwallet: %v", err)
	}
}

func TestCompleteSetup_EmptyStorageRootRejected(t *testing.T) {
	svc := NewSetupService()
	err := svc.CompleteSetup("", "sk-test")
	if err == nil {
		t.Fatal("esperava erro para storageRoot vazio, veio nil")
	}
}
```

- [ ] **Step 5: Run the tests**

Run: `go test ./services/... -v`
Expected: PASS on all tests (the four `TestIsDirWritable_*` now in
`storage_folder_test.go`, the two `TestCompleteSetup_*` in `setup_test.go`).

- [ ] **Step 6: `go vet`**

Run: `go vet ./...`
Expected: no output (clean).

- [ ] **Step 7: Commit**

```bash
git add services/storage_folder.go services/storage_folder_test.go services/setup.go services/setup_test.go
git commit -m "refactor: extract shared storage folder picker helper"
```

---

### Task 2: `SettingsService` — storage

**Files:**
- Create: `services/settings.go`
- Create: `services/settings_test.go`

**Interfaces:**
- Consumes: `chooseStorageFolder`/`isDirWritable` (Task 1); `dbRepo` (unexported, already defined
  in `services/import.go`); `importer.Scan(root string, repo importer.Repo) (importer.Summary, error)`
  (`internal/importer`); `config.Load`/`config.Save`/`config.AppConfig` (`internal/config`);
  `ScanSummary` (already defined in `services/import.go`).
- Produces: `SettingsService` (`conn *sql.DB`, `storageRoot func() (string, error)`),
  `NewSettingsService(conn *sql.DB, storageRoot func() (string, error)) *SettingsService`,
  `(*SettingsService).GetStorageRoot() (string, error)`,
  `(*SettingsService).ChooseStorageFolder() (string, error)`,
  `(*SettingsService).ChangeStorageFolder(newRoot string) (ScanSummary, error)` — used by
  Task 5 (wiring in `main.go`) and by the frontend (Task 6).

- [ ] **Step 1: Write the tests (will fail — `SettingsService` doesn't exist yet)**

Create `services/settings_test.go`:

```go
package services

import (
	"os"
	"path/filepath"
	"testing"

	"assistente-idiomas/internal/config"
	"assistente-idiomas/internal/db"
	"assistente-idiomas/internal/importer"
)

// configStorageRoot resolves storage_root from config.Load() — the same
// closure that main.go builds for the real worker/middleware/services.
func configStorageRoot() (string, error) {
	cfg, err := config.Load()
	if err != nil {
		return "", err
	}
	return cfg.StorageRoot, nil
}

func TestSettingsService_ChangeStorageFolder_UpdatesConfigAndReconcilesRenamedVideo(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	oldRoot := t.TempDir()
	if err := config.Save(&config.AppConfig{StorageRoot: oldRoot}); err != nil {
		t.Fatalf("config.Save() falhou: %v", err)
	}

	// The new folder already has the video, but under a different name —
	// simulates the user having renamed the file outside the app before
	// changing the folder here.
	newRoot := t.TempDir()
	renamedPath := filepath.Join(newRoot, "aula-renomeada.mp4")
	if err := os.WriteFile(renamedPath, []byte("conteudo-fake-do-video"), 0o644); err != nil {
		t.Fatalf("escrever vídeo de fixture falhou: %v", err)
	}
	hash, err := importer.HashFile(renamedPath)
	if err != nil {
		t.Fatalf("HashFile() falhou: %v", err)
	}

	lessonID := mustInsertLesson(t, conn, "2026-07-20", "Sarah M.", "aula-antiga.mp4")
	if _, err := conn.Exec(`UPDATE lessons SET video_hash = ? WHERE id = ?`, hash, lessonID); err != nil {
		t.Fatalf("gravar video_hash de fixture falhou: %v", err)
	}

	svc := NewSettingsService(conn, configStorageRoot)
	sum, err := svc.ChangeStorageFolder(newRoot)
	if err != nil {
		t.Fatalf("ChangeStorageFolder() erro inesperado: %v", err)
	}
	if sum.Updated != 1 {
		t.Errorf("ChangeStorageFolder() sum = %+v, esperado 1 lesson atualizada por hash", sum)
	}

	got, err := svc.GetStorageRoot()
	if err != nil {
		t.Fatalf("GetStorageRoot() erro inesperado: %v", err)
	}
	if got != newRoot {
		t.Errorf("GetStorageRoot() = %q, esperado %q", got, newRoot)
	}

	lesson, err := db.FindLessonByID(conn, lessonID)
	if err != nil {
		t.Fatalf("FindLessonByID() erro inesperado: %v", err)
	}
	if lesson.VideoPath != "aula-renomeada.mp4" {
		t.Errorf("lesson.VideoPath = %q, esperado video_path atualizado pro nome novo", lesson.VideoPath)
	}
}

func TestSettingsService_ChangeStorageFolder_ReadOnlyDirRejectedConfigUnchanged(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	oldRoot := t.TempDir()
	if err := config.Save(&config.AppConfig{StorageRoot: oldRoot}); err != nil {
		t.Fatalf("config.Save() falhou: %v", err)
	}

	readOnly := t.TempDir()
	if err := os.Chmod(readOnly, 0o500); err != nil {
		t.Fatalf("não foi possível preparar dir somente-leitura: %v", err)
	}
	t.Cleanup(func() { os.Chmod(readOnly, 0o700) })

	svc := NewSettingsService(conn, configStorageRoot)
	if _, err := svc.ChangeStorageFolder(readOnly); err == nil {
		t.Fatal("ChangeStorageFolder() esperava erro numa pasta somente-leitura, veio nil")
	}

	got, err := svc.GetStorageRoot()
	if err != nil {
		t.Fatalf("GetStorageRoot() erro inesperado: %v", err)
	}
	if got != oldRoot {
		t.Errorf("GetStorageRoot() = %q, esperado manter %q após falha", got, oldRoot)
	}
}

func TestSettingsService_ChangeStorageFolder_EmptyRejected(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	svc := NewSettingsService(conn, configStorageRoot)
	if _, err := svc.ChangeStorageFolder(""); err == nil {
		t.Fatal("ChangeStorageFolder() esperava erro pra pasta vazia, veio nil")
	}
}
```

- [ ] **Step 2: Run the tests and confirm they fail (compilation)**

Run: `go test ./services/... -run TestSettingsService -v`
Expected: FAIL — `undefined: NewSettingsService`.

- [ ] **Step 3: Implement `services/settings.go`**

```go
package services

import (
	"database/sql"
	"fmt"

	"assistente-idiomas/internal/config"
	"assistente-idiomas/internal/importer"
)

// SettingsService covers the Settings screen (Story 8): view/change the
// storage root and (re-)register the STT provider credential.
type SettingsService struct {
	conn        *sql.DB
	storageRoot func() (string, error)
}

func NewSettingsService(conn *sql.DB, storageRoot func() (string, error)) *SettingsService {
	return &SettingsService{conn: conn, storageRoot: storageRoot}
}

// GetStorageRoot returns the currently configured storage_root.
func (s *SettingsService) GetStorageRoot() (string, error) {
	return s.storageRoot()
}

// ChooseStorageFolder opens the native dialog (same write validation as
// the wizard) and returns the chosen path, without saving anything yet.
// Returns an empty path (no error) if the user cancels the dialog.
func (s *SettingsService) ChooseStorageFolder() (string, error) {
	return chooseStorageFolder("Escolha a nova pasta — os arquivos já devem estar lá dentro")
}

// ChangeStorageFolder saves newRoot to config.json (always, even if the
// scan that follows finds problems) and runs the same hash-based
// reconciliation from Story 3 (internal/importer.Scan): videos with a
// different name in the new folder get video_path updated by hash; new
// videos in the new folder become pending candidates; the change itself is
// never blocked by videos that aren't found (those keep the old path and
// stay "missing" — see LibraryService.videoMissing, Task 4). ScanSummary
// is the type already defined in import.go, reused here without
// duplicating the summary format.
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
		return ScanSummary{}, err
	}
	return ScanSummary{New: sum.New, Updated: sum.Updated, Skipped: sum.Skipped, Errors: sum.Errors}, nil
}
```

- [ ] **Step 4: Run the tests again**

Run: `go test ./services/... -run TestSettingsService -v`
Expected: PASS on all three tests.

- [ ] **Step 5: `go vet` and full suite**

Run: `go vet ./... && go test ./...`
Expected: no output from vet; all packages PASS.

- [ ] **Step 6: Commit**

```bash
git add services/settings.go services/settings_test.go
git commit -m "feat: add SettingsService with storage root change"
```

---

### Task 3: `SettingsService` — STT provider credential

**Files:**
- Modify: `services/settings.go`
- Modify: `services/settings_test.go`

**Interfaces:**
- Consumes: `config.GetSTTAPIKey`/`config.SaveSTTAPIKey` (`internal/config`); `keyring.ErrNotFound`
  (`github.com/zalando/go-keyring`, already a project dependency).
- Produces: `(*SettingsService).HasSTTCredential() (bool, error)`,
  `(*SettingsService).SaveSTTAPIKey(apiKey string) error` — used by the frontend (Task 6).

- [ ] **Step 1: Add the tests at the end of `services/settings_test.go`**

```go
func TestSettingsService_HasSTTCredential_FalseWhenNotConfigured(t *testing.T) {
	keyring.MockInit()

	svc := NewSettingsService(nil, configStorageRoot)
	has, err := svc.HasSTTCredential()
	if err != nil {
		t.Fatalf("HasSTTCredential() erro inesperado: %v", err)
	}
	if has {
		t.Error("HasSTTCredential() = true, esperado false (nenhuma credencial gravada ainda)")
	}
}

func TestSettingsService_HasSTTCredential_TrueAfterSave(t *testing.T) {
	keyring.MockInit()

	svc := NewSettingsService(nil, configStorageRoot)
	if err := svc.SaveSTTAPIKey("sk-test-123"); err != nil {
		t.Fatalf("SaveSTTAPIKey() erro inesperado: %v", err)
	}

	has, err := svc.HasSTTCredential()
	if err != nil {
		t.Fatalf("HasSTTCredential() erro inesperado: %v", err)
	}
	if !has {
		t.Error("HasSTTCredential() = false, esperado true após SaveSTTAPIKey")
	}
}

func TestSettingsService_HasSTTCredential_KeyringUnavailablePropagatesError(t *testing.T) {
	sentinel := errors.New("secret service indisponível")
	keyring.MockInitWithError(sentinel)
	t.Cleanup(keyring.MockInit)

	svc := NewSettingsService(nil, configStorageRoot)
	_, err := svc.HasSTTCredential()
	if !errors.Is(err, sentinel) {
		t.Errorf("HasSTTCredential() erro = %v, esperado envolver %v", err, sentinel)
	}
	if !strings.Contains(err.Error(), "gnome-keyring") {
		t.Errorf("erro não menciona gnome-keyring/kwallet: %v", err)
	}
}
```

Update the `import` block of `services/settings_test.go` (top of the file) to include the new
packages used by these tests:

```go
import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"assistente-idiomas/internal/config"
	"assistente-idiomas/internal/db"
	"assistente-idiomas/internal/importer"

	"github.com/zalando/go-keyring"
)
```

- [ ] **Step 2: Run the tests and confirm they fail (compilation)**

Run: `go test ./services/... -run TestSettingsService_HasSTTCredential -v`
Expected: FAIL — `undefined: (*SettingsService).HasSTTCredential`.

- [ ] **Step 3: Add the methods to the end of `services/settings.go`**

Update the `import` block of `services/settings.go`:

```go
import (
	"database/sql"
	"errors"
	"fmt"

	"assistente-idiomas/internal/config"
	"assistente-idiomas/internal/importer"

	"github.com/zalando/go-keyring"
)
```

Add at the end of the file:

```go
// HasSTTCredential reports whether a credential is stored in the keyring,
// without revealing the value. false (no error) if simply not configured
// yet; an error only on a real keyring access failure (Secret Service
// unavailable, project risk 3).
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

// SaveSTTAPIKey saves/overwrites the STT provider credential.
func (s *SettingsService) SaveSTTAPIKey(apiKey string) error {
	return config.SaveSTTAPIKey(apiKey)
}
```

- [ ] **Step 4: Run the tests again**

Run: `go test ./services/... -run TestSettingsService -v`
Expected: PASS on all.

- [ ] **Step 5: `go vet` and full suite**

Run: `go vet ./... && go test ./...`
Expected: no output from vet; all packages PASS.

- [ ] **Step 6: Commit**

```bash
git add services/settings.go services/settings_test.go
git commit -m "feat: add STT credential (re-)registration to SettingsService"
```

---

### Task 4: `LibraryService` — missing video indicator

**Files:**
- Modify: `services/library.go`
- Modify: `services/library_test.go`
- Modify: `main.go` (only the `LibraryService` construction line, see Step 5)

**Interfaces:**
- Consumes: nothing new from other packages (only stdlib `os`/`path/filepath`).
- Produces: `NewLibraryService(conn *sql.DB, storageRoot func() (string, error)) *LibraryService`
  (signature changes — consumed by `main.go`, Task 5, and by this file's tests);
  `Lesson.VideoMissing bool` (json: `videoMissing`) — consumed by the frontend in Task 7.

- [ ] **Step 1: Prepare `services/library_test.go`**

First, rename the 10 existing occurrences of `NewLibraryService(conn)` (which will break as
soon as the signature changes in Step 3) to the new form:

Run: `sed -i 's/NewLibraryService(conn)/NewLibraryService(conn, testStorageRoot(t))/g' services/library_test.go`

Then, add the `testStorageRoot` helper and three new tests at the end of the file:

```go
// testStorageRoot returns a fixed storage_root resolver, pointing to an
// empty temporary directory — used by tests unrelated to the missing-video
// check (that one has its own tests below).
func testStorageRoot(t *testing.T) func() (string, error) {
	t.Helper()
	dir := t.TempDir()
	return func() (string, error) { return dir, nil }
}

func TestLibraryService_ListLessons_VideoMissingWhenFileNotFound(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLesson(t, conn, "2026-07-20", "Sarah M.", "aula.mp4")
	mustInsertJobWithStatus(t, conn, lessonID, "extract_audio", "done", "")
	mustInsertJobWithStatus(t, conn, lessonID, "transcribe", "done", "")

	root := t.TempDir() // empty — the "aula.mp4" file doesn't exist here
	svc := NewLibraryService(conn, func() (string, error) { return root, nil })
	lessons, err := svc.ListLessons(LessonFilter{})
	if err != nil {
		t.Fatalf("ListLessons() erro inesperado: %v", err)
	}
	if len(lessons) != 1 || !lessons[0].VideoMissing {
		t.Errorf("ListLessons()[0].VideoMissing = %v, esperado true (arquivo não existe)", lessons[0].VideoMissing)
	}
}

func TestLibraryService_ListLessons_VideoNotMissingWhenFileExists(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "aula.mp4"), []byte("conteudo-fake"), 0o644); err != nil {
		t.Fatalf("escrever vídeo de fixture falhou: %v", err)
	}

	lessonID := mustInsertLesson(t, conn, "2026-07-20", "Sarah M.", "aula.mp4")
	mustInsertJobWithStatus(t, conn, lessonID, "extract_audio", "done", "")
	mustInsertJobWithStatus(t, conn, lessonID, "transcribe", "done", "")

	svc := NewLibraryService(conn, func() (string, error) { return root, nil })
	lessons, err := svc.ListLessons(LessonFilter{})
	if err != nil {
		t.Fatalf("ListLessons() erro inesperado: %v", err)
	}
	if len(lessons) != 1 || lessons[0].VideoMissing {
		t.Errorf("ListLessons()[0].VideoMissing = %v, esperado false (arquivo existe)", lessons[0].VideoMissing)
	}
}

func TestLibraryService_GetLesson_VideoMissingWhenFileNotFound(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLesson(t, conn, "2026-07-20", "Sarah M.", "aula.mp4")

	root := t.TempDir()
	svc := NewLibraryService(conn, func() (string, error) { return root, nil })
	lesson, err := svc.GetLesson(lessonID)
	if err != nil {
		t.Fatalf("GetLesson() erro inesperado: %v", err)
	}
	if !lesson.VideoMissing {
		t.Errorf("GetLesson().VideoMissing = %v, esperado true (arquivo não existe)", lesson.VideoMissing)
	}
}
```

Update the `import` of `services/library_test.go` to include `"os"`:

```go
import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"assistente-idiomas/internal/db"
)
```

- [ ] **Step 2: Run the tests and confirm they fail (compilation)**

Run: `go test ./services/... -run TestLibraryService -v`
Expected: FAIL — `not enough arguments in call to NewLibraryService` (signature still has 1
parameter) and `lessons[0].VideoMissing undefined`.

- [ ] **Step 3: Update `services/library.go`**

Final file contents:

```go
package services

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"assistente-idiomas/internal/db"
)

// LibraryService exposes already-confirmed lessons to the Library —
// listing with status derived from jobs and duration (Story 5), filter by
// tutor/period, reprocessing of lessons with errors, fetching a lesson for
// the Detail view and its synced transcript (Story 6), and checking
// whether the video file is present in the current storage_root (Story 8).
type LibraryService struct {
	conn        *sql.DB
	storageRoot func() (string, error)
}

func NewLibraryService(conn *sql.DB, storageRoot func() (string, error)) *LibraryService {
	return &LibraryService{conn: conn, storageRoot: storageRoot}
}

// Lesson is a confirmed lesson, in the format exposed to the frontend.
// Status is always one of "processando", "pronta", "erro" (see
// db.LessonWithStatus); ErrorMessage is only filled when Status == "erro".
// DurationSeconds is nil until the duration probe (best effort, at import
// confirmation) succeeds. StudentSpeakerLabel is nil until the user marks
// who the student is in the Detail view's toggle (Story 6). VideoMissing
// is recalculated on every read (never stored in the database) — true when
// the video_path file isn't found in the current storage_root (Story 8:
// folder changed without the video reappearing, or file deleted/moved
// outside the app).
type Lesson struct {
	ID                  int64   `json:"id"`
	LessonDate          string  `json:"lessonDate"`
	Tutor               string  `json:"tutor"`
	VideoPath           string  `json:"videoPath"`
	DurationSeconds     *int64  `json:"durationSeconds"`
	Status              string  `json:"status"`
	ErrorMessage        string  `json:"errorMessage"`
	StudentSpeakerLabel *string `json:"studentSpeakerLabel"`
	VideoMissing        bool    `json:"videoMissing"`
}

// LessonFilter filters ListLessons — empty fields are ignored (no filter
// on that criterion).
type LessonFilter struct {
	Tutor    string `json:"tutor"`
	DateFrom string `json:"dateFrom"`
	DateTo   string `json:"dateTo"`
}

// Transcript is a lesson's transcript, in the format exposed to the Detail
// view (Story 6).
type Transcript struct {
	Utterances []Utterance `json:"utterances"`
}

// Utterance is one line of the transcript. Timestamps in seconds — same
// unit as HTMLVideoElement.currentTime in the frontend, converted here at
// the service boundary (the database stores time.Duration).
type Utterance struct {
	Speaker      string  `json:"speaker"`
	Text         string  `json:"text"`
	StartSeconds float64 `json:"startSeconds"`
	EndSeconds   float64 `json:"endSeconds"`
}

// ListLessons lists confirmed lessons with status/duration, most recent
// first, applying filter.
func (s *LibraryService) ListLessons(filter LessonFilter) ([]Lesson, error) {
	rows, err := db.ListLessonsWithStatus(s.conn, db.LessonFilter{
		Tutor:    filter.Tutor,
		DateFrom: filter.DateFrom,
		DateTo:   filter.DateTo,
	})
	if err != nil {
		return nil, err
	}
	out := make([]Lesson, 0, len(rows))
	for _, r := range rows {
		out = append(out, Lesson{
			ID:                  r.ID,
			LessonDate:          r.LessonDate,
			Tutor:               r.Tutor,
			VideoPath:           r.VideoPath,
			DurationSeconds:     r.DurationSeconds,
			Status:              r.Status,
			ErrorMessage:        r.ErrorMessage,
			StudentSpeakerLabel: r.StudentSpeakerLabel,
			VideoMissing:        s.videoMissing(r.VideoPath),
		})
	}
	return out, nil
}

// ListTutors lists the distinct tutors already registered, for the
// Library's filter dropdown.
func (s *LibraryService) ListTutors() ([]string, error) {
	return db.ListTutors(s.conn)
}

// RetryLesson resets the lesson's errored jobs to "pending" — the jobs
// worker (internal/jobs) resumes the pipeline on its own on the next poll
// (~5s), without needing to be explicitly woken (same decision as
// Story 4). It's not an error if the lesson currently has no job in
// error.
func (s *LibraryService) RetryLesson(lessonID int64) error {
	_, err := db.ResetErrorJobsForLesson(s.conn, lessonID)
	return err
}

// GetLesson looks up a lesson by id, with status/error derived from the
// jobs, for the Detail view (Story 6) — which now opens for any status:
// "processando" and "erro" show the video without a transcript (see
// LessonDetail.svelte), "pronta" enables GetTranscript.
func (s *LibraryService) GetLesson(id int64) (Lesson, error) {
	lws, err := db.FindLessonWithStatusByID(s.conn, id)
	if err != nil {
		return Lesson{}, err
	}
	if lws == nil {
		return Lesson{}, fmt.Errorf("aula %d não encontrada", id)
	}
	return Lesson{
		ID:                  lws.ID,
		LessonDate:          lws.LessonDate,
		Tutor:               lws.Tutor,
		VideoPath:           lws.VideoPath,
		DurationSeconds:     lws.DurationSeconds,
		Status:              lws.Status,
		ErrorMessage:        lws.ErrorMessage,
		StudentSpeakerLabel: lws.StudentSpeakerLabel,
		VideoMissing:        s.videoMissing(lws.VideoPath),
	}, nil
}

// GetTranscript looks up a lesson's transcript for the Detail view
// (Story 6). Should only be called once GetLesson has already returned
// Status == "pronta" — the Detail view doesn't call this for lessons that
// are processing/errored, which show the status instead of the transcript
// panel.
func (s *LibraryService) GetTranscript(lessonID int64) (Transcript, error) {
	t, err := db.FindTranscriptByLessonID(s.conn, lessonID)
	if err != nil {
		return Transcript{}, err
	}
	if t == nil {
		return Transcript{}, fmt.Errorf("aula %d ainda não tem transcrição", lessonID)
	}
	out := Transcript{Utterances: make([]Utterance, 0, len(t.Utterances))}
	for _, u := range t.Utterances {
		out.Utterances = append(out.Utterances, Utterance{
			Speaker:      u.Speaker,
			Text:         u.Text,
			StartSeconds: u.Start.Seconds(),
			EndSeconds:   u.End.Seconds(),
		})
	}
	return out, nil
}

// SetStudentSpeaker stores which raw speaker (e.g. "speaker_0") is the
// student in this lesson — the Detail view's toggle (Story 6).
func (s *LibraryService) SetStudentSpeaker(lessonID int64, speakerLabel string) error {
	return db.SetStudentSpeaker(s.conn, lessonID, speakerLabel)
}

// videoMissing reports whether a lesson's video file isn't found in the
// current storage_root. Any os.Stat error (not just "doesn't exist") is
// treated as missing — resilience: never lets the Library break because of
// this, and it's not worth distinguishing "missing" from "no permission"
// in this slice (Story 8).
func (s *LibraryService) videoMissing(videoPath string) bool {
	root, err := s.storageRoot()
	if err != nil {
		return true
	}
	_, err = os.Stat(filepath.Join(root, filepath.FromSlash(videoPath)))
	return err != nil
}
```

- [ ] **Step 4: Run the tests again**

Run: `go test ./services/... -run TestLibraryService -v`
Expected: PASS on all.

- [ ] **Step 5: Update the call in `main.go`**

In `main.go`, the line:

```go
Services: []application.Service{
    application.NewService(services.NewSetupService()),
    application.NewService(importService),
    application.NewService(services.NewLibraryService(conn)),
    application.NewService(services.NewQueueService(conn)),
},
```

becomes:

```go
Services: []application.Service{
    application.NewService(services.NewSetupService()),
    application.NewService(importService),
    application.NewService(services.NewLibraryService(conn, storageRoot)),
    application.NewService(services.NewQueueService(conn)),
},
```

(`storageRoot` already exists in `main.go` — it's the same closure over `config.Load()` already
passed to the worker and to `VideoAssetMiddleware`.)

- [ ] **Step 6: Full build + `go vet` + full suite**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: build with no error; vet with no output; all packages PASS.

- [ ] **Step 7: Commit**

```bash
git add services/library.go services/library_test.go main.go
git commit -m "feat: LibraryService flags missing video in the current storage_root"
```

---

### Task 5: Register `SettingsService` in `main.go` and generate bindings

**Files:**
- Modify: `main.go`

**Interfaces:**
- Consumes: `services.NewSettingsService(conn *sql.DB, storageRoot func() (string, error)) *SettingsService` (Tasks 2-3).
- Produces: `frontend/bindings/assistente-idiomas/services/settingsservice.ts` (generated, not
  committed) — consumed by the frontend in Tasks 6-7. `models.ts` gains `ScanSummary` (if not
  already present) and `Lesson` gains the `videoMissing` field.

- [ ] **Step 1: Register the service in `main.go`**

In the `Services` list of `application.New`, add `SettingsService` (using the same `storageRoot`
already resolved in `main.go`):

```go
Services: []application.Service{
    application.NewService(services.NewSetupService()),
    application.NewService(importService),
    application.NewService(services.NewLibraryService(conn, storageRoot)),
    application.NewService(services.NewQueueService(conn)),
    application.NewService(services.NewSettingsService(conn, storageRoot)),
},
```

- [ ] **Step 2: Build**

Run: `go build ./...`
Expected: no error.

- [ ] **Step 3: Generate the frontend bindings**

Run: `wails3 generate bindings -ts -i ./...`
Expected: no error; `frontend/bindings/assistente-idiomas/services/settingsservice.ts` now
exists, exporting `GetStorageRoot`, `ChooseStorageFolder`, `ChangeStorageFolder`,
`HasSTTCredential`, `SaveSTTAPIKey`; `frontend/bindings/assistente-idiomas/services/models.ts`
now has `Lesson.videoMissing: boolean`.

- [ ] **Step 4: Confirm the frontend still type-checks cleanly with the new bindings**

Run: `cd frontend && pnpm run check`
Expected: no type errors (no consumer uses the new fields/services yet — this just confirms the
generation didn't break anything existing).

- [ ] **Step 5: Commit** (only `main.go` — `frontend/bindings` is gitignored)

```bash
git add main.go
git commit -m "feat: register SettingsService in the Wails app"
```

---

### Task 6: Frontend — Settings route and screen

**Files:**
- Modify: `frontend/src/lib/Header.svelte`
- Modify: `frontend/src/App.svelte`
- Create: `frontend/src/lib/screens/Settings.svelte`

**Interfaces:**
- Consumes: `frontend/bindings/assistente-idiomas/services/settingsservice` (Task 5):
  `GetStorageRoot()`, `ChooseStorageFolder()`, `ChangeStorageFolder(newRoot: string)`,
  `HasSTTCredential()`, `SaveSTTAPIKey(apiKey: string)`; `$models.ScanSummary` (fields `new`,
  `updated`, `skipped`, `errors`).
- Produces: `onOpenSettings: () => void` prop on `Header.svelte`; `{ screen: "settings" }` variant
  on `App.svelte`'s `Route`.

- [ ] **Step 1: Update `frontend/src/lib/Header.svelte`**

```svelte
<script lang="ts">
  import { colors } from "./theme";

  let { onOpenSettings }: { onOpenSettings: () => void } = $props();
</script>

<header class="header" style="border-bottom: 1px solid {colors.line};">
  <button
    class="settings-button"
    style="color: {colors.mut};"
    onclick={onOpenSettings}
    aria-label="Configurações"
  >
    ⚙
  </button>
</header>

<style>
  .header {
    height: 3rem;
    flex-shrink: 0;
    display: flex;
    align-items: center;
    justify-content: flex-end;
    padding: 0 1rem;
  }
  .settings-button {
    background: transparent;
    border: none;
    cursor: pointer;
    font-size: 1.1rem;
    line-height: 1;
    padding: 0.25rem;
  }
</style>
```

- [ ] **Step 2: Create `frontend/src/lib/screens/Settings.svelte`**

```svelte
<script lang="ts">
  import { onMount } from "svelte";
  import { colors, fonts } from "../theme";
  import * as SettingsService from "../../../bindings/assistente-idiomas/services/settingsservice";

  let storageRoot: string = $state("");
  let loading: boolean = $state(true);
  let loadError: string = $state("");

  let changingFolder: boolean = $state(false);
  let folderError: string = $state("");
  let scanSummary: string = $state("");

  let hasCredential: boolean = $state(false);
  let credentialError: string = $state("");
  let apiKeyInput: string = $state("");
  let savingCredential: boolean = $state(false);
  let saveCredentialError: string = $state("");
  let saveCredentialSuccess: boolean = $state(false);

  async function loadStorageRoot() {
    storageRoot = await SettingsService.GetStorageRoot();
  }

  async function loadCredentialStatus() {
    try {
      hasCredential = await SettingsService.HasSTTCredential();
      credentialError = "";
    } catch (e) {
      credentialError = String(e);
    }
  }

  async function changeFolder() {
    folderError = "";
    scanSummary = "";
    let chosen: string;
    try {
      chosen = await SettingsService.ChooseStorageFolder();
    } catch (e) {
      folderError = String(e);
      return;
    }
    if (!chosen) return;

    changingFolder = true;
    try {
      const summary = await SettingsService.ChangeStorageFolder(chosen);
      scanSummary = `${summary.new} novas, ${summary.updated} atualizadas, ${summary.skipped} puladas, ${summary.errors} erros`;
      await loadStorageRoot();
    } catch (e) {
      folderError = String(e);
    } finally {
      changingFolder = false;
    }
  }

  async function saveCredential() {
    saveCredentialError = "";
    saveCredentialSuccess = false;
    savingCredential = true;
    try {
      await SettingsService.SaveSTTAPIKey(apiKeyInput);
      apiKeyInput = "";
      saveCredentialSuccess = true;
      await loadCredentialStatus();
    } catch (e) {
      saveCredentialError = String(e);
    } finally {
      savingCredential = false;
    }
  }

  onMount(async () => {
    try {
      await Promise.all([loadStorageRoot(), loadCredentialStatus()]);
    } catch (e) {
      loadError = String(e);
    } finally {
      loading = false;
    }
  });
</script>

<div class="screen" style="font-family: {fonts.body}; color: {colors.text};">
  <h1 style="font-family: {fonts.display};">Configurações</h1>

  {#if loading}
    <p style="color: {colors.mut};">Carregando…</p>
  {:else}
    {#if loadError}
      <p class="error" style="color: {colors.red};">{loadError}</p>
    {/if}

    <section class="card" style="background: {colors.surface}; border: 1px solid {colors.line};">
      <h2 style="font-family: {fonts.display};">Armazenamento</h2>
      <p class="path" style="font-family: {fonts.mono}; color: {colors.mut};">{storageRoot}</p>
      <p class="hint" style="color: {colors.mut};">
        Se você já moveu a pasta de aulas manualmente, aponte o app pra ela aqui — os vídeos não
        são copiados nem movidos pelo app.
      </p>
      <button onclick={changeFolder} disabled={changingFolder}>
        {changingFolder ? "Trocando…" : "Trocar pasta"}
      </button>
      {#if scanSummary}
        <p class="hint" style="color: {colors.mut};">{scanSummary}</p>
      {/if}
      {#if folderError}
        <p class="error" style="color: {colors.red};">{folderError}</p>
      {/if}
    </section>

    <section class="card" style="background: {colors.surface}; border: 1px solid {colors.line};">
      <h2 style="font-family: {fonts.display};">Credencial do provedor de transcrição</h2>
      {#if credentialError}
        <p class="error" style="color: {colors.red};">{credentialError}</p>
      {:else}
        <p class="status" style="color: {hasCredential ? colors.green : colors.mut};">
          {hasCredential ? "Credencial configurada" : "Nenhuma credencial configurada"}
        </p>
      {/if}
      <div class="credential-form">
        <input
          type="password"
          bind:value={apiKeyInput}
          placeholder="Nova API key da ElevenLabs"
          autocomplete="off"
          style="border: 1px solid {colors.line}; background: transparent; color: {colors.text};"
        />
        <button onclick={saveCredential} disabled={savingCredential || !apiKeyInput}>
          {savingCredential ? "Salvando…" : "Salvar"}
        </button>
      </div>
      {#if saveCredentialSuccess}
        <p class="hint" style="color: {colors.green};">Credencial salva.</p>
      {/if}
      {#if saveCredentialError}
        <p class="error" style="color: {colors.red};">{saveCredentialError}</p>
      {/if}
    </section>
  {/if}
</div>

<style>
  .screen {
    padding: 2rem;
    max-width: 48rem;
    margin: 0 auto;
    width: 100%;
  }
  h1 {
    font-size: 1.4rem;
    margin: 0 0 1.25rem;
  }
  .card {
    border-radius: 0.75rem;
    padding: 1.25rem;
    margin-bottom: 1.25rem;
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }
  .card h2 {
    font-size: 1rem;
    margin: 0 0 0.25rem;
  }
  .path {
    font-size: 0.85rem;
    word-break: break-all;
  }
  .hint {
    font-size: 0.8rem;
    line-height: 1.4;
    margin: 0;
  }
  .error {
    font-size: 0.85rem;
    margin: 0;
  }
  .status {
    font-size: 0.85rem;
    margin: 0;
  }
  button {
    align-self: flex-start;
    padding: 0.5rem 1rem;
    border-radius: 0.5rem;
    cursor: pointer;
  }
  .credential-form {
    display: flex;
    gap: 0.5rem;
    align-items: center;
    flex-wrap: wrap;
  }
  .credential-form input {
    flex: 1;
    min-width: 12rem;
    padding: 0.5rem 0.75rem;
    border-radius: 0.5rem;
  }
</style>
```

- [ ] **Step 3: Update `frontend/src/App.svelte`**

```svelte
<script lang="ts">
  import { onMount } from "svelte";
  import Sidebar from "./lib/Sidebar.svelte";
  import Header from "./lib/Header.svelte";
  import Library from "./lib/screens/Library.svelte";
  import LessonDetail from "./lib/screens/LessonDetail.svelte";
  import Progress from "./lib/screens/Progress.svelte";
  import Queue from "./lib/screens/Queue.svelte";
  import Settings from "./lib/screens/Settings.svelte";
  import SetupWizard from "./lib/SetupWizard.svelte";
  import { colors, fonts } from "./lib/theme";
  import { initJobsStore } from "./lib/jobsStore.svelte";
  import * as SetupService from "../bindings/assistente-idiomas/services/setupservice";

  type NavScreen = "library" | "progress" | "queue";
  type Route =
    | { screen: "library" }
    | { screen: "lesson-detail"; lessonId: number }
    | { screen: "progress" }
    | { screen: "queue" }
    | { screen: "settings" };

  let route: Route = $state({ screen: "library" });
  let checkingFirstRun = $state(true);
  let firstRun = $state(false);

  function navigate(screen: NavScreen) {
    route = { screen };
  }

  function openLesson(lessonId: number) {
    route = { screen: "lesson-detail", lessonId };
  }

  onMount(async () => {
    initJobsStore();
    try {
      firstRun = await SetupService.IsFirstRun();
    } finally {
      checkingFirstRun = false;
    }
  });
</script>

{#if checkingFirstRun}
  <div class="shell" style="background: {colors.bg};"></div>
{:else if firstRun}
  <SetupWizard onComplete={() => (firstRun = false)} />
{:else}
  <div class="shell" style="background: {colors.bg}; font-family: {fonts.body};">
    <Sidebar
      active={route.screen === "lesson-detail" || route.screen === "settings" ? "library" : route.screen}
      onNavigate={navigate}
    />
    <main class="main">
      <Header onOpenSettings={() => (route = { screen: "settings" })} />
      <div class="content">
        {#if route.screen === "library"}
          <Library onOpenLesson={openLesson} />
        {:else if route.screen === "lesson-detail"}
          <LessonDetail lessonId={route.lessonId} onBack={() => navigate("library")} />
        {:else if route.screen === "progress"}
          <Progress />
        {:else if route.screen === "settings"}
          <Settings />
        {:else}
          <Queue />
        {/if}
      </div>
    </main>
  </div>
{/if}

<style>
  .shell {
    display: flex;
    width: 100%;
    height: 100vh;
  }
  .main {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .content {
    flex: 1;
    overflow-y: auto;
  }
</style>
```

- [ ] **Step 4: Type check**

Run: `cd frontend && pnpm run check`
Expected: no errors.

- [ ] **Step 5: Build**

Run: `cd frontend && pnpm run build`
Expected: build with no error.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/lib/Header.svelte frontend/src/App.svelte frontend/src/lib/screens/Settings.svelte
git commit -m "feat: add Settings screen (storage and credential)"
```

---

### Task 7: Frontend — "missing video" indicator

**Files:**
- Modify: `frontend/src/lib/screens/Library.svelte`
- Modify: `frontend/src/lib/screens/LessonDetail.svelte`

**Interfaces:**
- Consumes: `Lesson.videoMissing: boolean` (bindings generated in Task 5).

- [ ] **Step 1: Update the lesson item in `frontend/src/lib/screens/Library.svelte`**

Locate the block (inside `{#each lessons as lesson (lesson.id)}`):

```svelte
              {#if lesson.status === "erro"}
                <div class="status-block">
                  <span class="badge" style="color: {colors.red}; background: rgba(224,108,108,.1);">erro</span>
                  <span class="error-message" style="color: {colors.mut};">{lesson.errorMessage}</span>
                  <button onclick={() => retry(lesson.id)} disabled={retryingId === lesson.id}>
                    {retryingId === lesson.id ? "Reprocessando…" : "Reprocessar"}
                  </button>
                </div>
              {:else}
                <span
                  class="badge"
                  style="color: {lesson.status === 'pronta'
                    ? colors.green
                    : colors.blue}; background: {lesson.status === 'pronta'
                    ? 'rgba(111,191,142,.1)'
                    : 'rgba(110,168,254,.1)'};"
                >
                  {STATUS_LABEL[lesson.status] ?? lesson.status}
                </span>
              {/if}
```

Replace with (same block, with the missing-video badge added alongside it, independent of
`status`):

```svelte
              {#if lesson.status === "erro"}
                <div class="status-block">
                  <span class="badge" style="color: {colors.red}; background: rgba(224,108,108,.1);">erro</span>
                  <span class="error-message" style="color: {colors.mut};">{lesson.errorMessage}</span>
                  <button onclick={() => retry(lesson.id)} disabled={retryingId === lesson.id}>
                    {retryingId === lesson.id ? "Reprocessando…" : "Reprocessar"}
                  </button>
                </div>
              {:else}
                <span
                  class="badge"
                  style="color: {lesson.status === 'pronta'
                    ? colors.green
                    : colors.blue}; background: {lesson.status === 'pronta'
                    ? 'rgba(111,191,142,.1)'
                    : 'rgba(110,168,254,.1)'};"
                >
                  {STATUS_LABEL[lesson.status] ?? lesson.status}
                </span>
              {/if}
              {#if lesson.videoMissing}
                <span class="badge" style="color: {colors.amber}; background: rgba(227,164,76,.1);">
                  vídeo ausente
                </span>
              {/if}
```

- [ ] **Step 2: Update the lesson header in `frontend/src/lib/screens/LessonDetail.svelte`**

Locate:

```svelte
    <div class="header-row">
      <h1 style="font-family: {fonts.display};">{formatLessonDateTime(lesson.lessonDate)}</h1>
      <span class="meta" style="color: {colors.mut}; font-family: {fonts.mono};"
        >{lesson.tutor}{formatDuration(lesson.durationSeconds) ? ` · ${formatDuration(lesson.durationSeconds)}` : ""}</span
      >
    </div>
```

Replace with:

```svelte
    <div class="header-row">
      <h1 style="font-family: {fonts.display};">{formatLessonDateTime(lesson.lessonDate)}</h1>
      <span class="meta" style="color: {colors.mut}; font-family: {fonts.mono};"
        >{lesson.tutor}{formatDuration(lesson.durationSeconds) ? ` · ${formatDuration(lesson.durationSeconds)}` : ""}</span
      >
      {#if lesson.videoMissing}
        <span
          class="video-missing-badge"
          style="color: {colors.amber}; background: rgba(227,164,76,.1);"
        >
          vídeo não encontrado na pasta atual
        </span>
      {/if}
    </div>
```

Add to the `<style>` block (near `.meta`):

```css
  .video-missing-badge {
    font-size: 0.75rem;
    padding: 0.25rem 0.6rem;
    border-radius: 999px;
  }
```

- [ ] **Step 3: Type check**

Run: `cd frontend && pnpm run check`
Expected: no errors.

- [ ] **Step 4: Build**

Run: `cd frontend && pnpm run build`
Expected: build with no error.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/lib/screens/Library.svelte frontend/src/lib/screens/LessonDetail.svelte
git commit -m "feat: flag missing video in the Library and Detail views"
```

---

## Manual verification (outside the scope of automated tests, log as pending)

Same pattern as previous stories — pending on a real window (Windows/Linux):
1. `wails3 dev`; open Settings via the Header icon.
2. Actually change the storage folder by moving a video under a different name into it and
   confirm the Library reflects the new `video_path` (no "missing video" on that lesson).
3. Delete a video from disk and confirm the Library and the Detail view show "missing video"
   without breaking the screen or preventing navigation.
4. Re-register the credential and confirm the status changes to "Credential configured".
