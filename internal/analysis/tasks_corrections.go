// internal/analysis/tasks_corrections.go
package analysis

import (
	"encoding/json"
	"fmt"
	"log/slog"
)

// Correction é uma correção de uma fala do Aluno, derivada pela análise
// (ao contrário de TutorCorrection, dada ao vivo pelo próprio Tutor).
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

// ParseCorrectionsResult decodifica um result_json já persistido (gravado
// por Execute a partir desta mesma tarefa — um array JSON de Correction,
// sem envelope) de volta em []Correction. Reaproveitado por quem precisa
// reconstituir o resultado salvo sem chamar o provedor de novo
// (services.AnalysisService).
func ParseCorrectionsResult(resultJSON json.RawMessage) ([]Correction, error) {
	var out []Correction
	if err := json.Unmarshal(resultJSON, &out); err != nil {
		return nil, fmt.Errorf("analysis: desserializar resultado de analyze_corrections: %w", err)
	}
	return out, nil
}
