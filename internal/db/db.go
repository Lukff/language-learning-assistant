// Package db abre o banco SQLite local do app e aplica as migrations goose
// embutidas no binário. Não importa nada do Wails (camada fina).
package db

import (
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Open abre (criando se necessário) o banco SQLite em path, ativa WAL e
// aplica as migrations pendentes. O diretório pai de path é criado se não
// existir.
func Open(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("criar diretório do banco: %w", err)
	}

	conn, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("abrir banco: %w", err)
	}

	if _, err := conn.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("ativar WAL: %w", err)
	}

	goose.SetBaseFS(migrationsFS)
	defer goose.SetBaseFS(nil)
	if err := goose.SetDialect("sqlite"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("configurar dialeto goose: %w", err)
	}
	if err := goose.Up(conn, "migrations"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("rodar migrations: %w", err)
	}

	return conn, nil
}
