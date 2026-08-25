//go:build windows

package app

import "os"

// trashIsRecoverable reports false so the wizard can warn that "delete" is
// permanent here. Wiring the real Recycle Bin means SHFileOperationW with
// FOF_ALLOWUNDO; until that exists, saying so plainly beats implying an undo
// that does not exist.
func trashIsRecoverable() bool { return false }

// moveToTrash permanently removes the file. See trashIsRecoverable.
func moveToTrash(absPath string) error {
	return os.Remove(absPath)
}
