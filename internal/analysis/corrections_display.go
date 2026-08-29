// internal/analysis/corrections_display.go
package analysis

import (
	"strings"
	"unicode"

	"assistente-idiomas/internal/stt"
)

// CorrectionDisplay is a Correction already prepared for the frontend to render:
// the wrong excerpt (Wrong) already separated from the rest of the utterance (Before/After) via
// normalized matching against the actual text of the corresponding utterance.
// Original is always filled with the raw excerpt returned by the model,
// even when Wrong == "" (not found) — it's what the caller uses to
// build the standalone fallback note, since Before/Wrong/After are empty
// in that case.
type CorrectionDisplay struct {
	UtteranceIndex int
	Original       string
	Before         string
	Wrong          string
	After          string
	Correction     string
	Explanation    string
}

// MatchCorrections locates, for each Correction, the Original excerpt within
// the actual text of the corresponding utterance (utterances[c.UtteranceIdx]),
// normalizing (case-insensitive, consecutive spaces collapsed into one)
// when the exact match fails. Uses the first occurrence when Original
// appears more than once in the utterance. When not found (not even normalized),
// Before/Wrong/After stay empty — the caller shows the correction as a
// standalone note in that case, never discards it. utterances and corrections already come
// with utterance_index validated (filterAnchored, Story 1); an out-of-range
// index here would be a caller bug (or an old persisted result
// out of sync with a different transcript), and is simply
// silently discarded — extra defense, not an expected path.
func MatchCorrections(utterances []stt.Utterance, corrections []Correction) []CorrectionDisplay {
	out := make([]CorrectionDisplay, 0, len(corrections))
	for _, c := range corrections {
		if c.UtteranceIdx < 0 || c.UtteranceIdx >= len(utterances) {
			continue
		}
		text := utterances[c.UtteranceIdx].Text
		before, wrong, after := splitByOriginal(text, c.Original)
		out = append(out, CorrectionDisplay{
			UtteranceIndex: c.UtteranceIdx,
			Original:       c.Original,
			Before:         before,
			Wrong:          wrong,
			After:          after,
			Correction:     c.CorrectionTx,
			Explanation:    c.Explanation,
		})
	}
	return out
}

// splitByOriginal locates original within text and returns the three
// pieces (before, the excerpt itself as it appears in text, after). Tries
// an exact match first (faster, preserves original indices without
// mapping); if that fails, normalizes (lowercase + consecutive spaces
// collapsed) and maps the found index back to the original text via
// normalizeWithOffsets. With no match at all, returns three empty strings — the
// caller interprets wrong == "" as "not found".
func splitByOriginal(text, original string) (before, wrong, after string) {
	if original == "" {
		return "", "", ""
	}
	if idx := strings.Index(text, original); idx != -1 {
		return text[:idx], text[idx : idx+len(original)], text[idx+len(original):]
	}

	normText, offsets := normalizeWithOffsets(text)
	normOriginal, _ := normalizeWithOffsets(original)
	if normOriginal == "" {
		return "", "", ""
	}
	idx := strings.Index(normText, normOriginal)
	if idx == -1 {
		return "", "", ""
	}
	startRune := len([]rune(normText[:idx]))
	endRune := startRune + len([]rune(normOriginal))
	start := offsets[startRune]
	end := offsets[endRune]
	return text[:start], text[start:end], text[end:]
}

// normalizeWithOffsets lowercases text and collapses each run of consecutive
// whitespace into a single space, also returning an offsets slice
// where offsets[i] is the byte index, in the original text, of the
// normalized rune at position i — offsets[len(normalized runes)] == len(text),
// to allow locating the end of a match that ends at the end of the string.
func normalizeWithOffsets(text string) (string, []int) {
	runes := []rune(text)
	byteOffsets := make([]int, len(runes)+1)
	pos := 0
	for i, r := range runes {
		byteOffsets[i] = pos
		pos += len(string(r))
	}
	byteOffsets[len(runes)] = pos

	var b strings.Builder
	offsets := make([]int, 0, len(runes)+1)
	lastWasSpace := false
	for i, r := range runes {
		if unicode.IsSpace(r) {
			if lastWasSpace {
				continue
			}
			lastWasSpace = true
			b.WriteRune(' ')
			offsets = append(offsets, byteOffsets[i])
			continue
		}
		lastWasSpace = false
		b.WriteRune(unicode.ToLower(r))
		offsets = append(offsets, byteOffsets[i])
	}
	offsets = append(offsets, byteOffsets[len(runes)])
	return b.String(), offsets
}
