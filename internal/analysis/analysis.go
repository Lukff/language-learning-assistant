// internal/analysis/analysis.go
package analysis

import (
	"context"
	"encoding/json"
)

// Provider is the single interface implemented by each candidate LLM
// analysis service (DeepSeek, and in future slices: Anthropic, OpenAI, Gemini, GLM,
// Qwen — see docs/superpowers/specs/2026-07-19-analysis-llm-v1-design.md).
// Task-agnostic: it doesn't know about Correction/VocabularyItem/etc, it just exchanges
// a system prompt + the transcript for a raw response JSON — each
// TaskDef (see task.go) is what knows how to interpret that JSON.
type Provider interface {
	Name() string
	Model() string
	Complete(ctx context.Context, systemPrompt, transcript string) (json.RawMessage, error)
}
