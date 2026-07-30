// internal/analysis/prompts_test.go
package analysis

import (
	"path/filepath"
	"testing"

	"assistente-idiomas/internal/db"
)

func TestRegisterPrompts_InsertsAllTasksAndIsIdempotent(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	if err := RegisterPrompts(conn); err != nil {
		t.Fatalf("RegisterPrompts() erro inesperado: %v", err)
	}
	if err := RegisterPrompts(conn); err != nil {
		t.Fatalf("segunda RegisterPrompts() erro inesperado: %v", err)
	}

	var count int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM prompts`).Scan(&count); err != nil {
		t.Fatalf("contar prompts falhou: %v", err)
	}
	if count != len(Tasks) {
		t.Errorf("count = %d, esperado %d (um por tarefa, sem duplicar na segunda chamada)", count, len(Tasks))
	}

	for _, tk := range Tasks {
		var version int
		err := conn.QueryRow(`SELECT version FROM prompts WHERE name = ?`, tk.Name()).Scan(&version)
		if err != nil {
			t.Errorf("prompt %q não encontrado: %v", tk.Name(), err)
			continue
		}
		if version != tk.Version() {
			t.Errorf("prompt %q: version = %d, esperado %d", tk.Name(), version, tk.Version())
		}
	}
}
