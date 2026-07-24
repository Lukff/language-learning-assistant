package services

import (
	"errors"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

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
