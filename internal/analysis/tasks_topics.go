// internal/analysis/tasks_topics.go
package analysis

import (
	"encoding/json"
	"fmt"
	"strings"
)

// maxTopicsPerLesson limita a quantidade de tópicos aceitos por aula. O
// prompt já instrui o LLM a devolver no máximo 4, mas aplicamos o corte
// aqui também como rede de segurança contra respostas fora da instrução.
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

// NewTopicsTask devolve a tarefa de tópicos na versão 4 do prompt
// (granularidade geral + reaproveitamento de tópicos existentes + máximo de
// 4 tópicos por aula + tópicos em inglês).
func NewTopicsTask() TaskDef {
	return task[[]string]{name: "analyze_topics", version: 4, prompt: mustLoadPrompt("analyze-topics-v4.md"), parse: parseTopics}
}

// ParseTopicsResult decodifica um result_json persistido (array JSON de
// strings — resultJSON é json.Marshal([]string), sem envelope) de volta em
// []string.
func ParseTopicsResult(resultJSON json.RawMessage) ([]string, error) {
	var out []string
	if err := json.Unmarshal(resultJSON, &out); err != nil {
		return nil, fmt.Errorf("analysis: desserializar resultado de analyze_topics: %w", err)
	}
	return out, nil
}

// AppendExistingTopics anexa a lista de tópicos já existentes ao conteúdo da
// transcrição, num bloco final que o prompt reconhece. Devolve transcript
// inalterado quando não há tópicos existentes.
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
