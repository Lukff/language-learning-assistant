// Package config resolves the app's data paths on the local machine, reads/writes
// the configuration (config.json), and stores/reads the STT provider's
// credential via the OS keyring. Imports nothing from Wails (thin layer).
package config

import (
	"fmt"
	"os"
	"path/filepath"
)

const appDirName = "assistente-idiomas"

// AppDataDir resolves (creating it if needed) the app's data directory in the
// OS's configuration directory: %AppData%\assistente-idiomas on Windows,
// ~/.config/assistente-idiomas on Linux (respects XDG_CONFIG_HOME).
func AppDataDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve OS config directory: %w", err)
	}
	dir := filepath.Join(base, appDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create app data directory: %w", err)
	}
	return dir, nil
}

// DBPath resolves the path to the SQLite database file inside
// AppDataDir. The parent directory is created by db.Open, not here.
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

// AudioCacheDir resolves (creating it if needed) the directory for intermediate
// audio cache (WAVs extracted to call the STT API) inside
// AppDataDir — outside the synced folder, since these files are
// disposable as soon as the transcript is saved (see internal/jobs).
func AudioCacheDir() (string, error) {
	dir, err := AppDataDir()
	if err != nil {
		return "", err
	}
	cacheDir := filepath.Join(dir, "audio-cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", fmt.Errorf("create audio cache directory: %w", err)
	}
	return cacheDir, nil
}
