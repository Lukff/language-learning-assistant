// internal/analysis/task.go
package analysis

import (
	"context"
	"encoding/json"
	"fmt"

	"assistente-idiomas/prompts"
)

// TaskDef is the common interface for the 7 analysis tasks — allows iterating
// over all of them in a single list (var Tasks, see Task 4 of this plan) despite each
// one having a different result type (generics don't allow a slice of
// task[T] with a variable T, hence this non-generic interface on top).
type TaskDef interface {
	Name() string   // e.g.: "analyze_corrections" — same value stored in prompts.name and analysis_results.task
	Version() int   // prompt version (manual bump in code when the .md content changes)
	Prompt() string // prompt content (embed.FS)

	// Execute calls provider.Complete, parses the result and (when the task is
	// anchored) discards items with an invalid utterance_index. Returns the
	// already-validated JSON (ready to be stored in analysis_results.result_json) and the
	// raw content returned by the provider, for the caller to decide what
	// to do with it.
	Execute(ctx context.Context, provider Provider, transcript string, utteranceCount int) (resultJSON json.RawMessage, raw json.RawMessage, err error)
}

type task[T any] struct {
	name    string
	version int
	prompt  string
	parse   func(raw json.RawMessage, utteranceCount int) (T, error)
}

func (t task[T]) Name() string   { return t.name }
func (t task[T]) Version() int   { return t.version }
func (t task[T]) Prompt() string { return t.prompt }

func (t task[T]) Execute(ctx context.Context, provider Provider, transcript string, utteranceCount int) (json.RawMessage, json.RawMessage, error) {
	raw, err := provider.Complete(ctx, t.prompt, transcript)
	if err != nil {
		return nil, raw, fmt.Errorf("analysis: task %s: %w", t.name, err)
	}
	parsed, err := t.parse(raw, utteranceCount)
	if err != nil {
		return nil, raw, fmt.Errorf("analysis: task %s: parse: %w", t.name, err)
	}
	resultJSON, err := json.Marshal(parsed)
	if err != nil {
		return nil, raw, fmt.Errorf("analysis: task %s: serialize result: %w", t.name, err)
	}
	return resultJSON, raw, nil
}

// anchored is implemented by item types whose parse references a
// specific utterance in the transcript (Correction, TutorCorrection,
// TutorFeedbackItem, see Task 4) — a negative UtteranceIndex represents
// "absent from the model's JSON", treated the same as an out-of-range index.
type anchored interface {
	UtteranceIndex() int
}

// filterAnchored discards (also returning the discarded count, for
// logging) items whose UtteranceIndex doesn't fall in [0, utteranceCount).
func filterAnchored[T anchored](items []T, utteranceCount int) (kept []T, discarded int) {
	kept = items[:0]
	for _, it := range items {
		idx := it.UtteranceIndex()
		if idx < 0 || idx >= utteranceCount {
			discarded++
			continue
		}
		kept = append(kept, it)
	}
	return kept, discarded
}

// mustLoadPrompt reads a prompt embedded in prompts.FS (prompts/embed.go,
// Task 2) — panicking when it's missing is intentional: a missing prompt is a
// build/packaging error, not a runtime condition to handle
// gracefully (same spirit as a template.Must).
func mustLoadPrompt(filename string) string {
	b, err := prompts.FS.ReadFile(filename)
	if err != nil {
		panic(fmt.Sprintf("analysis: prompt %s not found: %v", filename, err))
	}
	return string(b)
}
