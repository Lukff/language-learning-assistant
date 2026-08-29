// internal/analysis/parsing.go
package analysis

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// stripTrailingCodeFence removes a leftover closing code fence (```)
// at the end of the content — the only possible mess when the provider uses
// prefill (see openai_compatible.go); harmless no-op for providers without
// prefill (JSON already comes clean).
func stripTrailingCodeFence(raw []byte) []byte {
	trimmed := bytes.TrimSpace(raw)
	trimmed = bytes.TrimSuffix(trimmed, []byte("```"))
	return bytes.TrimSpace(trimmed)
}

// unmarshalJSON deserializes raw (already without HTTP envelope or code fence — see
// Provider.Complete) into v. Shared by all 7 tasks; doesn't know anything
// about the schema of any specific task.
func unmarshalJSON(raw json.RawMessage, v any) error {
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("analysis: invalid json: %w", err)
	}
	return nil
}
