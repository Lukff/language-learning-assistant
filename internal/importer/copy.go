// copy.go — ver importer.go pro doc do package. Cópia de um arquivo externo
// (drag-and-drop, História 3b) pra dentro da raiz de armazenamento. Não sabe
// de Wails nem de banco — só I/O de arquivo (camada fina, mesmo princípio do
// resto do internal/).
package importer

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// CopyIntoStorageRoot copia o conteúdo de srcPath pra dentro de
// storageRoot, sem subpasta, usando o nome-base de srcPath. Copia primeiro
// pra um arquivo temporário dentro de storageRoot (mesmo filesystem,
// então o rename final é atômico) e só depois move pro nome definitivo —
// uma cópia interrompida no meio nunca deixa um arquivo parcial com o
// nome final. Resolve colisão de nome no destino com sufixo "-2", "-3",
// ... antes da extensão. Retorna o nome do arquivo copiado (sem
// diretório — nunca cria subpasta).
func CopyIntoStorageRoot(srcPath, storageRoot string) (string, error) {
	src, err := os.Open(srcPath)
	if err != nil {
		return "", fmt.Errorf("abrir arquivo de origem: %w", err)
	}
	defer src.Close()

	tmp, err := os.CreateTemp(storageRoot, "importing-*.tmp")
	if err != nil {
		return "", fmt.Errorf("criar arquivo temporário de cópia: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := io.Copy(tmp, src); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return "", fmt.Errorf("copiar conteúdo do vídeo: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("finalizar cópia do vídeo: %w", err)
	}

	base := filepath.Base(srcPath)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)

	candidate := base
	for i := 2; ; i++ {
		destPath := filepath.Join(storageRoot, candidate)
		if _, statErr := os.Stat(destPath); os.IsNotExist(statErr) {
			if err := os.Rename(tmpPath, destPath); err != nil {
				os.Remove(tmpPath)
				return "", fmt.Errorf("mover cópia pro nome final: %w", err)
			}
			return candidate, nil
		} else if statErr != nil {
			os.Remove(tmpPath)
			return "", fmt.Errorf("checar colisão de nome no destino: %w", statErr)
		}
		candidate = fmt.Sprintf("%s-%d%s", stem, i, ext)
	}
}
