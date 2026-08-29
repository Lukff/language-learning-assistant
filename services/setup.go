// Package services contains the services exposed to the frontend via Wails v3
// bindings — the shell that connects internal/config and internal/db to the UI. Unlike
// internal/, this package imports Wails on purpose.
package services

import (
	"fmt"

	"assistente-idiomas/internal/config"
)

// SetupService covers the first-run wizard: choosing the storage
// folder and registering the ElevenLabs API key.
type SetupService struct{}

func NewSetupService() *SetupService {
	return &SetupService{}
}

// IsFirstRun indicates whether the app doesn't yet have a config.json written (no
// complete configuration so far). Any error loading the config
// (missing file, corrupted, no permission) is treated as "not yet
// configured" — the worst case is the user redoing the wizard, not data
// loss: CompleteSetup only overwrites config.json and the credential.
func (s *SetupService) IsFirstRun() bool {
	_, err := config.Load()
	return err != nil
}

// ChooseStorageFolder opens the native folder-picker dialog and validates that
// it is writable. Returns an empty path (with no error) if the user cancels the
// dialog.
func (s *SetupService) ChooseStorageFolder() (string, error) {
	return chooseStorageFolder("Choose the folder where lessons will be stored")
}

// CompleteSetup saves the ElevenLabs credential (keyring) and, only if that
// succeeds, saves storageRoot to config.json. In that order: if the credential
// fails, config.json isn't touched and the app keeps detecting first-run.
func (s *SetupService) CompleteSetup(storageRoot string, apiKey string) error {
	if storageRoot == "" {
		return fmt.Errorf("storage folder cannot be empty")
	}
	if err := config.SaveSTTAPIKey(apiKey); err != nil {
		return fmt.Errorf("could not access the system credential manager (check that gnome-keyring/kwallet is running): %w", err)
	}
	if err := config.Save(&config.AppConfig{StorageRoot: storageRoot}); err != nil {
		return fmt.Errorf("write configuration: %w", err)
	}
	return nil
}
