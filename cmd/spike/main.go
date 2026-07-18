// cmd/spike/main.go
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"assistente-idiomas/internal/media"
	"assistente-idiomas/internal/stt"
)

var providerFactories = map[string]func() (stt.Provider, error){
	"gladia": func() (stt.Provider, error) {
		return stt.NewGladiaProvider(os.Getenv("GLADIA_API_KEY"))
	},
	"assemblyai": func() (stt.Provider, error) {
		return stt.NewAssemblyAIProvider(os.Getenv("ASSEMBLYAI_API_KEY"))
	},
}

func main() {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	if err := loadDotEnv(".env"); err != nil {
		logger.Error("carregar .env", "erro", err)
		os.Exit(1)
	}

	providersFlag := flag.String("providers", "", "lista separada por vírgula dos provedores a rodar (ex.: gladia,assemblyai)")
	flag.Parse()

	if *providersFlag == "" {
		logger.Error("flag -providers é obrigatória", "exemplo", "-providers=gladia,assemblyai")
		os.Exit(1)
	}
	names := strings.Split(*providersFlag, ",")
	for _, name := range names {
		if _, ok := providerFactories[name]; !ok {
			logger.Error("provedor desconhecido", "nome", name)
			os.Exit(1)
		}
	}

	const (
		videoPath = "local/input/aula-01.mp4"
		audioPath = "local/output/aula-01/audio.wav"
		outDir    = "local/output/aula-01"
	)

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		logger.Error("criar diretório de saída", "erro", err)
		os.Exit(1)
	}

	logger.Info("extraindo áudio", "video", videoPath)
	start := time.Now()
	if err := media.ExtractAudio(ctx, videoPath, audioPath); err != nil {
		logger.Error("extração de áudio falhou", "erro", err)
		os.Exit(1)
	}
	logger.Info("áudio extraído", "duração", time.Since(start))

	hadFailure := false
	for _, name := range names {
		if !runProvider(ctx, logger, providerFactories[name], name, audioPath, outDir) {
			hadFailure = true
		}
	}

	if hadFailure {
		os.Exit(1)
	}
}

// runProvider roda um único provedor de STT e salva suas saídas. Retorna
// false se o provedor falhou (erro já logado) — o chamador decide se isso
// deve interromper o processo ou apenas seguir para o próximo provedor.
func runProvider(ctx context.Context, logger *slog.Logger, factory func() (stt.Provider, error), name, audioPath, outDir string) bool {
	provider, err := factory()
	if err != nil {
		logger.Error("criar provider", "provedor", name, "erro", err)
		return false
	}

	providerOutDir := filepath.Join(outDir, provider.Name())
	if err := os.MkdirAll(providerOutDir, 0o755); err != nil {
		logger.Error("criar diretório de saída do provedor", "provedor", name, "erro", err)
		return false
	}

	logger.Info("transcrevendo", "provedor", provider.Name(), "audio", audioPath)
	start := time.Now()
	result, err := provider.Transcribe(ctx, audioPath)
	if err != nil {
		logger.Error("transcrição falhou", "provedor", provider.Name(), "erro", err)
		if result != nil && len(result.RawResponse) > 0 {
			if saveErr := saveRawResponse(providerOutDir, result); saveErr != nil {
				logger.Error("salvar JSON bruto após falha", "provedor", provider.Name(), "erro", saveErr)
			} else {
				logger.Info("JSON bruto salvo apesar da falha de transcrição", "provedor", provider.Name(), "path", filepath.Join(providerOutDir, "raw.json"))
			}
		}
		return false
	}
	logger.Info("transcrição concluída", "provedor", provider.Name(), "duração", time.Since(start), "utterances", len(result.Utterances))

	if err := saveRawResponse(providerOutDir, result); err != nil {
		logger.Error("salvar JSON bruto", "provedor", provider.Name(), "erro", err)
		return false
	}
	logger.Info("JSON bruto salvo", "provedor", provider.Name(), "path", filepath.Join(providerOutDir, "raw.json"))

	txtPath := filepath.Join(providerOutDir, "transcript.txt")
	if err := writeReadableTranscript(txtPath, result); err != nil {
		logger.Error("salvar transcrição legível", "provedor", provider.Name(), "erro", err)
		return false
	}
	logger.Info("transcrição legível salva", "provedor", provider.Name(), "path", txtPath)
	return true
}

// loadDotEnv lê pares CHAVE=VALOR de path e os define como variáveis de
// ambiente, sem sobrescrever variáveis já definidas no processo. Arquivo
// ausente não é erro (uso do .env é opcional — export manual continua
// funcionando).
func loadDotEnv(path string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if _, exists := os.LookupEnv(key); !exists {
			os.Setenv(key, value)
		}
	}
	return nil
}

// saveRawResponse grava o JSON bruto do provedor em outDir/raw.json.
func saveRawResponse(outDir string, result *stt.Result) error {
	rawPath := filepath.Join(outDir, "raw.json")
	return os.WriteFile(rawPath, result.RawResponse, 0o644)
}

func writeReadableTranscript(path string, result *stt.Result) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	for _, u := range result.Utterances {
		line := fmt.Sprintf("[%s - %s] %s: %s\n", formatTimestamp(u.Start), formatTimestamp(u.End), u.Speaker, u.Text)
		if _, err := file.WriteString(line); err != nil {
			return err
		}
	}
	return nil
}

func formatTimestamp(d time.Duration) string {
	minutes := int(d.Minutes())
	seconds := d.Seconds() - float64(minutes)*60
	return fmt.Sprintf("%02d:%04.1f", minutes, seconds)
}
