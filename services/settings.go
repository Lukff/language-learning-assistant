package services

import (
	"database/sql"
	"errors"
	"fmt"

	"assistente-idiomas/internal/config"
	"assistente-idiomas/internal/importer"

	"github.com/zalando/go-keyring"
)

// SettingsService cobre a tela de Configurações (História 8): ver/trocar a
// raiz de armazenamento e (re)cadastrar a credencial do provedor STT.
type SettingsService struct {
	conn        *sql.DB
	storageRoot func() (string, error)
}

func NewSettingsService(conn *sql.DB, storageRoot func() (string, error)) *SettingsService {
	return &SettingsService{conn: conn, storageRoot: storageRoot}
}

// GetStorageRoot retorna a storage_root configurada atualmente.
func (s *SettingsService) GetStorageRoot() (string, error) {
	return s.storageRoot()
}

// ChooseStorageFolder abre o dialog nativo (mesma validação de escrita do
// wizard) e retorna o path escolhido, sem gravar nada ainda. Retorna path
// vazio (sem erro) se o usuário cancelar o dialog.
func (s *SettingsService) ChooseStorageFolder() (string, error) {
	return chooseStorageFolder("Escolha a nova pasta — os arquivos já devem estar lá dentro")
}

// ChangeStorageFolder grava newRoot em config.json (sempre, mesmo que a
// varredura a seguir encontre problemas) e roda a mesma reconciliação por
// hash da História 3 (internal/importer.Scan): vídeos com nome diferente na
// pasta nova têm o video_path atualizado por hash; vídeos novos na pasta
// nova viram candidatos pendentes; a troca em si nunca é bloqueada por
// vídeos que não forem encontrados (esses continuam com o path antigo e
// ficam "ausentes" — ver LibraryService.videoMissing, Task 4). ScanSummary
// é o tipo já definido em import.go, reaproveitado aqui sem duplicar o
// formato de resumo.
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

// HasSTTCredential indica se há uma credencial gravada no keyring, sem
// revelar o valor. false (sem erro) se simplesmente não configurada ainda;
// erro só em falha real de acesso ao keyring (Secret Service indisponível,
// risco 3 do projeto).
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

// SaveSTTAPIKey grava/sobrescreve a credencial do provedor STT.
func (s *SettingsService) SaveSTTAPIKey(apiKey string) error {
	return config.SaveSTTAPIKey(apiKey)
}
