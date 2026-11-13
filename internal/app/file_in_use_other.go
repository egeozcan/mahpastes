//go:build !windows

package app

// isFileInUseError is always false here: an open file can be unlinked.
func isFileInUseError(error) bool { return false }
