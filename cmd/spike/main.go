// cmd/spike/main.go
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"assistente-idiomas/internal/media"
	"assistente-idiomas/internal/stt"
)

func main() {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

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

	provider, err := stt.NewGladiaProvider(os.Getenv("GLADIA_API_KEY"))
	if err != nil {
		logger.Error("criar provider gladia", "erro", err)
		os.Exit(1)
	}

	logger.Info("transcrevendo via gladia", "audio", audioPath)
	start = time.Now()
	result, err := provider.Transcribe(ctx, audioPath)
	if err != nil {
		logger.Error("transcrição gladia falhou", "erro", err)
		os.Exit(1)
	}
	logger.Info("transcrição concluída", "duração", time.Since(start), "utterances", len(result.Utterances))

	rawPath := filepath.Join(outDir, "gladia.json")
	if err := os.WriteFile(rawPath, result.RawResponse, 0o644); err != nil {
		logger.Error("salvar JSON bruto", "erro", err)
		os.Exit(1)
	}
	logger.Info("JSON bruto salvo", "path", rawPath)

	txtPath := filepath.Join(outDir, "gladia.txt")
	if err := writeReadableTranscript(txtPath, result); err != nil {
		logger.Error("salvar transcrição legível", "erro", err)
		os.Exit(1)
	}
	logger.Info("transcrição legível salva", "path", txtPath)
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
