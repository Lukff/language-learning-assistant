package db

import (
	"path/filepath"
	"testing"
)

func TestHasTranscript_FalseThenTrueAfterInsert(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	res, err := conn.Exec(
		`INSERT INTO lessons (lesson_date, tutor, video_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"2026-07-22", "Fulano", "aula.mp4", "2026-07-22T09:00:00Z", "2026-07-22T09:00:00Z",
	)
	if err != nil {
		t.Fatalf("inserir lesson de fixture falhou: %v", err)
	}
	lessonID, _ := res.LastInsertId()

	has, err := HasTranscript(conn, lessonID)
	if err != nil {
		t.Fatalf("HasTranscript() erro inesperado: %v", err)
	}
	if has {
		t.Error("HasTranscript() = true antes de inserir, esperado false")
	}

	if err := InsertTranscript(conn, lessonID, "aula.transcript.json", `[{"speaker":"speaker_0","text":"hi"}]`); err != nil {
		t.Fatalf("InsertTranscript() erro inesperado: %v", err)
	}

	has, err = HasTranscript(conn, lessonID)
	if err != nil {
		t.Fatalf("HasTranscript() erro inesperado: %v", err)
	}
	if !has {
		t.Error("HasTranscript() = false após inserir, esperado true")
	}
}

func TestInsertTranscript_PersistsRawPathAndUtterances(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	res, err := conn.Exec(
		`INSERT INTO lessons (lesson_date, tutor, video_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"2026-07-22", "Fulano", "aula.mp4", "2026-07-22T09:00:00Z", "2026-07-22T09:00:00Z",
	)
	if err != nil {
		t.Fatalf("inserir lesson de fixture falhou: %v", err)
	}
	lessonID, _ := res.LastInsertId()

	if err := InsertTranscript(conn, lessonID, "aula.transcript.json", `[{"speaker":"speaker_0","text":"hi"}]`); err != nil {
		t.Fatalf("InsertTranscript() erro inesperado: %v", err)
	}

	var rawPath, utterances string
	err = conn.QueryRow(`SELECT raw_json_path, utterances FROM transcripts WHERE lesson_id = ?`, lessonID).Scan(&rawPath, &utterances)
	if err != nil {
		t.Fatalf("select em transcripts falhou: %v", err)
	}
	if rawPath != "aula.transcript.json" {
		t.Errorf("raw_json_path = %q, esperado aula.transcript.json", rawPath)
	}
	if utterances != `[{"speaker":"speaker_0","text":"hi"}]` {
		t.Errorf("utterances = %q, não bate com o que foi inserido", utterances)
	}
}
