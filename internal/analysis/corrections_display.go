// internal/analysis/corrections_display.go
package analysis

import (
	"strings"
	"unicode"

	"assistente-idiomas/internal/stt"
)

// CorrectionDisplay é uma Correction já pronta para o frontend renderizar:
// o trecho errado (Wrong) já separado do resto da fala (Before/After) via
// matching normalizado contra o texto real da utterance correspondente.
// Original é sempre preenchido com o trecho cru devolvido pelo modelo,
// mesmo quando Wrong == "" (não localizado) — é o que o chamador usa pra
// montar a nota avulsa de fallback, já que Before/Wrong/After ficam vazios
// nesse caso.
type CorrectionDisplay struct {
	UtteranceIndex int
	Original       string
	Before         string
	Wrong          string
	After          string
	Correction     string
	Explanation    string
}

// MatchCorrections localiza, para cada Correction, o trecho Original dentro
// do texto real da utterance correspondente (utterances[c.UtteranceIdx]),
// normalizando (case-insensitive, espaços consecutivos colapsados em um só)
// quando o match exato falha. Usa a primeira ocorrência quando Original
// aparece mais de uma vez na fala. Quando não encontra (nem normalizado),
// Before/Wrong/After ficam vazios — o chamador mostra a correção como nota
// avulsa nesse caso, nunca a descarta. utterances e corrections já vieram
// com utterance_index validado (filterAnchored, História 1); um índice fora
// do range aqui seria bug de chamador (ou um resultado persistido antigo
// dessincronizado de uma transcrição diferente), e é simplesmente
// descartado silenciosamente — defesa extra, não um caminho esperado.
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

// splitByOriginal localiza original dentro de text e devolve os três
// pedaços (antes, o próprio trecho como aparece em text, depois). Tenta
// primeiro um match exato (mais rápido, preserva índices originais sem
// mapeamento); se falhar, normaliza (minúsculas + espaços consecutivos
// colapsados) e mapeia o índice encontrado de volta pro texto original via
// normalizeWithOffsets. Sem nenhum match, devolve três strings vazias — o
// chamador interpreta wrong == "" como "não localizado".
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

// normalizeWithOffsets minusculiza text e colapsa cada run de espaços em
// branco consecutivos num único espaço, devolvendo junto um slice offsets
// onde offsets[i] é o índice em bytes, no text original, do rune
// normalizado de posição i — offsets[len(runes normalizados)] == len(text),
// pra permitir localizar o fim de um match que termina no fim da string.
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
