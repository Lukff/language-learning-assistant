// internal/analysis/tasks_corrections.go
package analysis

import (
	"encoding/json"
	"fmt"
	"log/slog"
)

// Correction is a correction to a Student utterance, derived by the analysis
// (unlike TutorCorrection, given live by the Tutor themself).
type Correction struct {
	UtteranceIdx int    `json:"utterance_index"`
	Original     string `json:"original"`
	CorrectionTx string `json:"correction"`
	Explanation  string `json:"explanation"`
}

func (c Correction) UtteranceIndex() int { return c.UtteranceIdx }

func parseCorrections(raw json.RawMessage, utteranceCount int) ([]Correction, error) {
	var parsed struct {
		Corrections []Correction `json:"corrections"`
	}
	if err := unmarshalJSON(raw, &parsed); err != nil {
		return nil, err
	}
	kept, discarded := filterAnchored(parsed.Corrections, utteranceCount)
	if discarded > 0 {
		slog.Warn("analysis: itens descartados por utterance_index inválido", "tarefa", "analyze_corrections", "descartados", discarded)
	}
	return kept, nil
}

func NewCorrectionsTask() TaskDef {
	return task[[]Correction]{name: "analyze_corrections", version: 1, prompt: mustLoadPrompt("analyze-corrections-v1.md"), parse: parseCorrections}
}

// ParseCorrectionsResult decodes an already-persisted result_json (written
// by Execute for this same task — a JSON array of Correction,
// with no envelope) back into []Correction. Reused by whoever needs to
// reconstitute the saved result without calling the provider again
// (services.AnalysisService).
func ParseCorrectionsResult(resultJSON json.RawMessage) ([]Correction, error) {
	var out []Correction
	if err := json.Unmarshal(resultJSON, &out); err != nil {
		return nil, fmt.Errorf("analysis: desserializar resultado de analyze_corrections: %w", err)
	}
	return out, nil
}
