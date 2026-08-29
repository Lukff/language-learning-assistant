// internal/analysis/tasks_tutor_corrections_test.go
package analysis

import (
	"encoding/json"
	"testing"
)

func TestParseTutorCorrections_Valid(t *testing.T) {
	raw := json.RawMessage(`{"tutor_corrections":[{"utterance_index":1,"tutor_said":"I went there","note":"Student used the wrong verb tense"}]}`)
	got, err := parseTutorCorrections(raw, 3)
	if err != nil {
		t.Fatalf("parseTutorCorrections unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].TutorSaid != "I went there" {
		t.Errorf("got = %+v, unexpected", got)
	}
}

func TestParseTutorCorrections_DropsOutOfRangeIndex(t *testing.T) {
	raw := json.RawMessage(`{"tutor_corrections":[{"utterance_index":-1,"tutor_said":"x","note":"y"}]}`)
	got, err := parseTutorCorrections(raw, 3)
	if err != nil {
		t.Fatalf("parseTutorCorrections unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got = %+v, expected empty (negative index)", got)
	}
}

func TestParseTutorCorrections_InvalidJSON(t *testing.T) {
	if _, err := parseTutorCorrections(json.RawMessage("not json"), 3); err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}
