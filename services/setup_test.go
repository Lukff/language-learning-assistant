package services

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestIsDirWritable_WritableDirReturnsNil(t *testing.T) {
	if err := isDirWritable(t.TempDir()); err != nil {
		t.Errorf("isDirWritable() erro inesperado num dir gravável: %v", err)
	}
}

func TestIsDirWritable_ReadOnlyDirReturnsError(t *testing.T) {
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

func TestCompleteSetup_KeyringUnavailableReturnsActionableError(t *testing.T) {
	sentinel := errors.New("secret service indisponível")
	keyring.MockInitWithError(sentinel)
	t.Cleanup(keyring.MockInit)

	svc := NewSetupService()
	err := svc.CompleteSetup("/some/path", "sk-test")
	if err == nil {
		t.Fatal("esperava erro, veio nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("erro não envolve o erro original do keyring: %v", err)
	}
	if !strings.Contains(err.Error(), "gnome-keyring") {
		t.Errorf("erro não menciona gnome-keyring/kwallet: %v", err)
	}
}

func TestCompleteSetup_EmptyStorageRootRejected(t *testing.T) {
	svc := NewSetupService()
	err := svc.CompleteSetup("", "sk-test")
	if err == nil {
		t.Fatal("esperava erro para storageRoot vazio, veio nil")
	}
}
