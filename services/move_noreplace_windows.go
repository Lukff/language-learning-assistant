//go:build windows

package services

import (
	"fmt"
	"syscall"
)

// MoveFileW sem flags nunca substitui um destino existente e também cobre
// a alteração apenas de casing em filesystems case-insensitive.
func moveFileNoReplace(oldPath, newPath string) error {
	oldPathPtr, err := syscall.UTF16PtrFromString(oldPath)
	if err != nil {
		return fmt.Errorf("converter path de origem para UTF-16: %w", err)
	}
	newPathPtr, err := syscall.UTF16PtrFromString(newPath)
	if err != nil {
		return fmt.Errorf("converter path de destino para UTF-16: %w", err)
	}
	if err := syscall.MoveFile(oldPathPtr, newPathPtr); err != nil {
		return fmt.Errorf("mover arquivo sem substituir destino: %w", err)
	}
	return nil
}
