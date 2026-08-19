// services/topics.go
package services

import (
	"database/sql"
	"fmt"
	"strings"

	"assistente-idiomas/internal/db"
)

// Topic é um tópico cadastrado, no formato exposto ao frontend — entidade
// reutilizada por TopicsService (gestão) e por AnalysisService (TopicsResult).
type Topic struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// TopicsService cobre a gestão de tópicos como entidade (História 3): listar
// pro painel de Configurações, renomear globalmente, e adicionar/remover o
// vínculo de um tópico a uma aula específica (chips do Detalhe).
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

// AddTopic resolve o nome para uma entidade (criando se necessário) e vincula
// à lessonID. lessonID <= 0 cria/reutiliza a entidade sem vincular a nenhuma
// aula — usado pelo painel de Configurações; qualquer chamador real (ex.: uma
// futura chamada do frontend passando um 0 acidental/não definido) precisa
// estar ciente de que esse é um no-op silencioso de vínculo, não um erro.
// Devolve o tópico resolvido; o frontend re-busca GetTopics depois para
// reconciliar nomes canônicos.
func (s *TopicsService) AddTopic(lessonID int64, name string) (Topic, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Topic{}, fmt.Errorf("tópico não pode ser vazio")
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

// RemoveTopic desvincula topicID de lessonID (não apaga a entidade).
func (s *TopicsService) RemoveTopic(lessonID, topicID int64) error {
	return db.RemoveLessonTopic(s.conn, lessonID, topicID)
}

// DeleteTopic apaga a entidade globalmente, removendo o vínculo de qualquer
// aula que a usava (chips somem das aulas afetadas).
func (s *TopicsService) DeleteTopic(id int64) error {
	return db.DeleteTopic(s.conn, id)
}

// DeleteAllTopics apaga todos os tópicos cadastrados de uma vez — atalho de
// reset em massa pra testes manuais, não um fluxo do dia a dia do usuário.
func (s *TopicsService) DeleteAllTopics() error {
	return db.DeleteAllTopics(s.conn)
}
