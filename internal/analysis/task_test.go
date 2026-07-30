// internal/analysis/task_test.go
package analysis

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type testAnchoredItem struct {
	idx int
}

func (i testAnchoredItem) UtteranceIndex() int { return i.idx }

func TestFilterAnchored_DropsOutOfRange(t *testing.T) {
	items := []testAnchoredItem{{idx: 0}, {idx: 5}, {idx: -1}, {idx: 2}}
	kept, discarded := filterAnchored(items, 3)
	if len(kept) != 2 || kept[0].idx != 0 || kept[1].idx != 2 {
		t.Errorf("kept = %+v, esperado índices 0 e 2", kept)
	}
	if discarded != 2 {
		t.Errorf("discarded = %d, esperado 2", discarded)
	}
}

func TestFilterAnchored_KeepsAllWhenValid(t *testing.T) {
	items := []testAnchoredItem{{idx: 0}, {idx: 1}}
	kept, discarded := filterAnchored(items, 2)
	if len(kept) != 2 {
		t.Errorf("kept = %+v, esperado os 2 itens", kept)
	}
	if discarded != 0 {
		t.Errorf("discarded = %d, esperado 0", discarded)
	}
}

func TestMustLoadPrompt_PanicsWhenMissing(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("esperava panic para prompt inexistente, não houve panic")
		}
	}()
	mustLoadPrompt("nao-existe-v99.md")
}

type fakeProvider struct {
	content json.RawMessage
	err     error
}

func (f fakeProvider) Name() string { return "fake" }
func (f fakeProvider) Complete(ctx context.Context, systemPrompt, transcript string) (json.RawMessage, error) {
	return f.content, f.err
}

func TestTaskExecute_ReturnsParsedResultAndRaw(t *testing.T) {
	parseCalls := 0
	tk := task[[]string]{
		name: "fake_task", version: 1, prompt: "system prompt",
		parse: func(raw json.RawMessage, utteranceCount int) ([]string, error) {
			parseCalls++
			var out []string
			if err := json.Unmarshal(raw, &out); err != nil {
				return nil, err
			}
			return out, nil
		},
	}
	provider := fakeProvider{content: json.RawMessage(`["a","b"]`)}
	resultJSON, raw, err := tk.Execute(context.Background(), provider, "transcript", 3)
	if err != nil {
		t.Fatalf("Execute erro inesperado: %v", err)
	}
	if string(raw) != `["a","b"]` {
		t.Errorf("raw = %s, inesperado", raw)
	}
	if string(resultJSON) != `["a","b"]` {
		t.Errorf("resultJSON = %s, inesperado", resultJSON)
	}
	if parseCalls != 1 {
		t.Errorf("parse chamado %d vezes, esperado 1", parseCalls)
	}
}

func TestTaskExecute_ProviderErrorPreservesRaw(t *testing.T) {
	tk := task[[]string]{
		name: "fake_task", version: 1, prompt: "p",
		parse: func(raw json.RawMessage, n int) ([]string, error) { return nil, nil },
	}
	provider := fakeProvider{content: json.RawMessage(`partial`), err: errors.New("boom")}
	resultJSON, raw, err := tk.Execute(context.Background(), provider, "t", 1)
	if err == nil {
		t.Fatal("esperava erro, obteve nil")
	}
	if resultJSON != nil {
		t.Errorf("resultJSON = %s, esperado nil em caso de erro", resultJSON)
	}
	if string(raw) != "partial" {
		t.Errorf("raw = %s, esperado preservado mesmo com erro", raw)
	}
}
