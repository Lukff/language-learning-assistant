# Story 3 — Import Lesson: Scanning the Existing Folder: Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The app finds video lessons already sitting in the user's storage folder (no assumed
subfolder structure), lets the user review and confirm each one (date/tutor) at their own pace,
and registers confirmed videos as real `lessons` with `extract_audio`/`transcribe` jobs pending.

**Architecture:** A new Wails-free `internal/importer` package walks the storage folder and
decides, per file, whether it's known (skip), moved (update path), or new (stage as a pending
candidate) — driven entirely through a small `Repo` interface so it's unit-testable without a
real database. `internal/db` gains the repository functions (`lessons.go`, `pending_imports.go`)
and a new migration (`00002_pending_imports.sql`: stat-cache columns on `lessons`, a unique index
on `video_hash`, and the `pending_imports` staging table). `services/import.go` is the thin
Wails-facing bridge: it adapts `internal/db`'s hash-oriented repository functions to
`internal/importer.Repo`, and exposes `ScanFolder`/`ListPendingImports`/`ConfirmImport` to the
frontend. The frontend gets a new `ImportConfirmModal.svelte` (reused by the future drag-and-drop
story, Story 3b), a "N aulas aguardando revisão" section + "Sincronizar pasta" button in
`Library.svelte`, and a third wizard step in `SetupWizard.svelte` that runs the first scan
automatically right after `CompleteSetup` succeeds.

**Tech Stack:** No new external dependencies — `internal/importer` uses only `crypto/sha256`,
`io/fs`, `path/filepath`, `regexp`, stdlib. Same `modernc.org/sqlite`/`goose` from Story 2.
Svelte 5 runes, Wails v3 `v3.0.0-alpha2.117` (already pinned).

Every piece of code in this plan — the Go packages, the migration, the generated bindings, and
the Svelte components — was written and verified end-to-end during planning: `go build`/`go
vet`/`go test ./...` all pass, `wails3 generate bindings` was actually run against the real
`services/import.go` to confirm the generated TypeScript signatures used below, `npm run check`
reports `0 ERRORS 0 WARNINGS`, `npm run build` succeeds, and a full `wails3 build` produces the
binary. The code in each step is the exact code that was verified, not a sketch.

## Global Constraints

- `internal/importer` never imports `database/sql`, `internal/db`, or anything under
  `github.com/wailsapp/wails/v3` — it depends only on the `Repo` interface it defines itself.
  This is what makes `Scan` testable with an in-memory fake instead of a real SQLite file.
- Identification and dedup are always by **filename + SHA-256** — never by directory structure.
  `Scan` does not care what subfolders (if any) exist under the storage root.
- Extension recognized in this slice: **`.mp4` only** (`internal/importer.videoExtensions`, a
  one-line slice — trivial to extend to `.mov`/`.mkv`/`.webm` later, deliberately not done now).
- No "descartar"/"ignorar" action on a pending candidate — decided during brainstorming: the
  storage folder is expected to contain only real lessons, so every video found is a genuine
  candidate. Don't add a dismiss/ignore path.
- Drag-and-drop manual import is **out of scope** (Story 3b, future). Don't add a drop zone or
  a "copy into `aulas/AAAA/AAAA-MM-DD/`" step in this story.
- `updated_at` on `lessons` and `created_at` on `pending_imports` are always "now"
  (`time.Now().UTC().Format(time.RFC3339)`), computed inside `internal/db` — never the file's own
  `mtime`, which is a separate, independently-tracked column (`file_mtime`) used only for the
  stat-cache comparison.
- SQL in `internal/db/migrations/*.sql` stays portable across SQLite drivers (no
  `modernc.org`-specific syntax) — same rule as Story 2's migration.
- Svelte 5 runes only (`$state`, `$props`) — no legacy syntax.
- **Toolchain prerequisites confirmed during planning (fresh Linux environment):**
  - `CGO_ENABLED=1` + `libgtk-4-dev libwebkitgtk-6.0-dev pkg-config build-essential` (gtk4 /
    webkitgtk-6.0, **not** gtk3) — same as Story 2's plan, needed by any package importing
    Wails (`services/`, `main.go`).
  - The `wails3` CLI itself is **not** a project dependency (it's not in `go.mod`) — on a machine
    that doesn't already have it, install the exact pinned version before generating bindings or
    building: `go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-alpha2.117`, then ensure
    `$(go env GOPATH)/bin` is on `PATH`.
  - A first `npm install` inside `frontend/` may report `Cannot find native binding` from
    `rolldown` (an upstream npm optional-dependency bug, not specific to this project). Running
    `npm install` again resolved it during planning — if it recurs, the upstream issue is
    https://github.com/npm/cli/issues/4828.
- `go vet ./...`/`gofmt -l .` must stay clean for every package touched by this story. (`go build
  ./...` on this repo already fails on `build/ios/...` with `function main is undeclared` — this
  is **pre-existing**, unrelated to this story, and reproduces on a clean checkout with no files
  from this plan; scope your `go build`/`go vet` checks to `./internal/... ./services/... .` to
  avoid the noise, as the steps below do.)
- Implementers **stage** (`git add`) their changes at the end of each task but do **not**
  commit — the user controls commit timing (same convention as Stories 1–2).

---

### Task 1: `internal/importer` — pure scan logic

**Files:**
- Create: `internal/importer/importer.go`
- Test: `internal/importer/importer_test.go`

**Interfaces:**
- Consumes: nothing (first task; only stdlib).
- Produces, in package `assistente-idiomas/internal/importer`:
  ```go
  type Candidate struct {
      Path          string // relative to the scan root, always "/" (filepath.ToSlash)
      Size          int64
      MTime         string // RFC3339 (UTC)
      SHA256        string
      SuggestedDate string // YYYY-MM-DD
  }
  type Summary struct{ New, Updated, Skipped, Errors int }
  type Repo interface {
      StatMatch(path string, size int64, mtime string) (bool, error)
      LessonByHash(hash string) (path string, found bool, err error)
      UpdateLessonPath(hash string, path string, size int64, mtime string) error
      PendingExists(hash string) (bool, error)
      InsertPending(c Candidate) error
  }
  func Scan(root string, repo Repo) (Summary, error)
  ```
  `Repo` is implemented by a fake in this task's own tests, and later (Task 3) by an adapter over
  `internal/db`. `Scan` is consumed by `services/import.go` in Task 3.

- [ ] **Step 1: Write the failing tests**

```go
// internal/importer/importer_test.go
package importer

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeRepo is an in-memory Repo — this package's tests never touch a real
// database, only Scan's decision logic.
type fakeRepo struct {
	lessonPathByHash map[string]string // hash -> path already registered as a lesson
	lessonStat       map[string]string // path -> "size:mtime" of the lesson registered at that path
	pending          map[string]bool   // hash -> already in pending_imports

	statMatchCalls int
	updatedPaths   map[string]string // hash -> novo path (chamadas de UpdateLessonPath)
	inserted       []Candidate
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		lessonPathByHash: map[string]string{},
		lessonStat:       map[string]string{},
		pending:          map[string]bool{},
		updatedPaths:     map[string]string{},
	}
}

func (r *fakeRepo) StatMatch(path string, size int64, mtime string) (bool, error) {
	r.statMatchCalls++
	want, ok := r.lessonStat[path]
	if !ok {
		return false, nil
	}
	return want == statKey(size, mtime), nil
}

func (r *fakeRepo) LessonByHash(hash string) (string, bool, error) {
	path, ok := r.lessonPathByHash[hash]
	return path, ok, nil
}

func (r *fakeRepo) UpdateLessonPath(hash string, path string, size int64, mtime string) error {
	r.updatedPaths[hash] = path
	delete(r.lessonStat, r.lessonPathByHash[hash])
	r.lessonPathByHash[hash] = path
	r.lessonStat[path] = statKey(size, mtime)
	return nil
}

func (r *fakeRepo) PendingExists(hash string) (bool, error) {
	return r.pending[hash], nil
}

func (r *fakeRepo) InsertPending(c Candidate) error {
	r.inserted = append(r.inserted, c)
	r.pending[c.SHA256] = true
	return nil
}

func statKey(size int64, mtime string) string {
	return mtime + ":" + itoa(size)
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) falhou: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) falhou: %v", path, err)
	}
}

func TestScan_NewVideoBecomesCandidate(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "aula-2026-07-15.mp4"), "conteudo-a")
	repo := newFakeRepo()

	sum, err := Scan(root, repo)
	if err != nil {
		t.Fatalf("Scan() unexpected error: %v", err)
	}
	if sum.New != 1 || sum.Skipped != 0 || sum.Updated != 0 || sum.Errors != 0 {
		t.Errorf("Summary = %+v, expected {New:1}", sum)
	}
	if len(repo.inserted) != 1 {
		t.Fatalf("inserted candidates = %d, expected 1", len(repo.inserted))
	}
	got := repo.inserted[0]
	if got.Path != "aula-2026-07-15.mp4" {
		t.Errorf("Path = %q, expected aula-2026-07-15.mp4", got.Path)
	}
	if got.SuggestedDate != "2026-07-15" {
		t.Errorf("SuggestedDate = %q, expected 2026-07-15 (extracted from the name)", got.SuggestedDate)
	}
	if got.SHA256 == "" {
		t.Error("SHA256 empty, expected a computed hash")
	}
}

func TestScan_IgnoresNonVideoFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "notas.txt"), "not a video")
	writeFile(t, filepath.Join(root, "aula.mov"), "extension not supported in this slice")
	repo := newFakeRepo()

	sum, err := Scan(root, repo)
	if err != nil {
		t.Fatalf("Scan() unexpected error: %v", err)
	}
	if sum.New != 0 {
		t.Errorf("Summary.New = %d, expected 0 (no .mp4 in the folder)", sum.New)
	}
}

func TestScan_KnownHashIsSkipped(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "aula.mp4"), "known-content")
	repo := newFakeRepo()

	// first scan: discovers and creates the candidate
	if _, err := Scan(root, repo); err != nil {
		t.Fatalf("first Scan() unexpected error: %v", err)
	}
	if len(repo.inserted) != 1 {
		t.Fatalf("setup: expected 1 candidate after the first scan, got %d", len(repo.inserted))
	}
	hash := repo.inserted[0].SHA256

	// simulates confirmation: the candidate became a lesson, leaves pending
	repo.lessonPathByHash[hash] = "aula.mp4"
	delete(repo.pending, hash)

	sum, err := Scan(root, repo)
	if err != nil {
		t.Fatalf("second Scan() unexpected error: %v", err)
	}
	if sum.Skipped != 1 || sum.New != 0 {
		t.Errorf("Summary = %+v, expected {Skipped:1} (same hash, same path)", sum)
	}
}

func TestScan_StatCacheSkipsHashingUnchangedFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "aula.mp4")
	writeFile(t, path, "stable-content")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() unexpected error: %v", err)
	}

	repo := newFakeRepo()
	repo.lessonPathByHash["already-known-hash"] = "aula.mp4"
	repo.lessonStat["aula.mp4"] = statKey(info.Size(), info.ModTime().UTC().Format("2006-01-02T15:04:05Z07:00"))

	sum, err := Scan(root, repo)
	if err != nil {
		t.Fatalf("Scan() unexpected error: %v", err)
	}
	if sum.Skipped != 1 || sum.New != 0 || sum.Updated != 0 {
		t.Errorf("Summary = %+v, expected {Skipped:1} via stat-cache", sum)
	}
	if repo.statMatchCalls != 1 {
		t.Errorf("StatMatch called %d times, expected 1", repo.statMatchCalls)
	}
	if len(repo.inserted) != 0 {
		t.Error("no candidate should have been inserted — the stat-cache should have avoided hashing")
	}
}

func TestScan_SameHashDifferentPathUpdatesLessonInsteadOfDuplicating(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "nova-pasta", "aula.mp4"), "moved-content")
	repo := newFakeRepo()

	// first scan at a different path, to discover the content's real hash
	if _, err := Scan(root, repo); err != nil {
		t.Fatalf("first Scan() unexpected error: %v", err)
	}
	hash := repo.inserted[0].SHA256

	// simulates: this lesson already existed, registered at an old path
	repo.pending = map[string]bool{}
	repo.inserted = nil
	repo.lessonPathByHash[hash] = "pasta-antiga/aula.mp4"

	sum, err := Scan(root, repo)
	if err != nil {
		t.Fatalf("second Scan() unexpected error: %v", err)
	}
	if sum.Updated != 1 || sum.New != 0 {
		t.Errorf("Summary = %+v, expected {Updated:1} (same hash, new path)", sum)
	}
	if got := repo.updatedPaths[hash]; got != "nova-pasta/aula.mp4" {
		t.Errorf("UpdateLessonPath called with path = %q, expected nova-pasta/aula.mp4", got)
	}
	if len(repo.inserted) != 0 {
		t.Error("should not have created a new candidate — it's the same file, just moved")
	}
}

func TestScan_ErrorOnOneFileDoesNotAbortTheRest(t *testing.T) {
	root := t.TempDir()
	unreadable := filepath.Join(root, "sem-permissao.mp4")
	writeFile(t, unreadable, "content")
	if err := os.Chmod(unreadable, 0o000); err != nil {
		t.Fatalf("Chmod() failed: %v", err)
	}
	t.Cleanup(func() { os.Chmod(unreadable, 0o644) })
	writeFile(t, filepath.Join(root, "ok.mp4"), "ok-content")
	repo := newFakeRepo()

	sum, err := Scan(root, repo)
	if err != nil {
		t.Fatalf("Scan() should not have returned a fatal error: %v", err)
	}
	if sum.Errors != 1 {
		t.Errorf("Summary.Errors = %d, expected 1 (file with no permission)", sum.Errors)
	}
	if sum.New != 1 {
		t.Errorf("Summary.New = %d, expected 1 (ok.mp4 still processed)", sum.New)
	}
}
```

`TestScan_ErrorOnOneFileDoesNotAbortTheRest` needs a non-root user to actually trigger a
permission error (`chmod 0o000` is a no-op for root) — this is already true of every environment
these tests have run in for this project, but note it if it ever runs as root in CI.

- [ ] **Step 2: Run the tests to confirm they fail (package doesn't exist yet)**

```bash
go test ./internal/importer/... -v
```

Expected: `FAIL` — build error, `Scan`/`Candidate`/etc. undefined (only the test file exists so
far).

- [ ] **Step 3: Write `internal/importer/importer.go`**

```go
// Package importer scans the storage folder looking for lesson videos that
// aren't in the database yet (Story 3, docs/phase-1-mvp.md). It doesn't
// assume any subfolder structure: identification and dedup are always by
// filename + SHA-256, never by path convention. It doesn't import anything
// from Wails (thin layer).
package importer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// videoExtensions lists the extensions recognized as a lesson video.
// Deliberately short in this Story 3 slice (only .mp4, the most common
// format from Cambly/browser) — extending it is just adding to this list.
var videoExtensions = []string{".mp4"}

// Candidate is a new video found by the scan, still with no lesson
// registered nor a pending candidate with the same hash.
type Candidate struct {
	Path          string // relative to the scan root, always "/" (filepath.ToSlash)
	Size          int64
	MTime         string // RFC3339 (UTC)
	SHA256        string
	SuggestedDate string // YYYY-MM-DD, extracted from the filename or the mtime
}

// Summary summarizes the result of a scan.
type Summary struct {
	New     int // new candidates written to pending_imports
	Updated int // existing lessons with an updated path (file moved/renamed)
	Skipped int // already-known files (lesson or existing pending), nothing changed
	Errors  int // files that failed to read/stat/hash — don't interrupt the scan
}

// Repo is what Scan needs from the database. Implemented by an adapter over
// internal/db in services/import.go; in this package's own tests, by an
// in-memory fake — Scan never imports internal/db nor database/sql directly.
type Repo interface {
	// StatMatch reports whether a lesson is already registered at exactly
	// this path, with this size and mtime — if so, the file is known and
	// unchanged, and Scan skips it without computing a hash.
	StatMatch(path string, size int64, mtime string) (bool, error)

	// LessonByHash returns the path of a lesson already registered with
	// this hash, if one exists.
	LessonByHash(hash string) (path string, found bool, err error)

	// UpdateLessonPath updates the path/size/mtime of the lesson with this
	// hash — used when the file has only moved/been renamed.
	UpdateLessonPath(hash string, path string, size int64, mtime string) error

	// PendingExists reports whether a pending candidate with this hash
	// already exists (from a previous scan not yet confirmed).
	PendingExists(hash string) (bool, error)

	// InsertPending writes a new candidate.
	InsertPending(c Candidate) error
}

// Scan walks root recursively, filters by videoExtensions, and decides,
// for each file, whether it's known (skip), moved (updates the lesson's
// path), or a new candidate (writes to pending_imports via
// repo.InsertPending). An error processing a specific file doesn't abort
// the scan — it's counted in Summary.Errors and the rest continues.
func Scan(root string, repo Repo) (Summary, error) {
	var sum Summary

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			sum.Errors++
			return nil
		}
		if d.IsDir() || !hasVideoExtension(path) {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			sum.Errors++
			return nil
		}
		relPath, err := filepath.Rel(root, path)
		if err != nil {
			sum.Errors++
			return nil
		}
		relPath = filepath.ToSlash(relPath)
		size := info.Size()
		mtime := info.ModTime().UTC().Format(time.RFC3339)

		matched, err := repo.StatMatch(relPath, size, mtime)
		if err != nil {
			sum.Errors++
			return nil
		}
		if matched {
			sum.Skipped++
			return nil
		}

		hash, err := hashFile(path)
		if err != nil {
			sum.Errors++
			return nil
		}

		existingPath, found, err := repo.LessonByHash(hash)
		if err != nil {
			sum.Errors++
			return nil
		}
		if found {
			if existingPath == relPath {
				sum.Skipped++
				return nil
			}
			if err := repo.UpdateLessonPath(hash, relPath, size, mtime); err != nil {
				sum.Errors++
				return nil
			}
			sum.Updated++
			return nil
		}

		pending, err := repo.PendingExists(hash)
		if err != nil {
			sum.Errors++
			return nil
		}
		if pending {
			sum.Skipped++
			return nil
		}

		candidate := Candidate{
			Path:          relPath,
			Size:          size,
			MTime:         mtime,
			SHA256:        hash,
			SuggestedDate: suggestDate(filepath.Base(path), info.ModTime()),
		}
		if err := repo.InsertPending(candidate); err != nil {
			sum.Errors++
			return nil
		}
		sum.New++
		return nil
	})
	if err != nil {
		return sum, fmt.Errorf("scan storage folder: %w", err)
	}
	return sum, nil
}

func hasVideoExtension(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	for _, want := range videoExtensions {
		if ext == want {
			return true
		}
	}
	return false
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

var isoDateInName = regexp.MustCompile(`(\d{4}-\d{2}-\d{2})`)

// suggestDate tries to find a YYYY-MM-DD date in the filename; failing
// that, it uses the mtime's date. It's just a pre-filled guess in the
// confirmation modal — the user can always correct it.
func suggestDate(filename string, mtime time.Time) string {
	if m := isoDateInName.FindString(filename); m != "" {
		return m
	}
	return mtime.Format("2006-01-02")
}
```

- [ ] **Step 4: Run the tests again to confirm they pass**

```bash
go test ./internal/importer/... -v
```

Expected: all 6 tests `PASS`.

- [ ] **Step 5: `go vet` and `gofmt` check**

```bash
go vet ./internal/importer/...
gofmt -l internal/importer
```

Expected: no output from either.

- [ ] **Step 6: Stage (do not commit — user controls commit timing)**

```bash
git add internal/importer
git status
```

---

### Task 2: `internal/db` — stat-cache columns, `pending_imports` table, repository functions

**Files:**
- Create: `internal/db/migrations/00002_pending_imports.sql`
- Create: `internal/db/lessons.go`
- Create: `internal/db/pending_imports.go`
- Test: `internal/db/lessons_test.go`
- Test: `internal/db/pending_imports_test.go`

**Interfaces:**
- Consumes: `db.Open` (existing, from Story 2 — the migration is picked up automatically via
  the package's `//go:embed migrations/*.sql`, no change to `db.go` needed).
- Produces, in package `assistente-idiomas/internal/db`:
  ```go
  type Lesson struct {
      ID        int64
      VideoPath string
      VideoHash string
      FileSize  int64
      FileMTime string
  }
  func FindLessonByPath(conn *sql.DB, path string) (*Lesson, error)
  func FindLessonByHash(conn *sql.DB, hash string) (*Lesson, error)
  func UpdateLessonPath(conn *sql.DB, lessonID int64, path string, size int64, fileMTime string) error

  type PendingImport struct {
      ID            int64
      Path          string
      FileSize      int64
      FileMTime     string
      SHA256        string
      SuggestedDate string
  }
  func FindPendingImportByHash(conn *sql.DB, hash string) (bool, error)
  func InsertPendingImport(conn *sql.DB, p PendingImport) error
  func ListPendingImports(conn *sql.DB) ([]PendingImport, error)
  func ConfirmPendingImport(conn *sql.DB, id int64, lessonDate string, tutor string) (int64, error)
  ```
  All consumed by `services/import.go` in Task 3.

- [ ] **Step 1: Write the migration file**

```sql
-- internal/db/migrations/00002_pending_imports.sql
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

`file_size`/`file_mtime` on `lessons` exist only for the stat-cache (skip rehashing an unchanged
file); the unique index on `video_hash` turns "duplicate import" into a database guarantee, not
just application logic. Verified during planning: `modernc.org/sqlite v1.54.0` applies this
migration (partial unique index + `ADD COLUMN` + `DROP COLUMN`) with no errors.

- [ ] **Step 2: Write the failing tests**

```go
// internal/db/lessons_test.go
package db

import (
	"path/filepath"
	"testing"
)

func TestFindLessonByPath_NotFoundReturnsNilNil(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() unexpected error: %v", err)
	}
	defer conn.Close()

	l, err := FindLessonByPath(conn, "aulas/nao-existe.mp4")
	if err != nil {
		t.Fatalf("FindLessonByPath() unexpected error: %v", err)
	}
	if l != nil {
		t.Errorf("FindLessonByPath() = %+v, expected nil", l)
	}
}

func TestFindLessonByPathAndByHash_FindExistingRow(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() unexpected error: %v", err)
	}
	defer conn.Close()

	_, err = conn.Exec(
		`INSERT INTO lessons (lesson_date, tutor, video_path, video_hash, file_size, file_mtime, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"2026-07-15", "Sarah", "aula-01.mp4", "hash-abc", 12345, "2026-07-15T10:00:00Z", "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z",
	)
	if err != nil {
		t.Fatalf("insert de fixture falhou: %v", err)
	}

	byPath, err := FindLessonByPath(conn, "aula-01.mp4")
	if err != nil {
		t.Fatalf("FindLessonByPath() unexpected error: %v", err)
	}
	if byPath == nil || byPath.VideoHash != "hash-abc" || byPath.FileSize != 12345 {
		t.Errorf("FindLessonByPath() = %+v, expected hash hash-abc e file_size 12345", byPath)
	}

	byHash, err := FindLessonByHash(conn, "hash-abc")
	if err != nil {
		t.Fatalf("FindLessonByHash() unexpected error: %v", err)
	}
	if byHash == nil || byHash.VideoPath != "aula-01.mp4" {
		t.Errorf("FindLessonByHash() = %+v, expected video_path aula-01.mp4", byHash)
	}
}

func TestUpdateLessonPath_ChangesPathSizeAndMTime(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() unexpected error: %v", err)
	}
	defer conn.Close()

	res, err := conn.Exec(
		`INSERT INTO lessons (lesson_date, tutor, video_path, video_hash, file_size, file_mtime, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"2026-07-15", "Sarah", "old/aula-01.mp4", "hash-abc", 100, "2026-07-15T10:00:00Z", "2026-07-15T10:00:00Z", "2026-07-15T10:00:00Z",
	)
	if err != nil {
		t.Fatalf("insert de fixture falhou: %v", err)
	}
	lessonID, _ := res.LastInsertId()

	if err := UpdateLessonPath(conn, lessonID, "new/aula-01.mp4", 200, "2026-07-20T10:00:00Z"); err != nil {
		t.Fatalf("UpdateLessonPath() unexpected error: %v", err)
	}

	updated, err := FindLessonByHash(conn, "hash-abc")
	if err != nil {
		t.Fatalf("FindLessonByHash() unexpected error: %v", err)
	}
	if updated.VideoPath != "new/aula-01.mp4" || updated.FileSize != 200 || updated.FileMTime != "2026-07-20T10:00:00Z" {
		t.Errorf("lesson after UpdateLessonPath = %+v, expected the new path/size/mtime", updated)
	}
}

func TestLessons_VideoHashUniqueIndexRejectsDuplicate(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() unexpected error: %v", err)
	}
	defer conn.Close()

	insert := `INSERT INTO lessons (lesson_date, tutor, video_path, video_hash, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`
	if _, err := conn.Exec(insert, "2026-07-15", "Sarah", "a.mp4", "hash-dup", "2026-07-15T10:00:00Z", "2026-07-15T10:00:00Z"); err != nil {
		t.Fatalf("first insert failed: %v", err)
	}
	if _, err := conn.Exec(insert, "2026-07-16", "Sarah", "b.mp4", "hash-dup", "2026-07-16T10:00:00Z", "2026-07-16T10:00:00Z"); err == nil {
		t.Error("expected a unique-index error on duplicate video_hash, got nil")
	}
}
```

```go
// internal/db/pending_imports_test.go
package db

import (
	"path/filepath"
	"testing"
)

func TestPendingImports_InsertListFindByHashRoundTrip(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() unexpected error: %v", err)
	}
	defer conn.Close()

	found, err := FindPendingImportByHash(conn, "hash-new")
	if err != nil {
		t.Fatalf("FindPendingImportByHash() unexpected error: %v", err)
	}
	if found {
		t.Error("FindPendingImportByHash() = true before inserting, expected false")
	}

	err = InsertPendingImport(conn, PendingImport{
		Path: "aula-nova.mp4", FileSize: 999, FileMTime: "2026-07-20T10:00:00Z",
		SHA256: "hash-new", SuggestedDate: "2026-07-20",
	})
	if err != nil {
		t.Fatalf("InsertPendingImport() unexpected error: %v", err)
	}

	found, err = FindPendingImportByHash(conn, "hash-new")
	if err != nil {
		t.Fatalf("FindPendingImportByHash() unexpected error: %v", err)
	}
	if !found {
		t.Error("FindPendingImportByHash() = false after inserting, expected true")
	}

	list, err := ListPendingImports(conn)
	if err != nil {
		t.Fatalf("ListPendingImports() unexpected error: %v", err)
	}
	if len(list) != 1 || list[0].Path != "aula-nova.mp4" || list[0].SuggestedDate != "2026-07-20" {
		t.Errorf("ListPendingImports() = %+v, expected 1 item aula-nova.mp4/2026-07-20", list)
	}
}

func TestConfirmPendingImport_CreatesLessonAndJobsRemovesPending(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() unexpected error: %v", err)
	}
	defer conn.Close()

	if err := InsertPendingImport(conn, PendingImport{
		Path: "aula-nova.mp4", FileSize: 999, FileMTime: "2026-07-20T10:00:00Z",
		SHA256: "hash-confirm", SuggestedDate: "2026-07-20",
	}); err != nil {
		t.Fatalf("InsertPendingImport() unexpected error: %v", err)
	}
	list, err := ListPendingImports(conn)
	if err != nil || len(list) != 1 {
		t.Fatalf("setup: ListPendingImports() = %+v, %v", list, err)
	}
	pendingID := list[0].ID

	lessonID, err := ConfirmPendingImport(conn, pendingID, "2026-07-20", "Sarah M.")
	if err != nil {
		t.Fatalf("ConfirmPendingImport() unexpected error: %v", err)
	}
	if lessonID == 0 {
		t.Fatal("ConfirmPendingImport() retornou lessonID = 0")
	}

	lesson, err := FindLessonByHash(conn, "hash-confirm")
	if err != nil {
		t.Fatalf("FindLessonByHash() unexpected error: %v", err)
	}
	if lesson == nil || lesson.VideoPath != "aula-nova.mp4" {
		t.Fatalf("lesson after confirmation = %+v, expected video_path aula-nova.mp4", lesson)
	}

	var tutor string
	if err := conn.QueryRow(`SELECT tutor FROM lessons WHERE id = ?`, lessonID).Scan(&tutor); err != nil {
		t.Fatalf("select tutor falhou: %v", err)
	}
	if tutor != "Sarah M." {
		t.Errorf("tutor = %q, expected \"Sarah M.\"", tutor)
	}

	rows, err := conn.Query(`SELECT kind, status FROM jobs WHERE lesson_id = ? ORDER BY kind`, lessonID)
	if err != nil {
		t.Fatalf("query de jobs falhou: %v", err)
	}
	defer rows.Close()
	var jobs [][2]string
	for rows.Next() {
		var kind, status string
		if err := rows.Scan(&kind, &status); err != nil {
			t.Fatalf("scan de job falhou: %v", err)
		}
		jobs = append(jobs, [2]string{kind, status})
	}
	want := [][2]string{{"extract_audio", "pending"}, {"transcribe", "pending"}}
	if len(jobs) != len(want) {
		t.Fatalf("jobs criados = %+v, expected %+v", jobs, want)
	}
	for i := range want {
		if jobs[i] != want[i] {
			t.Errorf("jobs[%d] = %+v, expected %+v", i, jobs[i], want[i])
		}
	}

	list, err = ListPendingImports(conn)
	if err != nil {
		t.Fatalf("ListPendingImports() unexpected error: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("ListPendingImports() after confirming = %+v, expected empty", list)
	}
}

func TestConfirmPendingImport_UnknownIDReturnsErrorAndTouchesNothing(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() unexpected error: %v", err)
	}
	defer conn.Close()

	if _, err := ConfirmPendingImport(conn, 999, "2026-07-20", "Sarah M."); err == nil {
		t.Error("ConfirmPendingImport() with a nonexistent id expected an error, got nil")
	}

	var count int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM lessons`).Scan(&count); err != nil {
		t.Fatalf("lessons count failed: %v", err)
	}
	if count != 0 {
		t.Errorf("lessons after ConfirmPendingImport fails = %d rows, expected 0", count)
	}
}
```

- [ ] **Step 3: Run the tests to confirm they fail**

```bash
go test ./internal/db/... -run 'TestFindLesson|TestUpdateLessonPath|TestLessons_VideoHash|TestPendingImports|TestConfirmPendingImport' -v
```

Expected: `FAIL` — build error (`FindLessonByPath`, `PendingImport`, etc. undefined; the migration
file alone doesn't add Go symbols).

- [ ] **Step 4: Write `internal/db/lessons.go`**

```go
package db

import (
	"database/sql"
	"fmt"
	"time"
)

// Lesson is a lessons row relevant to mapping the existing folder
// (Story 3): identity (path, hash) and the stat-cache
// (size/mtime) used by the scan to decide whether the content needs
// to be rehashed.
type Lesson struct {
	ID        int64
	VideoPath string
	VideoHash string
	FileSize  int64
	FileMTime string
}

// FindLessonByPath looks up the lesson whose video_path is exactly path.
// Returns (nil, nil) if there isn't one — an already-registered path is the
// common case, not an error.
func FindLessonByPath(conn *sql.DB, path string) (*Lesson, error) {
	var l Lesson
	err := conn.QueryRow(
		`SELECT id, video_path, COALESCE(video_hash, ''), COALESCE(file_size, 0), COALESCE(file_mtime, '') FROM lessons WHERE video_path = ?`,
		path,
	).Scan(&l.ID, &l.VideoPath, &l.VideoHash, &l.FileSize, &l.FileMTime)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find lesson by path: %w", err)
	}
	return &l, nil
}

// FindLessonByHash looks up the lesson whose video_hash is exactly hash.
// Returns (nil, nil) if there isn't one.
func FindLessonByHash(conn *sql.DB, hash string) (*Lesson, error) {
	var l Lesson
	err := conn.QueryRow(
		`SELECT id, video_path, COALESCE(video_hash, ''), COALESCE(file_size, 0), COALESCE(file_mtime, '') FROM lessons WHERE video_hash = ?`,
		hash,
	).Scan(&l.ID, &l.VideoPath, &l.VideoHash, &l.FileSize, &l.FileMTime)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find lesson by hash: %w", err)
	}
	return &l, nil
}

// UpdateLessonPath updates video_path/file_size/file_mtime of an
// already-registered lesson — used when the scan finds the same hash at a
// different path (the file just moved/was renamed, it's not a new lesson).
// file_mtime is the file's mtime on disk; updated_at (the mark of when the
// database row changed) is always "now", never the file's mtime.
func UpdateLessonPath(conn *sql.DB, lessonID int64, path string, size int64, fileMTime string) error {
	_, err := conn.Exec(
		`UPDATE lessons SET video_path = ?, file_size = ?, file_mtime = ?, updated_at = ? WHERE id = ?`,
		path, size, fileMTime, time.Now().UTC().Format(time.RFC3339), lessonID,
	)
	if err != nil {
		return fmt.Errorf("update lesson path: %w", err)
	}
	return nil
}
```

- [ ] **Step 5: Write `internal/db/pending_imports.go`**

```go
package db

import (
	"database/sql"
	"fmt"
	"time"
)

// PendingImport is a video found by the storage-folder scan that hasn't
// been confirmed (date/tutor) by the user yet — see Story 3
// in docs/phase-1-mvp.md.
type PendingImport struct {
	ID            int64
	Path          string
	FileSize      int64
	FileMTime     string
	SHA256        string
	SuggestedDate string
}

// FindPendingImportByHash reports whether a pending candidate with this
// hash already exists — avoids duplicating the same scan across successive
// runs.
func FindPendingImportByHash(conn *sql.DB, hash string) (bool, error) {
	var id int64
	err := conn.QueryRow(`SELECT id FROM pending_imports WHERE sha256 = ?`, hash).Scan(&id)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("find pending_import by hash: %w", err)
	}
	return true, nil
}

// InsertPendingImport writes a new candidate found by the scan.
func InsertPendingImport(conn *sql.DB, p PendingImport) error {
	_, err := conn.Exec(
		`INSERT INTO pending_imports (path, file_size, file_mtime, sha256, suggested_date, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		p.Path, p.FileSize, p.FileMTime, p.SHA256, p.SuggestedDate, time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("insert pending_import: %w", err)
	}
	return nil
}

// ListPendingImports lists the candidates awaiting review, most recent
// first — this is what the Library reads to build the "awaiting review" section.
func ListPendingImports(conn *sql.DB) ([]PendingImport, error) {
	rows, err := conn.Query(
		`SELECT id, path, file_size, file_mtime, sha256, COALESCE(suggested_date, '') FROM pending_imports ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list pending_imports: %w", err)
	}
	defer rows.Close()

	var out []PendingImport
	for rows.Next() {
		var p PendingImport
		if err := rows.Scan(&p.ID, &p.Path, &p.FileSize, &p.FileMTime, &p.SHA256, &p.SuggestedDate); err != nil {
			return nil, fmt.Errorf("read pending_import: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending_imports: %w", err)
	}
	return out, nil
}

// ConfirmPendingImport turns candidate id into a real lesson: inserts into
// lessons (with the lessonDate/tutor supplied by the user), creates the
// extract_audio and transcribe jobs as pending, and removes the candidate
// from pending_imports — all in a single transaction. If any step fails,
// the candidate remains intact in pending_imports for the user to try
// again.
func ConfirmPendingImport(conn *sql.DB, id int64, lessonDate string, tutor string) (int64, error) {
	tx, err := conn.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	var p PendingImport
	err = tx.QueryRow(
		`SELECT id, path, file_size, file_mtime, sha256 FROM pending_imports WHERE id = ?`,
		id,
	).Scan(&p.ID, &p.Path, &p.FileSize, &p.FileMTime, &p.SHA256)
	if err == sql.ErrNoRows {
		return 0, fmt.Errorf("candidato %d não encontrado (já foi confirmado ou removido?)", id)
	}
	if err != nil {
		return 0, fmt.Errorf("find pending_import: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	res, err := tx.Exec(
		`INSERT INTO lessons (lesson_date, tutor, video_path, video_hash, file_size, file_mtime, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		lessonDate, tutor, p.Path, p.SHA256, p.FileSize, p.FileMTime, now, now,
	)
	if err != nil {
		return 0, fmt.Errorf("insert lesson: %w", err)
	}
	lessonID, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("get lesson id: %w", err)
	}

	for _, kind := range []string{"extract_audio", "transcribe"} {
		if _, err := tx.Exec(
			`INSERT INTO jobs (lesson_id, kind, status, created_at, updated_at) VALUES (?, ?, 'pending', ?, ?)`,
			lessonID, kind, now, now,
		); err != nil {
			return 0, fmt.Errorf("create job %s: %w", kind, err)
		}
	}

	if _, err := tx.Exec(`DELETE FROM pending_imports WHERE id = ?`, id); err != nil {
		return 0, fmt.Errorf("remove pending_import: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit transaction: %w", err)
	}
	return lessonID, nil
}
```

- [ ] **Step 6: Run the tests again to confirm they pass**

```bash
go test ./internal/db/... -v
```

Expected: all tests in the package `PASS` — the 4 pre-existing Story 2 tests plus the 8 new
ones from this task (4 in `lessons_test.go`, 4 in `pending_imports_test.go`). You'll see goose's
migration log lines for both `00001_initial_schema.sql` and `00002_pending_imports.sql` on stdout.

- [ ] **Step 7: `go vet` and `gofmt` check**

```bash
go vet ./internal/db/...
gofmt -l internal/db
```

Expected: no output from either.

- [ ] **Step 8: Stage (do not commit — user controls commit timing)**

```bash
git add internal/db
git status
```

---

### Task 3: `services` package — `ImportService`

**Files:**
- Create: `services/import.go`
- Test: `services/import_test.go`

**Interfaces:**
- Consumes: `importer.Scan`, `importer.Candidate`, `importer.Repo` (Task 1); `db.FindLessonByPath`,
  `db.FindLessonByHash`, `db.UpdateLessonPath`, `db.PendingImport`, `db.FindPendingImportByHash`,
  `db.InsertPendingImport`, `db.ListPendingImports`, `db.ConfirmPendingImport` (Task 2);
  `config.Load`, `config.AppConfig` (existing, from Story 2).
- Produces, in package `assistente-idiomas/services`:
  ```go
  type ScanSummary struct{ New, Updated, Skipped, Errors int } // JSON-tagged: new/updated/skipped/errors
  type PendingImport struct {                                   // JSON-tagged: id/path/suggestedDate
      ID            int64
      Path          string
      SuggestedDate string
  }
  type ImportService struct{ /* unexported */ }
  func NewImportService(conn *sql.DB) *ImportService
  func (s *ImportService) ScanFolder() (ScanSummary, error)
  func (s *ImportService) ListPendingImports() ([]PendingImport, error)
  func (s *ImportService) ConfirmImport(id int64, lessonDate string, tutor string) error
  ```
  Consumed by `main.go` in Task 4 (registered as a Wails service) and, indirectly, by the
  generated TypeScript bindings the frontend calls in Task 5.

- [ ] **Step 1: Write the failing test**

```go
// services/import_test.go
package services

import (
	"os"
	"path/filepath"
	"testing"

	"assistente-idiomas/internal/config"
	"assistente-idiomas/internal/db"
)

func TestImportService_ScanFolderThenListThenConfirm(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	storageRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(storageRoot, "aula-2026-07-15.mp4"), []byte("content"), 0o644); err != nil {
		t.Fatalf("preparing fixture video failed: %v", err)
	}
	if err := config.Save(&config.AppConfig{StorageRoot: storageRoot}); err != nil {
		t.Fatalf("config.Save() failed: %v", err)
	}

	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() failed: %v", err)
	}
	defer conn.Close()

	svc := NewImportService(conn)

	sum, err := svc.ScanFolder()
	if err != nil {
		t.Fatalf("ScanFolder() unexpected error: %v", err)
	}
	if sum.New != 1 {
		t.Fatalf("ScanFolder() Summary = %+v, expected New=1", sum)
	}

	pending, err := svc.ListPendingImports()
	if err != nil {
		t.Fatalf("ListPendingImports() unexpected error: %v", err)
	}
	if len(pending) != 1 || pending[0].Path != "aula-2026-07-15.mp4" || pending[0].SuggestedDate != "2026-07-15" {
		t.Fatalf("ListPendingImports() = %+v, expected 1 item aula-2026-07-15.mp4/2026-07-15", pending)
	}

	if err := svc.ConfirmImport(pending[0].ID, "2026-07-15", "Sarah M."); err != nil {
		t.Fatalf("ConfirmImport() unexpected error: %v", err)
	}

	pending, err = svc.ListPendingImports()
	if err != nil {
		t.Fatalf("ListPendingImports() unexpected error: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("ListPendingImports() after confirming = %+v, expected empty", pending)
	}

	var count int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM lessons WHERE tutor = ?`, "Sarah M.").Scan(&count); err != nil {
		t.Fatalf("lessons count failed: %v", err)
	}
	if count != 1 {
		t.Errorf("lessons with tutor Sarah M. = %d, expected 1", count)
	}
}

func TestImportService_ScanFolderTwiceDoesNotDuplicateCandidate(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	storageRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(storageRoot, "aula.mp4"), []byte("stable-content"), 0o644); err != nil {
		t.Fatalf("preparing fixture video failed: %v", err)
	}
	if err := config.Save(&config.AppConfig{StorageRoot: storageRoot}); err != nil {
		t.Fatalf("config.Save() failed: %v", err)
	}

	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() failed: %v", err)
	}
	defer conn.Close()

	svc := NewImportService(conn)

	if _, err := svc.ScanFolder(); err != nil {
		t.Fatalf("first ScanFolder() unexpected error: %v", err)
	}
	sum, err := svc.ScanFolder()
	if err != nil {
		t.Fatalf("second ScanFolder() unexpected error: %v", err)
	}
	if sum.New != 0 || sum.Skipped != 1 {
		t.Errorf("second ScanFolder() Summary = %+v, expected {Skipped:1}", sum)
	}

	pending, err := svc.ListPendingImports()
	if err != nil {
		t.Fatalf("ListPendingImports() unexpected error: %v", err)
	}
	if len(pending) != 1 {
		t.Errorf("ListPendingImports() = %+v, expected still 1 candidate (not duplicated)", pending)
	}
}

func TestImportService_ConfirmImport_RejectsEmptyTutorOrDate(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() failed: %v", err)
	}
	defer conn.Close()
	svc := NewImportService(conn)

	if err := svc.ConfirmImport(1, "", "Sarah M."); err == nil {
		t.Error("ConfirmImport() with an empty date expected an error, got nil")
	}
	if err := svc.ConfirmImport(1, "2026-07-15", ""); err == nil {
		t.Error("ConfirmImport() with an empty tutor expected an error, got nil")
	}
}
```

- [ ] **Step 2: Run the test to confirm it fails**

```bash
go test ./services/... -run ImportService -v
```

Expected: `FAIL` — build error, `NewImportService`/`ImportService` undefined.

- [ ] **Step 3: Write `services/import.go`**

```go
package services

import (
	"database/sql"
	"fmt"

	"assistente-idiomas/internal/config"
	"assistente-idiomas/internal/db"
	"assistente-idiomas/internal/importer"
)

// ImportService covers Story 3: scanning the storage folder for
// not-yet-registered lesson videos, listing the candidates pending review,
// and confirming one of them (date/tutor) as a real lesson.
type ImportService struct {
	conn *sql.DB
}

func NewImportService(conn *sql.DB) *ImportService {
	return &ImportService{conn: conn}
}

// ScanSummary is the result of a scan, exposed to the frontend for a
// summary toast ("N novas, M atualizadas, E erros").
type ScanSummary struct {
	New     int `json:"new"`
	Updated int `json:"updated"`
	Skipped int `json:"skipped"`
	Errors  int `json:"errors"`
}

// PendingImport is a candidate awaiting review, in the format exposed to
// the frontend — only what the confirmation modal needs to show.
type PendingImport struct {
	ID            int64  `json:"id"`
	Path          string `json:"path"`
	SuggestedDate string `json:"suggestedDate"`
}

// ScanFolder scans storage_root (from config.Load) and updates
// pending_imports and lessons. Called automatically at the end of the
// first-run wizard and on demand via the Library's "Sincronizar pasta" button.
func (s *ImportService) ScanFolder() (ScanSummary, error) {
	cfg, err := config.Load()
	if err != nil {
		return ScanSummary{}, fmt.Errorf("load configuration: %w", err)
	}
	sum, err := importer.Scan(cfg.StorageRoot, &dbRepo{conn: s.conn})
	if err != nil {
		return ScanSummary{}, err
	}
	return ScanSummary{New: sum.New, Updated: sum.Updated, Skipped: sum.Skipped, Errors: sum.Errors}, nil
}

// ListPendingImports lists the candidates awaiting review, for the
// Library's "awaiting review" section.
func (s *ImportService) ListPendingImports() ([]PendingImport, error) {
	rows, err := db.ListPendingImports(s.conn)
	if err != nil {
		return nil, err
	}
	out := make([]PendingImport, 0, len(rows))
	for _, r := range rows {
		out = append(out, PendingImport{ID: r.ID, Path: r.Path, SuggestedDate: r.SuggestedDate})
	}
	return out, nil
}

// ConfirmImport writes candidate id as a real lesson (lessonDate in
// YYYY-MM-DD format, tutor free text) and creates the processing jobs.
func (s *ImportService) ConfirmImport(id int64, lessonDate string, tutor string) error {
	if lessonDate == "" {
		return fmt.Errorf("data da aula não pode ser vazia")
	}
	if tutor == "" {
		return fmt.Errorf("tutor não pode ser vazio")
	}
	_, err := db.ConfirmPendingImport(s.conn, id, lessonDate, tutor)
	return err
}

// dbRepo adapts internal/db (which exposes Lesson with path and hash
// together) to the hash-oriented interface internal/importer.Scan expects —
// Scan doesn't know about database/sql nor the internal/db package directly.
type dbRepo struct{ conn *sql.DB }

func (r *dbRepo) StatMatch(path string, size int64, mtime string) (bool, error) {
	lesson, err := db.FindLessonByPath(r.conn, path)
	if err != nil {
		return false, err
	}
	if lesson == nil {
		return false, nil
	}
	return lesson.FileSize == size && lesson.FileMTime == mtime, nil
}

func (r *dbRepo) LessonByHash(hash string) (string, bool, error) {
	lesson, err := db.FindLessonByHash(r.conn, hash)
	if err != nil {
		return "", false, err
	}
	if lesson == nil {
		return "", false, nil
	}
	return lesson.VideoPath, true, nil
}

func (r *dbRepo) UpdateLessonPath(hash string, path string, size int64, mtime string) error {
	lesson, err := db.FindLessonByHash(r.conn, hash)
	if err != nil {
		return err
	}
	if lesson == nil {
		return fmt.Errorf("lesson com hash %s não encontrada para atualizar path", hash)
	}
	return db.UpdateLessonPath(r.conn, lesson.ID, path, size, mtime)
}

func (r *dbRepo) PendingExists(hash string) (bool, error) {
	return db.FindPendingImportByHash(r.conn, hash)
}

func (r *dbRepo) InsertPending(c importer.Candidate) error {
	return db.InsertPendingImport(r.conn, db.PendingImport{
		Path:          c.Path,
		FileSize:      c.Size,
		FileMTime:     c.MTime,
		SHA256:        c.SHA256,
		SuggestedDate: c.SuggestedDate,
	})
}
```

- [ ] **Step 4: Run the tests again to confirm they pass**

```bash
go test ./services/... -run ImportService -v
```

Expected: all 3 tests `PASS`.

- [ ] **Step 5: `go build`, `go vet`, and `gofmt` check**

```bash
go build ./services/...
go vet ./services/...
gofmt -l services
```

Expected: no output from any of the three. If this fails with `undefined: pointer` or a
`pkg-config` error about `gtk4`/`webkitgtk-6.0`, see the Linux prerequisite note in Global
Constraints.

- [ ] **Step 6: Stage (do not commit — user controls commit timing)**

```bash
git add services
git status
```

---

### Task 4: Wire `main.go` — register `ImportService`

**Files:**
- Modify: `main.go`

**Interfaces:**
- Consumes: `services.NewImportService` (Task 3), the existing `conn *sql.DB` already opened in
  `main.go` from Story 2.
- Produces: the running app now exposes `ImportService` to the frontend. Nothing downstream in
  this story consumes `main.go` directly.

- [ ] **Step 1: Modify `main.go`**

Current content (from Story 2):

```go
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
```

Replace the `Services` slice with:

```go
	app := application.New(application.Options{
		Name:        "Assistente de Idiomas",
		Description: "Arquivo e análise de aulas de inglês do Cambly",
		Services: []application.Service{
			application.NewService(services.NewSetupService()),
			application.NewService(services.NewImportService(conn)),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})
```

(`conn` is the `*sql.DB` already opened a few lines above by `db.Open(dbPath)` — no new variable
needed.)

- [ ] **Step 2: `go build`, `go vet`, and `gofmt` check**

```bash
go build -o /tmp/assistente-idiomas-check .
go vet ./...
gofmt -l .
```

Expected: `go build`/`gofmt` clean. `go vet ./...` will print the **pre-existing**
`build/ios` `function main is undeclared` failure noted in Global Constraints — unrelated to this
change; if you want a clean run scoped to this story, use `go vet ./internal/... ./services/... .`
instead. `go build` here needs `frontend/dist` to exist (from a prior `wails3
build`/`npm run build`) for the `//go:embed all:frontend/dist` directive — if it doesn't exist
yet, run `cd frontend && npm run build && cd ..` first, or rely on Task 6's full `wails3 build`
instead.

- [ ] **Step 3: Stage (do not commit — user controls commit timing)**

```bash
git add main.go
git status
```

---

### Task 5: Frontend — pending-review list, confirmation modal, wizard scan step

**Files:**
- Create: `frontend/src/lib/ImportConfirmModal.svelte`
- Modify: `frontend/src/lib/screens/Library.svelte`
- Modify: `frontend/src/lib/SetupWizard.svelte`
- Create (generated, not hand-written — see Step 1): `frontend/bindings/assistente-idiomas/services/importservice.ts`, updated `frontend/bindings/assistente-idiomas/services/models.ts`

**Interfaces:**
- Consumes: the generated bindings for `ImportService` — `ScanFolder(): Promise<ScanSummary>`,
  `ListPendingImports(): Promise<PendingImport[] | null>`, `ConfirmImport(id: number, lessonDate:
  string, tutor: string): Promise<void>`, and the `PendingImport`/`ScanSummary` TypeScript
  interfaces (`{id, path, suggestedDate}` / `{new, updated, skipped, errors}`) — exact generated
  signatures, confirmed during planning by actually running the generator against
  `services/import.go` from Task 3. **Note the `| null`** on `ListPendingImports()` — the
  generator emits it because the Go slice can be `nil`; the frontend must coalesce it to `[]`.
- Produces: `ImportConfirmModal.svelte` (props `{ pending: PendingImport, onConfirmed: () => void,
  onClose: () => void }`), reused as-is by the future drag-and-drop story (Story 3b).

- [ ] **Step 1: Generate the bindings**

```bash
cd frontend && npm install && cd ..
wails3 generate bindings -ts -i ./...
```

(If `npm install` reports `Cannot find native binding` from `rolldown`, run `npm install` a second
time — see Global Constraints. If `wails3` isn't found, install it per Global Constraints first.)

Expected output includes a line like `Processed: N Packages, 2 Services, 6 Methods, 0 Enums, 2
Models, 0 Events` and creates/updates:
- `frontend/bindings/assistente-idiomas/services/importservice.ts` — exports `ScanFolder()`,
  `ListPendingImports()`, `ConfirmImport(id, lessonDate, tutor)`.
- `frontend/bindings/assistente-idiomas/services/models.ts` — gains `PendingImport` (`id: number`,
  `path: string`, `suggestedDate: string`) and `ScanSummary` (`new`, `updated`, `skipped`, `errors`
  — all `number`) interfaces, alongside whatever was already there from Story 2.

- [ ] **Step 2: Write `frontend/src/lib/ImportConfirmModal.svelte`**

```svelte
<script lang="ts">
  import { untrack } from "svelte";
  import { colors, fonts } from "./theme";
  import * as ImportService from "../../bindings/assistente-idiomas/services/importservice";
  import type { PendingImport } from "../../bindings/assistente-idiomas/services/models";

  let {
    pending,
    onConfirmed,
    onClose,
  }: {
    pending: PendingImport;
    onConfirmed: () => void;
    onClose: () => void;
  } = $props();

  // Editable copy of the suggested date — deliberately not reactive to
  // changes in `pending` (each candidate gets its own instance of this
  // component, see Library.svelte). `untrack` documents this intent for
  // the Svelte 5 linter.
  let lessonDate: string = $state(untrack(() => pending.suggestedDate));
  let tutor: string = $state("");
  let error: string = $state("");
  let saving: boolean = $state(false);

  async function confirm() {
    error = "";
    saving = true;
    try {
      await ImportService.ConfirmImport(pending.id, lessonDate, tutor);
      onConfirmed();
    } catch (e) {
      error = String(e);
    } finally {
      saving = false;
    }
  }
</script>

<div
  class="overlay"
  role="presentation"
  onclick={onClose}
  onkeydown={(e) => e.key === "Escape" && onClose()}
>
  <div
    class="card"
    style="background: {colors.surface}; border: 1px solid {colors.line}; color: {colors.text}; font-family: {fonts.body};"
    role="dialog"
    aria-modal="true"
    tabindex="-1"
    onclick={(e) => e.stopPropagation()}
    onkeydown={(e) => e.stopPropagation()}
  >
    <h2 style="font-family: {fonts.display};">Confirmar aula encontrada</h2>
    <p class="path" style="color: {colors.mut}; font-family: {fonts.mono};">{pending.path}</p>

    <label for="lesson-date">Data da aula</label>
    <input id="lesson-date" type="date" bind:value={lessonDate} />

    <label for="tutor">Tutor</label>
    <input id="tutor" type="text" bind:value={tutor} placeholder="Nome do tutor" />

    {#if error}
      <p class="error" style="color: {colors.red};">{error}</p>
    {/if}

    <div class="actions">
      <button class="secondary" onclick={onClose} disabled={saving}>Cancelar</button>
      <button class="primary" onclick={confirm} disabled={saving || !lessonDate || !tutor}>
        {saving ? "Salvando…" : "Confirmar"}
      </button>
    </div>
  </div>
</div>

<style>
  .overlay {
    position: fixed;
    inset: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    background: rgba(0, 0, 0, 0.6);
    z-index: 50;
    padding: 1.5rem;
  }
  .card {
    border-radius: 1rem;
    padding: 1.5rem;
    width: 100%;
    max-width: 24rem;
  }
  h2 {
    font-size: 1.1rem;
    margin: 0 0 0.5rem;
  }
  .path {
    font-size: 0.75rem;
    word-break: break-all;
    margin: 0 0 1.25rem;
  }
  label {
    display: block;
    font-size: 0.8rem;
    margin-bottom: 0.25rem;
  }
  input {
    width: 100%;
    padding: 0.5rem;
    margin-bottom: 1rem;
    box-sizing: border-box;
  }
  .error {
    font-size: 0.85rem;
    margin-bottom: 1rem;
  }
  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 0.5rem;
  }
  button {
    padding: 0.5rem 1rem;
    border-radius: 0.5rem;
    cursor: pointer;
  }
</style>
```

- [ ] **Step 3: Replace `frontend/src/lib/screens/Library.svelte`**

Current content (placeholder from Story 1):

```svelte
<script lang="ts">
  import { colors, fonts } from "../theme";
</script>

<div class="screen">
  <p style="font-family: {fonts.body}; color: {colors.mut};">
    Nenhuma aula importada ainda.
  </p>
</div>

<style>
  .screen {
    padding: 2rem;
    max-width: 64rem;
    margin: 0 auto;
    width: 100%;
  }
</style>
```

Replace it with:

```svelte
<script lang="ts">
  import { onMount } from "svelte";
  import { colors, fonts } from "../theme";
  import * as ImportService from "../../../bindings/assistente-idiomas/services/importservice";
  import type { PendingImport } from "../../../bindings/assistente-idiomas/services/models";
  import ImportConfirmModal from "../ImportConfirmModal.svelte";

  let pending: PendingImport[] = $state([]);
  let loading: boolean = $state(true);
  let syncing: boolean = $state(false);
  let syncMessage: string = $state("");
  let error: string = $state("");
  let reviewing: PendingImport | null = $state(null);

  async function loadPending() {
    try {
      pending = (await ImportService.ListPendingImports()) ?? [];
    } catch (e) {
      error = String(e);
    } finally {
      loading = false;
    }
  }

  async function syncFolder() {
    error = "";
    syncMessage = "";
    syncing = true;
    try {
      const summary = await ImportService.ScanFolder();
      syncMessage = `${summary.new} novas, ${summary.updated} atualizadas, ${summary.errors} erros`;
      await loadPending();
    } catch (e) {
      error = String(e);
    } finally {
      syncing = false;
    }
  }

  function closeReview() {
    reviewing = null;
  }

  async function onConfirmed() {
    reviewing = null;
    await loadPending();
  }

  onMount(loadPending);
</script>

<div class="screen" style="font-family: {fonts.body}; color: {colors.text};">
  <div class="header-row">
    <h1 style="font-family: {fonts.display};">Biblioteca</h1>
    <button onclick={syncFolder} disabled={syncing}>
      {syncing ? "Sincronizando…" : "Sincronizar pasta"}
    </button>
  </div>

  {#if syncMessage}
    <p class="hint" style="color: {colors.mut};">{syncMessage}</p>
  {/if}
  {#if error}
    <p class="error" style="color: {colors.red};">{error}</p>
  {/if}

  {#if loading}
    <p style="color: {colors.mut};">Carregando…</p>
  {:else if pending.length > 0}
    <section class="pending" style="background: {colors.surface}; border: 1px solid {colors.line};">
      <h2 style="font-family: {fonts.display};">{pending.length} aulas aguardando revisão</h2>
      <ul>
        {#each pending as item (item.id)}
          <li>
            <span class="path" style="font-family: {fonts.mono}; color: {colors.mut};">{item.path}</span>
            <button onclick={() => (reviewing = item)}>Revisar</button>
          </li>
        {/each}
      </ul>
    </section>
  {:else}
    <p style="color: {colors.mut};">Nenhuma aula importada ainda.</p>
  {/if}
</div>

{#if reviewing}
  <ImportConfirmModal pending={reviewing} {onConfirmed} onClose={closeReview} />
{/if}

<style>
  .screen {
    padding: 2rem;
    max-width: 64rem;
    margin: 0 auto;
    width: 100%;
  }
  .header-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 1rem;
  }
  h1 {
    font-size: 1.4rem;
    margin: 0;
  }
  button {
    padding: 0.5rem 1rem;
    border-radius: 0.5rem;
    cursor: pointer;
  }
  .hint {
    font-size: 0.85rem;
    margin: 0 0 1rem;
  }
  .error {
    font-size: 0.85rem;
    margin: 0 0 1rem;
  }
  .pending {
    border-radius: 0.75rem;
    padding: 1rem 1.25rem;
  }
  .pending h2 {
    font-size: 1rem;
    margin: 0 0 0.75rem;
  }
  .pending ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }
  .pending li {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
  }
  .path {
    font-size: 0.8rem;
    word-break: break-all;
  }
</style>
```

- [ ] **Step 4: Modify `frontend/src/lib/SetupWizard.svelte`**

Add the `ImportService` import and a third `"scanning"` step, right after the existing two-step
`<script>` block (from Story 2):

Change the imports and `Step` type:

```svelte
  import { colors, fonts } from "./theme";
  import * as SetupService from "../../bindings/assistente-idiomas/services/setupservice";
  import * as ImportService from "../../bindings/assistente-idiomas/services/importservice";

  let { onComplete }: { onComplete: () => void } = $props();

  type Step = "folder" | "credentials" | "scanning";
```

Change `complete()` to run the first scan before calling `onComplete`:

```svelte
  async function complete() {
    error = "";
    saving = true;
    try {
      await SetupService.CompleteSetup(storageRoot, apiKey);
      step = "scanning";
      // A scan failure must not lock up the wizard nor hide the app from
      // the user — setup is already saved; the next "Sincronizar pasta" in
      // the Library tries again.
      try {
        await ImportService.ScanFolder();
      } catch {
        // deliberately ignored — see comment above
      }
      onComplete();
    } catch (e) {
      error = String(e);
    } finally {
      saving = false;
    }
  }
```

Change the template's second branch to check for `"credentials"` explicitly and add the new
`"scanning"` branch:

```svelte
    {:else if step === "credentials"}
      <h1 style="font-family: {fonts.display};">Pasta selecionada</h1>
      <p class="mono" style="color: {colors.mut};">{storageRoot}</p>
      <h2 style="font-family: {fonts.display};">Chave da API (ElevenLabs)</h2>
      <input type="password" bind:value={apiKey} placeholder="sk-..." />
      <button onclick={complete} disabled={saving || apiKey.length === 0}>
        {saving ? "Salvando…" : "Concluir"}
      </button>
    {:else}
      <h1 style="font-family: {fonts.display};">Procurando aulas na pasta…</h1>
      <p style="color: {colors.mut};">
        Verificando se já existem vídeos de aula em {storageRoot}.
      </p>
    {/if}
```

(The rest of the file — `chooseFolder()`, the `{#if step === "folder"}` branch, the error
paragraph, and the `<style>` block — is unchanged from Story 2.)

- [ ] **Step 5: Verify with `svelte-check`**

```bash
cd frontend
npm run check
cd ..
```

Expected: `0 ERRORS 0 WARNINGS` (verified during planning with these exact components).

- [ ] **Step 6: Verify the production build**

```bash
cd frontend
npm run build
cd ..
```

Expected: ends with `✓ built in ...ms`, produces `frontend/dist/`.

- [ ] **Step 7: Stage (do not commit — user controls commit timing)**

```bash
git add frontend/src/lib/ImportConfirmModal.svelte frontend/src/lib/screens/Library.svelte frontend/src/lib/SetupWizard.svelte frontend/bindings
git status
```

---

### Task 6: Full build, manual verification, progress log

**Files:**
- Modify: `docs/phase-1-mvp.md` (progress table + checkboxes)

**Interfaces:**
- Consumes: everything from Tasks 1–5.
- Produces: nothing consumed by later tasks — this is the story's closing task.

- [ ] **Step 1: Full build**

```bash
wails3 build
```

Expected: succeeds, regenerates `frontend/bindings` (same content as Task 5, now via the full
Taskfile pipeline), produces the platform binary in `bin/`. Verified during planning: this
succeeds cleanly (only pre-existing GTK4 C-level deprecation warnings from Wails' own `linux_cgo.c`
on Linux, unrelated to this story's code).

- [ ] **Step 2: `go vet` and `gofmt` check, scoped to this story's packages**

```bash
go vet ./internal/... ./services/... .
gofmt -l .
```

Expected: no output from either. (`go vet ./...` unscoped will also report the pre-existing
`build/ios` failure from Global Constraints — not this story's concern.)

- [ ] **Step 3: Full test suite**

```bash
go test ./...
```

Expected: all packages `ok` (`internal/analysis`, `internal/config`, `internal/db`,
`internal/importer`, `internal/media`, `internal/stt`, `services` — the pre-existing packages from
earlier stories are unaffected by this story's changes).

- [ ] **Step 4: Manual visual verification**

```bash
wails3 dev
```

On a machine with **no** existing `config.json` (or with it removed for a clean test): drop a
couple of `.mp4` files directly into a test folder with **no** subfolder structure (e.g. flat,
some with a `YYYY-MM-DD` date in the filename, some without). Run the wizard, pick that folder as
the storage root, enter the ElevenLabs API key, click "Concluir". You should briefly see
"Procurando aulas na pasta…", then land on the normal shell with the Library showing "N aulas
aguardando revisão". Click "Revisar" on one: the modal should show its path, a date pre-filled
(the one from the filename, or today's/file's mtime date if the filename had none), and an empty
tutor field required before "Confirmar" enables. Confirm it — it should disappear from the
pending list. Click "Sincronizar pasta" again: summary should read `0 novas, 0 atualizadas, 0
erros` (everything already known). Rename one of the still-unconfirmed files on disk and click
"Sincronizar pasta" again — nothing changes for it (it's not a lesson yet, so this exercises the
"new candidate, no duplicate" path, not the path-update path; renaming an **already-confirmed**
lesson's file and re-syncing is what exercises the path-update behavior, and should not create a
second pending entry for it).

This step needs a real display, so it's manual — not scriptable in this environment.

- [ ] **Step 5: Update `docs/phase-1-mvp.md`**

Check all six boxes under `## Story 3 — Import lesson: scanning the existing folder` from
`- [ ]` to `- [x]`.

In the `## Progress log` table, add a row (keep the existing rows above it):

```
| 22/07/2026 | Story 3 implemented: recursive scan of the storage folder (identification by name+SHA-256, no assumed structure), stat-cache before hashing, pending candidates reviewed one by one in the Library (date/tutor modal), lesson+jobs created on confirmation | No extension besides `.mp4` recognized in this slice (easy to extend later); the `wails3` CLI must be installed manually (`go install .../cmd/wails3@v3.0.0-alpha2.117`) on a new machine, it's not a go.mod dependency |
```

- [ ] **Step 6: Stage (do not commit — user controls commit timing)**

```bash
git add docs/phase-1-mvp.md
git status
```

Expected: `git status` shows every file from Tasks 1–6 staged, nothing left unstaged from this
story. Do not run `git commit`.

---

## Self-Review Notes

- **Spec coverage:** all six `docs/phase-1-mvp.md` Story 3 acceptance criteria map to tasks —
  recursive scan without assumed structure + SHA-256 → Task 1 (`Scan`) and Task 2
  (`internal/db` hash storage); stat-cache before hashing → Task 1's `StatMatch` path + Task 2's
  `file_size`/`file_mtime` columns; known-hash-skip and moved-file path-update →
  `TestScan_KnownHashIsSkipped` / `TestScan_SameHashDifferentPathUpdatesLessonInsteadOfDuplicating`
  in Task 1; pending-review list + confirmation modal → Task 5; automatic scan at wizard end +
  on-demand "Sincronizar pasta" → Task 5 (`SetupWizard.svelte`, `Library.svelte`); duplicate
  import prevented by a database guarantee → Task 2's unique index on `video_hash`
  (`TestLessons_VideoHashUniqueIndexRejectsDuplicate`).
- **Placeholder scan:** no TBDs; every code step has literal file content; every command step
  states an expected output, including the manual-verification step (explicitly marked as such).
- **Type consistency:** `importer.Candidate`/`importer.Repo` (Task 1) are consumed unchanged by
  `dbRepo` in Task 3 — field names (`Path`, `Size`, `MTime`, `SHA256`, `SuggestedDate`) match
  exactly between the two. `services.PendingImport`/`services.ScanSummary` (Task 3) JSON field
  names (`id`/`path`/`suggestedDate`, `new`/`updated`/`skipped`/`errors`) match exactly what Task
  5's Svelte code reads, and match the actual generated `.ts` model interfaces confirmed by
  running the real generator during planning.
- **Deviation from the approved design doc, noted deliberately:** the design doc
  (`docs/superpowers/specs/2026-07-22-story-3-import-lesson-design.md`) sketched a `Repo`
  interface with `FindLessonByPathStat`/`LessonByHash` methods keyed by lesson ID
  (`(id int64, path string, found bool, err error)`). During planning this was simplified to a
  path-and-hash-oriented interface (`StatMatch(path, size, mtime) bool`,
  `LessonByHash(hash) (path string, found bool, err error)`,
  `UpdateLessonPath(hash, path, size, mtime) error`) — `internal/importer` never needs to see a
  lesson's numeric ID at all, only its path and hash, so threading `int64` IDs through the `Repo`
  interface would have been an unused abstraction. `dbRepo` (Task 3) looks up the ID internally
  (via `db.FindLessonByHash`) only where the underlying SQL actually needs it (`UPDATE ... WHERE
  id = ?`). Functionally identical behavior, smaller interface.
- **Environment discovery, not a design change:** a fresh sandbox needed the `wails3` CLI
  installed manually via `go install` (it's not vendored via `go.mod`, unlike every other
  dependency this story touches) and hit a transient `rolldown` native-binding error on the first
  `npm install` that a second `npm install` resolved. Neither is a code defect; both are recorded
  in Global Constraints and Task 5 Step 1 so they're not rediscovered from scratch on the next new
  machine. The GTK4/WebKitGTK-6 prerequisite itself is unchanged from Story 2's plan.
