// internal/analysis/analysis.go
package analysis

import (
	"context"
	"encoding/json"
)

// Provider é a interface única implementada por cada serviço de análise LLM
// candidato (DeepSeek, e em fatias futuras: Anthropic, OpenAI, Gemini, GLM,
// Qwen — ver docs/superpowers/specs/2026-07-19-analysis-llm-v1-design.md).
// Agnóstica de tarefa: não conhece Correction/VocabularyItem/etc, só troca
// um prompt de sistema + a transcrição por um JSON de resposta bruto — cada
// TaskDef (ver task.go) é quem sabe interpretar esse JSON.
type Provider interface {
	Name() string
	Complete(ctx context.Context, systemPrompt, transcript string) (json.RawMessage, error)
}
