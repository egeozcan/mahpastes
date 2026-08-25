//go:build windows

package app

import "os"

// openImportFile opens a scanned file for reading.
//
// Windows has no O_NOFOLLOW. The exposure is much narrower here — creating a
// symlink requires elevation or Developer Mode — and the scan already skips
// reparse points, so a plain open is the practical equivalent. If this ever
// needs hardening, the primitive is CreateFileW with
// FILE_FLAG_OPEN_REPARSE_POINT and an fstat of the resulting handle.
func openImportFile(path string) (*os.File, error) {
	return os.Open(path)
}
