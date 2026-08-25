//go:build !darwin && !windows

package app

import "os"

// trashIsRecoverable reports false so the wizard can warn that "delete" is
// permanent here. A freedesktop.org trash implementation (moving into
// $XDG_DATA_HOME/Trash with a .trashinfo sidecar) would be the Linux answer;
// until that exists, saying so plainly beats implying an undo that does not
// exist.
func trashIsRecoverable() bool { return false }

// moveToTrash permanently removes the file. See trashIsRecoverable.
func moveToTrash(absPath string) error {
	return os.Remove(absPath)
}
