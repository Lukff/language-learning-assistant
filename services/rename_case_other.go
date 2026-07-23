//go:build !windows

package services

import "fmt"

func renameCaseOnlyNoReplace(_, _ string) error {
	return fmt.Errorf("rename case-only só é suportado no Windows")
}
