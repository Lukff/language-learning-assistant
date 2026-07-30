// internal/analysis/tasks_tutor_feedback.go
package analysis

import (
	"encoding/json"
	"log/slog"
)

// TutorFeedbackItem é uma observação do Tutor sobre o desempenho do Aluno,
// ancorada na fala do Tutor em que foi dada.
type TutorFeedbackItem struct {
	UtteranceIdx int    `json:"utterance_index"`
	Feedback     string `json:"feedback"`
}

func (f TutorFeedbackItem) UtteranceIndex() int { return f.UtteranceIdx }

func parseTutorFeedback(raw json.RawMessage, utteranceCount int) ([]TutorFeedbackItem, error) {
	var parsed struct {
		TutorFeedback []TutorFeedbackItem `json:"tutor_feedback"`
	}
	if err := unmarshalJSON(raw, &parsed); err != nil {
		return nil, err
	}
	kept, discarded := filterAnchored(parsed.TutorFeedback, utteranceCount)
	if discarded > 0 {
		slog.Warn("analysis: itens descartados por utterance_index inválido", "tarefa", "analyze_tutor_feedback", "descartados", discarded)
	}
	return kept, nil
}

func newTutorFeedbackTask() TaskDef {
	return task[[]TutorFeedbackItem]{name: "analyze_tutor_feedback", version: 1, prompt: mustLoadPrompt("analyze-tutor-feedback-v1.md"), parse: parseTutorFeedback}
}
