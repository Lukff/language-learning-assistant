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
