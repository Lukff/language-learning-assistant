//go:build windows

package services

import (
	"fmt"
	"syscall"
)

// MoveFileW with no flags never replaces an existing destination and also covers
// case-only renames on case-insensitive filesystems.
func moveFileNoReplace(oldPath, newPath string) error {
	oldPathPtr, err := syscall.UTF16PtrFromString(oldPath)
	if err != nil {
		return fmt.Errorf("convert source path to UTF-16: %w", err)
	}
	newPathPtr, err := syscall.UTF16PtrFromString(newPath)
	if err != nil {
		return fmt.Errorf("convert destination path to UTF-16: %w", err)
	}
	if err := syscall.MoveFile(oldPathPtr, newPathPtr); err != nil {
		return fmt.Errorf("move file without replacing destination: %w", err)
	}
	return nil
}
