// internal/db/analysis_results.go
package db

import (
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

// UpsertPrompt insere (name, version, content) na tabela prompts se ainda
// não existir. Se (name, version) já existir com content diferente, é
// sinal de versão esquecida no código (convenção "-vN" no nome do arquivo
// em prompts/); loga um aviso e mantém o conteúdo já gravado — não
// sobrescreve, porque analysis_results já pode referenciar esse prompt_id.
func UpsertPrompt(conn *sql.DB, name string, version int, content string) (int64, error) {
	var id int64
	var existingContent string
	err := conn.QueryRow(`SELECT id, content FROM prompts WHERE name = ? AND version = ?`, name, version).Scan(&id, &existingContent)
	if err == nil {
		if existingContent != content {
			slog.Warn("analysis: conteúdo do prompt divergente pra versão já registrada", "nome", name, "versao", version)
		}
		return id, nil
	}
	if err != sql.ErrNoRows {
		return 0, fmt.Errorf("buscar prompt %s v%d: %w", name, version, err)
	}

	res, err := conn.Exec(
		`INSERT INTO prompts (name, version, content, created_at) VALUES (?, ?, ?, ?)`,
		name, version, content, time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return 0, fmt.Errorf("inserir prompt %s v%d: %w", name, version, err)
	}
	return res.LastInsertId()
}

// UpsertAnalysisResult grava (ou substitui, se já existir) o resultado de
// task para lessonID — reprocessar (História 2) sobrescreve a linha
// existente.
func UpsertAnalysisResult(conn *sql.DB, lessonID int64, task string, promptID int64, model, resultJSON, rawResponsePath string) error {
	_, err := conn.Exec(
		`INSERT INTO analysis_results (lesson_id, task, prompt_id, model, result_json, raw_response_path, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(lesson_id, task) DO UPDATE SET
		     prompt_id = excluded.prompt_id,
		     model = excluded.model,
		     result_json = excluded.result_json,
		     raw_response_path = excluded.raw_response_path,
		     created_at = excluded.created_at`,
		lessonID, task, promptID, model, resultJSON, rawResponsePath, time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("gravar analysis_result da lesson %d, task %s: %w", lessonID, task, err)
	}
	return nil
}

// AnalysisResult é o resultado persistido de uma tarefa de análise para uma lesson.
type AnalysisResult struct {
	LessonID        int64
	Task            string
	PromptID        int64
	Model           string
	ResultJSON      string
	RawResponsePath string
}

// FindAnalysisResult retorna (nil, nil) se a tarefa ainda não rodou pra
// essa lesson — estado normal enquanto o job correspondente (História 2)
// está pending/running/error, não um erro.
func FindAnalysisResult(conn *sql.DB, lessonID int64, task string) (*AnalysisResult, error) {
	r := AnalysisResult{LessonID: lessonID, Task: task}
	err := conn.QueryRow(
		`SELECT prompt_id, model, result_json, raw_response_path FROM analysis_results WHERE lesson_id = ? AND task = ?`,
		lessonID, task,
	).Scan(&r.PromptID, &r.Model, &r.ResultJSON, &r.RawResponsePath)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("buscar analysis_result da lesson %d, task %s: %w", lessonID, task, err)
	}
	return &r, nil
}

// ReplaceLessonTopics apaga os vínculos existentes e insere os novos (por
// topic_id) — a lista é sempre derivada por inteiro do resultado mais
// recente de analyze_topics, nunca um merge incremental. INSERT OR IGNORE
// absorve um tópico duplicado que o chamador eventualmente repita.
func ReplaceLessonTopics(conn *sql.DB, lessonID int64, topicIDs []int64) error {
	tx, err := conn.Begin()
	if err != nil {
		return fmt.Errorf("iniciar transação de lesson_topics da lesson %d: %w", lessonID, err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM lesson_topics WHERE lesson_id = ?`, lessonID); err != nil {
		return fmt.Errorf("apagar lesson_topics antigos da lesson %d: %w", lessonID, err)
	}
	for _, topicID := range topicIDs {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO lesson_topics (lesson_id, topic_id) VALUES (?, ?)`, lessonID, topicID); err != nil {
			return fmt.Errorf("inserir tópico %d da lesson %d: %w", topicID, lessonID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commitar lesson_topics da lesson %d: %w", lessonID, err)
	}
	return nil
}

// DeleteSpeakerDependentAnalysisResults apaga as análises que dependem de
// quem é aluno/tutor na aula (tudo exceto analyze_topics) — chamada quando o
// mapeamento aluno/tutor muda (services.LibraryService.SetStudentSpeaker).
// analyze_topics não menciona Aluno/Tutor no prompt e sobrevive; lesson_topics
// também fica intacto (a tabela é a fonte da verdade dos chips, independente
// da análise).
func DeleteSpeakerDependentAnalysisResults(conn *sql.DB, lessonID int64) error {
	if _, err := conn.Exec(`DELETE FROM analysis_results WHERE lesson_id = ? AND task != 'analyze_topics'`, lessonID); err != nil {
		return fmt.Errorf("apagar análises dependentes de falante da lesson %d: %w", lessonID, err)
	}
	return nil
}
