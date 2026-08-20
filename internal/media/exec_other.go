//go:build !windows

package media

import "os/exec"

// hideWindow não faz nada fora do Windows — não há janela de console a esconder.
func hideWindow(cmd *exec.Cmd) {}
