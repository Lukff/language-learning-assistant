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

	_, err = conn.Exec(
		`INSERT INTO lessons (lesson_date, tutor, video_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"2026-07-21", "Fulano", "aulas/2026/x.mp4", "2026-07-21T10:00:00Z", "2026-07-21T10:00:00Z",
	)
	if err != nil {
		t.Fatalf("insert em lessons falhou: %v", err)
	}

	var tutor string
	if err := conn.QueryRow(`SELECT tutor FROM lessons WHERE video_path = ?`, "aulas/2026/x.mp4").Scan(&tutor); err != nil {
		t.Fatalf("select em lessons falhou: %v", err)
	}
	if tutor != "Fulano" {
		t.Errorf("tutor = %q, esperado \"Fulano\"", tutor)
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
