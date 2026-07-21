// Package services contém os serviços expostos ao frontend via bindings do
// Wails v3 — a casca que liga internal/config e internal/db à UI. Diferente
// de internal/, este pacote importa Wails de propósito.
package services

import (
	"fmt"
	"os"

	"assistente-idiomas/internal/config"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// SetupService cobre o wizard de primeira execução: escolher a pasta de
// armazenamento e cadastrar a API key da ElevenLabs.
type SetupService struct{}

func NewSetupService() *SetupService {
	return &SetupService{}
}

// IsFirstRun indica se o app ainda não tem config.json gravado (nenhuma
// configuração completa até agora). Qualquer erro ao carregar a config
// (arquivo ausente, corrompido, sem permissão) é tratado como "ainda não
// configurado" — o pior caso é o usuário refazer o wizard, não perda de
// dados: CompleteSetup apenas sobrescreve config.json e a credencial.
func (s *SetupService) IsFirstRun() bool {
	_, err := config.Load()
	return err != nil
}

// ChooseStorageFolder abre o dialog nativo de escolha de pasta e valida que
// ela é gravável. Retorna path vazio (sem erro) se o usuário cancelar o
// dialog.
func (s *SetupService) ChooseStorageFolder() (string, error) {
	dir, err := application.Get().Dialog.OpenFile().
		SetTitle("Escolha a pasta onde as aulas ficarão guardadas").
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

// CompleteSetup grava a credencial da ElevenLabs (keyring) e, só se isso
// funcionar, grava storageRoot em config.json. Nessa ordem: se a credencial
// falhar, config.json não é tocado e o app continua detectando first-run.
func (s *SetupService) CompleteSetup(storageRoot string, apiKey string) error {
	if err := config.SaveSTTAPIKey(apiKey); err != nil {
		return err
	}
	if err := config.Save(&config.AppConfig{StorageRoot: storageRoot}); err != nil {
		return fmt.Errorf("gravar configuração: %w", err)
	}
	return nil
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
