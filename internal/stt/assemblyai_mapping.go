package stt

import (
	"encoding/json"
	"fmt"
	"time"
)

type assemblyAIResponse struct {
	Status     string                `json:"status"`
	Utterances []assemblyAIUtterance `json:"utterances"`
}

type assemblyAIUtterance struct {
	Speaker string           `json:"speaker"`
	Text    string           `json:"text"`
	Start   int64            `json:"start"`
	End     int64            `json:"end"`
	Words   []assemblyAIWord `json:"words"`
}

type assemblyAIWord struct {
	Text  string `json:"text"`
	Start int64  `json:"start"`
	End   int64  `json:"end"`
}

// mapAssemblyAIResponse converte o JSON bruto do endpoint
// GET /v2/transcript/{id} do AssemblyAI para o domínio comum stt.Result.
// Mantida separada das chamadas HTTP (assemblyai.go) para ser testável com
// fixture, sem precisar de rede.
func mapAssemblyAIResponse(raw []byte) (*Result, error) {
	var parsed assemblyAIResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("json inválido: %w", err)
	}
	if parsed.Status != "completed" {
		return nil, fmt.Errorf("status inesperado: %q", parsed.Status)
	}

	utterances := make([]Utterance, 0, len(parsed.Utterances))
	for _, u := range parsed.Utterances {
		words := make([]Word, 0, len(u.Words))
		for _, w := range u.Words {
			words = append(words, Word{
				Text:  w.Text,
				Start: millisToDuration(w.Start),
				End:   millisToDuration(w.End),
			})
		}
		utterances = append(utterances, Utterance{
			Speaker: fmt.Sprintf("speaker_%s", u.Speaker),
			Text:    u.Text,
			Start:   millisToDuration(u.Start),
			End:     millisToDuration(u.End),
			Words:   words,
		})
	}

	return &Result{
		RawResponse: raw,
		Utterances:  utterances,
	}, nil
}

// millisToDuration converte milissegundos inteiros (formato do AssemblyAI)
// para time.Duration. Sem arredondamento necessário — ao contrário da
// Gladia (float64 segundos), o AssemblyAI já entrega inteiros.
func millisToDuration(ms int64) time.Duration {
	return time.Duration(ms) * time.Millisecond
}
