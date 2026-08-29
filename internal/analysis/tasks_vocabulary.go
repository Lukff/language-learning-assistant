// internal/analysis/tasks_vocabulary.go
package analysis

import "encoding/json"

// VocabularyItem is a new word or expression for the Student to learn —
// includes PT/ES words used as a resort to the native language, never
// treated as an English mistake (see analyze-corrections-v2.md).
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
