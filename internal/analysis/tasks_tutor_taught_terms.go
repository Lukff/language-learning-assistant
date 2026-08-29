// internal/analysis/tasks_tutor_taught_terms.go
package analysis

import "encoding/json"

// TutorTaughtTerm is a term/expression the Tutor explicitly explained or
// taught during the lesson (unlike Expression, which is just natural
// use in the conversation).
type TutorTaughtTerm struct {
	Term        string `json:"term"`
	Translation string `json:"translation"`
	Context     string `json:"context"`
}

func parseTutorTaughtTerms(raw json.RawMessage, utteranceCount int) ([]TutorTaughtTerm, error) {
	var parsed struct {
		TutorTaughtTerms []TutorTaughtTerm `json:"tutor_taught_terms"`
	}
	if err := unmarshalJSON(raw, &parsed); err != nil {
		return nil, err
	}
	return parsed.TutorTaughtTerms, nil
}

func newTutorTaughtTermsTask() TaskDef {
	return task[[]TutorTaughtTerm]{name: "analyze_tutor_taught_terms", version: 1, prompt: mustLoadPrompt("analyze-tutor-taught-terms-v1.md"), parse: parseTutorTaughtTerms}
}
