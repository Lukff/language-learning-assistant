package db

import (
	"path/filepath"
	"testing"
)

func TestPendingImports_InsertListFindByHashRoundTrip(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	found, err := FindPendingImportByHash(conn, "hash-new")
	if err != nil {
		t.Fatalf("FindPendingImportByHash() erro inesperado: %v", err)
	}
	if found {
		t.Error("FindPendingImportByHash() = true antes de inserir, esperado false")
	}

	err = InsertPendingImport(conn, PendingImport{
		Path: "aula-nova.mp4", FileSize: 999, FileMTime: "2026-07-20T10:00:00Z",
		SHA256: "hash-new", SuggestedDate: "2026-07-20",
	})
	if err != nil {
		t.Fatalf("InsertPendingImport() erro inesperado: %v", err)
	}

	found, err = FindPendingImportByHash(conn, "hash-new")
	if err != nil {
		t.Fatalf("FindPendingImportByHash() erro inesperado: %v", err)
	}
	if !found {
		t.Error("FindPendingImportByHash() = false após inserir, esperado true")
	}

	list, err := ListPendingImports(conn)
	if err != nil {
		t.Fatalf("ListPendingImports() erro inesperado: %v", err)
	}
	if len(list) != 1 || list[0].Path != "aula-nova.mp4" || list[0].SuggestedDate != "2026-07-20" {
		t.Errorf("ListPendingImports() = %+v, esperado 1 item aula-nova.mp4/2026-07-20", list)
	}
}

func TestConfirmPendingImport_CreatesLessonAndJobsRemovesPending(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	if err := InsertPendingImport(conn, PendingImport{
		Path: "aula-nova.mp4", FileSize: 999, FileMTime: "2026-07-20T10:00:00Z",
		SHA256: "hash-confirm", SuggestedDate: "2026-07-20",
	}); err != nil {
		t.Fatalf("InsertPendingImport() erro inesperado: %v", err)
	}
	list, err := ListPendingImports(conn)
	if err != nil || len(list) != 1 {
		t.Fatalf("setup: ListPendingImports() = %+v, %v", list, err)
	}
	pendingID := list[0].ID

	lessonID, err := ConfirmPendingImport(conn, pendingID, "2026-07-20", "Sarah M.")
	if err != nil {
		t.Fatalf("ConfirmPendingImport() erro inesperado: %v", err)
	}
	if lessonID == 0 {
		t.Fatal("ConfirmPendingImport() retornou lessonID = 0")
	}

	lesson, err := FindLessonByHash(conn, "hash-confirm")
	if err != nil {
		t.Fatalf("FindLessonByHash() erro inesperado: %v", err)
	}
	if lesson == nil || lesson.VideoPath != "aula-nova.mp4" {
		t.Fatalf("lesson após confirmação = %+v, esperado video_path aula-nova.mp4", lesson)
	}

	var tutor string
	if err := conn.QueryRow(`SELECT tutor FROM lessons WHERE id = ?`, lessonID).Scan(&tutor); err != nil {
		t.Fatalf("select tutor falhou: %v", err)
	}
	if tutor != "Sarah M." {
		t.Errorf("tutor = %q, esperado \"Sarah M.\"", tutor)
	}

	rows, err := conn.Query(`SELECT kind, status FROM jobs WHERE lesson_id = ? ORDER BY kind`, lessonID)
	if err != nil {
		t.Fatalf("query de jobs falhou: %v", err)
	}
	defer rows.Close()
	var jobs [][2]string
	for rows.Next() {
		var kind, status string
		if err := rows.Scan(&kind, &status); err != nil {
			t.Fatalf("scan de job falhou: %v", err)
		}
		jobs = append(jobs, [2]string{kind, status})
	}
	want := [][2]string{{"extract_audio", "pending"}, {"transcribe", "pending"}}
	if len(jobs) != len(want) {
		t.Fatalf("jobs criados = %+v, esperado %+v", jobs, want)
	}
	for i := range want {
		if jobs[i] != want[i] {
			t.Errorf("jobs[%d] = %+v, esperado %+v", i, jobs[i], want[i])
		}
	}

	list, err = ListPendingImports(conn)
	if err != nil {
		t.Fatalf("ListPendingImports() erro inesperado: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("ListPendingImports() após confirmar = %+v, esperado vazio", list)
	}
}

func TestConfirmPendingImport_UnknownIDReturnsErrorAndTouchesNothing(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	if _, err := ConfirmPendingImport(conn, 999, "2026-07-20", "Sarah M."); err == nil {
		t.Error("ConfirmPendingImport() com id inexistente esperava erro, veio nil")
	}

	var count int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM lessons`).Scan(&count); err != nil {
		t.Fatalf("count de lessons falhou: %v", err)
	}
	if count != 0 {
		t.Errorf("lessons após ConfirmPendingImport falhar = %d linhas, esperado 0", count)
	}
}
