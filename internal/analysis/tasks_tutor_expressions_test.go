// internal/analysis/tasks_tutor_expressions_test.go
package analysis

import (
	"encoding/json"
	"testing"
)

func TestParseTutorExpressions_Valid(t *testing.T) {
	raw := json.RawMessage(`{"tutor_expressions":[{"text":"let's circle back to that","note":"retomar um assunto depois"}]}`)
	got, err := parseTutorExpressions(raw, 3)
	if err != nil {
		t.Fatalf("parseTutorExpressions erro inesperado: %v", err)
	}
	if len(got) != 1 || got[0].Text != "let's circle back to that" {
		t.Errorf("got = %+v, inesperado", got)
	}
}

func TestParseTutorExpressions_InvalidJSON(t *testing.T) {
	if _, err := parseTutorExpressions(json.RawMessage("not json"), 3); err == nil {
		t.Fatal("esperava erro para JSON inválido, obteve nil")
	}
}
