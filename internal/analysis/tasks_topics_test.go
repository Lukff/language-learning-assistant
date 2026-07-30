// internal/analysis/tasks_topics_test.go
package analysis

import (
	"encoding/json"
	"testing"
)

func TestParseTopics_Valid(t *testing.T) {
	raw := json.RawMessage(`{"topics":["planos de viagem","trabalho remoto"]}`)
	got, err := parseTopics(raw, 3)
	if err != nil {
		t.Fatalf("parseTopics erro inesperado: %v", err)
	}
	if len(got) != 2 || got[0] != "planos de viagem" {
		t.Errorf("got = %+v, inesperado", got)
	}
}

func TestParseTopics_InvalidJSON(t *testing.T) {
	if _, err := parseTopics(json.RawMessage("not json"), 3); err == nil {
		t.Fatal("esperava erro para JSON inválido, obteve nil")
	}
}
