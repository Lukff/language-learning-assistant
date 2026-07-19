package stt

import (
	"encoding/json"
	"fmt"
)

type deepgramResponse struct {
	Results struct {
		Utterances []deepgramUtterance `json:"utterances"`
	} `json:"results"`
}

type deepgramUtterance struct {
	Start      float64        `json:"start"`
	End        float64        `json:"end"`
	Speaker    int            `json:"speaker"`
	Transcript string         `json:"transcript"`
	Words      []deepgramWord `json:"words"`
}

type deepgramWord struct {
	PunctuatedWord string  `json:"punctuated_word"`
	Start          float64 `json:"start"`
	End            float64 `json:"end"`
}

// mapDeepgramResponse converte o JSON bruto da resposta síncrona de
// POST /v1/listen do Deepgram para o domínio comum stt.Result. Mantida
// separada da chamada HTTP (deepgram.go) para ser testável com fixture,
// sem precisar de rede.
//
// Ao contrário da Gladia/AssemblyAI, não há campo de status a validar
// aqui: a resposta síncrona do Deepgram só existe quando a transcrição já
// terminou com sucesso — um erro de transcrição chega como HTTP não-2xx,
// tratado em deepgram.go antes desta função ser chamada.
func mapDeepgramResponse(raw []byte) (*Result, error) {
	var parsed deepgramResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("json inválido: %w", err)
	}

	utterances := make([]Utterance, 0, len(parsed.Results.Utterances))
	for _, u := range parsed.Results.Utterances {
		words := make([]Word, 0, len(u.Words))
		for _, w := range u.Words {
			words = append(words, Word{
				Text:  w.PunctuatedWord,
				Start: secondsToDuration(w.Start),
				End:   secondsToDuration(w.End),
			})
		}
		utterances = append(utterances, Utterance{
			Speaker: fmt.Sprintf("speaker_%d", u.Speaker),
			Text:    u.Transcript,
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
