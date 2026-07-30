// internal/analysis/transcript_test.go
package analysis

import (
	"testing"

	"assistente-idiomas/internal/stt"
)

func syntheticUtterances() []stt.Utterance {
	return []stt.Utterance{
		{Speaker: "speaker_0", Text: "Hi, how was your week?"},
		{Speaker: "speaker_1", Text: "It was good, I felt a lot of saudade for my hometown though."},
		{Speaker: "speaker_0", Text: "That's understandable."},
	}
}

func TestFormatTranscript(t *testing.T) {
	roles := map[string]string{"speaker_0": "tutor", "speaker_1": "aluno"}

	got, err := FormatTranscript(syntheticUtterances(), roles)
	if err != nil {
		t.Fatalf("FormatTranscript retornou erro: %v", err)
	}

	want := "[0] Tutor: Hi, how was your week?\n" +
		"[1] Aluno: It was good, I felt a lot of saudade for my hometown though.\n" +
		"[2] Tutor: That's understandable.\n"
	if got != want {
		t.Errorf("FormatTranscript = %q, esperava %q", got, want)
	}
}

func TestFormatTranscript_MissingRole(t *testing.T) {
	roles := map[string]string{"speaker_0": "tutor"}

	_, err := FormatTranscript(syntheticUtterances(), roles)
	if err == nil {
		t.Fatal("esperava erro para locutor sem papel mapeado, obteve nil")
	}
}

func TestFormatTranscript_InvalidRole(t *testing.T) {
	roles := map[string]string{"speaker_0": "tutor", "speaker_1": "narrator"}

	_, err := FormatTranscript(syntheticUtterances(), roles)
	if err == nil {
		t.Fatal("esperava erro para papel inválido, obteve nil")
	}
}

func TestSpeakerExamples(t *testing.T) {
	got := SpeakerExamples(syntheticUtterances(), 1)

	if len(got["speaker_0"]) != 1 || got["speaker_0"][0] != "Hi, how was your week?" {
		t.Errorf("speaker_0 examples = %+v, inesperado", got["speaker_0"])
	}
	if len(got["speaker_1"]) != 1 || got["speaker_1"][0] != "It was good, I felt a lot of saudade for my hometown though." {
		t.Errorf("speaker_1 examples = %+v, inesperado", got["speaker_1"])
	}
}

func TestSpeakerExamples_LimitsToN(t *testing.T) {
	got := SpeakerExamples(syntheticUtterances(), 1)
	if len(got["speaker_0"]) != 1 {
		t.Errorf("esperava no máximo 1 exemplo por locutor, obteve %d", len(got["speaker_0"]))
	}
}
