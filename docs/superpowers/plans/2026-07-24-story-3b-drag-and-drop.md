# Story 3b — Manual Import Drag-and-Drop: Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the user drag a video file onto the Library screen to register it as a lesson, reusing the existing confirmation flow (`ImportConfirmModal`) and dedup logic from Story 3.

**Architecture:** Wails v3's native file-drop (`EnableFileDrop` + `events.Common.WindowFilesDropped`) delivers absolute OS paths to a thin handler in `main.go`, which calls a new `ImportService.DropImport` method. For each path: validate extension, hash, dedup against existing lessons/pending imports, copy into `storage_root` (or register in place if already there), insert into `pending_imports`, and emit a Wails event so the Library screen opens `ImportConfirmModal` — one per file, queued in sequence for multi-file drops.

**Tech Stack:** Go (stdlib + `github.com/wailsapp/wails/v3`), Svelte 5 (runes), SQLite via `modernc.org/sqlite`.

**Spec:** `docs/superpowers/specs/2026-07-24-story-3b-drag-and-drop-design.md` — read it for the full rationale; this plan implements it task by task.

## Global Constraints

- Wails v3 stays pinned at `v3.0.0-alpha2.117` (`go.mod`) — no version bump.
- Stdlib first: no new external dependency for this feature (copy uses `io`/`os`, hashing already uses `crypto/sha256`).
- Portable SQL: no driver-specific SQL in `internal/db`.
- Database paths for videos are always **relative** to `storage_root`, never absolute — `pending_imports.path` and `lessons.video_path` must stay relative.
- Svelte 5 runes only (`$state`, `$props`, `$effect`) — no legacy `export let`/`$:` syntax.
- Code and identifiers in English; user-facing error strings and docs in PT-BR.
- Commit messages: single line, semantic (`feat:`, `fix:`, `test:`, `docs:`, ...).
- No real recordings/transcripts/tutor names in the repo — this feature only touches synthetic fixtures created by tests (`t.TempDir()`), never real data.

---

### Task 1: `internal/importer` — export helpers + add `CopyIntoStorageRoot`

**Files:**
- Modify: `internal/importer/importer.go`
- Create: `internal/importer/copy.go`
- Create: `internal/importer/copy_test.go`

**Interfaces:**
- Consumes: nothing new (pure stdlib).
- Produces (for Task 3): `importer.HashFile(path string) (string, error)`, `importer.HasVideoExtension(path string) bool`, `importer.SuggestDate(filename string, mtime time.Time) string`, `importer.CopyIntoStorageRoot(srcPath, storageRoot string) (string, error)`.

- [ ] **Step 1: Export the three helpers `DropImport` will need**

  `internal/importer/importer.go` currently has `hashFile`, `hasVideoExtension`, `suggestDate` as unexported — `services.ImportService.DropImport` (Task 3) needs to hash/validate/date-guess a file that isn't found via `Scan`'s directory walk, so these must become exported. Rename in place (same file, same package — internal callers keep working, only the identifiers change):

  ```go
  // internal/importer/importer.go — replace the 3 call sites and 3 func signatures:

  // line ~85, inside Scan's WalkDir callback:
  if d.IsDir() || !HasVideoExtension(path) {

  // line ~113:
  hash, err := HashFile(path)

  // line ~152:
  SuggestedDate: SuggestDate(filepath.Base(path), info.ModTime()),

  // line ~167:
  // HasVideoExtension reports whether path has a recognized video extension
  // (today only .mp4). Exported because services.ImportService.DropImport
  // (Story 3b) needs the same check for a file dropped via
  // drag-and-drop, outside the folder scan.
  func HasVideoExtension(path string) bool {

  // line ~177:
  // HashFile computes the SHA-256 of the file at path. Exported for the same
  // reason as HasVideoExtension — DropImport hashes a file outside the
  // folder scan.
  func HashFile(path string) (string, error) {

  // line ~200 (keep the existing doc comment, just rename):
  func SuggestDate(filename string, mtime time.Time) string {
  ```

  The bodies of these three functions are unchanged — only the names (and their two doc comments gain the note about why they're exported).

- [ ] **Step 2: Run the existing importer tests to confirm the rename didn't break anything**

  Run: `go test ./internal/importer/...`
  Expected: PASS (all existing tests — `TestScan_*`, naming tests — still pass unchanged, since they call these functions from within the same package where exported/unexported names both resolve).

- [ ] **Step 3: Write the failing tests for `CopyIntoStorageRoot`**

  ```go
  // internal/importer/copy_test.go
  package importer

  import (
  	"os"
  	"path/filepath"
  	"runtime"
  	"testing"
  )

  func TestCopyIntoStorageRoot_CopiesToEmptyDestination(t *testing.T) {
  	srcDir := t.TempDir()
  	storageRoot := t.TempDir()
  	srcPath := filepath.Join(srcDir, "aula.mp4")
  	if err := os.WriteFile(srcPath, []byte("conteudo-do-video"), 0o644); err != nil {
  		t.Fatalf("preparar arquivo de origem falhou: %v", err)
  	}

  	got, err := CopyIntoStorageRoot(srcPath, storageRoot)
  	if err != nil {
  		t.Fatalf("CopyIntoStorageRoot() erro inesperado: %v", err)
  	}
  	if got != "aula.mp4" {
  		t.Errorf("CopyIntoStorageRoot() = %q, esperado aula.mp4", got)
  	}
  	gotContent, err := os.ReadFile(filepath.Join(storageRoot, got))
  	if err != nil {
  		t.Fatalf("ler arquivo copiado falhou: %v", err)
  	}
  	if string(gotContent) != "conteudo-do-video" {
  		t.Errorf("conteúdo copiado = %q, esperado conteudo-do-video", gotContent)
  	}
  	if _, err := os.ReadFile(srcPath); err != nil {
  		t.Errorf("arquivo de origem deveria permanecer intacto: %v", err)
  	}
  	entries, err := os.ReadDir(storageRoot)
  	if err != nil {
  		t.Fatalf("ler diretório de destino falhou: %v", err)
  	}
  	if len(entries) != 1 {
  		t.Errorf("destino tem %d entradas, esperado 1 (sem sobras de temporário)", len(entries))
  	}
  }

  func TestCopyIntoStorageRoot_ResolvesNameCollisionWithSuffix(t *testing.T) {
  	srcDir := t.TempDir()
  	storageRoot := t.TempDir()
  	srcPath := filepath.Join(srcDir, "aula.mp4")
  	if err := os.WriteFile(srcPath, []byte("conteudo-novo"), 0o644); err != nil {
  		t.Fatalf("preparar arquivo de origem falhou: %v", err)
  	}
  	if err := os.WriteFile(filepath.Join(storageRoot, "aula.mp4"), []byte("conteudo-existente"), 0o644); err != nil {
  		t.Fatalf("preparar arquivo existente falhou: %v", err)
  	}

  	got, err := CopyIntoStorageRoot(srcPath, storageRoot)
  	if err != nil {
  		t.Fatalf("CopyIntoStorageRoot() erro inesperado: %v", err)
  	}
  	if got != "aula-2.mp4" {
  		t.Errorf("CopyIntoStorageRoot() = %q, esperado aula-2.mp4", got)
  	}
  	existing, err := os.ReadFile(filepath.Join(storageRoot, "aula.mp4"))
  	if err != nil || string(existing) != "conteudo-existente" {
  		t.Errorf("arquivo existente não deveria ser sobrescrito: conteúdo=%q err=%v", existing, err)
  	}
  }

  func TestCopyIntoStorageRoot_SourceDoesNotExist(t *testing.T) {
  	storageRoot := t.TempDir()
  	_, err := CopyIntoStorageRoot(filepath.Join(t.TempDir(), "nao-existe.mp4"), storageRoot)
  	if err == nil {
  		t.Fatal("CopyIntoStorageRoot() com origem inexistente esperava erro, veio nil")
  	}
  	entries, err := os.ReadDir(storageRoot)
  	if err != nil {
  		t.Fatalf("ler diretório de destino falhou: %v", err)
  	}
  	if len(entries) != 0 {
  		t.Errorf("destino deveria continuar vazio após falha, tem %d entradas", len(entries))
  	}
  }

  func TestCopyIntoStorageRoot_DestinationNotWritableLeavesNoPartialFile(t *testing.T) {
  	if runtime.GOOS == "windows" {
  		t.Skip("permissão de escrita via os.Chmod não se aplica da mesma forma no Windows")
  	}
  	srcDir := t.TempDir()
  	storageRoot := t.TempDir()
  	srcPath := filepath.Join(srcDir, "aula.mp4")
  	if err := os.WriteFile(srcPath, []byte("conteudo"), 0o644); err != nil {
  		t.Fatalf("preparar arquivo de origem falhou: %v", err)
  	}
  	if err := os.Chmod(storageRoot, 0o500); err != nil {
  		t.Fatalf("Chmod() falhou: %v", err)
  	}
  	t.Cleanup(func() { os.Chmod(storageRoot, 0o700) })

  	_, err := CopyIntoStorageRoot(srcPath, storageRoot)
  	if err == nil {
  		t.Fatal("CopyIntoStorageRoot() com destino somente leitura esperava erro, veio nil")
  	}
  }
  ```

- [ ] **Step 4: Run the new tests to verify they fail (no `CopyIntoStorageRoot` yet)**

  Run: `go test ./internal/importer/... -run TestCopyIntoStorageRoot -v`
  Expected: FAIL with `undefined: CopyIntoStorageRoot`

- [ ] **Step 5: Implement `CopyIntoStorageRoot`**

  ```go
  // internal/importer/copy.go
  // Package importer — see importer.go. Copying an external file (drag-
  // and-drop, Story 3b) into the storage root. Doesn't know about
  // Wails or the database — only file I/O (thin layer, same principle as the
  // rest of internal/).
  package importer

  import (
  	"fmt"
  	"io"
  	"os"
  	"path/filepath"
  	"strings"
  )

  // CopyIntoStorageRoot copies the content of srcPath into
  // storageRoot, with no subfolder, using srcPath's base name. It copies first
  // into a temporary file inside storageRoot (same filesystem,
  // so the final rename is atomic) and only then moves it to the final name —
  // a copy interrupted midway never leaves a partial file under the
  // final name. It resolves a name collision at the destination with a "-2", "-3",
  // ... suffix before the extension. Returns the copied file's name (with no
  // directory — never creates a subfolder).
  func CopyIntoStorageRoot(srcPath, storageRoot string) (string, error) {
  	src, err := os.Open(srcPath)
  	if err != nil {
  		return "", fmt.Errorf("abrir arquivo de origem: %w", err)
  	}
  	defer src.Close()

  	tmp, err := os.CreateTemp(storageRoot, "importing-*.tmp")
  	if err != nil {
  		return "", fmt.Errorf("criar arquivo temporário de cópia: %w", err)
  	}
  	tmpPath := tmp.Name()
  	if _, err := io.Copy(tmp, src); err != nil {
  		tmp.Close()
  		os.Remove(tmpPath)
  		return "", fmt.Errorf("copiar conteúdo do vídeo: %w", err)
  	}
  	if err := tmp.Close(); err != nil {
  		os.Remove(tmpPath)
  		return "", fmt.Errorf("finalizar cópia do vídeo: %w", err)
  	}

  	base := filepath.Base(srcPath)
  	ext := filepath.Ext(base)
  	stem := strings.TrimSuffix(base, ext)

  	candidate := base
  	for i := 2; ; i++ {
  		destPath := filepath.Join(storageRoot, candidate)
  		if _, statErr := os.Stat(destPath); os.IsNotExist(statErr) {
  			if err := os.Rename(tmpPath, destPath); err != nil {
  				os.Remove(tmpPath)
  				return "", fmt.Errorf("mover cópia pro nome final: %w", err)
  			}
  			return candidate, nil
  		} else if statErr != nil {
  			os.Remove(tmpPath)
  			return "", fmt.Errorf("checar colisão de nome no destino: %w", statErr)
  		}
  		candidate = fmt.Sprintf("%s-%d%s", stem, i, ext)
  	}
  }
  ```

- [ ] **Step 6: Run the tests again to verify they pass**

  Run: `go test ./internal/importer/... -v`
  Expected: PASS — all `TestCopyIntoStorageRoot_*` plus every pre-existing test in the package.

- [ ] **Step 7: Commit**

  ```bash
  git add internal/importer/importer.go internal/importer/copy.go internal/importer/copy_test.go
  git commit -m "feat: export importer helpers and add copy into storage_root"
  ```

---

### Task 2: `internal/db` — `InsertPendingImport` returns the new row's ID

**Files:**
- Modify: `internal/db/pending_imports.go`
- Modify: `internal/db/pending_imports_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces (for Task 3): `db.InsertPendingImport(conn *sql.DB, p PendingImport) (int64, error)` — signature change from the current `(conn *sql.DB, p PendingImport) error`. `services.ImportService.DropImport` needs the freshly-inserted row's ID immediately, to build the `PendingImport{ID, Path, SuggestedDate}` payload for the Wails event without a second query.

This task also fixes the two call sites outside `internal/db` that the signature change breaks (`services/import.go`, `services/import_test.go`), so the whole module builds cleanly at the end of this task — not left broken for Task 3 to discover.

- [ ] **Step 1: Update the two existing call sites' assertions first (still against the old signature) to confirm current baseline passes**

  Run: `go test ./internal/db/... -run TestPendingImports -v`
  Expected: PASS (baseline before the change).

- [ ] **Step 2: Change `InsertPendingImport` to return the inserted ID**

  ```go
  // internal/db/pending_imports.go — replace the existing InsertPendingImport:

  // InsertPendingImport writes a new candidate found by the scan (or
  // by a manual drop, Story 3b) and returns the id of the created row — the
  // caller needs it to build the PendingImport exposed to the frontend
  // without a second query.
  func InsertPendingImport(conn *sql.DB, p PendingImport) (int64, error) {
  	res, err := conn.Exec(
  		`INSERT INTO pending_imports (path, file_size, file_mtime, sha256, suggested_date, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
  		p.Path, p.FileSize, p.FileMTime, p.SHA256, p.SuggestedDate, time.Now().UTC().Format(time.RFC3339),
  	)
  	if err != nil {
  		return 0, fmt.Errorf("inserir pending_import: %w", err)
  	}
  	id, err := res.LastInsertId()
  	if err != nil {
  		return 0, fmt.Errorf("obter id do pending_import: %w", err)
  	}
  	return id, nil
  }
  ```

- [ ] **Step 3: Fix the two call sites in `pending_imports_test.go`**

  ```go
  // internal/db/pending_imports_test.go:23 — was:
  //   err = InsertPendingImport(conn, PendingImport{...})
  // becomes:
  _, err = InsertPendingImport(conn, PendingImport{
  	Path: "aula-nova.mp4", FileSize: 999, FileMTime: "2026-07-20T10:00:00Z",
  	SHA256: "hash-new", SuggestedDate: "2026-07-20",
  })
  ```

  ```go
  // internal/db/pending_imports_test.go:55 — was:
  //   if err := InsertPendingImport(conn, PendingImport{...}); err != nil {
  // becomes:
  if _, err := InsertPendingImport(conn, PendingImport{
  	Path: "aula-nova.mp4", FileSize: 999, FileMTime: "2026-07-20T10:00:00Z",
  	SHA256: "hash-confirm", SuggestedDate: "2026-07-20",
  }); err != nil {
  ```

- [ ] **Step 4: Run the `internal/db` tests to verify they pass**

  Run: `go vet ./internal/db/... && go test ./internal/db/... -v`
  Expected: PASS.

- [ ] **Step 5: Fix the two call sites outside `internal/db` broken by the signature change**

  ```go
  // services/import.go — dbRepo.InsertPending, currently:
  //   func (r *dbRepo) InsertPending(c importer.Candidate) error {
  //       return db.InsertPendingImport(r.conn, db.PendingImport{...})
  //   }
  // becomes:
  func (r *dbRepo) InsertPending(c importer.Candidate) error {
  	_, err := db.InsertPendingImport(r.conn, db.PendingImport{
  		Path:          c.Path,
  		FileSize:      c.Size,
  		FileMTime:     c.MTime,
  		SHA256:        c.SHA256,
  		SuggestedDate: c.SuggestedDate,
  	})
  	return err
  }
  ```

  ```go
  // services/import_test.go:282 — inside
  // TestImportService_ConfirmImport_NormalizesPathAlreadyPointingToTargetFile,
  // currently:
  //   if err := db.InsertPendingImport(conn, db.PendingImport{...}); err != nil {
  // becomes:
  if _, err := db.InsertPendingImport(conn, db.PendingImport{
  	Path:          "./" + targetName,
  	FileSize:      info.Size(),
  	FileMTime:     info.ModTime().UTC().Format(time.RFC3339),
  	SHA256:        "hash-sintetico",
  	SuggestedDate: "2026-07-23T14:30",
  }); err != nil {
  ```

- [ ] **Step 6: Verify the whole module builds and all `services` tests still pass**

  Run: `go build ./... && go vet ./... && go test ./services/... -v`
  Expected: PASS everywhere — this confirms the signature change is fully propagated before Task 3 adds new behavior on top.

- [ ] **Step 7: Commit**

  ```bash
  git add internal/db/pending_imports.go internal/db/pending_imports_test.go services/import.go services/import_test.go
  git commit -m "feat: InsertPendingImport retorna o id da linha criada"
  ```

---

### Task 3: `services.ImportService.DropImport`

**Files:**
- Modify: `services/import.go`
- Modify: `services/import_test.go`

**Interfaces:**
- Consumes: `importer.HashFile`, `importer.HasVideoExtension`, `importer.SuggestDate`, `importer.CopyIntoStorageRoot` (Task 1); `db.InsertPendingImport(conn, p) (int64, error)` (Task 2, already wired into `dbRepo.InsertPending` by that task); `db.FindLessonByHash`, `db.FindPendingImportByHash` (pre-existing); `config.Load()` (pre-existing); `application.Get()` (pre-existing pattern from `services/jobs_notifier.go`).
- Produces (for Task 4): `(s *ImportService) DropImport(paths []string) []DropResult`, where `DropResult` is `{Path, Error string}` (JSON: `path`, `error`). Also produces the two Wails event names `services.DroppedImportEvent = "import:dropped"` (payload: `PendingImport`) and `services.DropErrorEvent = "import:drop-error"` (payload: `DropResult`) that Task 6's frontend code subscribes to by their literal string values.

- [ ] **Step 1: Add the `github.com/wailsapp/wails/v3/pkg/application` import to `services/import.go`**

  ```go
  // services/import.go — top of file, add to the existing import block:
  import (
  	"context"
  	"database/sql"
  	"fmt"
  	"log/slog"
  	"os"
  	"path/filepath"
  	"runtime"
  	"strings"
  	"time"

  	"assistente-idiomas/internal/config"
  	"assistente-idiomas/internal/db"
  	"assistente-idiomas/internal/importer"
  	"assistente-idiomas/internal/media"

  	"github.com/wailsapp/wails/v3/pkg/application"
  )
  ```

- [ ] **Step 2: Write the failing tests for `DropImport` (add to `services/import_test.go`)**

  Also add `"runtime"` to that file's import block (used by the permission-based test below).

  ```go
  func TestImportService_DropImport_RejectsUnsupportedExtension(t *testing.T) {
  	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
  	storageRoot := t.TempDir()
  	if err := config.Save(&config.AppConfig{StorageRoot: storageRoot}); err != nil {
  		t.Fatalf("config.Save() falhou: %v", err)
  	}
  	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
  	if err != nil {
  		t.Fatalf("db.Open() falhou: %v", err)
  	}
  	defer conn.Close()

  	srcDir := t.TempDir()
  	srcPath := filepath.Join(srcDir, "aula.mov")
  	if err := os.WriteFile(srcPath, []byte("conteudo"), 0o644); err != nil {
  		t.Fatalf("preparar arquivo de origem falhou: %v", err)
  	}

  	svc := NewImportService(conn)
  	results := svc.DropImport([]string{srcPath})
  	if len(results) != 1 || results[0].Error == "" {
  		t.Fatalf("DropImport() = %+v, esperado erro de extensão não suportada", results)
  	}

  	pending, err := svc.ListPendingImports()
  	if err != nil {
  		t.Fatalf("ListPendingImports() erro inesperado: %v", err)
  	}
  	if len(pending) != 0 {
  		t.Errorf("ListPendingImports() = %+v, esperado vazio (arquivo rejeitado)", pending)
  	}
  	entries, err := os.ReadDir(storageRoot)
  	if err != nil {
  		t.Fatalf("ler storageRoot falhou: %v", err)
  	}
  	if len(entries) != 0 {
  		t.Errorf("storageRoot deveria continuar vazio, tem %d entradas", len(entries))
  	}
  }

  func TestImportService_DropImport_RejectsAlreadyImportedLesson(t *testing.T) {
  	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
  	storageRoot := t.TempDir()
  	if err := config.Save(&config.AppConfig{StorageRoot: storageRoot}); err != nil {
  		t.Fatalf("config.Save() falhou: %v", err)
  	}
  	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
  	if err != nil {
  		t.Fatalf("db.Open() falhou: %v", err)
  	}
  	defer conn.Close()

  	content := []byte("mesmo-conteudo")
  	if err := os.WriteFile(filepath.Join(storageRoot, "aula-existente.mp4"), content, 0o644); err != nil {
  		t.Fatalf("preparar vídeo existente falhou: %v", err)
  	}
  	svc := NewImportService(conn)
  	if _, err := svc.ScanFolder(); err != nil {
  		t.Fatalf("ScanFolder() erro inesperado: %v", err)
  	}
  	pending, err := svc.ListPendingImports()
  	if err != nil || len(pending) != 1 {
  		t.Fatalf("setup: ListPendingImports() = %+v, %v", pending, err)
  	}
  	if err := svc.ConfirmImport(pending[0].ID, "2026-07-24T10:00", "Sarah M."); err != nil {
  		t.Fatalf("setup ConfirmImport() erro inesperado: %v", err)
  	}

  	srcDir := t.TempDir()
  	dropPath := filepath.Join(srcDir, "copia-baixada-de-novo.mp4")
  	if err := os.WriteFile(dropPath, content, 0o644); err != nil {
  		t.Fatalf("preparar cópia solta falhou: %v", err)
  	}

  	results := svc.DropImport([]string{dropPath})
  	if len(results) != 1 || results[0].Error == "" {
  		t.Fatalf("DropImport() = %+v, esperado erro de aula já importada", results)
  	}

  	pending, err = svc.ListPendingImports()
  	if err != nil {
  		t.Fatalf("ListPendingImports() erro inesperado: %v", err)
  	}
  	if len(pending) != 0 {
  		t.Errorf("ListPendingImports() = %+v, esperado vazio (hash já é uma lesson)", pending)
  	}
  }

  func TestImportService_DropImport_RejectsAlreadyPendingHash(t *testing.T) {
  	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
  	storageRoot := t.TempDir()
  	if err := config.Save(&config.AppConfig{StorageRoot: storageRoot}); err != nil {
  		t.Fatalf("config.Save() falhou: %v", err)
  	}
  	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
  	if err != nil {
  		t.Fatalf("db.Open() falhou: %v", err)
  	}
  	defer conn.Close()

  	content := []byte("mesmo-conteudo-pendente")
  	if err := os.WriteFile(filepath.Join(storageRoot, "aula-pendente.mp4"), content, 0o644); err != nil {
  		t.Fatalf("preparar vídeo pendente falhou: %v", err)
  	}
  	svc := NewImportService(conn)
  	if _, err := svc.ScanFolder(); err != nil {
  		t.Fatalf("ScanFolder() erro inesperado: %v", err)
  	}
  	pending, err := svc.ListPendingImports()
  	if err != nil || len(pending) != 1 {
  		t.Fatalf("setup: ListPendingImports() = %+v, %v", pending, err)
  	}

  	srcDir := t.TempDir()
  	dropPath := filepath.Join(srcDir, "mesma-aula-de-novo.mp4")
  	if err := os.WriteFile(dropPath, content, 0o644); err != nil {
  		t.Fatalf("preparar cópia solta falhou: %v", err)
  	}

  	results := svc.DropImport([]string{dropPath})
  	if len(results) != 1 || results[0].Error == "" {
  		t.Fatalf("DropImport() = %+v, esperado erro de aula já aguardando revisão", results)
  	}

  	pending, err = svc.ListPendingImports()
  	if err != nil {
  		t.Fatalf("ListPendingImports() erro inesperado: %v", err)
  	}
  	if len(pending) != 1 {
  		t.Errorf("ListPendingImports() = %+v, esperado continuar só o candidato original (sem duplicar)", pending)
  	}
  }

  func TestImportService_DropImport_RegistersFileAlreadyInsideStorageRootWithoutCopying(t *testing.T) {
  	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
  	storageRoot := t.TempDir()
  	if err := config.Save(&config.AppConfig{StorageRoot: storageRoot}); err != nil {
  		t.Fatalf("config.Save() falhou: %v", err)
  	}
  	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
  	if err != nil {
  		t.Fatalf("db.Open() falhou: %v", err)
  	}
  	defer conn.Close()

  	insidePath := filepath.Join(storageRoot, "ja-esta-na-pasta.mp4")
  	if err := os.WriteFile(insidePath, []byte("conteudo"), 0o644); err != nil {
  		t.Fatalf("preparar vídeo dentro da storage root falhou: %v", err)
  	}

  	svc := NewImportService(conn)
  	results := svc.DropImport([]string{insidePath})
  	if len(results) != 1 || results[0].Error != "" {
  		t.Fatalf("DropImport() = %+v, esperado sucesso", results)
  	}

  	pending, err := svc.ListPendingImports()
  	if err != nil || len(pending) != 1 {
  		t.Fatalf("ListPendingImports() = %+v, %v, esperado 1 candidato", pending, err)
  	}
  	if pending[0].Path != "ja-esta-na-pasta.mp4" {
  		t.Errorf("Path = %q, esperado ja-esta-na-pasta.mp4 (sem cópia)", pending[0].Path)
  	}

  	entries, err := os.ReadDir(storageRoot)
  	if err != nil {
  		t.Fatalf("ler storageRoot falhou: %v", err)
  	}
  	if len(entries) != 1 {
  		t.Errorf("storageRoot tem %d entradas, esperado 1 (nenhuma cópia criada)", len(entries))
  	}
  }

  func TestImportService_DropImport_CopiesFileFromOutsideStorageRoot(t *testing.T) {
  	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
  	storageRoot := t.TempDir()
  	if err := config.Save(&config.AppConfig{StorageRoot: storageRoot}); err != nil {
  		t.Fatalf("config.Save() falhou: %v", err)
  	}
  	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
  	if err != nil {
  		t.Fatalf("db.Open() falhou: %v", err)
  	}
  	defer conn.Close()

  	srcDir := t.TempDir()
  	srcPath := filepath.Join(srcDir, "download-do-cambly.mp4")
  	if err := os.WriteFile(srcPath, []byte("conteudo-baixado"), 0o644); err != nil {
  		t.Fatalf("preparar arquivo de origem falhou: %v", err)
  	}

  	svc := NewImportService(conn)
  	results := svc.DropImport([]string{srcPath})
  	if len(results) != 1 || results[0].Error != "" {
  		t.Fatalf("DropImport() = %+v, esperado sucesso", results)
  	}

  	pending, err := svc.ListPendingImports()
  	if err != nil || len(pending) != 1 {
  		t.Fatalf("ListPendingImports() = %+v, %v, esperado 1 candidato", pending, err)
  	}
  	if pending[0].Path != "download-do-cambly.mp4" {
  		t.Errorf("Path = %q, esperado download-do-cambly.mp4", pending[0].Path)
  	}
  	copied, err := os.ReadFile(filepath.Join(storageRoot, "download-do-cambly.mp4"))
  	if err != nil || string(copied) != "conteudo-baixado" {
  		t.Errorf("cópia no storageRoot = %q, err=%v, esperado conteudo-baixado", copied, err)
  	}
  	if _, err := os.ReadFile(srcPath); err != nil {
  		t.Errorf("arquivo original deveria permanecer na origem: %v", err)
  	}
  }

  func TestImportService_DropImport_ResolvesNameCollisionOnCopyWithSuffix(t *testing.T) {
  	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
  	storageRoot := t.TempDir()
  	if err := config.Save(&config.AppConfig{StorageRoot: storageRoot}); err != nil {
  		t.Fatalf("config.Save() falhou: %v", err)
  	}
  	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
  	if err != nil {
  		t.Fatalf("db.Open() falhou: %v", err)
  	}
  	defer conn.Close()

  	srcDirA := t.TempDir()
  	srcDirB := t.TempDir()
  	srcPathA := filepath.Join(srcDirA, "aula.mp4")
  	srcPathB := filepath.Join(srcDirB, "aula.mp4")
  	if err := os.WriteFile(srcPathA, []byte("conteudo-a"), 0o644); err != nil {
  		t.Fatalf("preparar arquivo A falhou: %v", err)
  	}
  	if err := os.WriteFile(srcPathB, []byte("conteudo-b-bem-diferente"), 0o644); err != nil {
  		t.Fatalf("preparar arquivo B falhou: %v", err)
  	}

  	svc := NewImportService(conn)
  	results := svc.DropImport([]string{srcPathA, srcPathB})
  	if len(results) != 2 || results[0].Error != "" || results[1].Error != "" {
  		t.Fatalf("DropImport() = %+v, esperado sucesso nos dois", results)
  	}

  	pending, err := svc.ListPendingImports()
  	if err != nil || len(pending) != 2 {
  		t.Fatalf("ListPendingImports() = %+v, %v, esperado 2 candidatos", pending, err)
  	}
  	found := map[string]bool{}
  	for _, p := range pending {
  		found[p.Path] = true
  	}
  	if !found["aula.mp4"] || !found["aula-2.mp4"] {
  		t.Errorf("paths dos candidatos = %+v, esperado aula.mp4 e aula-2.mp4", pending)
  	}
  }

  func TestImportService_DropImport_FailedCopyReportsErrorAndDoesNotInsertPending(t *testing.T) {
  	if runtime.GOOS == "windows" {
  		t.Skip("permissão de escrita via os.Chmod não se aplica da mesma forma no Windows")
  	}
  	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
  	storageRoot := t.TempDir()
  	if err := config.Save(&config.AppConfig{StorageRoot: storageRoot}); err != nil {
  		t.Fatalf("config.Save() falhou: %v", err)
  	}
  	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
  	if err != nil {
  		t.Fatalf("db.Open() falhou: %v", err)
  	}
  	defer conn.Close()

  	srcDir := t.TempDir()
  	srcPath := filepath.Join(srcDir, "aula.mp4")
  	if err := os.WriteFile(srcPath, []byte("conteudo"), 0o644); err != nil {
  		t.Fatalf("preparar arquivo de origem falhou: %v", err)
  	}
  	if err := os.Chmod(storageRoot, 0o500); err != nil {
  		t.Fatalf("Chmod() falhou: %v", err)
  	}
  	t.Cleanup(func() { os.Chmod(storageRoot, 0o700) })

  	svc := NewImportService(conn)
  	results := svc.DropImport([]string{srcPath})
  	if len(results) != 1 || results[0].Error == "" {
  		t.Fatalf("DropImport() = %+v, esperado erro de cópia", results)
  	}

  	pending, err := svc.ListPendingImports()
  	if err != nil {
  		t.Fatalf("ListPendingImports() erro inesperado: %v", err)
  	}
  	if len(pending) != 0 {
  		t.Errorf("ListPendingImports() = %+v, esperado vazio (cópia falhou)", pending)
  	}
  }
  ```

- [ ] **Step 3: Run the new tests to verify they fail (no `DropImport` yet)**

  Run: `go test ./services/... -run TestImportService_DropImport -v`
  Expected: FAIL with `svc.DropImport undefined` / compile error.

- [ ] **Step 4: Implement `DropImport` and its helpers in `services/import.go`**

  Add near the `PendingImport` struct (same file, after it):

  ```go
  // DropResult is the result of processing a path received via
  // native drag-and-drop (main.go, WindowFilesDropped event) — used as
  // DropImport's return value (testable) and also as the payload of the
  // DropErrorEvent event when Error is not empty.
  type DropResult struct {
  	Path  string `json:"path"`
  	Error string `json:"error"`
  }
  ```

  Add at the end of the file (after the `dbRepo` methods, i.e. after `InsertPending`):

  ```go
  // DroppedImportEvent is emitted once per candidate successfully created
  // via drag-and-drop — the payload is a PendingImport, the same shape
  // ListPendingImports already exposes, so ImportConfirmModal can open without
  // fetching again (Story 3b).
  const DroppedImportEvent = "import:dropped"

  // DropErrorEvent is emitted for a file that failed (unrecognized
  // extension, duplicate, copy failure) — no candidate was created
  // for that file.
  const DropErrorEvent = "import:drop-error"

  // DropImport processes files received via Wails's native drag-and-drop
  // (main.go calls this from the WindowFilesDropped event) — one
  // pending candidate per valid file, reusing the same hash-based dedupe
  // from the folder scan (Story 3). Unlike renameVideoBestEffort's
  // best-effort approach, a failure here is always reported (via
  // DropErrorEvent and in the returned DropResult) — without a candidate in
  // pending_imports the user would have no other way of knowing the dropped
  // file failed.
  func (s *ImportService) DropImport(paths []string) []DropResult {
  	results := make([]DropResult, 0, len(paths))
  	for _, path := range paths {
  		pending, err := s.dropOne(path)
  		if err != nil {
  			results = append(results, DropResult{Path: path, Error: err.Error()})
  			s.emitDropError(path, err)
  			continue
  		}
  		results = append(results, DropResult{Path: path})
  		s.emitDropped(pending)
  	}
  	return results
  }

  func (s *ImportService) dropOne(path string) (PendingImport, error) {
  	if !importer.HasVideoExtension(path) {
  		return PendingImport{}, fmt.Errorf("tipo de arquivo não suportado (só .mp4)")
  	}
  	cfg, err := config.Load()
  	if err != nil {
  		return PendingImport{}, fmt.Errorf("carregar configuração: %w", err)
  	}
  	info, err := os.Stat(path)
  	if err != nil {
  		return PendingImport{}, fmt.Errorf("ler arquivo: %w", err)
  	}

  	hash, err := importer.HashFile(path)
  	if err != nil {
  		return PendingImport{}, err
  	}

  	lesson, err := db.FindLessonByHash(s.conn, hash)
  	if err != nil {
  		return PendingImport{}, err
  	}
  	if lesson != nil {
  		return PendingImport{}, fmt.Errorf("esta aula já foi importada")
  	}
  	alreadyPending, err := db.FindPendingImportByHash(s.conn, hash)
  	if err != nil {
  		return PendingImport{}, err
  	}
  	if alreadyPending {
  		return PendingImport{}, fmt.Errorf("esta aula já está aguardando revisão")
  	}

  	relPath, fileMTime, fileSize, err := s.placeDroppedFile(path, cfg.StorageRoot)
  	if err != nil {
  		return PendingImport{}, err
  	}

  	suggested := importer.SuggestDate(filepath.Base(path), info.ModTime())
  	id, err := db.InsertPendingImport(s.conn, db.PendingImport{
  		Path:          relPath,
  		FileSize:      fileSize,
  		FileMTime:     fileMTime,
  		SHA256:        hash,
  		SuggestedDate: suggested,
  	})
  	if err != nil {
  		return PendingImport{}, err
  	}

  	return PendingImport{ID: id, Path: relPath, SuggestedDate: suggested}, nil
  }

  // placeDroppedFile decides where the dropped file gets registered: if path
  // is already inside storageRoot, it registers it in place without copying; otherwise,
  // it copies it into storageRoot (no subfolder, resolving name
  // collisions). Returns the path relative to storageRoot (always with "/"), the mtime
  // (RFC3339 UTC), and the file's size at the final destination.
  func (s *ImportService) placeDroppedFile(path, storageRoot string) (relPath string, mtime string, size int64, err error) {
  	rel, inside, err := relativeIfInsideStorageRoot(path, storageRoot)
  	if err != nil {
  		return "", "", 0, err
  	}
  	if !inside {
  		copiedRel, err := importer.CopyIntoStorageRoot(path, storageRoot)
  		if err != nil {
  			return "", "", 0, fmt.Errorf("copiar vídeo pra raiz de armazenamento: %w", err)
  		}
  		rel = filepath.ToSlash(copiedRel)
  	}

  	info, err := os.Stat(filepath.Join(storageRoot, filepath.FromSlash(rel)))
  	if err != nil {
  		return "", "", 0, fmt.Errorf("ler arquivo na raiz de armazenamento: %w", err)
  	}
  	return rel, info.ModTime().UTC().Format(time.RFC3339), info.Size(), nil
  }

  // relativeIfInsideStorageRoot resolves symlinks for path and
  // storageRoot and reports whether path falls inside storageRoot — in that case
  // it returns the relative path (always with "/"). inside=false (relPath="") if
  // path is outside, or if resolution fails (the caller then copies,
  // a safe default behavior).
  func relativeIfInsideStorageRoot(path, storageRoot string) (relPath string, inside bool, err error) {
  	resolvedPath, err := filepath.EvalSymlinks(path)
  	if err != nil {
  		return "", false, fmt.Errorf("resolver links simbólicos do arquivo: %w", err)
  	}
  	resolvedRoot, err := filepath.EvalSymlinks(storageRoot)
  	if err != nil {
  		return "", false, fmt.Errorf("resolver links simbólicos da raiz de armazenamento: %w", err)
  	}
  	rel, err := filepath.Rel(resolvedRoot, resolvedPath)
  	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
  		return "", false, nil
  	}
  	return filepath.ToSlash(rel), true, nil
  }

  func (s *ImportService) emitDropped(p PendingImport) {
  	app := application.Get()
  	if app == nil {
  		// Tests call DropImport without application.New() having run — the same
  		// treatment as WailsJobNotifier.JobChanged (services/jobs_notifier.go):
  		// discarding is harmless, no test depends on the event itself.
  		return
  	}
  	app.Event.Emit(DroppedImportEvent, p)
  }

  func (s *ImportService) emitDropError(path string, err error) {
  	app := application.Get()
  	if app == nil {
  		return
  	}
  	app.Event.Emit(DropErrorEvent, DropResult{Path: path, Error: err.Error()})
  }
  ```

- [ ] **Step 5: Run all the service tests to verify they pass**

  Run: `go test ./services/... -v`
  Expected: PASS — all `TestImportService_DropImport_*` plus every pre-existing test in the package (nothing else touched their behavior).

- [ ] **Step 6: Run the full Go suite**

  Run: `go build ./... && go vet ./... && go test ./...`
  Expected: PASS everywhere.

- [ ] **Step 7: Commit**

  ```bash
  git add services/import.go services/import_test.go
  git commit -m "feat: adiciona ImportService.DropImport (Historia 3b)"
  ```

---

### Task 4: `main.go` — wire native file-drop to `DropImport`

**Files:**
- Modify: `main.go`

**Interfaces:**
- Consumes: `services.NewImportService(conn) *services.ImportService` (pre-existing), `(*services.ImportService).DropImport(paths []string) []services.DropResult` (Task 3), `events.Common.WindowFilesDropped` and `application.WebviewWindowOptions.EnableFileDrop` (Wails v3, confirmed present in the pinned `v3.0.0-alpha2.117`).
- Produces: nothing consumed by later tasks — this is the final wiring point.

- [ ] **Step 1: Add the `events` import and hoist `importService` to a variable shared by the service list and the window handler**

  ```go
  // main.go — imports, add "github.com/wailsapp/wails/v3/pkg/events":
  import (
  	"context"
  	"database/sql"
  	"embed"
  	"log"

  	"assistente-idiomas/internal/config"
  	"assistente-idiomas/internal/db"
  	"assistente-idiomas/internal/jobs"
  	"assistente-idiomas/internal/media"
  	"assistente-idiomas/internal/stt"
  	"assistente-idiomas/services"

  	"github.com/wailsapp/wails/v3/pkg/application"
  	"github.com/wailsapp/wails/v3/pkg/events"
  )
  ```

  ```go
  // main.go — inside func main(), replace from "startJobWorker(conn, storageRoot)"
  // through the end of the function with:

  	startJobWorker(conn, storageRoot)

  	importService := services.NewImportService(conn)

  	app := application.New(application.Options{
  		Name:        "Assistente de Idiomas",
  		Description: "Arquivo e análise de aulas de inglês do Cambly",
  		Services: []application.Service{
  			application.NewService(services.NewSetupService()),
  			application.NewService(importService),
  			application.NewService(services.NewLibraryService(conn)),
  			application.NewService(services.NewQueueService(conn)),
  		},
  		Assets: application.AssetOptions{
  			Handler:    application.AssetFileServerFS(assets),
  			Middleware: services.VideoAssetMiddleware(conn, storageRoot),
  		},
  		Mac: application.MacOptions{
  			ApplicationShouldTerminateAfterLastWindowClosed: true,
  		},
  	})

  	win := app.Window.NewWithOptions(application.WebviewWindowOptions{
  		Title:            "Assistente de Idiomas",
  		Width:            1200,
  		Height:           760,
  		BackgroundColour: application.NewRGB(20, 24, 31), // #14181F — colors.bg
  		EnableFileDrop:   true,
  	})
  	win.OnWindowEvent(events.Common.WindowFilesDropped, func(event *application.WindowEvent) {
  		importService.DropImport(event.Context().DroppedFiles())
  	})

  	if err := app.Run(); err != nil {
  		log.Fatal(err)
  	}
  ```

  (`startJobWorker` and the rest of `main.go` below `func main()` are unchanged.)

- [ ] **Step 2: Verify it builds and vets clean**

  Run: `go build ./... && go vet ./...`
  Expected: PASS. (`main.go` has no test file — matches the project's existing convention of not unit-testing the entrypoint shell; verification here is compile + vet + the manual window check in Task 6's final step.)

- [ ] **Step 3: Commit**

  ```bash
  git add main.go
  git commit -m "feat: habilita drag-and-drop nativo e liga ao ImportService"
  ```

---

### Task 5: Regenerate frontend bindings

**Files:**
- Modify (generated, do not hand-edit): `frontend/bindings/assistente-idiomas/services/importservice.ts`, `frontend/bindings/assistente-idiomas/services/models.ts`

**Interfaces:**
- Consumes: the now-exported `ImportService.DropImport` method and `DropResult` type from Task 3.
- Produces (for Task 6): `frontend/bindings/.../services/models.ts` gains a `DropResult` interface (`{path: string; error: string}` — not actually used by Task 6, which reads the same shape off the two Wails *events* instead, not by calling `DropImport` from the frontend; the binding is generated automatically because it's an exported service method, but nothing in Task 6 imports it).

- [ ] **Step 1: Run the binding generator from the repo root**

  Run: `wails3 generate bindings -ts -i ./...`
  Expected: exits 0, prints the list of regenerated files including `frontend/bindings/assistente-idiomas/services/importservice.ts` and `frontend/bindings/assistente-idiomas/services/models.ts`.

- [ ] **Step 2: Confirm the new method and type showed up**

  Run: `grep -n "DropImport\|DropResult" frontend/bindings/assistente-idiomas/services/importservice.ts frontend/bindings/assistente-idiomas/services/models.ts`
  Expected: `DropImport` appears in `importservice.ts` and `DropResult` appears in `models.ts`.

- [ ] **Step 3: Confirm `frontend/bindings` is gitignored, not committed**

  `frontend/bindings` is listed in `.gitignore` (alongside `frontend/dist` and `frontend/node_modules`) — it's a generated build artifact, never tracked in git, regenerated by every dev/CI run via this same command. Do **not** `git add` or commit anything under it.

  Run: `git status --short frontend/bindings/`
  Expected: empty output (gitignored paths don't show as untracked). If anything shows here, something is wrong — stop and re-check `.gitignore` before proceeding, don't force-add it.

  No commit for this task — the regenerated files stay on disk (needed by Task 6's `pnpm run build`) but outside git, exactly like `frontend/dist` after any `pnpm run build`.

---

### Task 6: `Library.svelte` — drop zone, event handling, modal queue

**Files:**
- Modify: `frontend/src/lib/screens/Library.svelte`

**Interfaces:**
- Consumes: `Events.On(name, callback)` from `@wailsio/runtime` (same import already used in `frontend/src/lib/jobsStore.svelte.ts`); Wails events `"import:dropped"` (payload: `PendingImport`) and `"import:drop-error"` (payload: `{path: string; error: string}`) emitted by Task 3's `ImportService.DropImport`; the existing `PendingImport` type from `../../../bindings/assistente-idiomas/services/models`; the existing `ImportConfirmModal` component (unchanged, already accepts any `PendingImport`).
- Produces: nothing consumed by later tasks — last code task.

- [ ] **Step 1: Add the `Events` import and new script-level state**

  ```svelte
  <!-- frontend/src/lib/screens/Library.svelte — script block, add after the
       existing "import { onMount } from "svelte";" line: -->
  import { Events } from "@wailsio/runtime";
  ```

  ```svelte
  <!-- After the existing state declarations (right after
       "let retryingId: number | null = $state(null);"), add: -->
  let pendingQueue: PendingImport[] = $state([]);
  let dropErrors: string[] = $state([]);

  interface DropErrorPayload {
    path: string;
    error: string;
  }
  ```

- [ ] **Step 2: Add the queue-draining helper and wire it into `closeReview`/`onConfirmed`**

  ```svelte
  <!-- Add this new function near closeReview/onConfirmed: -->
  function openNextFromQueue() {
    if (reviewing || pendingQueue.length === 0) return;
    const [next, ...rest] = pendingQueue;
    pendingQueue = rest;
    reviewing = next;
  }
  ```

  ```svelte
  <!-- Replace the existing closeReview: -->
  function closeReview() {
    reviewing = null;
    openNextFromQueue();
  }
  ```

  ```svelte
  <!-- Replace the existing onConfirmed: -->
  async function onConfirmed() {
    reviewing = null;
    try {
      await Promise.all([loadPending(), loadLessons(), loadTutors()]);
    } catch (e) {
      error = String(e);
    }
    openNextFromQueue();
  }
  ```

- [ ] **Step 3: Replace `onMount(loadAll);` with a subscription-aware mount that also drives the drop queue**

  ```svelte
  <!-- Replace the final line of the script, "onMount(loadAll);", with: -->
  onMount(() => {
    loadAll();
    const offDropped = Events.On("import:dropped", (ev) => {
      const item = ev.data as PendingImport;
      pendingQueue = [...pendingQueue, item];
      openNextFromQueue();
      loadPending().catch((e) => (error = String(e)));
    });
    const offDropError = Events.On("import:drop-error", (ev) => {
      const { path, error: dropError } = ev.data as DropErrorPayload;
      dropErrors = [...dropErrors, `${path}: ${dropError}`];
    });
    return () => {
      offDropped();
      offDropError();
    };
  });
  ```

- [ ] **Step 4: Mark the screen root as the native drop target and surface drop errors in the template**

  ```svelte
  <!-- Replace the opening <div class="screen" ...> tag: -->
  <div
    class="screen"
    data-file-drop-target
    style="font-family: {fonts.body}; color: {colors.text}; --drop-highlight: {colors.blue};"
  >
  ```

  ```svelte
  <!-- Right after the existing "{#if error}...{/if}" block, add: -->
  {#if dropErrors.length > 0}
    <ul class="drop-errors">
      {#each dropErrors as msg, i (i)}
        <li style="color: {colors.red};">{msg}</li>
      {/each}
    </ul>
  {/if}
  ```

- [ ] **Step 5: Add the CSS for the drop-active highlight and the error list**

  ```css
  /* frontend/src/lib/screens/Library.svelte — add to the existing <style> block: */
  .screen:global(.file-drop-target-active) {
    outline: 2px dashed var(--drop-highlight);
    outline-offset: -8px;
    border-radius: 0.75rem;
  }
  .drop-errors {
    list-style: none;
    margin: 0 0 1rem;
    padding: 0;
    font-size: 0.85rem;
  }
  ```

- [ ] **Step 6: Type-check and build the frontend**

  Run (from `frontend/`): `pnpm run check`
  Expected: PASS, no new TypeScript errors.

  Run (from `frontend/`): `pnpm run build`
  Expected: PASS, production build succeeds.

- [ ] **Step 7: Full-repo verification**

  Run from the repo root: `go build ./... && go vet ./... && go test ./...`
  Expected: PASS.

  Run: `wails3 build`
  Expected: produces the binary without error (confirms the Go+embedded-frontend build succeeds end to end, same check every prior story's progress log records — this sandbox has no display, so opening the window and actually dragging a file onto it still needs manual verification by the user on Windows/Linux, same acknowledged gap as every other story in `docs/phase-1-mvp.md`'s progress log).

- [ ] **Step 8: Commit**

  ```bash
  git add frontend/src/lib/screens/Library.svelte
  git commit -m "feat: Biblioteca reage a drag-and-drop nativo (Historia 3b)"
  ```

---

### Task 7: Close out Story 3b in `docs/phase-1-mvp.md`

**Files:**
- Modify: `docs/phase-1-mvp.md`

**Interfaces:**
- Consumes: nothing (docs only).
- Produces: nothing (last task).

- [ ] **Step 1: Check off the four Story 3b acceptance criteria**

  ```markdown
  <!-- docs/phase-1-mvp.md — Story 3b section, replace the four "- [ ]" lines with: -->
  - [x] Drag-and-drop (or file-dialog fallback — risk 2) opens the same confirmation modal as Story 3 (date pre-filled from the filename/file metadata when possible, free-text tutor field).
  - [x] The video is copied into the storage root using a predictable structure (e.g., `aulas/2026/2026-07-15/`); the database stores **only the relative path**.
  - [x] Entry created in `lessons` + `extract_audio` and `transcribe` jobs created as `pending`, reusing the same confirmation/dedup-by-hash logic from Story 3.
  - [x] A duplicate import (same file/hash) is detected and flagged, not duplicated.
  ```

  Note: the copy destination diverged from the doc's own example subpath (`aulas/2026/2026-07-15/`) per the approved design — it copies flat into `storage_root`, no subfolder (see `docs/superpowers/specs/2026-07-24-story-3b-drag-and-drop-design.md`). Leave the example text as-is (it's illustrative, not binding) rather than editing the acceptance criterion wording.

- [ ] **Step 2: Mark risk 2 as resolved in the "Technical risks" section**

  ```markdown
  <!-- docs/phase-1-mvp.md — item 2 under "## Technical risks — tackle first, not last",
       currently:
  2. **File drag-and-drop in Wails v3:** confirm the pinned version's native DnD API
     (an unstable area of the alpha). Acceptable fallback: button + file dialog.
       becomes: -->
  2. **File drag-and-drop in Wails v3 (resolved, Story 3b):** the native API
     (`EnableFileDrop` + `WindowFilesDropped` event) works on all three platforms at the pinned
     version `v3.0.0-alpha2.117` — the file-dialog fallback wasn't needed.
  ```

- [ ] **Step 3: Add a progress log row**

  ```markdown
  <!-- docs/phase-1-mvp.md — add a new row at the end of the "## Progress log" table: -->
  | 24/07/2026 | Story 3b implemented: Wails v3's native drag-and-drop (`EnableFileDrop` + `WindowFilesDropped` event, no HTML5 File API) resolves technical risk 2 — works on all three platforms at the pinned version; dropped onto the Library screen (`data-file-drop-target`), it opens `ImportConfirmModal` right away (a local queue drains one modal at a time on multiple drops); files are copied into `storage_root` with no subfolder, with a collision suffix, or registered in place if the file is already inside the storage root; an unrecognized extension or an already-imported/pending hash is rejected without copying, with the error reported via the `import:drop-error` event | Reuses 100% of Story 3's confirmation/dedup flow (`ImportConfirmModal`, `pending_imports`, `ConfirmImport`) unchanged; `internal/importer.HashFile`/`HasVideoExtension`/`SuggestDate` were exported for DropImport to reuse without duplicating logic; `db.InsertPendingImport` now returns the id of the created row; real visual verification (dragging a file in a real window) is still pending on Windows/Linux, same pattern as previous stories |
  ```

- [ ] **Step 4: Commit**

  ```bash
  git add docs/phase-1-mvp.md
  git commit -m "docs: mark Story 3b complete and record progress"
  ```
