//go:build !windows

package media

import "os/exec"

// hideWindow does nothing outside Windows — there's no console window to hide.
func hideWindow(cmd *exec.Cmd) {}
