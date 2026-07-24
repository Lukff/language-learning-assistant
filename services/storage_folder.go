package services

import (
	"fmt"
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// chooseStorageFolder abre o dialog nativo de escolha de pasta com o título
// dado e valida que ela é gravável. Retorna path vazio (sem erro) se o
// usuário cancelar o dialog. Compartilhado por SetupService (wizard de
// first-run) e SettingsService (troca de pasta, História 8).
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

// isDirWritable confirma que dir aceita escrita, criando e removendo um
// arquivo temporário nele.
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
