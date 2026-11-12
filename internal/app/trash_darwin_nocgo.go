//go:build darwin && !cgo

package app

import "os"

// Cross-compiled (CGO_ENABLED=0) darwin builds such as the headless
// mahpastesd cannot reach NSFileManager, so deletion stays permanent and the
// wizard says so.
func trashIsRecoverable() bool { return false }

// moveToTrash permanently removes the file. See trashIsRecoverable.
func moveToTrash(absPath string) error {
	return os.Remove(absPath)
}
