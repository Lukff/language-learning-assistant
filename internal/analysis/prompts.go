// internal/analysis/prompts.go
package analysis

import (
	"database/sql"
	"fmt"

	"assistente-idiomas/internal/db"
)

// RegisterPrompts grava (nome, versão, conteúdo) de cada TaskDef em Tasks
// na tabela prompts, se ainda não existir — idempotente entre reinícios do
// app. Chamado uma vez em main.go, logo após db.Open.
func RegisterPrompts(conn *sql.DB) error {
	for _, t := range Tasks {
		if _, err := db.UpsertPrompt(conn, t.Name(), t.Version(), t.Prompt()); err != nil {
			return fmt.Errorf("analysis: registrar prompt %s: %w", t.Name(), err)
		}
	}
	return nil
}
