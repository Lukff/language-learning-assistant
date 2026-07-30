// internal/analysis/tasks_tutor_expressions.go
package analysis

import "encoding/json"

// Expression é uma expressão que o Tutor usou naturalmente na conversa e
// que vale a pena o Aluno reutilizar — distinta de TutorTaughtTerm (termo
// que o Tutor explicou/ensinou explicitamente).
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
