package importer

import "testing"

func TestStandardFilename(t *testing.T) {
	tests := []struct {
		name       string
		lessonDate string
		tutor      string
		ext        string
		want       string
	}{
		{
			name:       "data, hora e tutor simples",
			lessonDate: "2026-07-23T14:30",
			tutor:      "Maria José",
			ext:        ".mp4",
			want:       "2026-07-23_14H30_maria-jose.mp4",
		},
		{
			name:       "tutor com ponto e espaço",
			lessonDate: "2026-07-15T09:05",
			tutor:      "Sarah M.",
			ext:        ".mp4",
			want:       "2026-07-15_09H05_sarah-m.mp4",
		},
		{
			name:       "extensão em maiúsculas normalizada",
			lessonDate: "2026-07-15T09:05",
			tutor:      "Sarah",
			ext:        ".MP4",
			want:       "2026-07-15_09H05_sarah.mp4",
		},
		{
			name:       "tutor com múltiplos espaços e acentos variados",
			lessonDate: "2026-01-05T23:59",
			tutor:      "  João  Ñandú  ",
			ext:        ".mp4",
			want:       "2026-01-05_23H59_joao-nandu.mp4",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StandardFilename(tt.lessonDate, tt.tutor, tt.ext)
			if got != tt.want {
				t.Errorf("StandardFilename(%q, %q, %q) = %q, want %q", tt.lessonDate, tt.tutor, tt.ext, got, tt.want)
			}
		})
	}
}
