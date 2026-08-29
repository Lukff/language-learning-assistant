//go:build !linux && !windows

package services

import (
	"fmt"
	"runtime"
)

func moveFileNoReplace(_, _ string) error {
	return fmt.Errorf("atomic move without replacement is not supported on %s", runtime.GOOS)
}
