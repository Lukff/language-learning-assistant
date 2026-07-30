// internal/analysis/tasks_tutor_corrections.go
package analysis

import (
	"encoding/json"
	"log/slog"
)

// TutorCorrection é uma correção que o próprio Tutor deu ao Aluno durante a
// aula (ao vivo, na conversa) — diferente de Correction (derivada pela
// análise), embora ambas possam apontar pra mesma fala.
type TutorCorrection struct {
	UtteranceIdx int    `json:"utterance_index"`
	TutorSaid    string `json:"tutor_said"`
	Note         string `json:"note"`
}

func (c TutorCorrection) UtteranceIndex() int { return c.UtteranceIdx }

func parseTutorCorrections(raw json.RawMessage, utteranceCount int) ([]TutorCorrection, error) {
	var parsed struct {
		TutorCorrections []TutorCorrection `json:"tutor_corrections"`
	}
	if err := unmarshalJSON(raw, &parsed); err != nil {
		return nil, err
	}
	kept, discarded := filterAnchored(parsed.TutorCorrections, utteranceCount)
	if discarded > 0 {
		slog.Warn("analysis: itens descartados por utterance_index inválido", "tarefa", "analyze_tutor_corrections", "descartados", discarded)
	}
	return kept, nil
}

func newTutorCorrectionsTask() TaskDef {
	return task[[]TutorCorrection]{name: "analyze_tutor_corrections", version: 1, prompt: mustLoadPrompt("analyze-tutor-corrections-v1.md"), parse: parseTutorCorrections}
}
