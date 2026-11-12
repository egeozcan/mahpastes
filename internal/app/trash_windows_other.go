//go:build windows && !(amd64 || arm64)

package app

import "os"

// SHFILEOPSTRUCTW is packed on 32-bit Windows, which the Recycle Bin call in
// trash_windows.go does not model. These targets are not shipped, so they keep
// the permanent delete and the wizard says so.
func trashIsRecoverable() bool { return false }

// moveToTrash permanently removes the file. See trashIsRecoverable.
func moveToTrash(absPath string) error {
	return os.Remove(absPath)
}
