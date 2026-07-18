package media

import (
	"context"
	"testing"
)

func TestExtractAudio_FfmpegNotInPath(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	err := ExtractAudio(context.Background(), "input.mp4", "output.wav")
	if err == nil {
		t.Fatal("esperava erro quando ffmpeg não está no PATH, obteve nil")
	}
}
