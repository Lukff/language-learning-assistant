package importer

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCopyIntoStorageRoot_CopiesToEmptyDestination(t *testing.T) {
	srcDir := t.TempDir()
	storageRoot := t.TempDir()
	srcPath := filepath.Join(srcDir, "aula.mp4")
	if err := os.WriteFile(srcPath, []byte("conteudo-do-video"), 0o644); err != nil {
		t.Fatalf("preparar arquivo de origem falhou: %v", err)
	}

	got, err := CopyIntoStorageRoot(srcPath, storageRoot)
	if err != nil {
		t.Fatalf("CopyIntoStorageRoot() erro inesperado: %v", err)
	}
	if got != "aula.mp4" {
		t.Errorf("CopyIntoStorageRoot() = %q, esperado aula.mp4", got)
	}
	gotContent, err := os.ReadFile(filepath.Join(storageRoot, got))
	if err != nil {
		t.Fatalf("ler arquivo copiado falhou: %v", err)
	}
	if string(gotContent) != "conteudo-do-video" {
		t.Errorf("conteúdo copiado = %q, esperado conteudo-do-video", gotContent)
	}
	if _, err := os.ReadFile(srcPath); err != nil {
		t.Errorf("arquivo de origem deveria permanecer intacto: %v", err)
	}
	entries, err := os.ReadDir(storageRoot)
	if err != nil {
		t.Fatalf("ler diretório de destino falhou: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("destino tem %d entradas, esperado 1 (sem sobras de temporário)", len(entries))
	}
}

func TestCopyIntoStorageRoot_ResolvesNameCollisionWithSuffix(t *testing.T) {
	srcDir := t.TempDir()
	storageRoot := t.TempDir()
	srcPath := filepath.Join(srcDir, "aula.mp4")
	if err := os.WriteFile(srcPath, []byte("conteudo-novo"), 0o644); err != nil {
		t.Fatalf("preparar arquivo de origem falhou: %v", err)
	}
	if err := os.WriteFile(filepath.Join(storageRoot, "aula.mp4"), []byte("conteudo-existente"), 0o644); err != nil {
		t.Fatalf("preparar arquivo existente falhou: %v", err)
	}

	got, err := CopyIntoStorageRoot(srcPath, storageRoot)
	if err != nil {
		t.Fatalf("CopyIntoStorageRoot() erro inesperado: %v", err)
	}
	if got != "aula-2.mp4" {
		t.Errorf("CopyIntoStorageRoot() = %q, esperado aula-2.mp4", got)
	}
	existing, err := os.ReadFile(filepath.Join(storageRoot, "aula.mp4"))
	if err != nil || string(existing) != "conteudo-existente" {
		t.Errorf("arquivo existente não deveria ser sobrescrito: conteúdo=%q err=%v", existing, err)
	}
}

func TestCopyIntoStorageRoot_SourceDoesNotExist(t *testing.T) {
	storageRoot := t.TempDir()
	_, err := CopyIntoStorageRoot(filepath.Join(t.TempDir(), "nao-existe.mp4"), storageRoot)
	if err == nil {
		t.Fatal("CopyIntoStorageRoot() com origem inexistente esperava erro, veio nil")
	}
	entries, err := os.ReadDir(storageRoot)
	if err != nil {
		t.Fatalf("ler diretório de destino falhou: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("destino deveria continuar vazio após falha, tem %d entradas", len(entries))
	}
}

func TestCopyIntoStorageRoot_DestinationNotWritableLeavesNoPartialFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permissão de escrita via os.Chmod não se aplica da mesma forma no Windows")
	}
	srcDir := t.TempDir()
	storageRoot := t.TempDir()
	srcPath := filepath.Join(srcDir, "aula.mp4")
	if err := os.WriteFile(srcPath, []byte("conteudo"), 0o644); err != nil {
		t.Fatalf("preparar arquivo de origem falhou: %v", err)
	}
	if err := os.Chmod(storageRoot, 0o500); err != nil {
		t.Fatalf("Chmod() falhou: %v", err)
	}
	t.Cleanup(func() { os.Chmod(storageRoot, 0o700) })

	_, err := CopyIntoStorageRoot(srcPath, storageRoot)
	if err == nil {
		t.Fatal("CopyIntoStorageRoot() com destino somente leitura esperava erro, veio nil")
	}
}
