//go:build windows

package media

import (
	"os/exec"
	"syscall"
)

// hideWindow evita que ffmpeg/ffprobe abram uma janela de console: como o
// app Wails roda sem console próprio, o Windows abre uma nova janela para
// cada processo console-subsystem lançado, a cada importação de vídeo.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
