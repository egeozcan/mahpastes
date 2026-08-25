//go:build !windows

package app

import (
	"os"
	"syscall"
)

// openImportFile opens a scanned file for reading without ever following a
// symlink on the final path component.
//
// This is what actually closes the TOCTOU window that a stat-then-open
// sequence leaves open: resolveSessionPath can only validate a *name*, and
// between that check and the read another process can swap the file for a
// symlink pointing anywhere. O_NOFOLLOW makes the kernel refuse that open, so
// validation and use are no longer separable. Everything downstream reads
// through the returned handle — never by re-opening the path — so the bytes
// hashed, previewed, imported and trashed all belong to one inode.
func openImportFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
}
