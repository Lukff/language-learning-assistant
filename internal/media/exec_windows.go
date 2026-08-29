//go:build windows

package media

import (
	"os/exec"
	"syscall"
)

// hideWindow prevents ffmpeg/ffprobe from opening a console window: since the
// Wails app runs without its own console, Windows opens a new window for
// every console-subsystem process launched, on every video import.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
