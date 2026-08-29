package services

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

// ImportService covers Story 3: scanning the storage folder for
// lesson videos not yet registered, listing the candidates
// pending review, and confirming one of them (date/tutor) as a real lesson.
type ImportService struct {
	conn     *sql.DB
	moveFile func(string, string) error
}

func NewImportService(conn *sql.DB) *ImportService {
	return &ImportService{
		conn:     conn,
		moveFile: moveFileNoReplace,
	}
}

// ScanSummary is the result of a scan, exposed to the frontend for a
// summary toast ("N new, M updated, E errors").
type ScanSummary struct {
	New     int `json:"new"`
	Updated int `json:"updated"`
	Skipped int `json:"skipped"`
	Errors  int `json:"errors"`
}

// PendingImport is a candidate awaiting review, in the format exposed to the
// frontend — only what the confirmation modal needs to show.
type PendingImport struct {
	ID            int64  `json:"id"`
	Path          string `json:"path"`
	SuggestedDate string `json:"suggestedDate"`
}

// DropResult is the result of processing a path received via
// native drag-and-drop (main.go, WindowFilesDropped event) — used as
// DropImport's return value (testable) and also as the payload of the
// DropErrorEvent event when Error is not empty.
type DropResult struct {
	Path  string `json:"path"`
	Error string `json:"error"`
}

// ScanFolder scans storage_root (from config.Load) and updates pending_imports
// and lessons. Called automatically at the end of the first-run wizard and on
// demand by the Library's "Sync folder" button.
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

// ListPendingImports lists the candidates awaiting review, for the Library's
// "awaiting review" section. A candidate whose file no longer exists
// in the current storage_root (e.g. the folder was changed in Settings —
// Story 8 — and the file wasn't found there) is excluded from the list.
// Check is always live (os.Stat), never persisted: same principle as
// LibraryService's VideoMissing — if the file reappears at the
// expected path, the candidate reappears on its own, with no need for another
// scan.
func (s *ImportService) ListPendingImports() ([]PendingImport, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("load configuration: %w", err)
	}
	rows, err := db.ListPendingImports(s.conn)
	if err != nil {
		return nil, err
	}
	out := make([]PendingImport, 0, len(rows))
	for _, r := range rows {
		if _, err := os.Stat(filepath.Join(cfg.StorageRoot, filepath.FromSlash(r.Path))); err != nil {
			continue
		}
		out = append(out, PendingImport{ID: r.ID, Path: r.Path, SuggestedDate: r.SuggestedDate})
	}
	return out, nil
}

// ConfirmImport saves candidate id as a real lesson (lessonDate in
// AAAA-MM-DDTHH:MM format, free-text tutor) and creates the processing jobs. After
// confirming, it tries to compute the video's duration (best effort — see
// setDurationBestEffort).
func (s *ImportService) ConfirmImport(id int64, lessonDate string, tutor string) error {
	if lessonDate == "" {
		return fmt.Errorf("lesson date cannot be empty")
	}
	if tutor == "" {
		return fmt.Errorf("tutor cannot be empty")
	}
	if !hasTimeComponent(lessonDate) {
		return fmt.Errorf("lesson time is required")
	}
	const lessonDateLayout = "2006-01-02T15:04"
	parsedLessonDate, err := time.Parse(lessonDateLayout, lessonDate)
	if err != nil || parsedLessonDate.Format(lessonDateLayout) != lessonDate {
		return fmt.Errorf("lesson date and time must be in YYYY-MM-DDTHH:MM format")
	}
	lessonID, err := db.ConfirmPendingImport(s.conn, id, lessonDate, tutor)
	if err != nil {
		return err
	}
	s.setDurationBestEffort(lessonID)
	renameVideoBestEffort(s.conn, s.moveFile, lessonID)
	return nil
}

// hasTimeComponent indicates whether lessonDate (format of <input type="datetime-local">,
// "AAAA-MM-DDTHH:MM") has a non-empty time component after the "T".
// ConfirmImport requires this because the standardized filename
// (StandardFilename, internal/importer) depends on a time always being present —
// see docs/superpowers/specs/2026-07-23-story-3-standardized-filename-design.md.
func hasTimeComponent(lessonDate string) bool {
	_, timePart, found := strings.Cut(lessonDate, "T")
	return found && timePart != ""
}

// setDurationBestEffort computes the newly-confirmed video's duration via
// ffprobe and saves it to lessons.duration_seconds. Duration is metadata
// intrinsic to the video, not a product of the transcription pipeline — it should stay
// available even if the pipeline fails (resilience principle,
// CLAUDE.md). That's why any failure here (ffprobe missing, invalid
// file, etc.) is only logged: never propagated as a ConfirmImport error,
// since the lesson has already been confirmed successfully.
func (s *ImportService) setDurationBestEffort(lessonID int64) {
	lesson, err := db.FindLessonByID(s.conn, lessonID)
	if err != nil || lesson == nil {
		return
	}
	cfg, err := config.Load()
	if err != nil {
		return
	}
	videoPath := filepath.Join(cfg.StorageRoot, filepath.FromSlash(lesson.VideoPath))
	dur, err := media.Duration(context.Background(), videoPath)
	if err != nil {
		slog.Warn("importer: could not compute video duration", "lesson_id", lessonID, "error", err)
		return
	}
	if err := db.SetLessonDuration(s.conn, lessonID, int64(dur.Seconds())); err != nil {
		slog.Warn("importer: could not save video duration", "lesson_id", lessonID, "error", err)
	}
}

// renameVideoBestEffort renames a lesson's video to the current standardized
// name (date/teacher), always in the same folder. Failures are logged and don't
// invalidate the caller — reused by both ImportService.ConfirmImport
// and LibraryService.UpdateLesson (Story 9).
func renameVideoBestEffort(conn *sql.DB, moveFile func(string, string) error, lessonID int64) {
	lesson, err := db.FindLessonByID(conn, lessonID)
	if err != nil {
		slog.Warn("importer: could not load lesson before renaming video", "lesson_id", lessonID, "error", err)
		return
	}
	if lesson == nil {
		return
	}
	cfg, err := config.Load()
	if err != nil {
		slog.Warn("importer: could not load configuration before renaming video", "lesson_id", lessonID, "error", err)
		return
	}

	relDir := filepath.Dir(filepath.FromSlash(lesson.VideoPath))
	targetName := importer.StandardFilename(lesson.LessonDate, lesson.TeacherName, filepath.Ext(lesson.VideoPath))
	targetExt := filepath.Ext(targetName)
	targetBase := strings.TrimSuffix(targetName, targetExt)
	currentAbsPath := filepath.Join(cfg.StorageRoot, filepath.FromSlash(lesson.VideoPath))
	targetAbsDir := filepath.Join(cfg.StorageRoot, relDir)
	info, err := os.Stat(currentAbsPath)
	if err != nil {
		slog.Warn("importer: could not read video before renaming", "lesson_id", lessonID, "error", err)
		return
	}

	candidate := targetName
	for i := 2; ; i++ {
		candidateAbsPath := filepath.Join(targetAbsDir, candidate)
		available, err := renameCandidateAvailable(info, candidateAbsPath)
		if err != nil {
			slog.Warn("importer: error checking standardized name collision", "lesson_id", lessonID, "error", err)
			return
		}
		if available {
			break
		}
		candidate = fmt.Sprintf("%s-%d%s", targetBase, i, targetExt)
	}

	targetAbsPath := filepath.Join(targetAbsDir, candidate)
	targetInfo, err := os.Stat(targetAbsPath)
	targetIsCurrent := err == nil && os.SameFile(info, targetInfo)
	if err != nil && !os.IsNotExist(err) {
		slog.Warn("importer: could not check destination before moving video", "lesson_id", lessonID, "error", err)
		return
	}
	var rollback func() error
	if targetIsCurrent {
		sameFileKind := classifySameFilePath(currentAbsPath, targetAbsPath, runtime.GOOS)
		if err := moveToExistingSameFile(currentAbsPath, targetAbsPath, sameFileKind, moveFile); err != nil {
			slog.Warn("importer: could not complete move to the same file", "lesson_id", lessonID, "error", err)
			return
		}
		switch sameFileKind {
		case sameFileCaseOnlyPath:
			rollback = func() error { return moveFile(targetAbsPath, currentAbsPath) }
		}
	} else {
		if err := moveFile(currentAbsPath, targetAbsPath); err != nil {
			slog.Warn("importer: could not move video to standardized name", "lesson_id", lessonID, "error", err)
			return
		}
		rollback = func() error { return moveFile(targetAbsPath, currentAbsPath) }
	}

	targetRelPath := filepath.ToSlash(filepath.Join(relDir, candidate))
	mtime := info.ModTime().UTC().Format(time.RFC3339)
	if err := db.UpdateLessonPath(conn, lessonID, targetRelPath, info.Size(), mtime); err != nil {
		if rollback != nil {
			rollbackErr := rollback()
			if rollbackErr != nil {
				slog.Error("importer: failed to update lesson path and to roll back the move", "lesson_id", lessonID, "original_error", err, "rollback_error", rollbackErr)
				return
			}
		}
		slog.Warn("importer: could not update lesson path after move; operation rolled back", "lesson_id", lessonID, "original_error", err)
	}
}

type sameFilePathKind uint8

const (
	sameFileExactPath sameFilePathKind = iota
	sameFileCaseOnlyPath
	sameFileDistinctHardLink
)

func classifySameFilePath(oldPath, newPath, goos string) sameFilePathKind {
	oldPath = filepath.Clean(oldPath)
	newPath = filepath.Clean(newPath)
	if oldPath == newPath {
		return sameFileExactPath
	}
	if goos == "windows" && strings.EqualFold(oldPath, newPath) {
		return sameFileCaseOnlyPath
	}
	return sameFileDistinctHardLink
}

func moveToExistingSameFile(oldPath, newPath string, kind sameFilePathKind, moveFile func(string, string) error) error {
	switch kind {
	case sameFileExactPath:
		return nil
	case sameFileCaseOnlyPath:
		return moveFile(oldPath, newPath)
	case sameFileDistinctHardLink:
		// There's no portable atomic conditional unlink. Preserves both names
		// to eliminate any risk of removing a concurrent entry.
		return nil
	default:
		return fmt.Errorf("unknown same-file classification: %d", kind)
	}
}
func renameCandidateAvailable(currentInfo os.FileInfo, candidatePath string) (bool, error) {
	candidateInfo, err := os.Stat(candidatePath)
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return os.SameFile(currentInfo, candidateInfo), nil
}

// dbRepo adapts internal/db (which exposes Lesson with path and hash together) to the
// hash-oriented interface that internal/importer.Scan expects — Scan does not
// know about database/sql or the internal/db package directly.
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
		return fmt.Errorf("lesson with hash %s not found to update path", hash)
	}
	return db.UpdateLessonPath(r.conn, lesson.ID, path, size, mtime)
}

func (r *dbRepo) PendingExists(hash string) (bool, error) {
	return db.FindPendingImportByHash(r.conn, hash)
}

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

// DroppedImportEvent is emitted once per candidate successfully created
// via drag-and-drop — the payload is a PendingImport, the same format that
// ListPendingImports already exposes, so ImportConfirmModal can open without fetching
// again (Story 3b).
const DroppedImportEvent = "import:dropped"

// DropErrorEvent is emitted for a file that failed (unrecognized
// extension, duplicate, copy failure) — no candidate was created
// for that file.
const DropErrorEvent = "import:drop-error"

// DropImport processes files received via Wails's native drag-and-drop
// (main.go calls this from the WindowFilesDropped event) — one
// pending candidate per valid file, reusing the same hash-based dedupe
// from the scan (Story 3). Unlike renameVideoBestEffort's best-effort
// approach, a failure here is always reported (via
// DropErrorEvent and in the returned DropResult) — without a candidate in
// pending_imports the user would have no other way of knowing that the dropped
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
		return PendingImport{}, fmt.Errorf("unsupported file type (only .mp4)")
	}
	cfg, err := config.Load()
	if err != nil {
		return PendingImport{}, fmt.Errorf("load configuration: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return PendingImport{}, fmt.Errorf("read file: %w", err)
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
		return PendingImport{}, fmt.Errorf("this lesson has already been imported")
	}
	alreadyPending, err := db.FindPendingImportByHash(s.conn, hash)
	if err != nil {
		return PendingImport{}, err
	}
	if alreadyPending {
		return PendingImport{}, fmt.Errorf("this lesson is already awaiting review")
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
			return "", "", 0, fmt.Errorf("copy video to storage root: %w", err)
		}
		rel = filepath.ToSlash(copiedRel)
	}

	info, err := os.Stat(filepath.Join(storageRoot, filepath.FromSlash(rel)))
	if err != nil {
		return "", "", 0, fmt.Errorf("read file at storage root: %w", err)
	}
	return rel, info.ModTime().UTC().Format(time.RFC3339), info.Size(), nil
}

// relativeIfInsideStorageRoot resolves symlinks for path and
// storageRoot and indicates whether path falls inside storageRoot — in that case
// it returns the relative path (always with "/"). inside=false (relPath="") if
// path is outside, or if resolution fails (the caller then copies,
// a safe-by-default handling).
func relativeIfInsideStorageRoot(path, storageRoot string) (relPath string, inside bool, err error) {
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", false, fmt.Errorf("resolve file symlinks: %w", err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(storageRoot)
	if err != nil {
		return "", false, fmt.Errorf("resolve storage root symlinks: %w", err)
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
		// Tests call DropImport without application.New() having run — same
		// handling as WailsJobNotifier.JobChanged (services/jobs_notifier.go):
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
