// internal/analysis/tasks_tutor_expressions.go
package analysis

import "encoding/json"

// Expression is an expression the Tutor naturally used in the conversation
// that's worth the Student reusing — distinct from TutorTaughtTerm (a term
// the Tutor explicitly explained/taught).
type Expression struct {
	Text string `json:"text"`
	Note string `json:"note"`
}

func parseTutorExpressions(raw json.RawMessage, utteranceCount int) ([]Expression, error) {
	var parsed struct {
		TutorExpressions []Expression `json:"tutor_expressions"`
	}
	if err := unmarshalJSON(raw, &parsed); err != nil {
		return nil, err
	}
	return parsed.TutorExpressions, nil
}

func newTutorExpressionsTask() TaskDef {
	return task[[]Expression]{name: "analyze_tutor_expressions", version: 1, prompt: mustLoadPrompt("analyze-tutor-expressions-v1.md"), parse: parseTutorExpressions}
}
