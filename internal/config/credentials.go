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

// SaveSTTAPIKey writes the ElevenLabs API key to the OS's native
// credential manager, via go-keyring. Never in plain text. If the Secret Service
// (Linux) or equivalent isn't available, returns an error — with no
// fallback to an environment variable or file.
func SaveSTTAPIKey(apiKey string) error {
	if err := keyring.Set(keyringService, keyringUserElevenLabs, apiKey); err != nil {
		return fmt.Errorf("gravar credencial no gerenciador do sistema: %w", err)
	}
	return nil
}

// GetSTTAPIKey reads the ElevenLabs API key previously saved via
// SaveSTTAPIKey.
func GetSTTAPIKey() (string, error) {
	apiKey, err := keyring.Get(keyringService, keyringUserElevenLabs)
	if err != nil {
		return "", fmt.Errorf("ler credencial do gerenciador do sistema: %w", err)
	}
	return apiKey, nil
}

// SaveAnalysisAPIKey writes the DeepSeek API key to the OS's native
// credential manager, via go-keyring. Never in plain text.
func SaveAnalysisAPIKey(apiKey string) error {
	if err := keyring.Set(keyringService, keyringUserDeepSeek, apiKey); err != nil {
		return fmt.Errorf("gravar credencial no gerenciador do sistema: %w", err)
	}
	return nil
}

// GetAnalysisAPIKey reads the DeepSeek API key previously saved via
// SaveAnalysisAPIKey.
func GetAnalysisAPIKey() (string, error) {
	apiKey, err := keyring.Get(keyringService, keyringUserDeepSeek)
	if err != nil {
		return "", fmt.Errorf("ler credencial do gerenciador do sistema: %w", err)
	}
	return apiKey, nil
}
