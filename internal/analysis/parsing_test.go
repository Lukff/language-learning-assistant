// internal/analysis/parsing_test.go
package analysis

import "testing"

func TestParseAnalysisResponse(t *testing.T) {
	raw := []byte(`{
		"corrections": [{"original": "I go yesterday", "correction": "I went yesterday", "explanation": "Passado simples irregular."}],
		"vocabulary": [{"term": "homesick", "translation": "com saudade de casa"}],
		"tutor_expressions": [{"text": "let's circle back to that", "note": "retomar um assunto depois"}]
	}`)

	result, err := parseAnalysisResponse(raw)
	if err != nil {
		t.Fatalf("parseAnalysisResponse retornou erro: %v", err)
	}

	if len(result.Corrections) != 1 || result.Corrections[0].Original != "I go yesterday" {
		t.Errorf("Corrections = %+v, inesperado", result.Corrections)
	}
	if result.Corrections[0].Correction != "I went yesterday" || result.Corrections[0].Explanation != "Passado simples irregular." {
		t.Errorf("Corrections[0] = %+v, inesperado", result.Corrections[0])
	}
	if len(result.Vocabulary) != 1 || result.Vocabulary[0].Term != "homesick" || result.Vocabulary[0].Translation != "com saudade de casa" {
		t.Errorf("Vocabulary = %+v, inesperado", result.Vocabulary)
	}
	if len(result.TutorExpressions) != 1 || result.TutorExpressions[0].Text != "let's circle back to that" {
		t.Errorf("TutorExpressions = %+v, inesperado", result.TutorExpressions)
	}
}

func TestParseAnalysisResponse_EmptyLists(t *testing.T) {
	raw := []byte(`{"corrections": [], "vocabulary": [], "tutor_expressions": []}`)

	result, err := parseAnalysisResponse(raw)
	if err != nil {
		t.Fatalf("parseAnalysisResponse retornou erro: %v", err)
	}
	if len(result.Corrections) != 0 || len(result.Vocabulary) != 0 || len(result.TutorExpressions) != 0 {
		t.Errorf("esperava listas vazias, obteve %+v", result)
	}
}

func TestParseAnalysisResponse_StripsTrailingCodeFence(t *testing.T) {
	raw := []byte("{\"corrections\": [], \"vocabulary\": [], \"tutor_expressions\": []}\n```")

	result, err := parseAnalysisResponse(raw)
	if err != nil {
		t.Fatalf("parseAnalysisResponse retornou erro: %v", err)
	}
	if len(result.Corrections) != 0 || len(result.Vocabulary) != 0 || len(result.TutorExpressions) != 0 {
		t.Errorf("esperava listas vazias após remover o fence, obteve %+v", result)
	}
}

func TestParseAnalysisResponse_InvalidJSON(t *testing.T) {
	_, err := parseAnalysisResponse([]byte("not json"))
	if err == nil {
		t.Fatal("esperava erro para JSON inválido, obteve nil")
	}
}
