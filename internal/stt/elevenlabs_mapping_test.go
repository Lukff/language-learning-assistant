package stt

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMapElevenLabsResponse(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "elevenlabs_response.json"))
	if err != nil {
		t.Fatalf("erro lendo fixture: %v", err)
	}

	result, err := mapElevenLabsResponse(raw)
	if err != nil {
		t.Fatalf("mapElevenLabsResponse retornou erro: %v", err)
	}

	if len(result.Utterances) != 2 {
		t.Fatalf("esperava 2 utterances, obteve %d", len(result.Utterances))
	}

	first := result.Utterances[0]
	if first.Speaker != "speaker_0" {
		t.Errorf("Speaker = %q, esperava %q", first.Speaker, "speaker_0")
	}
	if first.Text != "Hi, how was your week?" {
		t.Errorf("Text = %q, inesperado", first.Text)
	}
	if first.Start != 420*time.Millisecond {
		t.Errorf("Start = %v, esperava %v", first.Start, 420*time.Millisecond)
	}
	if len(first.Words) != 5 {
		t.Fatalf("esperava 5 words, obteve %d", len(first.Words))
	}
	if first.Words[0].Text != "Hi," {
		t.Errorf("Words[0].Text = %q, inesperado", first.Words[0].Text)
	}

	second := result.Utterances[1]
	if second.Speaker != "speaker_1" {
		t.Errorf("Speaker = %q, esperava %q", second.Speaker, "speaker_1")
	}
	if len(second.Words) != 13 {
		t.Fatalf("esperava 13 words, obteve %d", len(second.Words))
	}
	if second.Words[8].Text != "saudade" {
		t.Errorf("Words[8].Text = %q, esperava %q", second.Words[8].Text, "saudade")
	}

	if string(result.RawResponse) != string(raw) {
		t.Error("RawResponse deveria preservar o JSON bruto exatamente como recebido")
	}
}

func TestMapElevenLabsResponse_InvalidJSON(t *testing.T) {
	_, err := mapElevenLabsResponse([]byte("not json"))
	if err == nil {
		t.Fatal("esperava erro para JSON inválido, obteve nil")
	}
}

func TestMapElevenLabsResponse_ExcludesNonWordEntriesFromWords(t *testing.T) {
	raw := []byte(`{
		"words": [
			{"text": "Well", "type": "word", "start": 0.0, "end": 0.3, "speaker_id": "speaker_0"},
			{"text": " ", "type": "spacing", "start": 0.3, "end": 0.35, "speaker_id": "speaker_0"},
			{"text": "(laughs)", "type": "audio_event", "start": 0.35, "end": 0.9, "speaker_id": "speaker_0"},
			{"text": " ", "type": "spacing", "start": 0.9, "end": 0.95, "speaker_id": "speaker_0"},
			{"text": "ok", "type": "word", "start": 0.95, "end": 1.1, "speaker_id": "speaker_0"}
		]
	}`)

	result, err := mapElevenLabsResponse(raw)
	if err != nil {
		t.Fatalf("mapElevenLabsResponse retornou erro: %v", err)
	}
	if len(result.Utterances) != 1 {
		t.Fatalf("esperava 1 utterance, obteve %d", len(result.Utterances))
	}

	u := result.Utterances[0]
	if u.Text != "Well (laughs) ok" {
		t.Errorf("Text = %q, esperava %q", u.Text, "Well (laughs) ok")
	}
	if len(u.Words) != 2 {
		t.Fatalf("esperava 2 words (spacing/audio_event excluídos), obteve %d", len(u.Words))
	}
	if u.Words[0].Text != "Well" || u.Words[1].Text != "ok" {
		t.Errorf("Words = %+v, inesperado", u.Words)
	}
}
