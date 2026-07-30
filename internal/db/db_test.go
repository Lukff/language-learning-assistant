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

	teacherID, err := GetOrCreateTeacherByName(conn, "Fulano")
	if err != nil {
		t.Fatalf("GetOrCreateTeacherByName() erro inesperado: %v", err)
	}
	_, err = conn.Exec(
		`INSERT INTO lessons (lesson_date, teacher_id, video_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"2026-07-21", teacherID, "aulas/2026/x.mp4", "2026-07-21T10:00:00Z", "2026-07-21T10:00:00Z",
	)
	if err != nil {
		t.Fatalf("insert em lessons falhou: %v", err)
	}

	lesson, err := FindLessonByPath(conn, "aulas/2026/x.mp4")
	if err != nil {
		t.Fatalf("FindLessonByPath() erro inesperado: %v", err)
	}
	if lesson == nil || lesson.TeacherName != "Fulano" {
		t.Errorf("TeacherName = %+v, esperado \"Fulano\"", lesson)
	}
}

func TestOpen_ForeignKeysEnforced(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")

	conn, err := Open(path)
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	_, err = conn.Exec(
		`INSERT INTO transcripts (lesson_id, raw_json_path, utterances, created_at) VALUES (?, ?, ?, ?)`,
		999999, "x.json", "[]", "2026-07-21T10:00:00Z",
	)
	if err == nil {
		t.Error("esperava erro de foreign key ao inserir transcript com lesson_id inexistente, veio nil")
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
