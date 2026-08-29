// internal/db/teachers.go
package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	sqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// Teacher is a row from teachers — the entity that replaces the old
// free-text lessons.tutor column (see Story 9). Name is unique: renaming is a
// single-row UPDATE, reflected in all lessons via JOIN.
type Teacher struct {
	ID   int64
	Name string
}

// ListTeachers lists the registered teachers in alphabetical order —
// feeds both the import/edit combobox and the
// "Teachers" panel in Settings.
func ListTeachers(conn *sql.DB) ([]Teacher, error) {
	rows, err := conn.Query(`SELECT id, name FROM teachers ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("list teachers: %w", err)
	}
	defer rows.Close()

	out := make([]Teacher, 0)
	for rows.Next() {
		var t Teacher
		if err := rows.Scan(&t.ID, &t.Name); err != nil {
			return nil, fmt.Errorf("read teacher: %w", err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate teachers: %w", err)
	}
	return out, nil
}

// execer is satisfied by both *sql.DB and *sql.Tx — lets
// getOrCreateTeacherByName run either standalone (GetOrCreateTeacherByName)
// or inside an already open transaction (ConfirmPendingImport, which
// needs the teacher creation, if it's a new one, to be part of the same
// transaction as the lesson). Same pattern as rowScanner in lesson_status.go.
type execer interface {
	QueryRow(query string, args ...any) *sql.Row
	Exec(query string, args ...any) (sql.Result, error)
}

// getOrCreateTeacherByName is the implementation shared by
// GetOrCreateTeacherByName and ConfirmPendingImport (via tx). name is
// trimmed (TrimSpace) before any lookup/write — prevents stray
// whitespace from the combobox (e.g. "Sarah M. " vs "Sarah M.") from becoming a
// duplicate teacher, exactly the diverging-spelling problem the
// teachers entity exists to prevent (see the Story 9 design
// context).
func getOrCreateTeacherByName(q execer, name string) (int64, error) {
	name = strings.TrimSpace(name)
	var id int64
	err := q.QueryRow(`SELECT id FROM teachers WHERE name = ?`, name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("fetch teacher by name: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	res, err := q.Exec(
		`INSERT INTO teachers (name, created_at, updated_at) VALUES (?, ?, ?)`,
		name, now, now,
	)
	if err != nil {
		return 0, fmt.Errorf("create teacher: %w", err)
	}
	id, err = res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("get created teacher id: %w", err)
	}
	return id, nil
}

// GetOrCreateTeacherByName resolves the free-text name typed into the combobox
// (lesson import or edit) to a teacher_id: returns the existing
// id if the name is already registered, otherwise creates a new teacher.
func GetOrCreateTeacherByName(conn *sql.DB, name string) (int64, error) {
	return getOrCreateTeacherByName(conn, name)
}

// RenameTeacher renames teacher id — automatically reflected in all
// their lessons (JOIN, there's no name copy in lessons). A collision with a
// name already used by another teacher (UNIQUE) becomes a readable error, without
// merging records (out of scope for Story 9).
func RenameTeacher(conn *sql.DB, id int64, newName string) error {
	newName = strings.TrimSpace(newName)
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := conn.Exec(`UPDATE teachers SET name = ?, updated_at = ? WHERE id = ?`, newName, now, id)
	if err != nil {
		if isUniqueConstraintError(err) {
			return fmt.Errorf("a teacher with this name already exists")
		}
		return fmt.Errorf("rename teacher: %w", err)
	}
	return nil
}

// isUniqueConstraintError detects a UNIQUE violation from the SQLite driver —
// isolated here (the only driver-specific dependency in the
// repository layer, per CLAUDE.md) so RenameTeacher can return a readable
// message instead of SQLite's raw error.
func isUniqueConstraintError(err error) bool {
	var sqliteErr *sqlite.Error
	return errors.As(err, &sqliteErr) && sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE
}
