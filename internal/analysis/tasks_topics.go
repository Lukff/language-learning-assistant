// internal/analysis/tasks_topics.go
package analysis

import "encoding/json"

func parseTopics(raw json.RawMessage, utteranceCount int) ([]string, error) {
	var parsed struct {
		Topics []string `json:"topics"`
	}
	if err := unmarshalJSON(raw, &parsed); err != nil {
		return nil, err
	}
	return parsed.Topics, nil
}

func newTopicsTask() TaskDef {
	return task[[]string]{name: "analyze_topics", version: 1, prompt: mustLoadPrompt("analyze-topics-v1.md"), parse: parseTopics}
}
