# Story 2 — Local Database and Machine Configuration: Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The app creates/opens its local SQLite database (schema v1: `lessons`, `transcripts`,
`jobs`, `prompts`), resolves and persists where the user's synced storage folder lives, and stores
the ElevenLabs API key through the OS keyring — all set up through a two-step first-run wizard.

**Architecture:** Two new Wails-free Go packages (`internal/db`, `internal/config`) hold all the
logic and are unit-tested directly. A new top-level `services/` package (deliberately **not**
under `internal/` — it imports `github.com/wailsapp/wails/v3/pkg/application`, which `internal/`
packages must never do) hosts `SetupService`, the thin Wails-facing binding that the frontend
calls. `main.go` opens the database unconditionally at startup (schema doesn't depend on the
storage root) and registers `SetupService`. The frontend gates the existing shell behind a
`SetupWizard.svelte` component, shown only when `SetupService.IsFirstRun()` returns true.

**Tech Stack:** `modernc.org/sqlite` v1.54.0 (WAL mode), `github.com/pressly/goose/v3` v3.27.2
(embedded migrations via `embed.FS`), `github.com/zalando/go-keyring` v0.2.8, Wails v3
`v3.0.0-alpha2.117` (already pinned), Svelte 5 runes.

Every piece of code in this plan — the Go packages, the generated bindings, and the Svelte
components — was written and verified end-to-end during planning: `go build`/`go vet`/`go test`
all pass on the real dependency versions listed above, `wails3 generate bindings` was actually run
to confirm the generated file paths and TypeScript signatures used below, and `npm run check` /
`npm run build` both pass on the real frontend with these components added. The code in each step
is the exact code that was verified, not a sketch.

## Global Constraints

- `internal/db` and `internal/config` never import anything under
  `github.com/wailsapp/wails/v3` — thin-layer principle (`CLAUDE.md`), verified by these packages'
  own tests never touching Wails.
- `SetupService` (and any future Wails-facing service) lives in the new top-level `services/`
  package, not `internal/`, and not literally in `package main` — this keeps the binding's
  generated output path predictable (`frontend/bindings/assistente-idiomas/services/...`, verified
  by actually running `wails3 generate bindings` during planning) and avoids Go's "unique
  `package main`" special-casing in the bindings generator.
- SQL in `internal/db/migrations/*.sql` stays portable across SQLite drivers (no
  `modernc.org`-specific syntax) — `CLAUDE.md`'s portable-SQL rule.
- Keyring failures (Secret Service unavailable on Linux — risk 3 in `docs/phase-1-mvp.md`) surface
  as a clear error, propagated up to the UI. No fallback to environment variables or plaintext,
  ever — this is different from `cmd/spike`, which is a dev tool and may keep using env vars.
  `config.json` is only written by `SetupService.CompleteSetup` **after** the keyring write
  succeeds, so a keyring failure never leaves the app in a half-configured state.
  `services/setup.go` doc-comments this ordering explicitly — don't reorder it. The chosen folder
  path itself lives only in the Svelte wizard's local state until `CompleteSetup` succeeds, so it
  isn't lost if the user has to retry after fixing a keyring error.
- Svelte 5 runes only (`$state`, `$props`) — no legacy syntax, per `CLAUDE.md`.
- **Linux build/vet/test prerequisite, discovered during planning:** compiling anything that
  imports `github.com/wailsapp/wails/v3/pkg/application` on Linux requires `CGO_ENABLED=1`
  (auto-detected once a C compiler is on `PATH`) plus GTK4/WebKitGTK 6 dev headers. On
  Debian/Ubuntu-family systems: `sudo apt-get install -y build-essential libgtk-4-dev
  libwebkitgtk-6.0-dev pkg-config`. Without these, `go build`/`go vet`/`go test` on `services/` or
  `main.go` fail with `undefined: pointer` (cgo files not being compiled) or `pkg-config` errors
  (`gtk4`/`webkitgtk-6.0` not found) — this is an environment gap, not a code defect, if you hit
  it. (Note: it's **gtk4**/**webkitgtk-6.0**, not gtk3 — this project's wails3 alpha version links
  against GTK4.)
- `go.mod`'s `go` directive may auto-bump (e.g. `1.25.0` → `1.25.7`) when running `go get`/`go mod
  tidy` below, matching whatever toolchain is available — expected, not an error (same behavior
  documented in the Story 1 plan).
- Every task's Go changes must leave `go vet ./...` and `gofmt -l .` (no output) clean.
- Implementers **stage** (`git add`) their changes at the end of each task but do **not**
  commit — the user controls commit timing (established project convention, `docs/superpowers/plans/2026-07-20-story-1-app-skeleton.md`).

---

### Task 1: `internal/db` — schema v1 migration and `Open()`

**Files:**
- Create: `internal/db/migrations/00001_initial_schema.sql`
- Create: `internal/db/db.go`
- Test: `internal/db/db_test.go`
- Modify: `go.mod`, `go.sum` (adds `modernc.org/sqlite`, `github.com/pressly/goose/v3` as direct
  requires)

**Interfaces:**
- Consumes: nothing (first task).
- Produces: `func Open(path string) (*sql.DB, error)` in package `assistente-idiomas/internal/db`
  — opens/creates the SQLite file at `path` (creating its parent directory if needed), sets WAL
  mode, and applies all pending migrations. Consumed by `main.go` in Task 5.

- [ ] **Step 1: Write the migration file**

```sql
-- internal/db/migrations/00001_initial_schema.sql
-- +goose Up
CREATE TABLE lessons (
    id INTEGER PRIMARY KEY,
    lesson_date TEXT NOT NULL,
    tutor TEXT NOT NULL,
    video_path TEXT NOT NULL,
    video_hash TEXT,
    duration_seconds INTEGER,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE transcripts (
    id INTEGER PRIMARY KEY,
    lesson_id INTEGER NOT NULL REFERENCES lessons(id),
    raw_json_path TEXT NOT NULL,
    utterances TEXT NOT NULL,
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
    kind TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    attempts INTEGER NOT NULL DEFAULT 0,
    last_error TEXT,
    payload TEXT,
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

- [ ] **Step 2: Write the failing test**

```go
// internal/db/db_test.go
package db

import (
	"path/filepath"
	"testing"
)

func TestOpen_AppliesMigrationsAndCreatesAllTables(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")

	conn, err := Open(path)
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	tables := []string{"lessons", "transcripts", "prompts", "jobs"}
	for _, table := range tables {
		var name string
		err := conn.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name)
		if err != nil {
			t.Errorf("tabela %q não encontrada: %v", table, err)
		}
	}
}

func TestOpen_LessonRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")

	conn, err := Open(path)
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	_, err = conn.Exec(
		`INSERT INTO lessons (lesson_date, tutor, video_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"2026-07-21", "Fulano", "aulas/2026/x.mp4", "2026-07-21T10:00:00Z", "2026-07-21T10:00:00Z",
	)
	if err != nil {
		t.Fatalf("insert em lessons falhou: %v", err)
	}

	var tutor string
	if err := conn.QueryRow(`SELECT tutor FROM lessons WHERE video_path = ?`, "aulas/2026/x.mp4").Scan(&tutor); err != nil {
		t.Fatalf("select em lessons falhou: %v", err)
	}
	if tutor != "Fulano" {
		t.Errorf("tutor = %q, esperado \"Fulano\"", tutor)
	}
}

func TestOpen_ReopenIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")

	conn, err := Open(path)
	if err != nil {
		t.Fatalf("primeira abertura falhou: %v", err)
	}
	conn.Close()

	conn2, err := Open(path)
	if err != nil {
		t.Fatalf("reabertura falhou: %v", err)
	}
	defer conn2.Close()
}
```

- [ ] **Step 3: Run the tests to confirm they fail (package doesn't exist yet)**

```bash
go test ./internal/db/... -v
```

Expected: `FAIL` — `Open` is undefined (only the test file exists so far).

- [ ] **Step 4: Write `internal/db/db.go`**

```go
// Package db opens the app's local SQLite database and applies the goose
// migrations embedded in the binary. Imports nothing from Wails (thin layer).
package db

import (
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Open opens (creating if necessary) the SQLite database at path, enables
// WAL, and applies pending migrations. path's parent directory is created if
// it doesn't exist.
func Open(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("criar diretório do banco: %w", err)
	}

	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("abrir banco: %w", err)
	}

	if _, err := conn.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("ativar WAL: %w", err)
	}

	goose.SetBaseFS(migrationsFS)
	defer goose.SetBaseFS(nil)
	if err := goose.SetDialect("sqlite"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("configurar dialeto goose: %w", err)
	}
	if err := goose.Up(conn, "migrations"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("rodar migrations: %w", err)
	}

	return conn, nil
}
```

- [ ] **Step 5: Add the dependencies and tidy**

```bash
go get modernc.org/sqlite@v1.54.0 github.com/pressly/goose/v3@v3.27.2
go mod tidy
```

Expected: `go.mod` gains `modernc.org/sqlite v1.54.0` and `github.com/pressly/goose/v3 v3.27.2` as
direct requires (verified during planning — running `go get` **after** `db.go` exists, so `go mod
tidy` doesn't prune them as unused). Both packages were already present as *indirect* deps pulled
in by Wails itself, at older versions — this just promotes and upgrades them.

- [ ] **Step 6: Run the tests again to confirm they pass**

```bash
go test ./internal/db/... -v
```

Expected: all 3 tests `PASS`. You'll see goose's own log lines
(`goose: successfully migrated database to version: 1`, etc.) on stdout — that's expected noise
from goose's default logger, not a test failure.

- [ ] **Step 7: `go vet` and `gofmt` check**

```bash
go vet ./internal/db/...
gofmt -l internal/db
```

Expected: no output from either (clean).

- [ ] **Step 8: Stage (do not commit — user controls commit timing)**

```bash
git add internal/db go.mod go.sum
git status
```

---

### Task 2: `internal/config` — machine paths and `config.json`

**Files:**
- Create: `internal/config/paths.go`
- Create: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: nothing new (no new external dependency — `os`/`path/filepath`/`encoding/json` only).
- Produces, in package `assistente-idiomas/internal/config`:
  ```go
  func AppDataDir() (string, error) // os.UserConfigDir()/assistente-idiomas, created if missing
  func DBPath() (string, error)     // AppDataDir()/db/app.db — consumed by main.go in Task 5
  type AppConfig struct {
      StorageRoot string `json:"storage_root"`
  }
  func Load() (*AppConfig, error) // error satisfies errors.Is(err, os.ErrNotExist) when config.json is missing
  func Save(cfg *AppConfig) error
  ```
  `Load`/`Save`/`AppConfig` are consumed by `services/setup.go` in Task 4; `DBPath` is consumed by
  `main.go` in Task 5.

- [ ] **Step 1: Write the failing tests**

```go
// internal/config/config_test.go
package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestAppDataDir_CreatesAndReturnsPath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	dir, err := AppDataDir()
	if err != nil {
		t.Fatalf("AppDataDir() erro inesperado: %v", err)
	}
	if filepath.Base(dir) != appDirName {
		t.Errorf("dir = %q, esperado terminar em %q", dir, appDirName)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("diretório não foi criado: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("%q não é um diretório", dir)
	}
}

func TestDBPath_IsUnderAppDataDirDbSubdir(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	appDir, err := AppDataDir()
	if err != nil {
		t.Fatalf("AppDataDir() erro inesperado: %v", err)
	}
	dbPath, err := DBPath()
	if err != nil {
		t.Fatalf("DBPath() erro inesperado: %v", err)
	}
	want := filepath.Join(appDir, "db", "app.db")
	if dbPath != want {
		t.Errorf("DBPath() = %q, esperado %q", dbPath, want)
	}
}

func TestLoad_MissingConfigReturnsErrNotExist(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	_, err := Load()
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Load() erro = %v, esperado errors.Is(err, os.ErrNotExist)", err)
	}
}

func TestSaveThenLoad_RoundTrips(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	want := &AppConfig{StorageRoot: "/home/user/GoogleDrive/aulas"}
	if err := Save(want); err != nil {
		t.Fatalf("Save() erro inesperado: %v", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() erro inesperado: %v", err)
	}
	if got.StorageRoot != want.StorageRoot {
		t.Errorf("StorageRoot = %q, esperado %q", got.StorageRoot, want.StorageRoot)
	}
}
```

`t.Setenv("XDG_CONFIG_HOME", ...)` redirects `os.UserConfigDir()` on Linux to a temp directory —
this is how these tests stay hermetic without touching the real machine's config directory. (On
the Windows dev machine, `os.UserConfigDir()` reads `%AppData%` instead; these tests only need to
pass on whichever machine runs `go test`, and `XDG_CONFIG_HOME` is a no-op override there too if
ever run cross-platform under Wine/etc. — not a concern here.)

- [ ] **Step 2: Run the tests to confirm they fail**

```bash
go test ./internal/config/... -v
```

Expected: `FAIL` — build error, nothing defined yet (`appDirName`, `AppDataDir`, etc.).

- [ ] **Step 3: Write `internal/config/paths.go`**

```go
// Package config resolves the app's data paths on the local machine,
// reads/writes the configuration (config.json), and stores/reads the STT
// provider's credential via the OS keyring. Imports nothing from Wails
// (thin layer).
package config

import (
	"fmt"
	"os"
	"path/filepath"
)

const appDirName = "assistente-idiomas"

// AppDataDir resolves (creating if necessary) the app's data directory
// inside the OS's configuration directory: %AppData%\assistente-idiomas on
// Windows, ~/.config/assistente-idiomas on Linux (respects XDG_CONFIG_HOME).
func AppDataDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolver diretório de configuração do SO: %w", err)
	}
	dir := filepath.Join(base, appDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("criar diretório de dados do app: %w", err)
	}
	return dir, nil
}

// DBPath resolves the path of the SQLite database file inside
// AppDataDir. The parent directory is created by db.Open, not here.
func DBPath() (string, error) {
	dir, err := AppDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "db", "app.db"), nil
}

func configPath() (string, error) {
	dir, err := AppDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}
```

- [ ] **Step 4: Write `internal/config/config.go`**

```go
package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// AppConfig is the local machine configuration, persisted to config.json in
// AppDataDir. StorageRoot is an absolute (machine-specific) path to the
// synced folder where imported lessons are kept.
type AppConfig struct {
	StorageRoot string `json:"storage_root"`
}

// Load reads config.json from AppDataDir. If the file doesn't exist, the
// returned error satisfies errors.Is(err, os.ErrNotExist) — this is how
// callers detect "first run".
func Load() (*AppConfig, error) {
	path, err := configPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("ler config: %w", err)
	}
	var cfg AppConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsear config: %w", err)
	}
	return &cfg, nil
}

// Save writes cfg to config.json in AppDataDir, overwriting whatever is there.
func Save(cfg *AppConfig) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("serializar config: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("gravar config: %w", err)
	}
	return nil
}
```

- [ ] **Step 5: Run the tests to confirm they pass**

```bash
go test ./internal/config/... -v
```

Expected: all 4 tests `PASS`.

- [ ] **Step 6: `go vet` and `gofmt` check**

```bash
go vet ./internal/config/...
gofmt -l internal/config
```

Expected: no output from either.

- [ ] **Step 7: Stage (do not commit — user controls commit timing)**

```bash
git add internal/config
git status
```

---

### Task 3: `internal/config` — ElevenLabs credential via keyring

**Files:**
- Create: `internal/config/credentials.go`
- Test: `internal/config/credentials_test.go`
- Modify: `go.mod`, `go.sum` (adds `github.com/zalando/go-keyring` as a direct require)

**Interfaces:**
- Consumes: nothing from Tasks 1–2 directly (parallel-safe with them, but sequenced here).
- Produces, in package `assistente-idiomas/internal/config`:
  ```go
  func SaveSTTAPIKey(apiKey string) error
  func GetSTTAPIKey() (string, error)
  ```
  Consumed by `services/setup.go` in Task 4.

- [ ] **Step 1: Write the failing tests**

```go
// internal/config/credentials_test.go
package config

import (
	"errors"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestSaveThenGetSTTAPIKey_RoundTrips(t *testing.T) {
	keyring.MockInit()

	if err := SaveSTTAPIKey("sk-test-123"); err != nil {
		t.Fatalf("SaveSTTAPIKey() erro inesperado: %v", err)
	}

	got, err := GetSTTAPIKey()
	if err != nil {
		t.Fatalf("GetSTTAPIKey() erro inesperado: %v", err)
	}
	if got != "sk-test-123" {
		t.Errorf("GetSTTAPIKey() = %q, esperado \"sk-test-123\"", got)
	}
}

func TestSaveSTTAPIKey_KeyringUnavailablePropagatesError(t *testing.T) {
	sentinel := errors.New("secret service indisponível")
	keyring.MockInitWithError(sentinel)
	t.Cleanup(keyring.MockInit)

	err := SaveSTTAPIKey("sk-test-123")
	if !errors.Is(err, sentinel) {
		t.Errorf("SaveSTTAPIKey() erro = %v, esperado envolver %v", err, sentinel)
	}
}
```

`keyring.MockInit()`/`keyring.MockInitWithError(err)` (from `go-keyring` itself) swap the package's
backend for an in-memory fake for the rest of the process — this is what lets these tests run
without a real OS keyring/Secret Service, and also lets the second test simulate exactly the
"Secret Service unavailable" failure from risk 3 in `docs/phase-1-mvp.md`, deterministically. (The
design doc for this story mentioned wrapping `go-keyring` behind a custom interface for
testability; that turned out to be unnecessary — `go-keyring`'s own mock support already covers
both the success and failure paths tested here, so the extra abstraction was dropped in favor of
using the library directly. See Self-Review Notes.)

- [ ] **Step 2: Run the tests to confirm they fail**

```bash
go test ./internal/config/... -run STTAPIKey -v
```

Expected: `FAIL` — build error, `SaveSTTAPIKey`/`GetSTTAPIKey` undefined.

- [ ] **Step 3: Write `internal/config/credentials.go`**

```go
package config

import (
	"fmt"

	"github.com/zalando/go-keyring"
)

const (
	keyringService        = "assistente-idiomas"
	keyringUserElevenLabs = "elevenlabs"
)

// SaveSTTAPIKey writes the ElevenLabs API key to the OS's native
// credential manager, via go-keyring. Never in plaintext. If the Secret
// Service (Linux) or equivalent isn't available, returns an error — with no
// fallback to an environment variable or file.
func SaveSTTAPIKey(apiKey string) error {
	if err := keyring.Set(keyringService, keyringUserElevenLabs, apiKey); err != nil {
		return fmt.Errorf("gravar credencial no gerenciador do sistema: %w", err)
	}
	return nil
}

// GetSTTAPIKey reads the ElevenLabs API key previously saved via
// SaveSTTAPIKey.
func GetSTTAPIKey() (string, error) {
	apiKey, err := keyring.Get(keyringService, keyringUserElevenLabs)
	if err != nil {
		return "", fmt.Errorf("ler credencial do gerenciador do sistema: %w", err)
	}
	return apiKey, nil
}
```

- [ ] **Step 4: Add the dependency and tidy**

```bash
go get github.com/zalando/go-keyring@v0.2.8
go mod tidy
```

Expected: `go.mod` gains `github.com/zalando/go-keyring v0.2.8` as a direct require (same
already-indirect-then-promoted situation as Task 1's dependencies — verified during planning).

- [ ] **Step 5: Run the tests again to confirm they pass**

```bash
go test ./internal/config/... -v
```

Expected: all 6 tests in the package (4 from Task 2 + 2 from this task) `PASS`.

- [ ] **Step 6: `go vet` and `gofmt` check**

```bash
go vet ./internal/config/...
gofmt -l internal/config
```

Expected: no output from either.

- [ ] **Step 7: Stage (do not commit — user controls commit timing)**

```bash
git add internal/config go.mod go.sum
git status
```

---

### Task 4: `services` package — `SetupService`

**Files:**
- Create: `services/setup.go`
- Test: `services/setup_test.go`

**Interfaces:**
- Consumes: `config.AppDataDir`, `config.Load`, `config.Save`, `config.AppConfig`,
  `config.SaveSTTAPIKey` (Tasks 2–3); `github.com/wailsapp/wails/v3/pkg/application` (external).
- Produces, in package `assistente-idiomas/services`:
  ```go
  type SetupService struct{}
  func NewSetupService() *SetupService
  func (s *SetupService) IsFirstRun() bool
  func (s *SetupService) ChooseStorageFolder() (string, error)
  func (s *SetupService) CompleteSetup(storageRoot string, apiKey string) error
  ```
  Consumed by `main.go` in Task 5 (registered as a Wails service) and, indirectly, by the
  generated TypeScript bindings the frontend calls in Task 6.

**Before starting this task:** confirm the Linux build prerequisite from Global Constraints
(`build-essential`, `libgtk-4-dev`, `libwebkitgtk-6.0-dev`) is installed — this task is the first
one to compile code that imports Wails, so it's the first task where a missing prerequisite would
surface.

- [ ] **Step 1: Write the failing tests for the pure, unit-testable piece (`isDirWritable`)**

```go
// services/setup_test.go
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

- [ ] **Step 2: Run the tests to confirm they fail**

```bash
go test ./services/... -v
```

Expected: `FAIL` — `isDirWritable` undefined (package doesn't exist yet).

- [ ] **Step 3: Write `services/setup.go`**

```go
// Package services contains the services exposed to the frontend via Wails
// v3 bindings — the shell that connects internal/config and internal/db to
// the UI. Unlike internal/, this package deliberately imports Wails.
package services

import (
	"fmt"
	"os"

	"assistente-idiomas/internal/config"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// SetupService covers the first-run wizard: choosing the storage folder
// and registering the ElevenLabs API key.
type SetupService struct{}

func NewSetupService() *SetupService {
	return &SetupService{}
}

// IsFirstRun reports whether the app doesn't have config.json written yet
// (no complete configuration so far). Any error loading the config (missing
// file, corrupted, no permission) is treated as "not configured yet" — the
// worst case is the user redoing the wizard, not data loss: CompleteSetup
// simply overwrites config.json and the credential.
func (s *SetupService) IsFirstRun() bool {
	_, err := config.Load()
	return err != nil
}

// ChooseStorageFolder opens the native folder-choice dialog and validates
// that it's writable. Returns an empty path (with no error) if the user
// cancels the dialog.
func (s *SetupService) ChooseStorageFolder() (string, error) {
	dir, err := application.Get().Dialog.OpenFile().
		SetTitle("Escolha a pasta onde as aulas ficarão guardadas").
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

// CompleteSetup writes the ElevenLabs credential (keyring) and, only if that
// succeeds, writes storageRoot to config.json. In that order: if the
// credential fails, config.json isn't touched and the app keeps detecting
// first-run.
func (s *SetupService) CompleteSetup(storageRoot string, apiKey string) error {
	if err := config.SaveSTTAPIKey(apiKey); err != nil {
		return err
	}
	if err := config.Save(&config.AppConfig{StorageRoot: storageRoot}); err != nil {
		return fmt.Errorf("gravar configuração: %w", err)
	}
	return nil
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

- [ ] **Step 4: Run the tests again to confirm they pass**

```bash
go test ./services/... -v
```

Expected: all 4 `isDirWritable` tests `PASS`. (`SetupService`'s three methods aren't exercised by
this test file — `ChooseStorageFolder` needs a running Wails app via `application.Get()`, so it's
only verified by the build/vet below plus the manual end-to-end check in Task 7. `CompleteSetup`
is effectively covered by Tasks 2–3's tests of the `config` functions it calls.)

- [ ] **Step 5: `go build`, `go vet`, and `gofmt` check**

```bash
go build ./services/...
go vet ./services/...
gofmt -l services
```

Expected: no output from any of the three (clean). This is the first package in this story that
imports Wails — if this fails with `undefined: pointer` or a `pkg-config` error about
`gtk4`/`webkitgtk-6.0`, see the Linux prerequisite note in Global Constraints.

- [ ] **Step 6: Stage (do not commit — user controls commit timing)**

```bash
git add services
git status
```

---

### Task 5: Wire `main.go` — open the database at startup, register `SetupService`

**Files:**
- Modify: `main.go`

**Interfaces:**
- Consumes: `db.Open` (Task 1), `config.DBPath` (Task 2), `services.NewSetupService` (Task 4).
- Produces: the running app now opens/migrates its database on every launch and exposes
  `SetupService` to the frontend. Nothing downstream in this story consumes `main.go` directly.

- [ ] **Step 1: Replace `main.go`**

Current content (from Story 1):

```go
package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := application.New(application.Options{
		Name:        "Assistente de Idiomas",
		Description: "Arquivo e análise de aulas de inglês do Cambly",
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "Assistente de Idiomas",
		Width:            1200,
		Height:           760,
		BackgroundColour: application.NewRGB(20, 24, 31), // #14181F — colors.bg
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
```

Replace it with:

```go
package main

import (
	"embed"
	"log"

	"assistente-idiomas/internal/config"
	"assistente-idiomas/internal/db"
	"assistente-idiomas/services"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	dbPath, err := config.DBPath()
	if err != nil {
		log.Fatalf("resolver caminho do banco: %v", err)
	}
	conn, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("abrir banco de dados: %v", err)
	}
	defer conn.Close()

	app := application.New(application.Options{
		Name:        "Assistente de Idiomas",
		Description: "Arquivo e análise de aulas de inglês do Cambly",
		Services: []application.Service{
			application.NewService(services.NewSetupService()),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "Assistente de Idiomas",
		Width:            1200,
		Height:           760,
		BackgroundColour: application.NewRGB(20, 24, 31), // #14181F — colors.bg
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
```

The database is opened **unconditionally**, before the first-run check — schema v1 doesn't depend
on `storage_root`, so there's no reason to gate it behind the wizard. The frontend decides whether
to show the wizard by calling `SetupService.IsFirstRun()` itself once the app is up (Task 6).

- [ ] **Step 2: `go build`, `go vet`, and `gofmt` check**

```bash
go build -o /tmp/assistente-idiomas-check .
go vet ./...
gofmt -l .
```

Expected: all three clean. `go build` here needs `frontend/dist` to exist (from a prior `wails3
build`/`npm run build`) for the `//go:embed all:frontend/dist` directive to find files — if it
doesn't exist yet on your machine, run `cd frontend && npm run build && cd ..` first, or skip this
particular check and rely on Task 6/7's full `wails3 build`/`wails3 dev` instead.

- [ ] **Step 3: Stage (do not commit — user controls commit timing)**

```bash
git add main.go
git status
```

---

### Task 6: Frontend — first-run wizard

**Files:**
- Create: `frontend/src/lib/SetupWizard.svelte`
- Modify: `frontend/src/App.svelte`
- Create (generated, not hand-written — see Step 1): `frontend/bindings/assistente-idiomas/services/setupservice.ts`, `frontend/bindings/assistente-idiomas/services/index.ts`

**Interfaces:**
- Consumes: the generated bindings for `SetupService` — `IsFirstRun(): Promise<boolean>`,
  `ChooseStorageFolder(): Promise<string>`, `CompleteSetup(storageRoot: string, apiKey: string):
  Promise<void>` (exact generated signatures, confirmed during planning by actually running the
  generator against `services/setup.go` from Task 4).
- Produces: `SetupWizard.svelte`, a two-prop-free component taking `{ onComplete: () => void }`,
  rendered by `App.svelte` in place of the normal shell when `IsFirstRun()` is true.

- [ ] **Step 1: Generate the bindings**

```bash
cd frontend && npm install && cd ..
wails3 generate bindings -ts -i ./...
```

(`-ts -i` matches what `build/Taskfile.yml`'s `generate:bindings` task already runs during `wails3
dev`/`wails3 build` — running it standalone here just lets you inspect the output before wiring up
the Svelte side. `wails3 dev`/`wails3 build` will regenerate it again in Task 7 regardless.)

Expected output includes a line like `Processed: N Packages, 1 Service, 3 Methods, 0 Enums, 0
Models, 0 Events` and creates:
- `frontend/bindings/assistente-idiomas/services/setupservice.ts` — exports `IsFirstRun()`,
  `ChooseStorageFolder()`, `CompleteSetup(storageRoot, apiKey)`, each returning a
  `$CancellablePromise<T>` (a `Promise` subtype — plain `await` works).
- `frontend/bindings/assistente-idiomas/services/index.ts` — re-exports the above as
  `SetupService`.

If this step fails with a build error, it's almost certainly the same Linux GTK4/WebKitGTK
prerequisite noted in Global Constraints (the generator type-checks the whole Go module, so it
needs to compile `services/setup.go` too).

- [ ] **Step 2: Write `frontend/src/lib/SetupWizard.svelte`**

```svelte
<script lang="ts">
  import { colors, fonts } from "./theme";
  import * as SetupService from "../../bindings/assistente-idiomas/services/setupservice";

  let { onComplete }: { onComplete: () => void } = $props();

  type Step = "folder" | "credentials";
  let step: Step = $state("folder");
  let storageRoot: string = $state("");
  let apiKey: string = $state("");
  let error: string = $state("");
  let choosing: boolean = $state(false);
  let saving: boolean = $state(false);

  async function chooseFolder() {
    error = "";
    choosing = true;
    try {
      const dir = await SetupService.ChooseStorageFolder();
      if (dir) {
        storageRoot = dir;
        step = "credentials";
      }
    } catch (e) {
      error = String(e);
    } finally {
      choosing = false;
    }
  }

  async function complete() {
    error = "";
    saving = true;
    try {
      await SetupService.CompleteSetup(storageRoot, apiKey);
      onComplete();
    } catch (e) {
      error = String(e);
    } finally {
      saving = false;
    }
  }
</script>

<div class="wizard" style="background: {colors.bg}; font-family: {fonts.body}; color: {colors.text};">
  <div class="card" style="background: {colors.surface}; border: 1px solid {colors.line};">
    {#if step === "folder"}
      <h1 style="font-family: {fonts.display};">Onde ficam suas aulas?</h1>
      <p style="color: {colors.mut};">
        Escolha a pasta sincronizada (por exemplo, dentro do Google Drive) onde os vídeos
        importados serão guardados.
      </p>
      <button onclick={chooseFolder} disabled={choosing}>
        {choosing ? "Abrindo…" : "Escolher pasta"}
      </button>
    {:else}
      <h1 style="font-family: {fonts.display};">Pasta selecionada</h1>
      <p class="mono" style="color: {colors.mut};">{storageRoot}</p>
      <h2 style="font-family: {fonts.display};">Chave da API (ElevenLabs)</h2>
      <input type="password" bind:value={apiKey} placeholder="sk-..." />
      <button onclick={complete} disabled={saving || apiKey.length === 0}>
        {saving ? "Salvando…" : "Concluir"}
      </button>
    {/if}
    {#if error}
      <p class="error" style="color: {colors.red};">{error}</p>
    {/if}
  </div>
</div>

<style>
  .wizard {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 100%;
    height: 100vh;
  }
  .card {
    padding: 2rem;
    border-radius: 0.75rem;
    max-width: 28rem;
    width: 100%;
  }
  h1 {
    font-size: 1.25rem;
    margin: 0 0 0.5rem;
  }
  h2 {
    font-size: 1rem;
    margin: 1.5rem 0 0.5rem;
  }
  .mono {
    font-family: "JetBrains Mono", ui-monospace, monospace;
    font-size: 0.8rem;
    word-break: break-all;
  }
  input {
    width: 100%;
    padding: 0.5rem;
    margin-top: 0.5rem;
    box-sizing: border-box;
  }
  button {
    margin-top: 1rem;
    padding: 0.5rem 1rem;
    cursor: pointer;
  }
  .error {
    margin-top: 1rem;
    font-size: 0.85rem;
  }
</style>
```

- [ ] **Step 3: Modify `frontend/src/App.svelte`**

Current content (from Story 1):

```svelte
<script lang="ts">
  import Sidebar from "./lib/Sidebar.svelte";
  import Header from "./lib/Header.svelte";
  import Library from "./lib/screens/Library.svelte";
  import Progress from "./lib/screens/Progress.svelte";
  import Queue from "./lib/screens/Queue.svelte";
  import { colors, fonts } from "./lib/theme";

  type Screen = "library" | "progress" | "queue";

  let screen: Screen = $state("library");
</script>

<div class="shell" style="background: {colors.bg}; font-family: {fonts.body};">
  <Sidebar active={screen} onNavigate={(s) => (screen = s)} />
  <main class="main">
    <Header />
    <div class="content">
      {#if screen === "library"}
        <Library />
      {:else if screen === "progress"}
        <Progress />
      {:else}
        <Queue />
      {/if}
    </div>
  </main>
</div>

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

Replace it with:

```svelte
<script lang="ts">
  import { onMount } from "svelte";
  import Sidebar from "./lib/Sidebar.svelte";
  import Header from "./lib/Header.svelte";
  import Library from "./lib/screens/Library.svelte";
  import Progress from "./lib/screens/Progress.svelte";
  import Queue from "./lib/screens/Queue.svelte";
  import SetupWizard from "./lib/SetupWizard.svelte";
  import { colors, fonts } from "./lib/theme";
  import * as SetupService from "../bindings/assistente-idiomas/services/setupservice";

  type Screen = "library" | "progress" | "queue";

  let screen: Screen = $state("library");
  let checkingFirstRun = $state(true);
  let firstRun = $state(false);

  onMount(async () => {
    firstRun = await SetupService.IsFirstRun();
    checkingFirstRun = false;
  });
</script>

{#if checkingFirstRun}
  <div class="shell" style="background: {colors.bg};"></div>
{:else if firstRun}
  <SetupWizard onComplete={() => (firstRun = false)} />
{:else}
  <div class="shell" style="background: {colors.bg}; font-family: {fonts.body};">
    <Sidebar active={screen} onNavigate={(s) => (screen = s)} />
    <main class="main">
      <Header />
      <div class="content">
        {#if screen === "library"}
          <Library />
        {:else if screen === "progress"}
          <Progress />
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

- [ ] **Step 4: Verify with `svelte-check`**

```bash
cd frontend
npm run check
cd ..
```

Expected: `0 ERRORS 0 WARNINGS` (verified during planning with this exact component and import
paths).

- [ ] **Step 5: Verify the production build**

```bash
cd frontend
npm run build
cd ..
```

Expected: ends with `✓ built in ...ms`, produces `frontend/dist/`.

- [ ] **Step 6: Stage (do not commit — user controls commit timing)**

```bash
git add frontend/src/lib/SetupWizard.svelte frontend/src/App.svelte frontend/bindings
git status
```

---

### Task 7: Full build, manual verification, progress log

**Files:**
- Modify: `docs/phase-1-mvp.md` (progress table + checkboxes)

**Interfaces:**
- Consumes: everything from Tasks 1–6.
- Produces: nothing consumed by later tasks — this is the story's closing task.

- [ ] **Step 1: Full build**

```bash
wails3 build
```

Expected: succeeds, regenerates `frontend/bindings` (same content as Task 6, now via the full
Taskfile pipeline with `-clean=true`), produces `bin/assistente-idiomas.exe` (or the
platform-appropriate binary). This is the first full rebuild since Task 5's `main.go` and Task 6's
frontend changes landed together — a clean build here confirms nothing was left dangling.

- [ ] **Step 2: `go vet` and `gofmt` check on the whole module**

```bash
go vet ./...
gofmt -l .
```

Expected: no output from either.

- [ ] **Step 3: Full test suite**

```bash
go test ./...
```

Expected: all packages `ok` (the pre-existing `internal/stt` fixture tests from earlier stories
are unaffected by this story's changes).

- [ ] **Step 4: Manual visual verification (both machines — risk 3 area)**

```bash
wails3 dev
```

On a machine **without** a `config.json` yet (or with `%AppData%\assistente-idiomas\config.json` /
`~/.config/assistente-idiomas/config.json` removed for a clean test): the wizard should appear —
"Onde ficam suas aulas?" with a "Escolher pasta" button. Pick a real folder (ideally inside your
Google Drive sync folder), confirm it advances to the API key step showing the chosen path, enter
a real (or throwaway) ElevenLabs API key, click "Concluir". The app should then show the normal
sidebar shell (Library/Progress/Queue) from Story 1. Close and reopen the app (`wails3 dev`
again): the wizard should **not** reappear — confirms `IsFirstRun()` now returns false.

Repeat this on both the Windows and the Linux machine — this is exactly risk 3 from
`docs/phase-1-mvp.md` ("Keyring on Linux"): if the Linux machine's Secret Service isn't running,
`CompleteSetup` should show a clear error message on the credentials step (not crash, not silently
swallow it), and the wizard should stay on that step — the already-chosen folder path stays filled
in (it's still held in the Svelte component's local state), so the user only has to retry entering
the API key after fixing the environment, not re-pick the folder too. `config.json` itself isn't
written until `CompleteSetup` fully succeeds — that's the already-reviewed trade-off from the
design doc's first-run flow section, not a bug.

This step needs a real display and real credentials, so it's manual — not scriptable in this
environment.

- [ ] **Step 5: Update `docs/phase-1-mvp.md`**

Check all four boxes under `## Story 2` from `- [ ]` to `- [x]`.

In the `## Progress log` table, add a row (keep the existing Story 1 row above it):

```
| 21/07/2026 | Story 2 implemented: SQLite database (schema v1: lessons/transcripts/jobs/prompts, goose migrations), local config (config.json), and ElevenLabs credential via keyring, all in the first-run wizard | On Linux, `go build`/`go vet`/`go test` for any package importing Wails requires `CGO_ENABLED=1` + `libgtk-4-dev libwebkitgtk-6.0-dev` installed (gtk4/webkitgtk-6.0, not gtk3) — worth documenting/installing this early on a new Linux machine |
```

- [ ] **Step 6: Stage (do not commit — user controls commit timing)**

```bash
git add docs/phase-1-mvp.md
git status
```

Expected: `git status` shows every file from Tasks 1–7 staged, nothing left unstaged from this
story. Do not run `git commit`.

---

## Self-Review Notes

- **Spec coverage:** all four `docs/phase-1-mvp.md` Story 2 acceptance criteria map to tasks —
  SQLite/WAL outside the synced folder → Task 1 + Task 2's `AppDataDir`/`DBPath`; goose migrations
  with the 4-table schema v1 → Task 1; config local with first-run visual flow → Tasks 2, 4, 6, 7;
  keyring credential, never plaintext, tested Windows+Linux → Task 3 (unit) + Task 7 Step 4
  (manual, both machines).
- **Placeholder scan:** no TBDs; every code step has literal file content; every command step has
  a stated expected output, including the ones that depend on manual verification (explicitly
  marked as such, not hand-waved).
- **Type consistency:** `AppConfig.StorageRoot` (Task 2) is the only field ever written/read,
  consistently, by `services/setup.go` (Task 4) and nowhere else. `SetupService`'s three method
  signatures (Task 4) match exactly what Task 6's Svelte code calls, and match the actual
  generated `.ts` signatures confirmed by running the real generator during planning — not
  inferred from documentation.
- **Deviation from the approved design doc, noted deliberately:** the design doc
  (`docs/superpowers/specs/2026-07-21-story-2-database-config-design.md`) proposed wrapping
  `go-keyring` behind a small `secretStore` interface so tests could inject a fake. During
  planning, this turned out to be unnecessary: `go-keyring` ships its own `MockInit()` /
  `MockInitWithError(err)` test helpers, which cover exactly the same two scenarios (success, and
  "Secret Service unavailable") without adding an abstraction layer whose only consumer would ever
  be the real `go-keyring` package. Dropped in favor of using the library directly — same test
  coverage, less code, consistent with the project's stated preference against unnecessary
  abstractions (`CLAUDE.md`).
- **Deviation, also noted:** the design doc's example service placement was "`SetupService` em
  `app.go`" (explicitly marked "ex." — an example, not a commitment). Planning found that a Go
  `package main` service has an ambiguous/edge-case binding output path in the Wails v3 generator,
  so `SetupService` was placed in a new top-level `services/` package instead — verified via the
  real generator to produce a clean, predictable `frontend/bindings/assistente-idiomas/services/`
  output. This also gives future stories (Stories 3/4/7's import/jobs/queue services) an obvious,
  already-precedented home.
- **Environment discovery, not a design change:** building/vetting/testing any package that
  imports Wails on Linux requires `CGO_ENABLED=1` plus GTK4/WebKitGTK-6 dev headers
  (`libgtk-4-dev`, `libwebkitgtk-6.0-dev`) — discovered by actually hitting the missing-dependency
  build errors during planning, then installing the correct packages and re-verifying. This isn't
  in `docs/phase-1-mvp.md`'s risk list (which only calls out the *keyring* half of the Linux story,
  risk 3) — Task 7 Step 5 records it in the progress log so it's not rediscovered from scratch on
  the next new Linux machine.
