// internal/analysis/tasks_tutor_corrections.go
package analysis

import (
	"encoding/json"
	"log/slog"
)

// TutorCorrection is a correction the Tutor themself gave the Student during
// the lesson (live, in conversation) — different from Correction (derived by the
// analysis), though both may point to the same utterance.
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
		slog.Warn("analysis: items discarded due to invalid utterance_index", "task", "analyze_tutor_corrections", "discarded", discarded)
	}
	return kept, nil
}

func newTutorCorrectionsTask() TaskDef {
	return task[[]TutorCorrection]{name: "analyze_tutor_corrections", version: 1, prompt: mustLoadPrompt("analyze-tutor-corrections-v1.md"), parse: parseTutorCorrections}
}
