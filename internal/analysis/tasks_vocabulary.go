// internal/analysis/tasks_vocabulary.go
package analysis

import "encoding/json"

// VocabularyItem é uma palavra ou expressão nova pro Aluno aprender —
// inclui palavras em PT/ES usadas como recurso ao idioma nativo, nunca
// tratadas como erro de inglês (ver analyze-corrections-v1.md).
type VocabularyItem struct {
	Term        string `json:"term"`
	Translation string `json:"translation"`
}

func parseVocabulary(raw json.RawMessage, utteranceCount int) ([]VocabularyItem, error) {
	var parsed struct {
		Vocabulary []VocabularyItem `json:"vocabulary"`
	}
	if err := unmarshalJSON(raw, &parsed); err != nil {
		return nil, err
	}
	return parsed.Vocabulary, nil
}

func newVocabularyTask() TaskDef {
	return task[[]VocabularyItem]{name: "analyze_vocabulary", version: 1, prompt: mustLoadPrompt("analyze-vocabulary-v1.md"), parse: parseVocabulary}
}
