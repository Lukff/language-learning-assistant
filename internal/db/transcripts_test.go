package db

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"assistente-idiomas/internal/stt"
)

func TestHasTranscript_FalseThenTrueAfterInsert(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	teacherID, err := GetOrCreateTeacherByName(conn, "Fulano")
	if err != nil {
		t.Fatalf("GetOrCreateTeacherByName() de fixture falhou: %v", err)
	}
	res, err := conn.Exec(
		`INSERT INTO lessons (lesson_date, teacher_id, video_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"2026-07-22", teacherID, "aula.mp4", "2026-07-22T09:00:00Z", "2026-07-22T09:00:00Z",
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

	teacherID, err := GetOrCreateTeacherByName(conn, "Fulano")
	if err != nil {
		t.Fatalf("GetOrCreateTeacherByName() de fixture falhou: %v", err)
	}
	res, err := conn.Exec(
		`INSERT INTO lessons (lesson_date, teacher_id, video_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"2026-07-22", teacherID, "aula.mp4", "2026-07-22T09:00:00Z", "2026-07-22T09:00:00Z",
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

func TestFindTranscriptByLessonID_NilWhenMissing(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	teacherID, err := GetOrCreateTeacherByName(conn, "Fulano")
	if err != nil {
		t.Fatalf("GetOrCreateTeacherByName() de fixture falhou: %v", err)
	}
	res, err := conn.Exec(
		`INSERT INTO lessons (lesson_date, teacher_id, video_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"2026-07-22", teacherID, "aula.mp4", "2026-07-22T09:00:00Z", "2026-07-22T09:00:00Z",
	)
	if err != nil {
		t.Fatalf("inserir lesson de fixture falhou: %v", err)
	}
	lessonID, _ := res.LastInsertId()

	tr, err := FindTranscriptByLessonID(conn, lessonID)
	if err != nil {
		t.Fatalf("FindTranscriptByLessonID() erro inesperado: %v", err)
	}
	if tr != nil {
		t.Errorf("FindTranscriptByLessonID() = %+v, esperado nil sem transcrição", tr)
	}
}

func TestFindTranscriptByLessonID_UnmarshalsUtterances(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	teacherID, err := GetOrCreateTeacherByName(conn, "Fulano")
	if err != nil {
		t.Fatalf("GetOrCreateTeacherByName() de fixture falhou: %v", err)
	}
	res, err := conn.Exec(
		`INSERT INTO lessons (lesson_date, teacher_id, video_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"2026-07-22", teacherID, "aula.mp4", "2026-07-22T09:00:00Z", "2026-07-22T09:00:00Z",
	)
	if err != nil {
		t.Fatalf("inserir lesson de fixture falhou: %v", err)
	}
	lessonID, _ := res.LastInsertId()

	want := []stt.Utterance{
		{Speaker: "speaker_0", Text: "Hello", Start: 0, End: 2 * time.Second},
		{Speaker: "speaker_1", Text: "Hi there", Start: 2 * time.Second, End: 5 * time.Second},
	}
	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("json.Marshal() de fixture falhou: %v", err)
	}
	if err := InsertTranscript(conn, lessonID, "aula.transcript.json", string(raw)); err != nil {
		t.Fatalf("InsertTranscript() erro inesperado: %v", err)
	}

	tr, err := FindTranscriptByLessonID(conn, lessonID)
	if err != nil {
		t.Fatalf("FindTranscriptByLessonID() erro inesperado: %v", err)
	}
	if tr == nil {
		t.Fatal("FindTranscriptByLessonID() = nil, esperado transcript encontrado")
	}
	if tr.RawJSONPath != "aula.transcript.json" {
		t.Errorf("RawJSONPath = %q, esperado aula.transcript.json", tr.RawJSONPath)
	}
	if len(tr.Utterances) != 2 {
		t.Fatalf("Utterances = %+v, esperado 2 falas", tr.Utterances)
	}
	if tr.Utterances[0].Speaker != "speaker_0" || tr.Utterances[0].End != 2*time.Second {
		t.Errorf("Utterances[0] = %+v, não bate com a fixture", tr.Utterances[0])
	}
	if tr.Utterances[1].Text != "Hi there" || tr.Utterances[1].Start != 2*time.Second {
		t.Errorf("Utterances[1] = %+v, não bate com a fixture", tr.Utterances[1])
	}
}
