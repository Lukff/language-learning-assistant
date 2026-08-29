// internal/analysis/transcript.go
package analysis

import (
	"fmt"
	"strings"

	"assistente-idiomas/internal/stt"
)

// FormatTranscript converts the diarized utterances into text readable by the
// prompt, labeling each utterance as "Student" or "Tutor" according to
// speakerRoles (accepted values: "student" or "tutor"). Errors if any
// Speaker isn't mapped or has a role other than these two —
// an explicit failure, with no silent guess that would contaminate the whole analysis.
func FormatTranscript(utterances []stt.Utterance, speakerRoles map[string]string) (string, error) {
	var b strings.Builder
	for i, u := range utterances {
		role, ok := speakerRoles[u.Speaker]
		if !ok {
			return "", fmt.Errorf("analysis: speaker %q has no role mapped in speakerRoles", u.Speaker)
		}

		var label string
		switch role {
		case "student":
			label = "Student"
		case "tutor":
			label = "Tutor"
		default:
			return "", fmt.Errorf("analysis: invalid role %q for speaker %q (expected \"student\" or \"tutor\")", role, u.Speaker)
		}

		fmt.Fprintf(&b, "[%d] %s: %s\n", i, label, u.Text)
	}
	return b.String(), nil
}

// SpeakerExamples returns up to n example utterances per speaker label, in the
// order they appear in utterances — input for a human to confirm who
// is the student and who is the tutor before building the speakerRoles used by
// FormatTranscript.
func SpeakerExamples(utterances []stt.Utterance, n int) map[string][]string {
	examples := make(map[string][]string)
	for _, u := range utterances {
		if len(examples[u.Speaker]) >= n {
			continue
		}
		examples[u.Speaker] = append(examples[u.Speaker], u.Text)
	}
	return examples
}
