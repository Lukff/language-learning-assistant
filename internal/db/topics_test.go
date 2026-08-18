package db

import (
	"path/filepath"
	"testing"
)

func TestGetOrCreateTopicByName_IdempotentAndTrims(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	id1, err := GetOrCreateTopicByName(conn, "viagens")
	if err != nil {
		t.Fatalf("GetOrCreateTopicByName() erro inesperado: %v", err)
	}
	id2, err := GetOrCreateTopicByName(conn, "  viagens  ")
	if err != nil {
		t.Fatalf("segunda GetOrCreateTopicByName() erro inesperado: %v", err)
	}
	if id1 != id2 {
		t.Errorf("id2 = %d, esperado igual a id1 = %d (mesmo nome após trim)", id2, id1)
	}
	var count int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM topics WHERE name = 'viagens'`).Scan(&count); err != nil {
		t.Fatalf("contar topics falhou: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, esperado 1", count)
	}
}

func TestListTopics_ReturnsDistinctSortedByName(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	for _, n := range []string{"viagens", "trabalho remoto", "receitas"} {
		if _, err := GetOrCreateTopicByName(conn, n); err != nil {
			t.Fatalf("GetOrCreateTopicByName(%q) erro inesperado: %v", n, err)
		}
	}
	topics, err := ListTopics(conn)
	if err != nil {
		t.Fatalf("ListTopics() erro inesperado: %v", err)
	}
	if len(topics) != 3 || topics[0].Name != "receitas" || topics[1].Name != "trabalho remoto" || topics[2].Name != "viagens" {
		t.Errorf("ListTopics() = %+v, esperado [receitas trabalho remoto viagens]", topics)
	}
}

func TestRenameTopic_ReflectsOnLinkedLessons(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := insertLessonFixture(t, conn)
	topicID, err := GetOrCreateTopicByName(conn, "viagens")
	if err != nil {
		t.Fatalf("GetOrCreateTopicByName() erro inesperado: %v", err)
	}
	if err := AddLessonTopic(conn, lessonID, topicID); err != nil {
		t.Fatalf("AddLessonTopic() erro inesperado: %v", err)
	}

	if err := RenameTopic(conn, topicID, "planos de viagem"); err != nil {
		t.Fatalf("RenameTopic() erro inesperado: %v", err)
	}

	topics, err := ListLessonTopics(conn, lessonID)
	if err != nil {
		t.Fatalf("ListLessonTopics() erro inesperado: %v", err)
	}
	if len(topics) != 1 || topics[0].Name != "planos de viagem" {
		t.Errorf("ListLessonTopics() = %+v, esperado [planos de viagem] (renome refletido via JOIN)", topics)
	}
}

func TestRenameTopic_CollidingNameReturnsFriendlyError(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	if _, err := GetOrCreateTopicByName(conn, "viagens"); err != nil {
		t.Fatalf("GetOrCreateTopicByName() erro inesperado: %v", err)
	}
	trabalho, err := GetOrCreateTopicByName(conn, "trabalho remoto")
	if err != nil {
		t.Fatalf("GetOrCreateTopicByName() erro inesperado: %v", err)
	}

	err = RenameTopic(conn, trabalho, "viagens")
	if err == nil {
		t.Fatal("RenameTopic() colidindo = nil, esperado erro amigável")
	}
	if err.Error() != "já existe um tópico com esse nome" {
		t.Errorf("RenameTopic() = %q, esperado \"já existe um tópico com esse nome\"", err.Error())
	}
}

func TestAddAndRemoveLessonTopic(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := insertLessonFixture(t, conn)
	topicID, err := GetOrCreateTopicByName(conn, "viagens")
	if err != nil {
		t.Fatalf("GetOrCreateTopicByName() erro inesperado: %v", err)
	}

	if err := AddLessonTopic(conn, lessonID, topicID); err != nil {
		t.Fatalf("AddLessonTopic() erro inesperado: %v", err)
	}
	if err := AddLessonTopic(conn, lessonID, topicID); err != nil {
		t.Fatalf("segunda AddLessonTopic() erro inesperado: %v", err)
	}
	var count int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM lesson_topics WHERE lesson_id = ?`, lessonID).Scan(&count); err != nil {
		t.Fatalf("contar lesson_topics falhou: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, esperado 1 (INSERT OR IGNORE)", count)
	}

	if err := RemoveLessonTopic(conn, lessonID, topicID); err != nil {
		t.Fatalf("RemoveLessonTopic() erro inesperado: %v", err)
	}
	var topicCount int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM topics WHERE id = ?`, topicID).Scan(&topicCount); err != nil {
		t.Fatalf("contar topics falhou: %v", err)
	}
	if topicCount != 1 {
		t.Errorf("topicCount = %d, esperado 1 (remover não apaga a entidade)", topicCount)
	}
}
