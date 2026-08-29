// internal/analysis/corrections_display_test.go
package analysis

import (
	"testing"

	"assistente-idiomas/internal/stt"
)

func TestMatchCorrections_SplitsTextAroundTheWrongSpan(t *testing.T) {
	tests := []struct {
		name       string
		text       string
		original   string
		wantBefore string
		wantWrong  string
		wantAfter  string
	}{
		{
			name:       "exact match",
			text:       "I go to school yesterday",
			original:   "I go",
			wantBefore: "",
			wantWrong:  "I go",
			wantAfter:  " to school yesterday",
		},
		{
			name:       "case difference",
			text:       "I GO to school yesterday",
			original:   "i go",
			wantBefore: "",
			wantWrong:  "I GO",
			wantAfter:  " to school yesterday",
		},
		{
			name:       "whitespace difference",
			text:       "I  go to school yesterday",
			original:   "I go",
			wantBefore: "",
			wantWrong:  "I  go",
			wantAfter:  " to school yesterday",
		},
		{
			name:       "not found",
			text:       "I go to school yesterday",
			original:   "she goes",
			wantBefore: "",
			wantWrong:  "",
			wantAfter:  "",
		},
		{
			name:       "first occurrence used when original repeats",
			text:       "go go go",
			original:   "go",
			wantBefore: "",
			wantWrong:  "go",
			wantAfter:  " go go",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			utterances := []stt.Utterance{{Speaker: "speaker_0", Text: tt.text}}
			corrections := []Correction{{UtteranceIdx: 0, Original: tt.original, CorrectionTx: "fix", Explanation: "why"}}
			got := MatchCorrections(utterances, corrections)
			if len(got) != 1 {
				t.Fatalf("len(got) = %d, expected 1", len(got))
			}
			if got[0].Before != tt.wantBefore || got[0].Wrong != tt.wantWrong || got[0].After != tt.wantAfter {
				t.Errorf("got[0] = %+v, expected Before=%q Wrong=%q After=%q", got[0], tt.wantBefore, tt.wantWrong, tt.wantAfter)
			}
		})
	}
}

func TestMatchCorrections_OriginalAlwaysPopulatedEvenWithoutMatch(t *testing.T) {
	utterances := []stt.Utterance{{Speaker: "speaker_0", Text: "I go to school yesterday"}}
	corrections := []Correction{{UtteranceIdx: 0, Original: "she goes", CorrectionTx: "fix", Explanation: "why"}}
	got := MatchCorrections(utterances, corrections)
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, expected 1", len(got))
	}
	if got[0].Original != "she goes" {
		t.Errorf("Original = %q, expected to be preserved even without a match", got[0].Original)
	}
	if got[0].Wrong != "" {
		t.Errorf("Wrong = %q, expected empty (no match)", got[0].Wrong)
	}
}

func TestMatchCorrections_MultipleCorrectionsAcrossUtterances(t *testing.T) {
	utterances := []stt.Utterance{
		{Speaker: "speaker_0", Text: "I go yesterday"},
		{Speaker: "speaker_1", Text: "OK"},
		{Speaker: "speaker_0", Text: "She go too"},
	}
	corrections := []Correction{
		{UtteranceIdx: 0, Original: "I go", CorrectionTx: "I went", Explanation: "a"},
		{UtteranceIdx: 2, Original: "She go", CorrectionTx: "She goes", Explanation: "b"},
	}
	got := MatchCorrections(utterances, corrections)
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, expected 2", len(got))
	}
	if got[0].UtteranceIndex != 0 || got[0].Wrong != "I go" {
		t.Errorf("got[0] = %+v, unexpected", got[0])
	}
	if got[1].UtteranceIndex != 2 || got[1].Wrong != "She go" {
		t.Errorf("got[1] = %+v, unexpected", got[1])
	}
}
