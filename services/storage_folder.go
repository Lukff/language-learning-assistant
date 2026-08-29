package services

import (
	"fmt"
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// chooseStorageFolder opens the native folder-picker dialog with the given
// title and validates that it is writable. Returns an empty path (with no error) if the
// user cancels the dialog. Shared by SetupService (first-run
// wizard) and SettingsService (folder change, Story 8).
func chooseStorageFolder(title string) (string, error) {
	dir, err := application.Get().Dialog.OpenFile().
		SetTitle(title).
		CanChooseFiles(false).
		CanChooseDirectories(true).
		CanCreateDirectories(true).
		PromptForSingleSelection()
	if err != nil {
		return "", fmt.Errorf("abrir diálogo de pasta: %w", err)
	}
	if dir == "" {
		return "", nil
	}
	if err := isDirWritable(dir); err != nil {
		return "", err
	}
	return dir, nil
}

// isDirWritable confirms that dir accepts writes, by creating and removing a
// temporary file in it.
func isDirWritable(dir string) error {
	f, err := os.CreateTemp(dir, ".assistente-idiomas-write-test-*")
	if err != nil {
		return fmt.Errorf("pasta sem permissão de escrita: %w", err)
	}
	name := f.Name()
	f.Close()
	if err := os.Remove(name); err != nil {
		return fmt.Errorf("não foi possível limpar arquivo de teste na pasta: %w", err)
	}
	return nil
}
