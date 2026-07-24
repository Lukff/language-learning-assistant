package services

import (
	"database/sql"
	"fmt"

	"assistente-idiomas/internal/config"
	"assistente-idiomas/internal/importer"
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
