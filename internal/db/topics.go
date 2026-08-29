// internal/db/topics.go
package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Topic is a row from topics — the entity that replaces the old
// lesson_topics.topic column (see Phase 2 Story 3). Name is unique: renaming is
// a single-row UPDATE, reflected in all lessons via JOIN.
type Topic struct {
	ID   int64
	Name string
}

// ListTopics lists the registered topics in alphabetical order — feeds the
// "Topics" panel in Settings and the prompt's reuse list.
func ListTopics(conn *sql.DB) ([]Topic, error) {
	rows, err := conn.Query(`SELECT id, name FROM topics ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("listar tópicos: %w", err)
	}
	defer rows.Close()

	out := make([]Topic, 0)
	for rows.Next() {
		var t Topic
		if err := rows.Scan(&t.ID, &t.Name); err != nil {
			return nil, fmt.Errorf("ler tópico: %w", err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterar tópicos: %w", err)
	}
	return out, nil
}

// getOrCreateTopicByName resolves a (trimmed) name to a topic_id —
// reuses execer (teachers.go) to run either standalone or inside a transaction.
func getOrCreateTopicByName(q execer, name string) (int64, error) {
	name = strings.TrimSpace(name)
	var id int64
	err := q.QueryRow(`SELECT id FROM topics WHERE name = ?`, name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("buscar tópico por nome: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	res, err := q.Exec(
		`INSERT INTO topics (name, created_at, updated_at) VALUES (?, ?, ?)`,
		name, now, now,
	)
	if err != nil {
		return 0, fmt.Errorf("criar tópico: %w", err)
	}
	id, err = res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("obter id do tópico criado: %w", err)
	}
	return id, nil
}

func GetOrCreateTopicByName(conn *sql.DB, name string) (int64, error) {
	return getOrCreateTopicByName(conn, name)
}

// RenameTopic renames topic id — automatically reflected in all its
// lessons (JOIN). A collision (UNIQUE) becomes a readable error.
func RenameTopic(conn *sql.DB, id int64, newName string) error {
	newName = strings.TrimSpace(newName)
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := conn.Exec(`UPDATE topics SET name = ?, updated_at = ? WHERE id = ?`, newName, now, id)
	if err != nil {
		if isUniqueConstraintError(err) {
			return fmt.Errorf("já existe um tópico com esse nome")
		}
		return fmt.Errorf("renomear tópico: %w", err)
	}
	return nil
}

// ListLessonTopics returns the topics of lessonID (JOIN topics), in
// alphabetical order.
func ListLessonTopics(conn *sql.DB, lessonID int64) ([]Topic, error) {
	rows, err := conn.Query(
		`SELECT t.id, t.name FROM lesson_topics lt JOIN topics t ON t.id = lt.topic_id WHERE lt.lesson_id = ? ORDER BY t.name ASC`,
		lessonID,
	)
	if err != nil {
		return nil, fmt.Errorf("listar tópicos da lesson %d: %w", lessonID, err)
	}
	defer rows.Close()

	out := make([]Topic, 0)
	for rows.Next() {
		var t Topic
		if err := rows.Scan(&t.ID, &t.Name); err != nil {
			return nil, fmt.Errorf("ler tópico da lesson: %w", err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterar tópicos da lesson: %w", err)
	}
	return out, nil
}

// AddLessonTopic links topicID to lessonID (INSERT OR IGNORE — already linked
// isn't an error).
func AddLessonTopic(conn *sql.DB, lessonID, topicID int64) error {
	_, err := conn.Exec(`INSERT OR IGNORE INTO lesson_topics (lesson_id, topic_id) VALUES (?, ?)`, lessonID, topicID)
	if err != nil {
		return fmt.Errorf("vincular tópico %d à lesson %d: %w", topicID, lessonID, err)
	}
	return nil
}

// RemoveLessonTopic unlinks topicID from lessonID (doesn't delete the entity).
func RemoveLessonTopic(conn *sql.DB, lessonID, topicID int64) error {
	_, err := conn.Exec(`DELETE FROM lesson_topics WHERE lesson_id = ? AND topic_id = ?`, lessonID, topicID)
	if err != nil {
		return fmt.Errorf("desvincular tópico %d da lesson %d: %w", topicID, lessonID, err)
	}
	return nil
}

// DeleteTopic deletes entity id, unlinking it first from any lesson
// that used it (the lesson_topics.topic_id FK doesn't have ON DELETE CASCADE, and the
// database runs with foreign_keys=ON — without this the DELETE on topics would fail).
func DeleteTopic(conn *sql.DB, id int64) error {
	tx, err := conn.Begin()
	if err != nil {
		return fmt.Errorf("iniciar transação pra apagar tópico %d: %w", id, err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM lesson_topics WHERE topic_id = ?`, id); err != nil {
		return fmt.Errorf("desvincular tópico %d das aulas: %w", id, err)
	}
	if _, err := tx.Exec(`DELETE FROM topics WHERE id = ?`, id); err != nil {
		return fmt.Errorf("apagar tópico %d: %w", id, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("confirmar exclusão do tópico %d: %w", id, err)
	}
	return nil
}

// DeleteAllTopics deletes all registered topics and their links to
// lessons — used for bulk reset during manual testing, not part of the
// user's everyday flow.
func DeleteAllTopics(conn *sql.DB) error {
	tx, err := conn.Begin()
	if err != nil {
		return fmt.Errorf("iniciar transação pra apagar todos os tópicos: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM lesson_topics`); err != nil {
		return fmt.Errorf("desvincular todos os tópicos das aulas: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM topics`); err != nil {
		return fmt.Errorf("apagar todos os tópicos: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("confirmar exclusão de todos os tópicos: %w", err)
	}
	return nil
}
