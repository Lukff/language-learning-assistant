// internal/analysis/parsing.go
package analysis

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// parseAnalysisResponse converte o texto de conteúdo devolvido pelo LLM (já
// sem o envelope HTTP do provedor) para o domínio comum Result. O schema é
// definido por nós em prompts/analyze-v1.md, não pelo provedor — por isso
// esta função é compartilhada por todos os clients, ao contrário do
// mapeamento por provedor do internal/stt.
//
// Quando o provedor suporta prefill (ver openai_compatible.go), o modelo já
// começa a resposta direto no conteúdo do JSON — a única sujeira possível é
// um fechamento de code fence sobrando no final, removido abaixo. Não há
// abertura de fence a remover, e provedores sem prefill (modo JSON nativo)
// já devolvem JSON puro, então o strip é um no-op inofensivo para eles.
func parseAnalysisResponse(raw []byte) (*Result, error) {
	trimmed := stripTrailingCodeFence(raw)

	var parsed analysisJSON
	if err := json.Unmarshal(trimmed, &parsed); err != nil {
		return nil, fmt.Errorf("analysis: json inválido: %w", err)
	}

	return &Result{
		Corrections:      parsed.corrections(),
		Vocabulary:       parsed.vocabulary(),
		TutorExpressions: parsed.tutorExpressions(),
	}, nil
}

func stripTrailingCodeFence(raw []byte) []byte {
	trimmed := bytes.TrimSpace(raw)
	trimmed = bytes.TrimSuffix(trimmed, []byte("```"))
	return bytes.TrimSpace(trimmed)
}

type analysisJSON struct {
	Corrections []struct {
		Original    string `json:"original"`
		Correction  string `json:"correction"`
		Explanation string `json:"explanation"`
	} `json:"corrections"`
	Vocabulary []struct {
		Term        string `json:"term"`
		Translation string `json:"translation"`
	} `json:"vocabulary"`
	TutorExpressions []struct {
		Text string `json:"text"`
		Note string `json:"note"`
	} `json:"tutor_expressions"`
}

func (a analysisJSON) corrections() []Correction {
	out := make([]Correction, 0, len(a.Corrections))
	for _, c := range a.Corrections {
		out = append(out, Correction{Original: c.Original, Correction: c.Correction, Explanation: c.Explanation})
	}
	return out
}

func (a analysisJSON) vocabulary() []VocabularyItem {
	out := make([]VocabularyItem, 0, len(a.Vocabulary))
	for _, v := range a.Vocabulary {
		out = append(out, VocabularyItem{Term: v.Term, Translation: v.Translation})
	}
	return out
}

func (a analysisJSON) tutorExpressions() []Expression {
	out := make([]Expression, 0, len(a.TutorExpressions))
	for _, e := range a.TutorExpressions {
		out = append(out, Expression{Text: e.Text, Note: e.Note})
	}
	return out
}
