// internal/analysis/tasks_corrections_test.go
package analysis

import (
	"encoding/json"
	"testing"
)

func TestParseCorrections_Valid(t *testing.T) {
	raw := json.RawMessage(`{"corrections":[{"utterance_index":1,"original":"I go yesterday","correction":"I went yesterday","explanation":"Passado simples irregular."}]}`)
	got, err := parseCorrections(raw, 3)
	if err != nil {
		t.Fatalf("parseCorrections erro inesperado: %v", err)
	}
	if len(got) != 1 || got[0].Original != "I go yesterday" || got[0].CorrectionTx != "I went yesterday" {
		t.Errorf("got = %+v, inesperado", got)
	}
}

func TestParseCorrections_DropsOutOfRangeIndex(t *testing.T) {
	raw := json.RawMessage(`{"corrections":[{"utterance_index":0,"original":"a","correction":"b","explanation":"c"},{"utterance_index":99,"original":"x","correction":"y","explanation":"z"}]}`)
	got, err := parseCorrections(raw, 1)
	if err != nil {
		t.Fatalf("parseCorrections erro inesperado: %v", err)
	}
	if len(got) != 1 || got[0].Original != "a" {
		t.Errorf("got = %+v, esperado só o item com índice válido", got)
	}
}

func TestParseCorrections_InvalidJSON(t *testing.T) {
	if _, err := parseCorrections(json.RawMessage("not json"), 3); err == nil {
		t.Fatal("esperava erro para JSON inválido, obteve nil")
	}
}

func TestNewCorrectionsTask_HasNameAndPrompt(t *testing.T) {
	tk := NewCorrectionsTask()
	if tk.Name() != "analyze_corrections" {
		t.Errorf("Name() = %q, esperado analyze_corrections", tk.Name())
	}
	if tk.Prompt() == "" {
		t.Error("Prompt() vazio, esperado conteúdo carregado do .md")
	}
}

func TestParseCorrectionsResult_Valid(t *testing.T) {
	resultJSON := json.RawMessage(`[{"utterance_index":0,"original":"I go","correction":"I went","explanation":"passado"}]`)
	got, err := ParseCorrectionsResult(resultJSON)
	if err != nil {
		t.Fatalf("ParseCorrectionsResult erro inesperado: %v", err)
	}
	if len(got) != 1 || got[0].Original != "I go" || got[0].CorrectionTx != "I went" {
		t.Errorf("got = %+v, inesperado", got)
	}
}

func TestParseCorrectionsResult_InvalidJSON(t *testing.T) {
	if _, err := ParseCorrectionsResult(json.RawMessage("not json")); err == nil {
		t.Fatal("esperava erro para JSON inválido, obteve nil")
	}
}
