//go:build !linux && !windows

package services

import (
	"fmt"
	"runtime"
)

func moveFileNoReplace(_, _ string) error {
	return fmt.Errorf("move atômico sem substituição não é suportado em %s", runtime.GOOS)
}
