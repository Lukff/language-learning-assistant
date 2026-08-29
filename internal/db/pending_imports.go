package db

import (
	"database/sql"
	"fmt"
	"time"
)

// PendingImport is a video found by the storage folder scan
// that hasn't been confirmed (date/tutor) by the user yet — see Story 3
// in docs/phase-1-mvp.md.
type PendingImport struct {
	ID            int64
	Path          string
	FileSize      int64
	FileMTime     string
	SHA256        string
	SuggestedDate string
}

// FindPendingImportByHash reports whether a pending candidate with
// this hash already exists — avoids duplicating the same scan across successive runs.
func FindPendingImportByHash(conn *sql.DB, hash string) (bool, error) {
	var id int64
	err := conn.QueryRow(`SELECT id FROM pending_imports WHERE sha256 = ?`, hash).Scan(&id)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("fetch pending_import by hash: %w", err)
	}
	return true, nil
}

// InsertPendingImport stores a new candidate found by the scan (or
// by a manual drop, Story 3b) and returns the id of the created row — the
// caller needs it to build the PendingImport exposed to the frontend
// without a second query.
func InsertPendingImport(conn *sql.DB, p PendingImport) (int64, error) {
	res, err := conn.Exec(
		`INSERT INTO pending_imports (path, file_size, file_mtime, sha256, suggested_date, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		p.Path, p.FileSize, p.FileMTime, p.SHA256, p.SuggestedDate, time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return 0, fmt.Errorf("insert pending_import: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("get pending_import id: %w", err)
	}
	return id, nil
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
// lessons (with lessonDate/teacherName provided by the user, the latter
// resolved to a teacher_id via GetOrCreateTeacherByName), creates the
// extract_audio and transcribe jobs as pending, and removes the candidate from
// pending_imports — all in a single transaction. If any step fails, the
// candidate remains intact in pending_imports for the user to try
// again.
func ConfirmPendingImport(conn *sql.DB, id int64, lessonDate string, teacherName string) (int64, error) {
	tx, err := conn.Begin()
	if err != nil {
		return 0, fmt.Errorf("start transaction: %w", err)
	}
	defer tx.Rollback()

	var p PendingImport
	err = tx.QueryRow(
		`SELECT id, path, file_size, file_mtime, sha256 FROM pending_imports WHERE id = ?`,
		id,
	).Scan(&p.ID, &p.Path, &p.FileSize, &p.FileMTime, &p.SHA256)
	if err == sql.ErrNoRows {
		return 0, fmt.Errorf("candidate %d not found (already confirmed or removed?)", id)
	}
	if err != nil {
		return 0, fmt.Errorf("fetch pending_import: %w", err)
	}

	teacherID, err := getOrCreateTeacherByName(tx, teacherName)
	if err != nil {
		return 0, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	res, err := tx.Exec(
		`INSERT INTO lessons (lesson_date, teacher_id, video_path, video_hash, file_size, file_mtime, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		lessonDate, teacherID, p.Path, p.SHA256, p.FileSize, p.FileMTime, now, now,
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
