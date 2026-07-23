//go:build linux

package services

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMoveFileNoReplace_MovesWhenDestinationDoesNotExist(t *testing.T) {
	dir := t.TempDir()
	originalPath := filepath.Join(dir, "original.mp4")
	targetPath := filepath.Join(dir, "target.mp4")
	if err := os.WriteFile(originalPath, []byte("video original"), 0o644); err != nil {
		t.Fatalf("preparar arquivo original falhou: %v", err)
	}

	if err := moveFileNoReplace(originalPath, targetPath); err != nil {
		t.Fatalf("moveFileNoReplace() falhou: %v", err)
	}
	if _, err := os.Stat(originalPath); !os.IsNotExist(err) {
		t.Errorf("origem deveria deixar de existir, err=%v", err)
	}
	if got, err := os.ReadFile(targetPath); err != nil || string(got) != "video original" {
		t.Errorf("destino deveria conter o arquivo original: conteúdo=%q err=%v", got, err)
	}
}

func TestMoveFileNoReplace_DoesNotOverwriteExistingDestination(t *testing.T) {
	dir := t.TempDir()
	originalPath := filepath.Join(dir, "original.mp4")
	targetPath := filepath.Join(dir, "target.mp4")
	if err := os.WriteFile(originalPath, []byte("video original"), 0o644); err != nil {
		t.Fatalf("preparar arquivo original falhou: %v", err)
	}
	if err := os.WriteFile(targetPath, []byte("arquivo intruso"), 0o644); err != nil {
		t.Fatalf("preparar destino ocupado falhou: %v", err)
	}

	if err := moveFileNoReplace(originalPath, targetPath); err == nil {
		t.Fatal("moveFileNoReplace() deveria falhar com destino existente")
	}
	if got, err := os.ReadFile(originalPath); err != nil || string(got) != "video original" {
		t.Errorf("origem deveria permanecer intacta: conteúdo=%q err=%v", got, err)
	}
	if got, err := os.ReadFile(targetPath); err != nil || string(got) != "arquivo intruso" {
		t.Errorf("destino não deveria ser sobrescrito: conteúdo=%q err=%v", got, err)
	}
}
