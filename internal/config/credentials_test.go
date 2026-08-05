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

func TestSaveThenGetAnalysisAPIKey_RoundTrips(t *testing.T) {
	keyring.MockInit()

	if err := SaveAnalysisAPIKey("sk-deepseek-test"); err != nil {
		t.Fatalf("SaveAnalysisAPIKey() erro inesperado: %v", err)
	}

	got, err := GetAnalysisAPIKey()
	if err != nil {
		t.Fatalf("GetAnalysisAPIKey() erro inesperado: %v", err)
	}
	if got != "sk-deepseek-test" {
		t.Errorf("GetAnalysisAPIKey() = %q, esperado \"sk-deepseek-test\"", got)
	}
}

func TestSaveAnalysisAPIKey_KeyringUnavailablePropagatesError(t *testing.T) {
	sentinel := errors.New("secret service indisponível")
	keyring.MockInitWithError(sentinel)
	t.Cleanup(keyring.MockInit)

	err := SaveAnalysisAPIKey("sk-deepseek-test")
	if !errors.Is(err, sentinel) {
		t.Errorf("SaveAnalysisAPIKey() erro = %v, esperado envolver %v", err, sentinel)
	}
}

func TestSaveSTTAndAnalysisAPIKeys_AreIndependent(t *testing.T) {
	keyring.MockInit()

	if err := SaveSTTAPIKey("sk-stt"); err != nil {
		t.Fatalf("SaveSTTAPIKey() erro inesperado: %v", err)
	}
	if err := SaveAnalysisAPIKey("sk-analysis"); err != nil {
		t.Fatalf("SaveAnalysisAPIKey() erro inesperado: %v", err)
	}

	stt, err := GetSTTAPIKey()
	if err != nil {
		t.Fatalf("GetSTTAPIKey() erro inesperado: %v", err)
	}
	if stt != "sk-stt" {
		t.Errorf("GetSTTAPIKey() = %q, esperado \"sk-stt\" (não deve ser sobrescrita pela credencial de análise)", stt)
	}
}
