package db

import (
	"path/filepath"
	"testing"
)

func TestFindLessonByPath_NotFoundReturnsNilNil(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	l, err := FindLessonByPath(conn, "aulas/nao-existe.mp4")
	if err != nil {
		t.Fatalf("FindLessonByPath() erro inesperado: %v", err)
	}
	if l != nil {
		t.Errorf("FindLessonByPath() = %+v, esperado nil", l)
	}
}

func TestFindLessonByPathAndByHash_FindExistingRow(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
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
		t.Fatalf("FindLessonByPath() erro inesperado: %v", err)
	}
	if byPath == nil || byPath.VideoHash != "hash-abc" || byPath.FileSize != 12345 {
		t.Errorf("FindLessonByPath() = %+v, esperado hash hash-abc e file_size 12345", byPath)
	}

	byHash, err := FindLessonByHash(conn, "hash-abc")
	if err != nil {
		t.Fatalf("FindLessonByHash() erro inesperado: %v", err)
	}
	if byHash == nil || byHash.VideoPath != "aula-01.mp4" {
		t.Errorf("FindLessonByHash() = %+v, esperado video_path aula-01.mp4", byHash)
	}
}

func TestUpdateLessonPath_ChangesPathSizeAndMTime(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
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
		t.Fatalf("UpdateLessonPath() erro inesperado: %v", err)
	}

	updated, err := FindLessonByHash(conn, "hash-abc")
	if err != nil {
		t.Fatalf("FindLessonByHash() erro inesperado: %v", err)
	}
	if updated.VideoPath != "new/aula-01.mp4" || updated.FileSize != 200 || updated.FileMTime != "2026-07-20T10:00:00Z" {
		t.Errorf("lesson após UpdateLessonPath = %+v, esperado path/size/mtime novos", updated)
	}
}

func TestLessons_VideoHashUniqueIndexRejectsDuplicate(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	insert := `INSERT INTO lessons (lesson_date, tutor, video_path, video_hash, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`
	if _, err := conn.Exec(insert, "2026-07-15", "Sarah", "a.mp4", "hash-dup", "2026-07-15T10:00:00Z", "2026-07-15T10:00:00Z"); err != nil {
		t.Fatalf("primeiro insert falhou: %v", err)
	}
	if _, err := conn.Exec(insert, "2026-07-16", "Sarah", "b.mp4", "hash-dup", "2026-07-16T10:00:00Z", "2026-07-16T10:00:00Z"); err == nil {
		t.Error("esperava erro de índice único em video_hash duplicado, veio nil")
	}
}

func TestFindLessonByID_FindsExistingAndNilWhenMissing(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	res, err := conn.Exec(
		`INSERT INTO lessons (lesson_date, tutor, video_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"2026-07-15", "Sarah", "aula-01.mp4", "2026-07-15T10:00:00Z", "2026-07-15T10:00:00Z",
	)
	if err != nil {
		t.Fatalf("insert de fixture falhou: %v", err)
	}
	id, _ := res.LastInsertId()

	found, err := FindLessonByID(conn, id)
	if err != nil {
		t.Fatalf("FindLessonByID() erro inesperado: %v", err)
	}
	if found == nil || found.VideoPath != "aula-01.mp4" {
		t.Errorf("FindLessonByID() = %+v, esperado video_path aula-01.mp4", found)
	}
	if found.DurationSeconds != nil {
		t.Errorf("DurationSeconds = %v, esperado nil antes de SetLessonDuration", *found.DurationSeconds)
	}

	missing, err := FindLessonByID(conn, id+999)
	if err != nil {
		t.Fatalf("FindLessonByID() erro inesperado: %v", err)
	}
	if missing != nil {
		t.Errorf("FindLessonByID() para id inexistente = %+v, esperado nil", missing)
	}
}

func TestSetLessonDuration_UpdatesDurationSeconds(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	res, err := conn.Exec(
		`INSERT INTO lessons (lesson_date, tutor, video_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"2026-07-15", "Sarah", "aula-01.mp4", "2026-07-15T10:00:00Z", "2026-07-15T10:00:00Z",
	)
	if err != nil {
		t.Fatalf("insert de fixture falhou: %v", err)
	}
	id, _ := res.LastInsertId()

	if err := SetLessonDuration(conn, id, 1860); err != nil {
		t.Fatalf("SetLessonDuration() erro inesperado: %v", err)
	}

	lesson, err := FindLessonByID(conn, id)
	if err != nil {
		t.Fatalf("FindLessonByID() erro inesperado: %v", err)
	}
	if lesson.DurationSeconds == nil || *lesson.DurationSeconds != 1860 {
		t.Errorf("DurationSeconds = %v, esperado 1860", lesson.DurationSeconds)
	}
}

func TestListTutors_ReturnsDistinctSortedTutors(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	insert := `INSERT INTO lessons (lesson_date, tutor, video_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`
	for _, row := range []struct{ date, tutor, path string }{
		{"2026-07-10", "Sarah M.", "a.mp4"},
		{"2026-07-11", "Sarah M.", "b.mp4"},
		{"2026-07-12", "James K.", "c.mp4"},
	} {
		if _, err := conn.Exec(insert, row.date, row.tutor, row.path, row.date+"T10:00:00Z", row.date+"T10:00:00Z"); err != nil {
			t.Fatalf("insert de fixture falhou: %v", err)
		}
	}

	tutors, err := ListTutors(conn)
	if err != nil {
		t.Fatalf("ListTutors() erro inesperado: %v", err)
	}
	if len(tutors) != 2 || tutors[0] != "James K." || tutors[1] != "Sarah M." {
		t.Errorf("ListTutors() = %+v, esperado [James K. Sarah M.] (distintos, ordem alfabética)", tutors)
	}
}
