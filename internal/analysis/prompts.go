// internal/analysis/prompts.go
package analysis

import (
	"database/sql"
	"fmt"

	"assistente-idiomas/internal/db"
)

// RegisterPrompts writes (name, version, content) of each TaskDef in Tasks
// to the prompts table, if it doesn't already exist — idempotent across app
// restarts. Called once in main.go, right after db.Open.
func RegisterPrompts(conn *sql.DB) error {
	for _, t := range Tasks {
		if _, err := db.UpsertPrompt(conn, t.Name(), t.Version(), t.Prompt()); err != nil {
			return fmt.Errorf("analysis: register prompt %s: %w", t.Name(), err)
		}
	}
	return nil
}
