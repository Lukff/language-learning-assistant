package services

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestIsDirWritable_WritableDirReturnsNil(t *testing.T) {
	if err := isDirWritable(t.TempDir()); err != nil {
		t.Errorf("isDirWritable() erro inesperado num dir gravável: %v", err)
	}
}

func TestIsDirWritable_ReadOnlyDirReturnsError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permissão de escrita via os.Chmod não se aplica da mesma forma no Windows")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("não foi possível preparar dir somente-leitura: %v", err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })

	if err := isDirWritable(dir); err == nil {
		t.Error("isDirWritable() esperava erro num dir somente-leitura, veio nil")
	}
}

func TestIsDirWritable_MissingDirReturnsError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nao-existe")
	if err := isDirWritable(missing); err == nil {
		t.Error("isDirWritable() esperava erro num dir inexistente, veio nil")
	}
}

func TestIsDirWritable_LeavesNoTempFileBehind(t *testing.T) {
	dir := t.TempDir()
	if err := isDirWritable(dir); err != nil {
		t.Fatalf("isDirWritable() erro inesperado: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir() erro inesperado: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("esperava dir vazio após isDirWritable, achou %d entradas", len(entries))
	}
}
