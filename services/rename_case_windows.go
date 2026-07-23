//go:build windows

package services

import "syscall"

// renameCaseOnlyNoReplace uses MoveFile without REPLACE_EXISTING. os.Rename
// uses MoveFileEx with replacement on Windows and is therefore unsuitable.
func renameCaseOnlyNoReplace(oldPath, newPath string) error {
	oldPathPtr, err := syscall.UTF16PtrFromString(oldPath)
	if err != nil {
		return err
	}
	newPathPtr, err := syscall.UTF16PtrFromString(newPath)
	if err != nil {
		return err
	}
	return syscall.MoveFile(oldPathPtr, newPathPtr)
}
