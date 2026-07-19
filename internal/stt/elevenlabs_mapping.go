package stt

import (
	"encoding/json"
	"fmt"
	"strings"
)

type elevenLabsResponse struct {
	Words []elevenLabsWord `json:"words"`
}

type elevenLabsWord struct {
	Text      string  `json:"text"`
	Type      string  `json:"type"`
	Start     float64 `json:"start"`
	End       float64 `json:"end"`
	SpeakerID string  `json:"speaker_id"`
}

// mapElevenLabsResponse converte o JSON bruto do endpoint POST
// /v1/speech-to-text do ElevenLabs Scribe para o domínio comum stt.Result.
// Mantida separada da chamada HTTP (elevenlabs.go) para ser testável com
// fixture, sem precisar de rede.
//
// Ao contrário da Gladia/AssemblyAI/Deepgram, a API não agrupa a resposta em
// utterances — devolve um array plano de words[], cada uma com type
// (word/spacing/audio_event) e speaker_id. O agrupamento em turnos de fala é
// feito aqui: uma nova Utterance começa sempre que o speaker_id muda.
//
// Também não há campo de status a validar: a resposta síncrona só existe
// quando a transcrição já terminou com sucesso — um erro chega como HTTP
// não-2xx, tratado em elevenlabs.go antes desta função ser chamada.
func mapElevenLabsResponse(raw []byte) (*Result, error) {
	var parsed elevenLabsResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("json inválido: %w", err)
	}

	return &Result{
		RawResponse: raw,
		Utterances:  groupElevenLabsWords(parsed.Words),
	}, nil
}

// groupElevenLabsWords agrupa entradas consecutivas do array plano de words
// pelo mesmo speaker_id em uma Utterance. O texto da utterance concatena o
// texto bruto de toda entrada do grupo (word, spacing e audio_event) na
// ordem original, preservando o espaçamento e mantendo marcadores de eventos
// não-verbais (ex.: "(laughs)") como contexto de leitura — decisão de
// produto, não filtrar. Só entradas type=="word" viram stt.Word: a lista de
// palavras clicáveis do domínio é só fala real, mesmo critério usado por
// Gladia/AssemblyAI/Deepgram. Não há segmentação por pausa de silêncio
// dentro do mesmo locutor — só a troca de speaker_id abre uma nova
// Utterance (decisão registrada na spec: evitar heurística extra no spike).
func groupElevenLabsWords(items []elevenLabsWord) []Utterance {
	utterances := make([]Utterance, 0)
	var current *Utterance
	var text strings.Builder

	flush := func() {
		if current == nil {
			return
		}
		current.Text = text.String()
		utterances = append(utterances, *current)
		text.Reset()
	}

	for _, w := range items {
		if current == nil || w.SpeakerID != current.Speaker {
			flush()
			current = &Utterance{Speaker: w.SpeakerID, Start: secondsToDuration(w.Start)}
		}
		text.WriteString(w.Text)
		current.End = secondsToDuration(w.End)
		if w.Type == "word" {
			current.Words = append(current.Words, Word{
				Text:  w.Text,
				Start: secondsToDuration(w.Start),
				End:   secondsToDuration(w.End),
			})
		}
	}
	flush()

	return utterances
}
