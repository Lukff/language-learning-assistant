package services

import (
	"database/sql"
	"errors"
	"fmt"

	"assistente-idiomas/internal/config"
	"assistente-idiomas/internal/importer"

	"github.com/zalando/go-keyring"
)

// SettingsService covers the Settings screen (Story 8): viewing/changing the
// storage root and (re)registering the STT provider credential.
type SettingsService struct {
	conn        *sql.DB
	storageRoot func() (string, error)
}

func NewSettingsService(conn *sql.DB, storageRoot func() (string, error)) *SettingsService {
	return &SettingsService{conn: conn, storageRoot: storageRoot}
}

// GetStorageRoot returns the currently configured storage_root.
func (s *SettingsService) GetStorageRoot() (string, error) {
	return s.storageRoot()
}

// ChooseStorageFolder opens the native dialog (same write validation as the
// wizard) and returns the chosen path, without writing anything yet. Returns an empty
// path (with no error) if the user cancels the dialog.
func (s *SettingsService) ChooseStorageFolder() (string, error) {
	return chooseStorageFolder("Escolha a nova pasta — os arquivos já devem estar lá dentro")
}

// ChangeStorageFolder writes newRoot to config.json (always, even if the
// scan that follows finds problems) and runs the same hash-based reconciliation
// from Story 3 (internal/importer.Scan): videos with a different name in the
// new folder have their video_path updated by hash; new videos in the
// new folder become pending candidates; the switch itself is never blocked by
// videos that aren't found (those keep the old path and
// stay "missing" — see LibraryService.videoMissing, Task 4). ScanSummary
// is the type already defined in import.go, reused here without duplicating the
// summary format.
func (s *SettingsService) ChangeStorageFolder(newRoot string) (ScanSummary, error) {
	if newRoot == "" {
		return ScanSummary{}, fmt.Errorf("pasta de armazenamento não pode ser vazia")
	}
	if err := isDirWritable(newRoot); err != nil {
		return ScanSummary{}, err
	}
	if err := config.Save(&config.AppConfig{StorageRoot: newRoot}); err != nil {
		return ScanSummary{}, fmt.Errorf("gravar configuração: %w", err)
	}
	sum, err := importer.Scan(newRoot, &dbRepo{conn: s.conn})
	if err != nil {
		return ScanSummary{}, err
	}
	return ScanSummary{New: sum.New, Updated: sum.Updated, Skipped: sum.Skipped, Errors: sum.Errors}, nil
}

// HasSTTCredential indicates whether a credential is stored in the keyring, without
// revealing the value. false (with no error) if simply not configured yet;
// error only on an actual keyring access failure (Secret Service unavailable,
// project risk 3).
func (s *SettingsService) HasSTTCredential() (bool, error) {
	_, err := config.GetSTTAPIKey()
	if err == nil {
		return true, nil
	}
	if errors.Is(err, keyring.ErrNotFound) {
		return false, nil
	}
	return false, fmt.Errorf("não foi possível acessar o gerenciador de credenciais do sistema (verifique se o gnome-keyring/kwallet está rodando): %w", err)
}

// SaveSTTAPIKey saves/overwrites the STT provider credential.
func (s *SettingsService) SaveSTTAPIKey(apiKey string) error {
	return config.SaveSTTAPIKey(apiKey)
}

// HasAnalysisCredential indicates whether an analysis provider credential
// (DeepSeek) is stored in the keyring, without revealing the value. Same behavior
// as HasSTTCredential: false (with no error) if not configured yet; error only
// on an actual keyring access failure.
func (s *SettingsService) HasAnalysisCredential() (bool, error) {
	_, err := config.GetAnalysisAPIKey()
	if err == nil {
		return true, nil
	}
	if errors.Is(err, keyring.ErrNotFound) {
		return false, nil
	}
	return false, fmt.Errorf("não foi possível acessar o gerenciador de credenciais do sistema (verifique se o gnome-keyring/kwallet está rodando): %w", err)
}

// SaveAnalysisAPIKey saves/overwrites the analysis provider credential.
func (s *SettingsService) SaveAnalysisAPIKey(apiKey string) error {
	return config.SaveAnalysisAPIKey(apiKey)
}
