// Package importer scans the storage folder for lesson videos
// not yet in the database (Story 3, docs/phase-1-mvp.md). It doesn't
// assume any subfolder structure: identification and dedup are always
// by file name + SHA-256, never by path convention. Imports
// nothing from Wails (thin layer).
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
// Deliberately short in this slice of Story 3 (only .mp4, the
// most common Cambly/browser format) — extending it is just adding here.
var videoExtensions = []string{".mp4"}

// Candidate is a new video found by the scan, with no lesson
// registered yet nor a pending candidate with the same hash.
type Candidate struct {
	Path          string // relative to the scan root, always with "/" (filepath.ToSlash)
	Size          int64
	MTime         string // RFC3339 (UTC)
	SHA256        string
	SuggestedDate string // YYYY-MM-DD, extracted from the file name or mtime
}

// Summary summarizes the result of a scan.
type Summary struct {
	New     int // new candidates written to pending_imports
	Updated int // existing lessons with an updated path (file moved/renamed)
	Skipped int // already-known files (existing lesson or pending), nothing changed
	Errors  int // files that failed to read/stat/hash — don't interrupt the scan
}

// Repo is what Scan needs from the database. Implemented by an adapter over
// internal/db in services/import.go; in this package's tests, by an in-memory
// fake — Scan never imports internal/db or database/sql directly.
type Repo interface {
	// StatMatch indicates whether a lesson is already registered at exactly this
	// path, with this size and mtime — if so, the file is known and
	// unchanged, and Scan skips it without computing a hash.
	StatMatch(path string, size int64, mtime string) (bool, error)

	// LessonByHash returns the path of a lesson already registered with this
	// hash, if it exists.
	LessonByHash(hash string) (path string, found bool, err error)

	// UpdateLessonPath updates the path/size/mtime of the lesson with this
	// hash — used when the file just moved/was renamed.
	UpdateLessonPath(hash string, path string, size int64, mtime string) error

	// PendingExists indicates whether a pending candidate with this
	// hash already exists (from a previous, not-yet-confirmed scan).
	PendingExists(hash string) (bool, error)

	// InsertPending writes a new candidate.
	InsertPending(c Candidate) error
}

// Scan walks root recursively, filters by videoExtensions, and decides,
// for each file, whether it's known (skip), moved (updates the
// lesson's path), or is a new candidate (writes to pending_imports via
// repo.InsertPending). An error processing a specific file doesn't abort the
// scan — it's counted in Summary.Errors and the rest continues.
func Scan(root string, repo Repo) (Summary, error) {
	var sum Summary

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if path == root {
				return walkErr
			}
			sum.Errors++
			return nil
		}
		if d.IsDir() || !HasVideoExtension(path) {
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

		hash, err := HashFile(path)
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
			SuggestedDate: SuggestDate(filepath.Base(path), info.ModTime()),
		}
		if err := repo.InsertPending(candidate); err != nil {
			sum.Errors++
			return nil
		}
		sum.New++
		return nil
	})
	if err != nil {
		return sum, fmt.Errorf("varrer pasta de armazenamento: %w", err)
	}
	return sum, nil
}

// HasVideoExtension indicates whether path has a recognized video extension
// (today only .mp4). Exported because services.ImportService.DropImport
// (Story 3b) needs the same check for a file dropped via
// drag-and-drop, outside the folder scan.
func HasVideoExtension(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	for _, want := range videoExtensions {
		if ext == want {
			return true
		}
	}
	return false
}

// HashFile computes the SHA-256 of the file at path. Exported for the same
// reason as HasVideoExtension — DropImport hashes a file outside the
// folder scan.
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("abrir arquivo: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("ler arquivo: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

var isoDateInName = regexp.MustCompile(`(\d{4}-\d{2}-\d{2})`)

// SuggestDate tries to find a YYYY-MM-DD date in the file name — in that
// case the time is unknown, suggested as 00:00. Lacking a date in the
// name, it uses the file's mtime date and time (a reasonable approximation: the
// file is usually downloaded right after the lesson). Format compatible
// with <input type="datetime-local"> (YYYY-MM-DDTHH:MM). It's just a
// pre-filled guess in the confirmation modal — the user can always correct
// the date and time.
func SuggestDate(filename string, mtime time.Time) string {
	if m := isoDateInName.FindString(filename); m != "" {
		return m + "T00:00"
	}
	return mtime.Format("2006-01-02T15:04")
}
