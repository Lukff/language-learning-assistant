package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// AppConfig is the machine-local configuration, persisted in config.json in
// AppDataDir. StorageRoot is an absolute path (machine-specific) to the
// synced folder where imported lessons are stored.
type AppConfig struct {
	StorageRoot string `json:"storage_root"`
}

// Load reads config.json from AppDataDir. If the file doesn't exist, the
// returned error satisfies errors.Is(err, os.ErrNotExist) — that's how
// callers detect "first run".
func Load() (*AppConfig, error) {
	path, err := configPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg AppConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &cfg, nil
}

// Save writes cfg to config.json in AppDataDir, overwriting whatever is there.
func Save(cfg *AppConfig) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}
