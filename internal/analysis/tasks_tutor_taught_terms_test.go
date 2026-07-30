// internal/analysis/tasks_tutor_taught_terms_test.go
package analysis

import (
	"encoding/json"
	"testing"
)

func TestParseTutorTaughtTerms_Valid(t *testing.T) {
	raw := json.RawMessage(`{"tutor_taught_terms":[{"term":"to bring up","translation":"trazer à tona","context":"o tutor explicou ao introduzir um assunto novo"}]}`)
	got, err := parseTutorTaughtTerms(raw, 3)
	if err != nil {
		t.Fatalf("parseTutorTaughtTerms erro inesperado: %v", err)
	}
	if len(got) != 1 || got[0].Term != "to bring up" || got[0].Context == "" {
		t.Errorf("got = %+v, inesperado", got)
	}
}

func TestParseTutorTaughtTerms_InvalidJSON(t *testing.T) {
	if _, err := parseTutorTaughtTerms(json.RawMessage("not json"), 3); err == nil {
		t.Fatal("esperava erro para JSON inválido, obteve nil")
	}
}
