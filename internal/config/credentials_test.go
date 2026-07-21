package config

import (
	"errors"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestSaveThenGetSTTAPIKey_RoundTrips(t *testing.T) {
	keyring.MockInit()

	if err := SaveSTTAPIKey("sk-test-123"); err != nil {
		t.Fatalf("SaveSTTAPIKey() erro inesperado: %v", err)
	}

	got, err := GetSTTAPIKey()
	if err != nil {
		t.Fatalf("GetSTTAPIKey() erro inesperado: %v", err)
	}
	if got != "sk-test-123" {
		t.Errorf("GetSTTAPIKey() = %q, esperado \"sk-test-123\"", got)
	}
}

func TestSaveSTTAPIKey_KeyringUnavailablePropagatesError(t *testing.T) {
	sentinel := errors.New("secret service indisponível")
	keyring.MockInitWithError(sentinel)
	t.Cleanup(keyring.MockInit)

	err := SaveSTTAPIKey("sk-test-123")
	if !errors.Is(err, sentinel) {
		t.Errorf("SaveSTTAPIKey() erro = %v, esperado envolver %v", err, sentinel)
	}
}
