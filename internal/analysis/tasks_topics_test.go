// internal/analysis/tasks_topics_test.go
package analysis

import (
	"encoding/json"
	"testing"
)

func TestParseTopics_Valid(t *testing.T) {
	raw := json.RawMessage(`{"topics":["travel plans","remote work"]}`)
	got, err := parseTopics(raw, 3)
	if err != nil {
		t.Fatalf("parseTopics unexpected error: %v", err)
	}
	if len(got) != 2 || got[0] != "travel plans" {
		t.Errorf("got = %+v, unexpected", got)
	}
}

func TestParseTopics_InvalidJSON(t *testing.T) {
	if _, err := parseTopics(json.RawMessage("not json"), 3); err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestParseTopics_TruncatesToMax(t *testing.T) {
	raw := json.RawMessage(`{"topics":["a","b","c","d","e","f"]}`)
	got, err := parseTopics(raw, 3)
	if err != nil {
		t.Fatalf("parseTopics unexpected error: %v", err)
	}
	want := []string{"a", "b", "c", "d"}
	if len(got) != len(want) {
		t.Fatalf("got = %+v, expected %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, expected %q", i, got[i], want[i])
		}
	}
}

func TestNewTopicsTask_HasNameVersionAndPrompt(t *testing.T) {
	tk := NewTopicsTask()
	if tk.Name() != "analyze_topics" {
		t.Errorf("Name() = %q, expected analyze_topics", tk.Name())
	}
	if tk.Version() != 5 {
		t.Errorf("Version() = %d, expected 5", tk.Version())
	}
	if tk.Prompt() == "" {
		t.Error("Prompt() empty, expected v5 content")
	}
}

func TestParseTopicsResult_Valid(t *testing.T) {
	resultJSON := json.RawMessage(`["travel","remote work"]`)
	got, err := ParseTopicsResult(resultJSON)
	if err != nil {
		t.Fatalf("ParseTopicsResult unexpected error: %v", err)
	}
	if len(got) != 2 || got[0] != "travel" || got[1] != "remote work" {
		t.Errorf("got = %+v, unexpected", got)
	}
}

func TestParseTopicsResult_InvalidJSON(t *testing.T) {
	if _, err := ParseTopicsResult(json.RawMessage("not json")); err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestAppendExistingTopics_EmptyLeavesTranscriptUnchanged(t *testing.T) {
	got := AppendExistingTopics("line", nil)
	if got != "line" {
		t.Errorf("got = %q, expected unchanged transcript", got)
	}
}

func TestAppendExistingTopics_AppendsBlock(t *testing.T) {
	got := AppendExistingTopics("line", []string{"travel", "remote work"})
	want := `line

Topics already used in other lessons (reuse when it makes sense):
- travel
- remote work
`
	if got != want {
		t.Errorf("got = %q, expected %q", got, want)
	}
}
