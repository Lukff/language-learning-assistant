package stt

import (
	"context"
	"time"
)

// Provider is the single interface implemented by each candidate STT
// service (Gladia, AssemblyAI, Deepgram, ElevenLabs Scribe).
type Provider interface {
	Name() string
	Transcribe(ctx context.Context, audioPath string) (*Result, error)
}

// Result carries both the provider's raw JSON (to save to disk without
// loss) and the transcript already mapped to the common domain.
type Result struct {
	RawResponse []byte
	Utterances  []Utterance
}

// Utterance is a speech segment attributed to a speaker. Speaker is the
// provider's raw label (e.g. "speaker_0") — mapping to
// student/tutor is a manual step in Story 2, outside this slice.
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
