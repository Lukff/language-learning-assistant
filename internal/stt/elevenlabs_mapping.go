package stt

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
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

// mapElevenLabsResponse converts the raw JSON from the ElevenLabs Scribe
// POST /v1/speech-to-text endpoint into the common stt.Result domain.
// Kept separate from the HTTP call (elevenlabs.go) so it's testable with a
// fixture, without needing the network.
//
// Unlike Gladia/AssemblyAI/Deepgram, the API doesn't group the response into
// utterances — it returns a flat array of words[], each with a type
// (word/spacing/audio_event) and speaker_id. Grouping into speech turns is
// done here: a new Utterance starts whenever speaker_id changes.
//
// There's also no status field to validate: the synchronous response only
// exists once transcription has already finished successfully — an error
// arrives as a non-2xx HTTP status, handled in elevenlabs.go before this function is called.
func mapElevenLabsResponse(raw []byte) (*Result, error) {
	var parsed elevenLabsResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("invalid json: %w", err)
	}

	return &Result{
		RawResponse: raw,
		Utterances:  groupElevenLabsWords(parsed.Words),
	}, nil
}

// groupElevenLabsWords groups consecutive entries of the flat words array
// with the same speaker_id into an Utterance. The utterance text concatenates the
// raw text of every entry in the group (word, spacing, and audio_event) in
// original order, preserving spacing and keeping non-verbal event
// markers (e.g. "(laughs)") as reading context — a product
// decision, not filtering them out. Only type=="word" entries become stt.Word: the domain's
// list of clickable words is real speech only, the same criterion used by
// Gladia/AssemblyAI/Deepgram. There's no segmentation by silence pause
// within the same speaker — only a speaker_id change opens a new
// Utterance (decision recorded in the spec: avoid extra heuristics in the spike).
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

func secondsToDuration(s float64) time.Duration {
	return time.Duration(math.Round(s*1000)) * time.Millisecond
}
