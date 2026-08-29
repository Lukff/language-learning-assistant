// internal/analysis/tasks_tutor_expressions_test.go
package analysis

import (
	"encoding/json"
	"testing"
)

func TestParseTutorExpressions_Valid(t *testing.T) {
	raw := json.RawMessage(`{"tutor_expressions":[{"text":"let's circle back to that","note":"revisit a topic later"}]}`)
	got, err := parseTutorExpressions(raw, 3)
	if err != nil {
		t.Fatalf("parseTutorExpressions unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Text != "let's circle back to that" {
		t.Errorf("got = %+v, unexpected", got)
	}
}

func TestParseTutorExpressions_InvalidJSON(t *testing.T) {
	if _, err := parseTutorExpressions(json.RawMessage("not json"), 3); err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}
