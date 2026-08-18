// internal/db/migration_backfill_test.go
package db

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestMigration00005_BackfillsTeachersFromExistingLessonsData exercita a
// migration 00005 contra um banco que já tem dados legados em lessons.tutor
// (schema anterior, sem teachers/teacher_id) — o cenário real de "sem perda
// de dados no backfill" que os testes em teachers_test.go não cobrem, já que
// eles sempre partem de Open() (todas as migrations aplicadas do zero).
func TestMigration00005_BackfillsTeachersFromExistingLessonsData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	conn, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("sql.Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	goose.SetBaseFS(migrationsFS)
	defer goose.SetBaseFS(nil)
	if err := goose.SetDialect("sqlite"); err != nil {
		t.Fatalf("SetDialect() erro inesperado: %v", err)
	}

	if err := goose.UpTo(conn, "migrations", 4); err != nil {
		t.Fatalf("UpTo(4) erro inesperado: %v", err)
	}

	// Schema pré-migration 5: lessons.tutor (TEXT), sem teacher_id ainda.
	legacyLessons := []struct {
		date, tutor, videoPath string
	}{
		{"2026-07-10", "Sarah M.", "aulas/2026/a.mp4"},
		{"2026-07-11", "Sarah M.", "aulas/2026/b.mp4"},
		{"2026-07-12", "James K.", "aulas/2026/c.mp4"},
	}
	for _, l := range legacyLessons {
		if _, err := conn.Exec(
			`INSERT INTO lessons (lesson_date, tutor, video_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
			l.date, l.tutor, l.videoPath, "2026-07-10T10:00:00Z", "2026-07-10T10:00:00Z",
		); err != nil {
			t.Fatalf("insert legado em lessons (%s) falhou: %v", l.videoPath, err)
		}
	}

	if err := goose.UpTo(conn, "migrations", 5); err != nil {
		t.Fatalf("UpTo(5) erro inesperado: %v", err)
	}

	var teacherCount int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM teachers`).Scan(&teacherCount); err != nil {
		t.Fatalf("contar teachers falhou: %v", err)
	}
	if teacherCount != 2 {
		t.Errorf("teacherCount = %d, esperado 2 (dedup por nome)", teacherCount)
	}

	teacherIDByPath := make(map[string]int64)
	for _, l := range legacyLessons {
		var teacherID sql.NullInt64
		if err := conn.QueryRow(`SELECT teacher_id FROM lessons WHERE video_path = ?`, l.videoPath).Scan(&teacherID); err != nil {
			t.Fatalf("select teacher_id de %s falhou: %v", l.videoPath, err)
		}
		if !teacherID.Valid {
			t.Fatalf("teacher_id de %s é NULL, esperado backfill não-nulo", l.videoPath)
		}
		teacherIDByPath[l.videoPath] = teacherID.Int64
	}

	if teacherIDByPath["aulas/2026/a.mp4"] != teacherIDByPath["aulas/2026/b.mp4"] {
		t.Errorf("lessons a.mp4 e b.mp4 (ambas Sarah M.) têm teacher_id diferentes: %d != %d",
			teacherIDByPath["aulas/2026/a.mp4"], teacherIDByPath["aulas/2026/b.mp4"])
	}
	if teacherIDByPath["aulas/2026/a.mp4"] == teacherIDByPath["aulas/2026/c.mp4"] {
		t.Errorf("lesson de Sarah M. e a de James K. têm o mesmo teacher_id = %d, esperado distinto",
			teacherIDByPath["aulas/2026/a.mp4"])
	}

	var sarahName, jamesName string
	if err := conn.QueryRow(`SELECT name FROM teachers WHERE id = ?`, teacherIDByPath["aulas/2026/a.mp4"]).Scan(&sarahName); err != nil {
		t.Fatalf("select name do teacher de a.mp4 falhou: %v", err)
	}
	if sarahName != "Sarah M." {
		t.Errorf("nome do teacher de a.mp4 = %q, esperado \"Sarah M.\"", sarahName)
	}
	if err := conn.QueryRow(`SELECT name FROM teachers WHERE id = ?`, teacherIDByPath["aulas/2026/c.mp4"]).Scan(&jamesName); err != nil {
		t.Fatalf("select name do teacher de c.mp4 falhou: %v", err)
	}
	if jamesName != "James K." {
		t.Errorf("nome do teacher de c.mp4 = %q, esperado \"James K.\"", jamesName)
	}

	var tutorColCount int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('lessons') WHERE name = 'tutor'`).Scan(&tutorColCount); err != nil {
		t.Fatalf("pragma_table_info(lessons) falhou: %v", err)
	}
	if tutorColCount != 0 {
		t.Errorf("coluna tutor ainda existe em lessons após migration 00005, esperado removida")
	}
}

// TestMigration00006_BackfillsTopicsFromExistingLessonTopics exercita a
// migration 00006 contra um banco que já tem dados legados em lesson_topics.topic
// (schema anterior, sem topics/topic_id) — o cenário real de "sem perda
// de dados no backfill" que os testes em analysis_results_test.go não cobrem.
func TestMigration00006_BackfillsTopicsFromExistingLessonTopics(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	conn, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("sql.Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	goose.SetBaseFS(migrationsFS)
	defer goose.SetBaseFS(nil)
	if err := goose.SetDialect("sqlite"); err != nil {
		t.Fatalf("SetDialect() erro inesperado: %v", err)
	}

	if err := goose.UpTo(conn, "migrations", 5); err != nil {
		t.Fatalf("UpTo(5) erro inesperado: %v", err)
	}

	// Schema pré-migration 6: lesson_topics(topic TEXT). Precisa de teacher e
	// lesson (lessons já aponta por teacher_id desde a migration 5).
	now := "2026-08-18T10:00:00Z"
	resT, err := conn.Exec(`INSERT INTO teachers (name, created_at, updated_at) VALUES ('Sarah M.', ?, ?)`, now, now)
	if err != nil {
		t.Fatalf("insert teacher legado falhou: %v", err)
	}
	teacherID, _ := resT.LastInsertId()
	resL, err := conn.Exec(
		`INSERT INTO lessons (lesson_date, teacher_id, video_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"2026-08-18", teacherID, "aulas/2026/a.mp4", now, now,
	)
	if err != nil {
		t.Fatalf("insert lesson legada falhou: %v", err)
	}
	lessonID, _ := resL.LastInsertId()
	for _, topic := range []string{"viagens", "trabalho remoto"} {
		if _, err := conn.Exec(`INSERT INTO lesson_topics (lesson_id, topic) VALUES (?, ?)`, lessonID, topic); err != nil {
			t.Fatalf("insert lesson_topics legado (%q) falhou: %v", topic, err)
		}
	}

	if err := goose.UpTo(conn, "migrations", 6); err != nil {
		t.Fatalf("UpTo(6) erro inesperado: %v", err)
	}

	var topicCount int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM topics`).Scan(&topicCount); err != nil {
		t.Fatalf("contar topics falhou: %v", err)
	}
	if topicCount != 2 {
		t.Errorf("topicCount = %d, esperado 2 (dedup por nome)", topicCount)
	}

	rows, err := conn.Query(`SELECT t.name FROM lesson_topics lt JOIN topics t ON t.id = lt.topic_id WHERE lt.lesson_id = ? ORDER BY t.name`, lessonID)
	if err != nil {
		t.Fatalf("query em lesson_topics pós-migration falhou: %v", err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatalf("scan de topic pós-migration falhou: %v", err)
		}
		names = append(names, n)
	}
	if len(names) != 2 || names[0] != "trabalho remoto" || names[1] != "viagens" {
		t.Errorf("names = %+v, esperado [trabalho remoto viagens]", names)
	}

	var topicCol int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('lesson_topics') WHERE name = 'topic'`).Scan(&topicCol); err != nil {
		t.Fatalf("pragma_table_info(lesson_topics) falhou: %v", err)
	}
	if topicCol != 0 {
		t.Errorf("coluna topic ainda existe em lesson_topics, esperado removida")
	}
}
