package stt

import (
	"context"
	"time"
)

// Provider é a interface única implementada por cada serviço de STT
// candidato (Gladia, AssemblyAI, Deepgram, ElevenLabs Scribe).
type Provider interface {
	Name() string
	Transcribe(ctx context.Context, audioPath string) (*Result, error)
}

// Result carrega tanto o JSON bruto do provedor (para salvar em disco sem
// perda) quanto a transcrição já mapeada para o domínio comum.
type Result struct {
	RawResponse []byte
	Utterances  []Utterance
}

// Utterance é um trecho de fala atribuído a um locutor. Speaker é o
// rótulo bruto do provedor (ex.: "speaker_0") — o mapeamento para
// aluno/tutor é um passo manual da História 2, fora desta fatia.
type Utterance struct {
	Speaker    string
	Text       string
	Start, End time.Duration
	Words      []Word
}

type Word struct {
	Text       string
	Start, End time.Duration
}
