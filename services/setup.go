// Package services contém os serviços expostos ao frontend via bindings do
// Wails v3 — a casca que liga internal/config e internal/db à UI. Diferente
// de internal/, este pacote importa Wails de propósito.
package services

import (
	"fmt"

	"assistente-idiomas/internal/config"
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
	return chooseStorageFolder("Escolha a pasta onde as aulas ficarão guardadas")
}

// CompleteSetup grava a credencial da ElevenLabs (keyring) e, só se isso
// funcionar, grava storageRoot em config.json. Nessa ordem: se a credencial
// falhar, config.json não é tocado e o app continua detectando first-run.
func (s *SetupService) CompleteSetup(storageRoot string, apiKey string) error {
	if storageRoot == "" {
		return fmt.Errorf("pasta de armazenamento não pode ser vazia")
	}
	if err := config.SaveSTTAPIKey(apiKey); err != nil {
		return fmt.Errorf("não foi possível acessar o gerenciador de credenciais do sistema (verifique se o gnome-keyring/kwallet está rodando): %w", err)
	}
	if err := config.Save(&config.AppConfig{StorageRoot: storageRoot}); err != nil {
		return fmt.Errorf("gravar configuração: %w", err)
	}
	return nil
}
