// internal/analysis/transcript.go
package analysis

import (
	"fmt"
	"strings"

	"assistente-idiomas/internal/stt"
)

// FormatTranscript converte as utterances diarizadas num texto legível pro
// prompt, rotulando cada fala como "Aluno" ou "Tutor" conforme
// speakerRoles (valores aceitos: "aluno" ou "tutor"). Erro se algum
// Speaker não estiver mapeado ou tiver um papel diferente desses dois —
// falha explícita, sem chute silencioso que contaminaria toda a análise.
func FormatTranscript(utterances []stt.Utterance, speakerRoles map[string]string) (string, error) {
	var b strings.Builder
	for _, u := range utterances {
		role, ok := speakerRoles[u.Speaker]
		if !ok {
			return "", fmt.Errorf("analysis: locutor %q sem papel mapeado em speakerRoles", u.Speaker)
		}

		var label string
		switch role {
		case "aluno":
			label = "Aluno"
		case "tutor":
			label = "Tutor"
		default:
			return "", fmt.Errorf("analysis: papel %q inválido para locutor %q (esperado \"aluno\" ou \"tutor\")", role, u.Speaker)
		}

		fmt.Fprintf(&b, "%s: %s\n", label, u.Text)
	}
	return b.String(), nil
}

// SpeakerExamples retorna até n falas de exemplo por rótulo de speaker, na
// ordem em que aparecem em utterances — insumo pra um humano confirmar quem
// é aluno e quem é tutor antes de montar o speakerRoles usado por
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
