package config

import (
	"fmt"

	"github.com/zalando/go-keyring"
)

const (
	keyringService        = "assistente-idiomas"
	keyringUserElevenLabs = "elevenlabs"
	keyringUserDeepSeek   = "deepseek"
)

// SaveSTTAPIKey grava a API key da ElevenLabs no gerenciador de credenciais
// nativo do SO, via go-keyring. Nunca em texto plano. Se o Secret Service
// (Linux) ou equivalente não estiver disponível, retorna erro — sem
// fallback para variável de ambiente ou arquivo.
func SaveSTTAPIKey(apiKey string) error {
	if err := keyring.Set(keyringService, keyringUserElevenLabs, apiKey); err != nil {
		return fmt.Errorf("gravar credencial no gerenciador do sistema: %w", err)
	}
	return nil
}

// GetSTTAPIKey lê a API key da ElevenLabs previamente salva via
// SaveSTTAPIKey.
func GetSTTAPIKey() (string, error) {
	apiKey, err := keyring.Get(keyringService, keyringUserElevenLabs)
	if err != nil {
		return "", fmt.Errorf("ler credencial do gerenciador do sistema: %w", err)
	}
	return apiKey, nil
}

// SaveAnalysisAPIKey grava a API key da DeepSeek no gerenciador de
// credenciais nativo do SO, via go-keyring. Nunca em texto plano.
func SaveAnalysisAPIKey(apiKey string) error {
	if err := keyring.Set(keyringService, keyringUserDeepSeek, apiKey); err != nil {
		return fmt.Errorf("gravar credencial no gerenciador do sistema: %w", err)
	}
	return nil
}

// GetAnalysisAPIKey lê a API key da DeepSeek previamente salva via
// SaveAnalysisAPIKey.
func GetAnalysisAPIKey() (string, error) {
	apiKey, err := keyring.Get(keyringService, keyringUserDeepSeek)
	if err != nil {
		return "", fmt.Errorf("ler credencial do gerenciador do sistema: %w", err)
	}
	return apiKey, nil
}
