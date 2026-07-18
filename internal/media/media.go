package media

import (
	"context"
	"fmt"
	"os/exec"
)

// ExtractAudio extrai a trilha de áudio de videoPath via ffmpeg, gravando
// um WAV mono 16kHz em outputPath — formato universalmente aceito pelas
// APIs de STT candidatas, evitando ambiguidade de codec.
func ExtractAudio(ctx context.Context, videoPath, outputPath string) error {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return fmt.Errorf("media: ffmpeg não encontrado no PATH: %w", err)
	}

	cmd := exec.CommandContext(ctx, "ffmpeg",
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
