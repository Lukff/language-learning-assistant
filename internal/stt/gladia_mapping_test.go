package stt

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMapGladiaResponse(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "gladia_response.json"))
	if err != nil {
		t.Fatalf("erro lendo fixture: %v", err)
	}

	result, err := mapGladiaResponse(raw)
	if err != nil {
		t.Fatalf("mapGladiaResponse retornou erro: %v", err)
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

func TestMapGladiaResponse_InvalidJSON(t *testing.T) {
	_, err := mapGladiaResponse([]byte("not json"))
	if err == nil {
		t.Fatal("esperava erro para JSON inválido, obteve nil")
	}
}

func TestMapGladiaResponse_UnexpectedStatus(t *testing.T) {
	_, err := mapGladiaResponse([]byte(`{"status":"processing","result":{}}`))
	if err == nil {
		t.Fatal("esperava erro para status diferente de done, obteve nil")
	}
}
