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

func TestListLessons_ReturnsAllOrderedByDateDesc(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	insert := `INSERT INTO lessons (lesson_date, tutor, video_path, video_hash, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`
	if _, err := conn.Exec(insert, "2026-07-10", "Sarah", "a.mp4", "hash-a", "2026-07-10T10:00:00Z", "2026-07-10T10:00:00Z"); err != nil {
		t.Fatalf("insert a falhou: %v", err)
	}
	if _, err := conn.Exec(insert, "2026-07-20", "Marcus", "b.mp4", "hash-b", "2026-07-20T10:00:00Z", "2026-07-20T10:00:00Z"); err != nil {
		t.Fatalf("insert b falhou: %v", err)
	}

	lessons, err := ListLessons(conn)
	if err != nil {
		t.Fatalf("ListLessons() erro inesperado: %v", err)
	}
	if len(lessons) != 2 {
		t.Fatalf("ListLessons() = %d lessons, esperado 2", len(lessons))
	}
	if lessons[0].VideoPath != "b.mp4" || lessons[0].LessonDate != "2026-07-20" || lessons[0].Tutor != "Marcus" {
		t.Errorf("lessons[0] = %+v, esperado b.mp4/2026-07-20/Marcus (mais recente primeiro)", lessons[0])
	}
	if lessons[1].VideoPath != "a.mp4" || lessons[1].LessonDate != "2026-07-10" || lessons[1].Tutor != "Sarah" {
		t.Errorf("lessons[1] = %+v, esperado a.mp4/2026-07-10/Sarah", lessons[1])
	}
}

func TestListLessons_EmptyReturnsEmptyNotNilError(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessons, err := ListLessons(conn)
	if err != nil {
		t.Fatalf("ListLessons() erro inesperado: %v", err)
	}
	if len(lessons) != 0 {
		t.Errorf("ListLessons() = %+v, esperado vazio", lessons)
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

	missing, err := FindLessonByID(conn, id+999)
	if err != nil {
		t.Fatalf("FindLessonByID() erro inesperado: %v", err)
	}
	if missing != nil {
		t.Errorf("FindLessonByID() para id inexistente = %+v, esperado nil", missing)
	}
}
