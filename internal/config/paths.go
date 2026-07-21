// Package config resolve os paths de dados do app na máquina local, lê/grava
// a configuração (config.json) e guarda/lê a credencial do provedor de STT
// via keyring do SO. Não importa nada do Wails (camada fina).
package config

import (
	"fmt"
	"os"
	"path/filepath"
)

const appDirName = "assistente-idiomas"

// AppDataDir resolve (criando se necessário) o diretório de dados do app no
// diretório de configuração do SO: %AppData%\assistente-idiomas no Windows,
// ~/.config/assistente-idiomas no Linux (respeita XDG_CONFIG_HOME).
func AppDataDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolver diretório de configuração do SO: %w", err)
	}
	dir := filepath.Join(base, appDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("criar diretório de dados do app: %w", err)
	}
	return dir, nil
}

// DBPath resolve o caminho do arquivo do banco SQLite dentro do
// AppDataDir. O diretório pai é criado por db.Open, não aqui.
func DBPath() (string, error) {
	dir, err := AppDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "db", "app.db"), nil
}

func configPath() (string, error) {
	dir, err := AppDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}
