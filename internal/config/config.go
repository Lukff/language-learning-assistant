package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// AppConfig é a configuração local da máquina, persistida em config.json no
// AppDataDir. StorageRoot é um path absoluto (específico da máquina) para a
// pasta sincronizada onde as aulas importadas são guardadas.
type AppConfig struct {
	StorageRoot string `json:"storage_root"`
}

// Load lê a config.json do AppDataDir. Se o arquivo não existir, o erro
// retornado satisfaz errors.Is(err, os.ErrNotExist) — é assim que os
// chamadores detectam "primeira execução".
func Load() (*AppConfig, error) {
	path, err := configPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("ler config: %w", err)
	}
	var cfg AppConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsear config: %w", err)
	}
	return &cfg, nil
}

// Save grava cfg em config.json no AppDataDir, sobrescrevendo o que houver.
func Save(cfg *AppConfig) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("serializar config: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("gravar config: %w", err)
	}
	return nil
}
