package media

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ExtractAudio extracts the audio track from videoPath via ffmpeg, writing
// a mono 16kHz WAV to outputPath — a format universally accepted by the
// candidate STT APIs, avoiding codec ambiguity.
func ExtractAudio(ctx context.Context, videoPath, outputPath string) error {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return fmt.Errorf("media: ffmpeg not found in PATH: %w", err)
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
	hideWindow(cmd)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("media: ffmpeg failed: %w\n%s", err, output)
	}
	return nil
}

// Duration reads the video's duration via ffprobe (ffmpeg's companion, the same
// external dependency already assumed by ExtractAudio) — used to record
// lessons.duration_seconds when the import is confirmed (Story 5). It's
// intrinsic video metadata, not a product of the transcription pipeline:
// it must work even if extract_audio/transcribe never run.
func Duration(ctx context.Context, videoPath string) (time.Duration, error) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		return 0, fmt.Errorf("media: ffprobe not found in PATH: %w", err)
	}

	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "csv=p=0",
		videoPath,
	)
	hideWindow(cmd)
	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("media: ffprobe failed: %w", err)
	}
	seconds, err := strconv.ParseFloat(strings.TrimSpace(string(output)), 64)
	if err != nil {
		return 0, fmt.Errorf("media: invalid duration in ffprobe output: %w", err)
	}
	return time.Duration(seconds * float64(time.Second)), nil
}
