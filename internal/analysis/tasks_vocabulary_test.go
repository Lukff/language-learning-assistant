// internal/analysis/tasks_vocabulary_test.go
package analysis

import (
	"encoding/json"
	"testing"
)

func TestParseVocabulary_Valid(t *testing.T) {
	raw := json.RawMessage(`{"vocabulary":[{"term":"homesick","translation":"com saudade de casa"}]}`)
	got, err := parseVocabulary(raw, 3)
	if err != nil {
		t.Fatalf("parseVocabulary erro inesperado: %v", err)
	}
	if len(got) != 1 || got[0].Term != "homesick" || got[0].Translation != "com saudade de casa" {
		t.Errorf("got = %+v, inesperado", got)
	}
}

func TestParseVocabulary_EmptyList(t *testing.T) {
	got, err := parseVocabulary(json.RawMessage(`{"vocabulary":[]}`), 3)
	if err != nil {
		t.Fatalf("parseVocabulary erro inesperado: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got = %+v, esperado lista vazia", got)
	}
}

func TestParseVocabulary_InvalidJSON(t *testing.T) {
	if _, err := parseVocabulary(json.RawMessage("not json"), 3); err == nil {
		t.Fatal("esperava erro para JSON inválido, obteve nil")
	}
}
