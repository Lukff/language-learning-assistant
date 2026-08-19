package services

import (
	"path/filepath"
	"testing"

	"assistente-idiomas/internal/db"
)

func TestTopicsService_ListTopics(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	svc := NewTopicsService(conn)
	if _, err := svc.AddTopic(0, "viagens"); err != nil {
		t.Fatalf("AddTopic() erro inesperado: %v", err)
	}
	topics, err := svc.ListTopics()
	if err != nil {
		t.Fatalf("ListTopics() erro inesperado: %v", err)
	}
	if len(topics) != 1 || topics[0].Name != "viagens" {
		t.Errorf("ListTopics() = %+v, esperado [viagens]", topics)
	}
}

func TestTopicsService_AddTopic_ReusesEntity(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLesson(t, conn, "2026-08-18", "Sarah M.", "aula.mp4")
	svc := NewTopicsService(conn)

	t1, err := svc.AddTopic(lessonID, "viagens")
	if err != nil {
		t.Fatalf("AddTopic() erro inesperado: %v", err)
	}
	t2, err := svc.AddTopic(lessonID, "  viagens  ")
	if err != nil {
		t.Fatalf("segunda AddTopic() erro inesperado: %v", err)
	}
	if t1.ID != t2.ID {
		t.Errorf("t2.ID = %d, esperado igual a t1.ID = %d (mesma entidade)", t2.ID, t1.ID)
	}
	topics, err := db.ListLessonTopics(conn, lessonID)
	if err != nil {
		t.Fatalf("ListLessonTopics() erro inesperado: %v", err)
	}
	if len(topics) != 1 {
		t.Errorf("len(topics) = %d, esperado 1 (INSERT OR IGNORE)", len(topics))
	}
}

func TestTopicsService_RemoveTopic_UnlinksOnly(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLesson(t, conn, "2026-08-18", "Sarah M.", "aula.mp4")
	svc := NewTopicsService(conn)

	topic, err := svc.AddTopic(lessonID, "viagens")
	if err != nil {
		t.Fatalf("AddTopic() erro inesperado: %v", err)
	}
	if err := svc.RemoveTopic(lessonID, topic.ID); err != nil {
		t.Fatalf("RemoveTopic() erro inesperado: %v", err)
	}
	topics, err := db.ListLessonTopics(conn, lessonID)
	if err != nil {
		t.Fatalf("ListLessonTopics() erro inesperado: %v", err)
	}
	if len(topics) != 0 {
		t.Errorf("len(topics) = %d, esperado 0 (desvinculado)", len(topics))
	}
	all, err := db.ListTopics(conn)
	if err != nil {
		t.Fatalf("ListTopics() erro inesperado: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("len(all) = %d, esperado 1 (entidade não apagada)", len(all))
	}
}

func TestTopicsService_DeleteTopic_RemovesEntityAndLinks(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLesson(t, conn, "2026-08-18", "Sarah M.", "aula.mp4")
	svc := NewTopicsService(conn)

	topic, err := svc.AddTopic(lessonID, "viagens")
	if err != nil {
		t.Fatalf("AddTopic() erro inesperado: %v", err)
	}
	if err := svc.DeleteTopic(topic.ID); err != nil {
		t.Fatalf("DeleteTopic() erro inesperado: %v", err)
	}
	topics, err := svc.ListTopics()
	if err != nil {
		t.Fatalf("ListTopics() erro inesperado: %v", err)
	}
	if len(topics) != 0 {
		t.Errorf("ListTopics() = %+v, esperado [] (entidade apagada)", topics)
	}
	lessonTopics, err := db.ListLessonTopics(conn, lessonID)
	if err != nil {
		t.Fatalf("ListLessonTopics() erro inesperado: %v", err)
	}
	if len(lessonTopics) != 0 {
		t.Errorf("ListLessonTopics() = %+v, esperado [] (vínculo removido)", lessonTopics)
	}
}

func TestTopicsService_RenameTopic_ReflectsGlobally(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	svc := NewTopicsService(conn)
	topic, err := svc.AddTopic(0, "viagens")
	if err != nil {
		t.Fatalf("AddTopic() erro inesperado: %v", err)
	}
	if err := svc.RenameTopic(topic.ID, "planos de viagem"); err != nil {
		t.Fatalf("RenameTopic() erro inesperado: %v", err)
	}
	topics, err := svc.ListTopics()
	if err != nil {
		t.Fatalf("ListTopics() erro inesperado: %v", err)
	}
	if len(topics) != 1 || topics[0].Name != "planos de viagem" {
		t.Errorf("ListTopics() = %+v, esperado [planos de viagem]", topics)
	}
}
