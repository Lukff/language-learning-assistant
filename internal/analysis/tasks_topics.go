// internal/analysis/tasks_topics.go
package analysis

import (
	"encoding/json"
	"fmt"
	"strings"
)

// maxTopicsPerLesson limits the number of topics accepted per lesson. The
// prompt already instructs the LLM to return at most 4, but we also apply the cutoff
// here as a safety net against responses that don't follow the instruction.
const maxTopicsPerLesson = 4

func parseTopics(raw json.RawMessage, utteranceCount int) ([]string, error) {
	var parsed struct {
		Topics []string `json:"topics"`
	}
	if err := unmarshalJSON(raw, &parsed); err != nil {
		return nil, err
	}
	if len(parsed.Topics) > maxTopicsPerLesson {
		parsed.Topics = parsed.Topics[:maxTopicsPerLesson]
	}
	return parsed.Topics, nil
}

// NewTopicsTask returns the topics task on prompt version 4
// (general granularity + reuse of existing topics + a maximum of
// 4 topics per lesson + topics in English).
func NewTopicsTask() TaskDef {
	return task[[]string]{name: "analyze_topics", version: 4, prompt: mustLoadPrompt("analyze-topics-v4.md"), parse: parseTopics}
}

// ParseTopicsResult decodes a persisted result_json (JSON array of
// strings — resultJSON is json.Marshal([]string), with no envelope) back into
// []string.
func ParseTopicsResult(resultJSON json.RawMessage) ([]string, error) {
	var out []string
	if err := json.Unmarshal(resultJSON, &out); err != nil {
		return nil, fmt.Errorf("analysis: desserializar resultado de analyze_topics: %w", err)
	}
	return out, nil
}

// AppendExistingTopics appends the list of already-existing topics to the
// transcript content, in a final block that the prompt recognizes. Returns transcript
// unchanged when there are no existing topics.
func AppendExistingTopics(transcript string, existing []string) string {
	if len(existing) == 0 {
		return transcript
	}
	var b strings.Builder
	b.WriteString(transcript)
	b.WriteString("\n\nTópicos já utilizados em outras aulas (reutilize quando fizer sentido):\n")
	for _, t := range existing {
		b.WriteString("- ")
		b.WriteString(t)
		b.WriteString("\n")
	}
	return b.String()
}
