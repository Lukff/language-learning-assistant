// copy.go — see importer.go for the package doc. Copies an external file
// (drag-and-drop, Story 3b) into the storage root. Knows nothing
// about Wails or the database — just file I/O (thin layer, same principle as the
// rest of internal/).
package importer

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// CopyIntoStorageRoot copies the contents of srcPath into
// storageRoot, without a subfolder, using srcPath's base name. It first copies
// to a temporary file inside storageRoot (same filesystem,
// so the final rename is atomic) and only then moves it to the final name —
// a copy interrupted midway never leaves a partial file under the
// final name. Resolves a name collision at the destination with a "-2", "-3",
// ... suffix before the extension. Returns the copied file's name (with no
// directory — never creates a subfolder).
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
