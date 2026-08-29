# Story 3 — Standardized filename on confirmation: Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** After the user confirms date/time/tutor in the import modal, the video
file is renamed *in place* to a standardized name (`YYYY-MM-DD_HHHMM_tutor-slug.ext`), and
confirmation now requires time (not just date) as mandatory.

**Architecture:** A new pure function `internal/importer.StandardFilename` computes the
standardized name from `lessonDate`/`tutor`/extension. `services/import.go` gains (a) a stricter
validation in `ConfirmImport` (time is mandatory, not just non-empty) and (b) a
best-effort step `renameVideoBestEffort`, called after the lesson has already been confirmed
successfully, which resolves name collisions on disk, runs `os.Rename`, and updates
`lessons.video_path` via `db.UpdateLessonPath` (an already-existing function, reused). No
change in `internal/jobs/worker.go` — it always rereads `video_path` from the database at run
time.

**Tech Stack:** Go stdlib (`path/filepath`, `os`, `strings`, `regexp`, `fmt`, `time`) — no
new dependency.

## Global Constraints

- No new external dependency (`CLAUDE.md`): accents removed via a manual substitution table in
  pure Go, not a transliteration library.
- Portable SQL in the repository layer — this slice adds no new SQL (reuses
  `db.UpdateLessonPath`, already existing).
- A rename failure can never bring down `ConfirmImport` nor leave the lesson in an inconsistent
  state (resilience principle from `CLAUDE.md`) — always best-effort, only logged via `slog.Warn`.
- Name format: `YYYY-MM-DD_HHHMM_tutor-slug.ext` (e.g.: `2026-07-23_14H30_maria-jose.mp4`) —
  `H` as the hour/minute separator, `_` between date and time and between time and slug, no
  "no time" fallback (upstream validation guarantees time is always present).
- Name collision: suffix `-2`, `-3`, ... before the extension.
- Rename is always *in place* (same folder) — never moves into a subfolder structure.

---

### Task 1: `internal/importer.StandardFilename` (pure function)

**Files:**
- Create: `internal/importer/naming.go`
- Test: `internal/importer/naming_test.go`

**Interfaces:**
- Produces: `func StandardFilename(lessonDate, tutor, ext string) string` — used by Task 3
  (`services/import.go`). `lessonDate` is always `"YYYY-MM-DDTHH:MM"` (guaranteed by Task 2's
  validation before any caller uses this function in production); `ext` is the extension with
  the dot (e.g.: `".mp4"`, as returned by `filepath.Ext`).

- [ ] **Step 1: Write the failing test**

Create `internal/importer/naming_test.go`:

```go
package importer

import "testing"

func TestStandardFilename(t *testing.T) {
	tests := []struct {
		name       string
		lessonDate string
		tutor      string
		ext        string
		want       string
	}{
		{
			name:       "date, time, and a simple tutor name",
			lessonDate: "2026-07-23T14:30",
			tutor:      "Maria José",
			ext:        ".mp4",
			want:       "2026-07-23_14H30_maria-jose.mp4",
		},
		{
			name:       "tutor with a period and a space",
			lessonDate: "2026-07-15T09:05",
			tutor:      "Sarah M.",
			ext:        ".mp4",
			want:       "2026-07-15_09H05_sarah-m.mp4",
		},
		{
			name:       "uppercase extension normalized",
			lessonDate: "2026-07-15T09:05",
			tutor:      "Sarah",
			ext:        ".MP4",
			want:       "2026-07-15_09H05_sarah.mp4",
		},
		{
			name:       "tutor with multiple spaces and varied accents",
			lessonDate: "2026-01-05T23:59",
			tutor:      "  João  Ñandú  ",
			ext:        ".mp4",
			want:       "2026-01-05_23H59_joao-nandu.mp4",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StandardFilename(tt.lessonDate, tt.tutor, tt.ext)
			if got != tt.want {
				t.Errorf("StandardFilename(%q, %q, %q) = %q, want %q", tt.lessonDate, tt.tutor, tt.ext, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run the test and confirm it fails**

Run: `go test ./internal/importer/... -run TestStandardFilename -v`
Expected: FAIL — `undefined: StandardFilename` (the function doesn't exist yet).

- [ ] **Step 3: Implement `StandardFilename`**

Create `internal/importer/naming.go`:

```go
// Package importer — see importer.go. This file covers standardizing the
// video filename after confirmation (Story 3, an additional criterion
// recorded in docs/fase-1-mvp.md and designed in
// docs/superpowers/specs/2026-07-23-historia-3-renomeacao-padronizada-design.md).
package importer

import (
	"fmt"
	"regexp"
	"strings"
)

// accentReplacer removes the most common accents in PT/ES proper names —
// avoids depending on a transliteration library just for this (CLAUDE.md: no
// new dependency without justification). The input is expected to already be
// lowercase (strings.ToLower already normalizes most of the corresponding
// uppercase/accented forms).
var accentReplacer = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a",
	"é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i",
	"ó", "o", "ò", "o", "ô", "o", "õ", "o", "ö", "o",
	"ú", "u", "ù", "u", "û", "u", "ü", "u",
	"ç", "c", "ñ", "n", "ý", "y",
)

var nonSlugRun = regexp.MustCompile(`[^a-z0-9]+`)

// slugify normalizes a tutor name for use in a filename: lowercase,
// no accents, any run of characters outside [a-z0-9] becomes a single "-",
// with no "-" at either end.
func slugify(s string) string {
	s = strings.ToLower(s)
	s = accentReplacer.Replace(s)
	s = nonSlugRun.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// StandardFilename derives the standardized post-confirmation filename, in
// the format "YYYY-MM-DD_HHHMM_tutor-slug.ext" (e.g.: "2026-07-23_14H30_maria-jose.mp4").
// lessonDate is the raw value from <input type="datetime-local">
// ("YYYY-MM-DDTHH:MM"); the caller (services/import.go ConfirmImport) already
// guarantees this format before calling this function — there is no fallback
// here for a date without a time. ext includes the dot (e.g.: ".mp4"), as
// returned by filepath.Ext, and is normalized to lowercase.
func StandardFilename(lessonDate, tutor, ext string) string {
	datePart, timePart, _ := strings.Cut(lessonDate, "T")
	timePart = strings.ReplaceAll(timePart, ":", "H")
	slug := slugify(tutor)
	return fmt.Sprintf("%s_%s_%s%s", datePart, timePart, slug, strings.ToLower(ext))
}
```

- [ ] **Step 4: Run the test and confirm it passes**

Run: `go test ./internal/importer/... -run TestStandardFilename -v`
Expected: PASS on every subtest.

- [ ] **Step 5: Run `go vet` and the whole package**

Run: `go vet ./internal/importer/... && go test ./internal/importer/... -v`
Expected: `go vet` produces no output; all tests in the package (including the pre-existing
`Scan` ones) pass.

- [ ] **Step 6: Commit**

```bash
git add internal/importer/naming.go internal/importer/naming_test.go
git commit -m "feat: add StandardFilename for standardized video naming"
```

---

### Task 2: Mandatory time validation in `ConfirmImport`

**Files:**
- Modify: `services/import.go:77-90` (`ConfirmImport`)
- Modify: `services/import_test.go` — adjust `TestImportService_ConfirmImport_SucceedsEvenWhenDurationProbeFails` (it used a date without a time) and add a new rejection test.

**Interfaces:**
- Consumes: nothing from previous tasks yet (this task doesn't use `StandardFilename`).
- Produces: `ConfirmImport` now returns an error (`"horário da aula é obrigatório"`) when
  `lessonDate` has no time component. Task 3 depends on this behavior already being in
  effect before wiring up the rename (otherwise `renameVideoBestEffort` would have to handle
  the "no time" case, which the design explicitly avoids).

- [ ] **Step 1: Write the failing test**

In `services/import_test.go`, add (after `TestImportService_ConfirmImport_RejectsEmptyTutorOrDate`):

```go
func TestImportService_ConfirmImport_RejectsDateWithoutTime(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	storageRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(storageRoot, "aula.mp4"), []byte("conteudo"), 0o644); err != nil {
		t.Fatalf("preparar vídeo de fixture falhou: %v", err)
	}
	if err := config.Save(&config.AppConfig{StorageRoot: storageRoot}); err != nil {
		t.Fatalf("config.Save() falhou: %v", err)
	}

	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	svc := NewImportService(conn)
	if _, err := svc.ScanFolder(); err != nil {
		t.Fatalf("ScanFolder() erro inesperado: %v", err)
	}
	pending, err := svc.ListPendingImports()
	if err != nil || len(pending) != 1 {
		t.Fatalf("setup: ListPendingImports() = %+v, %v", pending, err)
	}

	if err := svc.ConfirmImport(pending[0].ID, "2026-07-15", "Sarah M."); err == nil {
		t.Error("ConfirmImport() com data sem horário esperava erro, veio nil")
	}

	pending, err = svc.ListPendingImports()
	if err != nil {
		t.Fatalf("ListPendingImports() erro inesperado: %v", err)
	}
	if len(pending) != 1 {
		t.Errorf("candidato deveria continuar pendente após confirmação recusada, ListPendingImports() = %+v", pending)
	}

	var count int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM lessons`).Scan(&count); err != nil {
		t.Fatalf("count de lessons falhou: %v", err)
	}
	if count != 0 {
		t.Errorf("nenhuma lesson deveria ter sido criada, count = %d", count)
	}
}
```

Also adjust the existing call in `TestImportService_ConfirmImport_SucceedsEvenWhenDurationProbeFails`
(`services/import_test.go:150`), which today uses `"2026-07-22"` (without a time) — it will
start failing against the new validation unless it's adjusted:

```go
	if err := svc.ConfirmImport(pending[0].ID, "2026-07-22", "Sarah M."); err != nil {
```
becomes:
```go
	if err := svc.ConfirmImport(pending[0].ID, "2026-07-22T09:00", "Sarah M."); err != nil {
```

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `go test ./services/... -run TestImportService_ConfirmImport -v`
Expected: `TestImportService_ConfirmImport_RejectsDateWithoutTime` FAILs (expected an error, got
nil — `ConfirmImport` doesn't validate time yet). The other `ConfirmImport` tests keep
passing (Step 1's adjustment already made them compatible).

- [ ] **Step 3: Implement the validation**

In `services/import.go`, modify `ConfirmImport` (lines 77-90):

```go
func (s *ImportService) ConfirmImport(id int64, lessonDate string, tutor string) error {
	if lessonDate == "" {
		return fmt.Errorf("data da aula não pode ser vazia")
	}
	if !hasTimeComponent(lessonDate) {
		return fmt.Errorf("horário da aula é obrigatório")
	}
	if tutor == "" {
		return fmt.Errorf("tutor não pode ser vazio")
	}
	lessonID, err := db.ConfirmPendingImport(s.conn, id, lessonDate, tutor)
	if err != nil {
		return err
	}
	s.setDurationBestEffort(lessonID)
	return nil
}

// hasTimeComponent reports whether lessonDate (in the format from
// <input type="datetime-local">, "YYYY-MM-DDTHH:MM") has a non-empty time
// component after the "T". ConfirmImport requires this because the
// standardized filename (StandardFilename, internal/importer) depends on a
// time always being present — see
// docs/superpowers/specs/2026-07-23-historia-3-renomeacao-padronizada-design.md.
func hasTimeComponent(lessonDate string) bool {
	_, timePart, found := strings.Cut(lessonDate, "T")
	return found && timePart != ""
}
```

Add `"strings"` to `services/import.go`'s import block (not yet imported in this
file).

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `go test ./services/... -run TestImportService_ConfirmImport -v`
Expected: PASS on all of them, including `TestImportService_ConfirmImport_RejectsDateWithoutTime`.

- [ ] **Step 5: Run the whole package and `go vet`**

Run: `go vet ./services/... && go test ./services/... -v`
Expected: no output from `go vet`; every test in the package passes (no regression in the
`ScanFolder`/queue/library tests that already existed).

- [ ] **Step 6: Commit**

```bash
git add services/import.go services/import_test.go
git commit -m "feat: require time on import confirmation"
```

---

### Task 3: Best-effort rename of the video on confirmation

**Files:**
- Modify: `services/import.go` (add `renameVideoBestEffort` and call it from `ConfirmImport`)
- Modify: `services/import_test.go` (new success test)

**Interfaces:**
- Consumes: `importer.StandardFilename(lessonDate, tutor, ext string) string` (Task 1);
  `db.FindLessonByID(conn *sql.DB, id int64) (*db.Lesson, error)` and
  `db.UpdateLessonPath(conn *sql.DB, lessonID int64, path string, size int64, fileMTime string) error`
  (already existing in `internal/db/lessons.go`); `hasTimeComponent` (Task 2, guarantees that
  `lesson.LessonDate` always has a time when this code runs).
- Produces: `func (s *ImportService) renameVideoBestEffort(lessonID int64)` — called only
  internally by `ConfirmImport`, with no return value (best-effort, errors only logged). Task 4
  (collision) and Task 5 (rename failure) extend this same function's behavior — no
  signature change expected in either of them.

- [ ] **Step 1: Write the failing test**

In `services/import_test.go`, add:

```go
func TestImportService_ConfirmImport_RenamesVideoToStandardFilename(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	storageRoot := t.TempDir()
	originalName := "cambly-download-xyz.mp4"
	if err := os.WriteFile(filepath.Join(storageRoot, originalName), []byte("conteudo-de-video"), 0o644); err != nil {
		t.Fatalf("preparar vídeo de fixture falhou: %v", err)
	}
	if err := config.Save(&config.AppConfig{StorageRoot: storageRoot}); err != nil {
		t.Fatalf("config.Save() falhou: %v", err)
	}

	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	svc := NewImportService(conn)
	if _, err := svc.ScanFolder(); err != nil {
		t.Fatalf("ScanFolder() erro inesperado: %v", err)
	}
	pending, err := svc.ListPendingImports()
	if err != nil || len(pending) != 1 {
		t.Fatalf("setup: ListPendingImports() = %+v, %v", pending, err)
	}

	if err := svc.ConfirmImport(pending[0].ID, "2026-07-23T14:30", "Maria José"); err != nil {
		t.Fatalf("ConfirmImport() erro inesperado: %v", err)
	}

	wantPath := "2026-07-23_14H30_maria-jose.mp4"
	lesson, err := db.FindLessonByPath(conn, wantPath)
	if err != nil {
		t.Fatalf("FindLessonByPath() erro inesperado: %v", err)
	}
	if lesson == nil {
		t.Fatalf("lesson não encontrada no path padronizado %q — video_path não foi atualizado", wantPath)
	}

	if _, err := os.Stat(filepath.Join(storageRoot, wantPath)); err != nil {
		t.Errorf("arquivo renomeado não existe no disco em %q: %v", wantPath, err)
	}
	if _, err := os.Stat(filepath.Join(storageRoot, originalName)); !os.IsNotExist(err) {
		t.Errorf("arquivo original %q ainda existe no disco após rename (err=%v)", originalName, err)
	}
}
```

- [ ] **Step 2: Run the test and confirm it fails**

Run: `go test ./services/... -run TestImportService_ConfirmImport_RenamesVideoToStandardFilename -v`
Expected: FAIL — `lesson não encontrada no path padronizado` (the rename doesn't happen yet).

- [ ] **Step 3: Implement `renameVideoBestEffort` and wire it into `ConfirmImport`**

In `services/import.go`, add the call inside `ConfirmImport` (right after
`s.setDurationBestEffort(lessonID)`):

```go
	s.setDurationBestEffort(lessonID)
	s.renameVideoBestEffort(lessonID)
	return nil
}
```

And add the new function (after `setDurationBestEffort`):

```go
// renameVideoBestEffort renames the newly-confirmed video to the
// standardized name (internal/importer.StandardFilename), always within the
// same folder (never moves it to a different directory). It is best-effort,
// in the same spirit as setDurationBestEffort: any failure (permission,
// I/O, unresolvable collision) is only logged — the filename is cosmetic,
// never critical to the app's operation (resilience principle, CLAUDE.md).
// See docs/superpowers/specs/2026-07-23-historia-3-renomeacao-padronizada-design.md.
func (s *ImportService) renameVideoBestEffort(lessonID int64) {
	lesson, err := db.FindLessonByID(s.conn, lessonID)
	if err != nil || lesson == nil {
		return
	}
	cfg, err := config.Load()
	if err != nil {
		return
	}

	relDir := filepath.Dir(filepath.FromSlash(lesson.VideoPath))
	ext := filepath.Ext(lesson.VideoPath)
	targetName := importer.StandardFilename(lesson.LessonDate, lesson.Tutor, ext)

	currentAbsPath := filepath.Join(cfg.StorageRoot, filepath.FromSlash(lesson.VideoPath))
	targetAbsDir := filepath.Join(cfg.StorageRoot, relDir)

	candidate := targetName
	for i := 2; ; i++ {
		candidateAbsPath := filepath.Join(targetAbsDir, candidate)
		if candidateAbsPath == currentAbsPath {
			break // already has this name — nothing to do
		}
		if _, err := os.Stat(candidateAbsPath); os.IsNotExist(err) {
			break // name is free
		} else if err != nil {
			slog.Warn("importer: erro ao checar colisão de nome padronizado", "lesson_id", lessonID, "erro", err)
			return
		}
		base := strings.TrimSuffix(targetName, ext)
		candidate = fmt.Sprintf("%s-%d%s", base, i, ext)
	}

	targetAbsPath := filepath.Join(targetAbsDir, candidate)
	if targetAbsPath == currentAbsPath {
		return
	}

	if err := os.Rename(currentAbsPath, targetAbsPath); err != nil {
		slog.Warn("importer: não foi possível renomear o vídeo pro nome padronizado", "lesson_id", lessonID, "erro", err)
		return
	}

	info, err := os.Stat(targetAbsPath)
	if err != nil {
		slog.Warn("importer: não foi possível reler o vídeo após renomear", "lesson_id", lessonID, "erro", err)
		return
	}
	targetRelPath := filepath.ToSlash(filepath.Join(relDir, candidate))
	mtime := info.ModTime().UTC().Format(time.RFC3339)
	if err := db.UpdateLessonPath(s.conn, lessonID, targetRelPath, info.Size(), mtime); err != nil {
		slog.Warn("importer: não foi possível atualizar o path da lesson após renomear", "lesson_id", lessonID, "erro", err)
	}
}
```

Add `"os"`, `"strings"`, and `"time"` to `services/import.go`'s import block (`"strings"`
was already added in Task 2; `"os"` and `"time"` are new in this file — check whether they're
already imported before duplicating).

- [ ] **Step 4: Run the test and confirm it passes**

Run: `go test ./services/... -run TestImportService_ConfirmImport_RenamesVideoToStandardFilename -v`
Expected: PASS.

- [ ] **Step 5: Run the whole package and `go vet`**

Run: `go vet ./services/... && go test ./services/... -v`
Expected: no output from `go vet`; every test in the package passes, including
`TestImportService_ScanFolderThenListThenConfirm` (which now also triggers the rename, but makes
no assertion on the final path — it should keep passing unchanged).

- [ ] **Step 6: Commit**

```bash
git add services/import.go services/import_test.go
git commit -m "feat: rename video to the standardized name on import confirmation"
```

---

### Task 4: Name collision (`-2` suffix)

**Files:**
- Modify: `services/import_test.go` (new test)

**Interfaces:**
- Consumes: `renameVideoBestEffort` (Task 3, no signature change — the collision loop already
  implemented in Task 3 is exactly what this test validates).
- Produces: no new interface — a verification/coverage task over behavior already
  implemented in Task 3.

- [ ] **Step 1: Write the failing test (if Task 3 has any collision bug)**

In `services/import_test.go`, add:

```go
func TestImportService_ConfirmImport_ResolvesFilenameCollisionWithSuffix(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	storageRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(storageRoot, "a.mp4"), []byte("conteudo-a"), 0o644); err != nil {
		t.Fatalf("preparar vídeo a.mp4 falhou: %v", err)
	}
	if err := os.WriteFile(filepath.Join(storageRoot, "b.mp4"), []byte("conteudo-b-bem-diferente"), 0o644); err != nil {
		t.Fatalf("preparar vídeo b.mp4 falhou: %v", err)
	}
	if err := config.Save(&config.AppConfig{StorageRoot: storageRoot}); err != nil {
		t.Fatalf("config.Save() falhou: %v", err)
	}

	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	svc := NewImportService(conn)
	if _, err := svc.ScanFolder(); err != nil {
		t.Fatalf("ScanFolder() erro inesperado: %v", err)
	}
	pending, err := svc.ListPendingImports()
	if err != nil || len(pending) != 2 {
		t.Fatalf("setup: ListPendingImports() = %+v, %v", pending, err)
	}

	var idA, idB int64
	for _, p := range pending {
		switch p.Path {
		case "a.mp4":
			idA = p.ID
		case "b.mp4":
			idB = p.ID
		}
	}
	if idA == 0 || idB == 0 {
		t.Fatalf("não achei os dois candidatos esperados (a.mp4/b.mp4) em %+v", pending)
	}

	// Same date/time/tutor for both lessons — same target name, forces a collision.
	if err := svc.ConfirmImport(idA, "2026-07-23T14:30", "Maria José"); err != nil {
		t.Fatalf("ConfirmImport(a) erro inesperado: %v", err)
	}
	if err := svc.ConfirmImport(idB, "2026-07-23T14:30", "Maria José"); err != nil {
		t.Fatalf("ConfirmImport(b) erro inesperado: %v", err)
	}

	if _, err := os.Stat(filepath.Join(storageRoot, "2026-07-23_14H30_maria-jose.mp4")); err != nil {
		t.Errorf("primeira aula deveria ter o nome base, sem sufixo: %v", err)
	}
	if _, err := os.Stat(filepath.Join(storageRoot, "2026-07-23_14H30_maria-jose-2.mp4")); err != nil {
		t.Errorf("segunda aula deveria ter o sufixo -2: %v", err)
	}
}
```

- [ ] **Step 2: Run the test**

Run: `go test ./services/... -run TestImportService_ConfirmImport_ResolvesFilenameCollisionWithSuffix -v`
Expected: PASS (the collision logic was already implemented in Task 3 — this test is its
coverage; if it fails, review `renameVideoBestEffort`'s loop before continuing).

- [ ] **Step 3: Run the whole package**

Run: `go test ./services/... -v`
Expected: every test passes, no regression.

- [ ] **Step 4: Commit**

```bash
git add services/import_test.go
git commit -m "test: cover standardized name collision with -2 suffix"
```

---

### Task 5: A rename failure doesn't bring down confirmation

**Files:**
- Modify: `services/import_test.go` (new test)

**Interfaces:**
- Consumes: `renameVideoBestEffort` (Task 3) — this test validates the error path (best-effort)
  already implemented in it.
- Produces: no new interface — coverage of the resilience behavior.

- [ ] **Step 1: Write the test**

In `services/import_test.go`, add:

```go
func TestImportService_ConfirmImport_SucceedsEvenWhenRenameFails(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	storageRoot := t.TempDir()
	originalName := "aula-original.mp4"
	if err := os.WriteFile(filepath.Join(storageRoot, originalName), []byte("conteudo"), 0o644); err != nil {
		t.Fatalf("preparar vídeo de fixture falhou: %v", err)
	}
	if err := config.Save(&config.AppConfig{StorageRoot: storageRoot}); err != nil {
		t.Fatalf("config.Save() falhou: %v", err)
	}

	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	svc := NewImportService(conn)
	if _, err := svc.ScanFolder(); err != nil {
		t.Fatalf("ScanFolder() erro inesperado: %v", err)
	}
	pending, err := svc.ListPendingImports()
	if err != nil || len(pending) != 1 {
		t.Fatalf("setup: ListPendingImports() = %+v, %v", pending, err)
	}

	// Remove write permission from the storage folder — os.Rename can't
	// create/remove directory entries without it, forcing the rename to
	// fail for real (not simulated), without needing to run as root
	// (which would ignore the permission).
	if err := os.Chmod(storageRoot, 0o555); err != nil {
		t.Fatalf("chmod da pasta de armazenamento falhou: %v", err)
	}
	defer os.Chmod(storageRoot, 0o755) // lets t.TempDir() clean up afterward

	if err := svc.ConfirmImport(pending[0].ID, "2026-07-23T14:30", "Maria José"); err != nil {
		t.Fatalf("ConfirmImport() não deveria falhar mesmo com rename impossível: %v", err)
	}

	lesson, err := db.FindLessonByPath(conn, originalName)
	if err != nil {
		t.Fatalf("FindLessonByPath() erro inesperado: %v", err)
	}
	if lesson == nil {
		t.Fatal("lesson deveria ter sido confirmada com o path original, já que o rename falhou")
	}
}
```

- [ ] **Step 2: Run the test**

Run: `go test ./services/... -run TestImportService_ConfirmImport_SucceedsEvenWhenRenameFails -v`
Expected: PASS. If it fails because the test is running as root (permission ignored), run
`whoami`/`id` to confirm — this project's development environment doesn't run tests as
root (`agent`, uid 1000), so this shouldn't happen here; if it happens in a different
environment, it's a sign the test needs a different strategy to force the failure (out of
scope for this slice to resolve now).

- [ ] **Step 3: Run the project's full test suite**

Run: `go vet ./... && go test ./... -count=1`
Expected: `go vet` produces no output; every package is `ok`, no regression in any other package
(jobs worker, library, queue, etc.).

- [ ] **Step 4: Check off the acceptance criterion in `docs/fase-1-mvp.md`**

In `docs/fase-1-mvp.md`, in the Story 3 section, replace:

```markdown
- [ ] Once confirmed (date/time/tutor in the modal), the video file is renamed *in place* to `YYYY-MM-DD_HHHMM_tutor-slug.ext` (e.g., `2026-07-23_14H30_maria-jose.mp4`); a rename failure doesn't block confirmation (best-effort, logged). Time is now required at confirmation — a candidate missing date, time, or tutor stays pending. Design in `docs/superpowers/specs/2026-07-23-historia-3-renomeacao-padronizada-design.md`.
```

with:

```markdown
- [x] Once confirmed (date/time/tutor in the modal), the video file is renamed *in place* to `YYYY-MM-DD_HHHMM_tutor-slug.ext` (e.g., `2026-07-23_14H30_maria-jose.mp4`); a rename failure doesn't block confirmation (best-effort, logged). Time is now required at confirmation — a candidate missing date, time, or tutor stays pending. Design in `docs/superpowers/specs/2026-07-23-historia-3-renomeacao-padronizada-design.md`.
```

Also add a row to the "Progress log" table (end of the file), following the
pattern of the other rows (today's date, what was done, relevant notes — e.g. mentioning
that this is an additional slice on top of an already-delivered flow, and that the lack of
real visual verification follows the same pattern as the previous stories, if applicable).

- [ ] **Step 5: Commit**

```bash
git add services/import_test.go docs/fase-1-mvp.md
git commit -m "test: cover confirmation resilience to rename failure and close Story 3's criterion"
```

---

## Self-Review

**Spec coverage:** name format (Task 1), tutor slug with accents (Task 1), collision with a
suffix (Task 4), mandatory time / candidate stays pending (Task 2), best-effort rename
without breaking confirmation (Task 3 and 5), no change to `internal/jobs/worker.go` (confirmed
in the spec's architecture notes — there's no task for this because no code change is
needed there). Every item under the spec's "Out of scope" (subfolders, retroactivity, Story
3b) generates no tasks, as expected.

**Placeholders:** no "TBD"/"implement later" — all code is complete and executable as
written.

**Type consistency:** `StandardFilename(lessonDate, tutor, ext string) string` (Task 1) is
used in `renameVideoBestEffort` (Task 3) with the same three `string`s in the same order;
`hasTimeComponent(lessonDate string) bool` (Task 2) and `renameVideoBestEffort(lessonID int64)`
(Task 3) have no name conflict with anything existing in `services/import.go` (checked against
the current file).
