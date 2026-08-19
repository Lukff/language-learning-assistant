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

func TestParseTopics_TruncatesToMax(t *testing.T) {
	raw := json.RawMessage(`{"topics":["a","b","c","d","e","f"]}`)
	got, err := parseTopics(raw, 3)
	if err != nil {
		t.Fatalf("parseTopics erro inesperado: %v", err)
	}
	want := []string{"a", "b", "c", "d"}
	if len(got) != len(want) {
		t.Fatalf("got = %+v, esperado %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, esperado %q", i, got[i], want[i])
		}
	}
}

func TestNewTopicsTask_HasNameVersionAndPrompt(t *testing.T) {
	tk := NewTopicsTask()
	if tk.Name() != "analyze_topics" {
		t.Errorf("Name() = %q, esperado analyze_topics", tk.Name())
	}
	if tk.Version() != 4 {
		t.Errorf("Version() = %d, esperado 4", tk.Version())
	}
	if tk.Prompt() == "" {
		t.Error("Prompt() vazio, esperado conteúdo do v4")
	}
}

func TestParseTopicsResult_Valid(t *testing.T) {
	resultJSON := json.RawMessage(`["viagens","trabalho remoto"]`)
	got, err := ParseTopicsResult(resultJSON)
	if err != nil {
		t.Fatalf("ParseTopicsResult erro inesperado: %v", err)
	}
	if len(got) != 2 || got[0] != "viagens" || got[1] != "trabalho remoto" {
		t.Errorf("got = %+v, inesperado", got)
	}
}

func TestParseTopicsResult_InvalidJSON(t *testing.T) {
	if _, err := ParseTopicsResult(json.RawMessage("not json")); err == nil {
		t.Fatal("esperava erro para JSON inválido, obteve nil")
	}
}

func TestAppendExistingTopics_EmptyLeavesTranscriptUnchanged(t *testing.T) {
	got := AppendExistingTopics("linha", nil)
	if got != "linha" {
		t.Errorf("got = %q, esperado transcript inalterado", got)
	}
}

func TestAppendExistingTopics_AppendsBlock(t *testing.T) {
	got := AppendExistingTopics("linha", []string{"viagens", "trabalho remoto"})
	want := `linha

Tópicos já utilizados em outras aulas (reutilize quando fizer sentido):
- viagens
- trabalho remoto
`
	if got != want {
		t.Errorf("got = %q, esperado %q", got, want)
	}
}
