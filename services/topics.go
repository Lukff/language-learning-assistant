// services/topics.go
package services

import (
	"database/sql"
	"fmt"
	"strings"

	"assistente-idiomas/internal/db"
)

// Topic is a registered topic, in the format exposed to the frontend — an entity
// reused by TopicsService (management) and by AnalysisService (TopicsResult).
type Topic struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// TopicsService covers topic management as an entity (Story 3): listing
// for the Settings panel, renaming globally, and adding/removing the
// link between a topic and a specific lesson (Detail chips).
type TopicsService struct {
	conn *sql.DB
}

func NewTopicsService(conn *sql.DB) *TopicsService {
	return &TopicsService{conn: conn}
}

func (s *TopicsService) ListTopics() ([]Topic, error) {
	rows, err := db.ListTopics(s.conn)
	if err != nil {
		return nil, err
	}
	out := make([]Topic, 0, len(rows))
	for _, r := range rows {
		out = append(out, Topic{ID: r.ID, Name: r.Name})
	}
	return out, nil
}

func (s *TopicsService) RenameTopic(id int64, newName string) error {
	return db.RenameTopic(s.conn, id, newName)
}

// AddTopic resolves the name to an entity (creating it if necessary) and links
// it to lessonID. lessonID <= 0 creates/reuses the entity without linking it to any
// lesson — used by the Settings panel; any real caller (e.g. a
// future frontend call passing an accidental/undefined 0) needs to
// be aware that this is a silent link no-op, not an error.
// Returns the resolved topic; the frontend re-fetches GetTopics afterward to
// reconcile canonical names.
func (s *TopicsService) AddTopic(lessonID int64, name string) (Topic, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Topic{}, fmt.Errorf("topic cannot be empty")
	}
	id, err := db.GetOrCreateTopicByName(s.conn, name)
	if err != nil {
		return Topic{}, err
	}
	if lessonID > 0 {
		if err := db.AddLessonTopic(s.conn, lessonID, id); err != nil {
			return Topic{}, err
		}
	}
	return Topic{ID: id, Name: name}, nil
}

// RemoveTopic unlinks topicID from lessonID (does not delete the entity).
func (s *TopicsService) RemoveTopic(lessonID, topicID int64) error {
	return db.RemoveLessonTopic(s.conn, lessonID, topicID)
}

// DeleteTopic deletes the entity globally, removing the link from any
// lesson that used it (chips disappear from the affected lessons).
func (s *TopicsService) DeleteTopic(id int64) error {
	return db.DeleteTopic(s.conn, id)
}

// DeleteAllTopics deletes all registered topics at once — a bulk-reset
// shortcut for manual testing, not a day-to-day user flow.
func (s *TopicsService) DeleteAllTopics() error {
	return db.DeleteAllTopics(s.conn)
}
