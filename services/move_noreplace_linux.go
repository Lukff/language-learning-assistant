//go:build linux

package services

import (
	"fmt"

	"golang.org/x/sys/unix"
)

func moveFileNoReplace(oldPath, newPath string) error {
	if err := unix.Renameat2(unix.AT_FDCWD, oldPath, unix.AT_FDCWD, newPath, unix.RENAME_NOREPLACE); err != nil {
		return fmt.Errorf("move file without replacing destination: %w", err)
	}
	return nil
}
