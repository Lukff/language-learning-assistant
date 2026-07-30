// internal/analysis/tasks_tutor_feedback_test.go
package analysis

import (
	"encoding/json"
	"testing"
)

func TestParseTutorFeedback_Valid(t *testing.T) {
	raw := json.RawMessage(`{"tutor_feedback":[{"utterance_index":2,"feedback":"Fluência melhorou bastante nas últimas aulas."}]}`)
	got, err := parseTutorFeedback(raw, 3)
	if err != nil {
		t.Fatalf("parseTutorFeedback erro inesperado: %v", err)
	}
	if len(got) != 1 || got[0].Feedback == "" {
		t.Errorf("got = %+v, inesperado", got)
	}
}

func TestParseTutorFeedback_DropsOutOfRangeIndex(t *testing.T) {
	raw := json.RawMessage(`{"tutor_feedback":[{"utterance_index":50,"feedback":"x"}]}`)
	got, err := parseTutorFeedback(raw, 3)
	if err != nil {
		t.Fatalf("parseTutorFeedback erro inesperado: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got = %+v, esperado vazio (índice fora do range)", got)
	}
}

func TestParseTutorFeedback_InvalidJSON(t *testing.T) {
	if _, err := parseTutorFeedback(json.RawMessage("not json"), 3); err == nil {
		t.Fatal("esperava erro para JSON inválido, obteve nil")
	}
}
