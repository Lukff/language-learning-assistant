package db

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestUpsertPrompt_InsertsAndIsIdempotent(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	id1, err := UpsertPrompt(conn, "analyze_corrections", 1, "conteúdo v1")
	if err != nil {
		t.Fatalf("UpsertPrompt() erro inesperado: %v", err)
	}

	id2, err := UpsertPrompt(conn, "analyze_corrections", 1, "conteúdo v1")
	if err != nil {
		t.Fatalf("segunda UpsertPrompt() erro inesperado: %v", err)
	}
	if id1 != id2 {
		t.Errorf("id2 = %d, esperado igual a id1 = %d (idempotente)", id2, id1)
	}

	var count int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM prompts WHERE name = ?`, "analyze_corrections").Scan(&count); err != nil {
		t.Fatalf("contar prompts falhou: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, esperado 1 (sem duplicar)", count)
	}
}

func TestUpsertPrompt_DivergentContentKeepsExisting(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	id1, err := UpsertPrompt(conn, "analyze_corrections", 1, "conteúdo original")
	if err != nil {
		t.Fatalf("UpsertPrompt() erro inesperado: %v", err)
	}

	id2, err := UpsertPrompt(conn, "analyze_corrections", 1, "conteúdo diferente")
	if err != nil {
		t.Fatalf("segunda UpsertPrompt() erro inesperado: %v", err)
	}
	if id1 != id2 {
		t.Errorf("id2 = %d, esperado igual a id1 = %d", id2, id1)
	}

	var content string
	if err := conn.QueryRow(`SELECT content FROM prompts WHERE id = ?`, id1).Scan(&content); err != nil {
		t.Fatalf("select em prompts falhou: %v", err)
	}
	if content != "conteúdo original" {
		t.Errorf("content = %q, esperado \"conteúdo original\" (não sobrescrito)", content)
	}
}

func insertLessonFixture(t *testing.T, conn *sql.DB) int64 {
	t.Helper()
	teacherID, err := GetOrCreateTeacherByName(conn, "Fulano")
	if err != nil {
		t.Fatalf("GetOrCreateTeacherByName() de fixture falhou: %v", err)
	}
	res, err := conn.Exec(
		`INSERT INTO lessons (lesson_date, teacher_id, video_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"2026-07-29", teacherID, "aula.mp4", "2026-07-29T09:00:00Z", "2026-07-29T09:00:00Z",
	)
	if err != nil {
		t.Fatalf("inserir lesson de fixture falhou: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

func TestUpsertAnalysisResult_InsertThenReplace(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := insertLessonFixture(t, conn)
	promptID, err := UpsertPrompt(conn, "analyze_corrections", 1, "conteúdo")
	if err != nil {
		t.Fatalf("UpsertPrompt() erro inesperado: %v", err)
	}

	if err := UpsertAnalysisResult(conn, lessonID, "analyze_corrections", promptID, "deepseek", `{"corrections":[]}`, "aula.corrections.json"); err != nil {
		t.Fatalf("UpsertAnalysisResult() erro inesperado: %v", err)
	}
	if err := UpsertAnalysisResult(conn, lessonID, "analyze_corrections", promptID, "deepseek", `{"corrections":[{"original":"x"}]}`, "aula.corrections.v2.json"); err != nil {
		t.Fatalf("segunda UpsertAnalysisResult() erro inesperado: %v", err)
	}

	r, err := FindAnalysisResult(conn, lessonID, "analyze_corrections")
	if err != nil {
		t.Fatalf("FindAnalysisResult() erro inesperado: %v", err)
	}
	if r == nil {
		t.Fatal("FindAnalysisResult() = nil, esperado resultado encontrado")
	}
	if r.ResultJSON != `{"corrections":[{"original":"x"}]}` {
		t.Errorf("ResultJSON = %q, esperado a segunda gravação (substituída)", r.ResultJSON)
	}
	if r.RawResponsePath != "aula.corrections.v2.json" {
		t.Errorf("RawResponsePath = %q, esperado atualizado", r.RawResponsePath)
	}

	var count int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM analysis_results WHERE lesson_id = ? AND task = ?`, lessonID, "analyze_corrections").Scan(&count); err != nil {
		t.Fatalf("contar analysis_results falhou: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, esperado 1 (substituído, não duplicado)", count)
	}
}

func TestFindAnalysisResult_NilWhenMissing(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := insertLessonFixture(t, conn)

	r, err := FindAnalysisResult(conn, lessonID, "analyze_corrections")
	if err != nil {
		t.Fatalf("FindAnalysisResult() erro inesperado: %v", err)
	}
	if r != nil {
		t.Errorf("FindAnalysisResult() = %+v, esperado nil", r)
	}
}

func TestReplaceLessonTopics_ReplacesEntirely(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := insertLessonFixture(t, conn)

	if err := ReplaceLessonTopics(conn, lessonID, []string{"viagens", "trabalho remoto"}); err != nil {
		t.Fatalf("ReplaceLessonTopics() erro inesperado: %v", err)
	}
	if err := ReplaceLessonTopics(conn, lessonID, []string{"receitas de família"}); err != nil {
		t.Fatalf("segunda ReplaceLessonTopics() erro inesperado: %v", err)
	}

	rows, err := conn.Query(`SELECT topic FROM lesson_topics WHERE lesson_id = ? ORDER BY topic`, lessonID)
	if err != nil {
		t.Fatalf("query em lesson_topics falhou: %v", err)
	}
	defer rows.Close()
	var topics []string
	for rows.Next() {
		var topic string
		if err := rows.Scan(&topic); err != nil {
			t.Fatalf("scan de topic falhou: %v", err)
		}
		topics = append(topics, topic)
	}
	if len(topics) != 1 || topics[0] != "receitas de família" {
		t.Errorf("topics = %+v, esperado apenas [\"receitas de família\"]", topics)
	}
}

func TestDeleteAnalysisResultsForLesson_DeletesAllTasksForLessonOnly(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonA := insertLessonFixture(t, conn)
	lessonB := insertLessonFixture(t, conn)
	promptID, err := UpsertPrompt(conn, "analyze_corrections", 1, "conteúdo")
	if err != nil {
		t.Fatalf("UpsertPrompt() erro inesperado: %v", err)
	}
	if err := UpsertAnalysisResult(conn, lessonA, "analyze_corrections", promptID, "deepseek", "[]", "a.json"); err != nil {
		t.Fatalf("UpsertAnalysisResult() lessonA erro inesperado: %v", err)
	}
	if err := UpsertAnalysisResult(conn, lessonA, "analyze_vocabulary", promptID, "deepseek", "[]", "a2.json"); err != nil {
		t.Fatalf("UpsertAnalysisResult() lessonA (segunda task) erro inesperado: %v", err)
	}
	if err := UpsertAnalysisResult(conn, lessonB, "analyze_corrections", promptID, "deepseek", "[]", "b.json"); err != nil {
		t.Fatalf("UpsertAnalysisResult() lessonB erro inesperado: %v", err)
	}

	if err := DeleteAnalysisResultsForLesson(conn, lessonA); err != nil {
		t.Fatalf("DeleteAnalysisResultsForLesson() erro inesperado: %v", err)
	}

	var countA int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM analysis_results WHERE lesson_id = ?`, lessonA).Scan(&countA); err != nil {
		t.Fatalf("contar analysis_results de lessonA falhou: %v", err)
	}
	if countA != 0 {
		t.Errorf("countA = %d, esperado 0 (todas as tasks apagadas)", countA)
	}

	var countB int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM analysis_results WHERE lesson_id = ?`, lessonB).Scan(&countB); err != nil {
		t.Fatalf("contar analysis_results de lessonB falhou: %v", err)
	}
	if countB != 1 {
		t.Errorf("countB = %d, esperado 1 (não deve ser afetada)", countB)
	}
}
