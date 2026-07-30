// internal/analysis/tasks_tutor_corrections_test.go
package analysis

import (
	"encoding/json"
	"testing"
)

func TestParseTutorCorrections_Valid(t *testing.T) {
	raw := json.RawMessage(`{"tutor_corrections":[{"utterance_index":1,"tutor_said":"I went there","note":"Aluno usou o tempo verbal errado"}]}`)
	got, err := parseTutorCorrections(raw, 3)
	if err != nil {
		t.Fatalf("parseTutorCorrections erro inesperado: %v", err)
	}
	if len(got) != 1 || got[0].TutorSaid != "I went there" {
		t.Errorf("got = %+v, inesperado", got)
	}
}

func TestParseTutorCorrections_DropsOutOfRangeIndex(t *testing.T) {
	raw := json.RawMessage(`{"tutor_corrections":[{"utterance_index":-1,"tutor_said":"x","note":"y"}]}`)
	got, err := parseTutorCorrections(raw, 3)
	if err != nil {
		t.Fatalf("parseTutorCorrections erro inesperado: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got = %+v, esperado vazio (índice negativo)", got)
	}
}

func TestParseTutorCorrections_InvalidJSON(t *testing.T) {
	if _, err := parseTutorCorrections(json.RawMessage("not json"), 3); err == nil {
		t.Fatal("esperava erro para JSON inválido, obteve nil")
	}
}
