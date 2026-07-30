// internal/analysis/parsing.go
package analysis

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// stripTrailingCodeFence remove um fechamento de code fence (```) sobrando
// no final do conteúdo — a única sujeira possível quando o provedor usa
// prefill (ver openai_compatible.go); no-op inofensivo pra provedores sem
// prefill (JSON já vem puro).
func stripTrailingCodeFence(raw []byte) []byte {
	trimmed := bytes.TrimSpace(raw)
	trimmed = bytes.TrimSuffix(trimmed, []byte("```"))
	return bytes.TrimSpace(trimmed)
}

// unmarshalJSON desserializa raw (já sem envelope HTTP nem code fence — ver
// Provider.Complete) em v. Compartilhado pelas 7 tarefas; não sabe nada
// sobre o schema de nenhuma tarefa específica.
func unmarshalJSON(raw json.RawMessage, v any) error {
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("analysis: json inválido: %w", err)
	}
	return nil
}
