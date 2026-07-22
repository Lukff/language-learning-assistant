package media

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ExtractAudio extrai a trilha de áudio de videoPath via ffmpeg, gravando
// um WAV mono 16kHz em outputPath — formato universalmente aceito pelas
// APIs de STT candidatas, evitando ambiguidade de codec.
func ExtractAudio(ctx context.Context, videoPath, outputPath string) error {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return fmt.Errorf("media: ffmpeg não encontrado no PATH: %w", err)
	}

	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-y",
		"-i", videoPath,
		"-vn",
		"-ac", "1",
		"-ar", "16000",
		"-f", "wav",
		outputPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("media: ffmpeg falhou: %w\n%s", err, output)
	}
	return nil
}

// Duration lê a duração do vídeo via ffprobe (companion do ffmpeg, mesma
// dependência externa já assumida por ExtractAudio) — usado pra gravar
// lessons.duration_seconds na confirmação da importação (História 5). É
// metadado intrínseco do vídeo, não produto do pipeline de transcrição:
// deve funcionar mesmo que extract_audio/transcribe nunca rodem.
func Duration(ctx context.Context, videoPath string) (time.Duration, error) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		return 0, fmt.Errorf("media: ffprobe não encontrado no PATH: %w", err)
	}

	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "csv=p=0",
		videoPath,
	)
	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("media: ffprobe falhou: %w", err)
	}
	seconds, err := strconv.ParseFloat(strings.TrimSpace(string(output)), 64)
	if err != nil {
		return 0, fmt.Errorf("media: duração inválida na saída do ffprobe: %w", err)
	}
	return time.Duration(seconds * float64(time.Second)), nil
}
