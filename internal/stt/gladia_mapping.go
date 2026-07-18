package stt

import (
	"encoding/json"
	"fmt"
	"math"
	"time"
)

type gladiaResponse struct {
	Status string `json:"status"`
	Result struct {
		Transcription struct {
			Utterances []gladiaUtterance `json:"utterances"`
		} `json:"transcription"`
	} `json:"result"`
}

type gladiaUtterance struct {
	Start   float64      `json:"start"`
	End     float64      `json:"end"`
	Text    string       `json:"text"`
	Speaker int          `json:"speaker"`
	Words   []gladiaWord `json:"words"`
}

type gladiaWord struct {
	Word  string  `json:"word"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

// mapGladiaResponse converte o JSON bruto do endpoint
// GET /v2/pre-recorded/{id} da Gladia para o domínio comum stt.Result.
// Mantida separada das chamadas HTTP (gladia.go) para ser testável com
// fixture, sem precisar de rede.
func mapGladiaResponse(raw []byte) (*Result, error) {
	var parsed gladiaResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("json inválido: %w", err)
	}
	if parsed.Status != "done" {
		return nil, fmt.Errorf("status inesperado: %q", parsed.Status)
	}

	utterances := make([]Utterance, 0, len(parsed.Result.Transcription.Utterances))
	for _, u := range parsed.Result.Transcription.Utterances {
		words := make([]Word, 0, len(u.Words))
		for _, w := range u.Words {
			words = append(words, Word{
				Text:  w.Word,
				Start: secondsToDuration(w.Start),
				End:   secondsToDuration(w.End),
			})
		}
		utterances = append(utterances, Utterance{
			Speaker: fmt.Sprintf("speaker_%d", u.Speaker),
			Text:    u.Text,
			Start:   secondsToDuration(u.Start),
			End:     secondsToDuration(u.End),
			Words:   words,
		})
	}

	return &Result{
		RawResponse: raw,
		Utterances:  utterances,
	}, nil
}

// secondsToDuration arredonda para o milissegundo mais próximo, evitando
// artefatos de ponto flutuante do float64 vindo do JSON (precisão de
// milissegundo é mais que suficiente frente à tolerância de ~1s exigida
// pela História 2).
func secondsToDuration(s float64) time.Duration {
	return time.Duration(math.Round(s*1000)) * time.Millisecond
}
